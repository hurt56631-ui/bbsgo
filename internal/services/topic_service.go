package services

import (
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/req"
	"bbs-go/internal/permissions"
	"bbs-go/internal/pkg/errs"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/pkg/locales"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/sqls"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bbs-go/internal/models"
	"bbs-go/internal/repositories"
)

var TopicService = newTopicService()

func newTopicService() *topicService {
	return &topicService{listCache: newTopicListMicroCache(), tagsCache: newEntityTagsCache()}
}

type topicService struct {
	listCache *topicListMicroCache
	tagsCache *entityTagsCache
}

const timelineCursorEpoch int64 = 1577836800 // 2020-01-01 UTC
const timelineCursorIdMask int64 = 1<<32 - 1

const topicListSelectColumns = `
	id, type, category_id, qa_status, accepted_comment_id, solved_at, bounty_score,
	user_id, title, summary, content_type, image_list, vote_id, recommend,
	recommend_time, sticky, sticky_time, view_count, comment_count, like_count,
	status, last_comment_time, ip_location, create_time
`

// encodeTimelineCursor 将秒级排序时间和主键打包为一个正 int64 游标。
// API 仍保持单个 cursor 参数，但同一秒内的多条记录不会再漏掉。
func encodeTimelineCursor(timestamp, id int64) int64 {
	delta := timestamp - timelineCursorEpoch + 1
	if delta <= 0 || delta >= 1<<31 || id <= 0 || id > timelineCursorIdMask {
		// 极旧数据、过远未来时间或超过 32 位的主键退回旧的时间游标语义。
		return timestamp
	}
	return (delta << 32) | id
}

func decodeTimelineCursor(cursor int64) (timestamp, id int64, ok bool) {
	if cursor <= timelineCursorIdMask {
		return 0, 0, false
	}
	delta := cursor >> 32
	id = cursor & timelineCursorIdMask
	if delta <= 0 || id <= 0 {
		return 0, 0, false
	}
	return timelineCursorEpoch + delta - 1, id, true
}

func (s *topicService) Get(id int64) *models.Topic {
	return repositories.TopicRepository.Get(sqls.DB(), id)
}

func (s *topicService) Take(where ...interface{}) *models.Topic {
	return repositories.TopicRepository.Take(sqls.DB(), where...)
}

func (s *topicService) Find(cnd *sqls.Cnd) []models.Topic {
	return repositories.TopicRepository.Find(sqls.DB(), cnd)
}

func (s *topicService) FindOne(cnd *sqls.Cnd) *models.Topic {
	return repositories.TopicRepository.FindOne(sqls.DB(), cnd)
}

func (s *topicService) FindPageByParams(params *params.QueryParams) (list []models.Topic, paging *sqls.Paging) {
	return repositories.TopicRepository.FindPageByParams(sqls.DB(), params)
}

func (s *topicService) FindPageByCnd(cnd *sqls.Cnd) (list []models.Topic, paging *sqls.Paging) {
	return repositories.TopicRepository.FindPageByCnd(sqls.DB(), cnd)
}

func (s *topicService) Count(cnd *sqls.Cnd) int64 {
	return repositories.TopicRepository.Count(sqls.DB(), cnd)
}

func (s *topicService) InvalidateListCaches() {
	if s.listCache != nil {
		s.listCache.invalidate()
	}
}

func (s *topicService) Updates(id int64, columns map[string]interface{}) error {
	if err := repositories.TopicRepository.Updates(sqls.DB(), id, columns); err != nil {
		return err
	}

	s.InvalidateListCaches()
	// 添加索引
	SearchDeleteService.RefreshTopicIndex(id, s.Get(id))

	return nil
}

func (s *topicService) UpdateColumn(id int64, name string, value interface{}) error {
	if err := repositories.TopicRepository.UpdateColumn(sqls.DB(), id, name, value); err != nil {
		return err
	}

	s.InvalidateListCaches()
	// 添加索引
	SearchDeleteService.RefreshTopicIndex(id, s.Get(id))

	return nil
}

// qaBountyLedger returns the number of escrow deductions, refunds and answer
// payouts recorded for one topic. Delete/restore operations use the ledger
// rather than accepted_comment_id alone because an accepted comment may later
// be removed while the payout remains irreversible.
func qaBountyLedger(tx *gorm.DB, topicId int64) (deductions, refunds, rewards int64, err error) {
	sourceId := strconv.FormatInt(topicId, 10)
	count := func(sourceType string, scoreType int, target *int64) error {
		return tx.Model(&models.UserScoreLog{}).
			Where("source_type = ? AND source_id = ? AND type = ?", sourceType, sourceId, scoreType).
			Count(target).Error
	}
	if err = count(constants.SourceTypeQaBounty, constants.ScoreTypeDecr, &deductions); err != nil {
		return
	}
	if err = count(constants.SourceTypeQaBountyRefund, constants.ScoreTypeIncr, &refunds); err != nil {
		return
	}
	err = count(constants.SourceTypeQaBounty, constants.ScoreTypeIncr, &rewards)
	return
}

func hasQaBountyReward(tx *gorm.DB, topicId int64) (bool, error) {
	_, _, rewards, err := qaBountyLedger(tx, topicId)
	return rewards > 0, err
}

// Delete permanently removes a topic and its owned database graph.
// A row lock serializes concurrent requests; after commit the topic row no longer exists.
func (s *topicService) Delete(topicId, deleteUserId int64, r *http.Request) error {
	if topicId <= 0 {
		return nil
	}

	var (
		deletedTopic    *models.Topic
		emitDeleteEvent bool
	)
	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		// Lock the row instead of relying on a permanent soft-delete state. This
		// serializes concurrent delete requests while also allowing old rows that
		// were soft-deleted by a previous release to be physically purged.
		var topic models.Topic
		query := ctx.Tx
		if ctx.Tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Where("id = ?", topicId).Take(&topic).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		wasVisible := topic.Status != constants.StatusDeleted
		deletedTopic = &topic
		emitDeleteEvent = wasVisible

		// Refund only when the bounty is still held in escrow. Ledger rows are
		// deliberately retained for accounting even though the topic is removed.
		if topic.Type == constants.TopicTypeQA && topic.BountyScore > 0 {
			deductions, refunds, rewards, err := qaBountyLedger(ctx.Tx, topic.Id)
			if err != nil {
				return err
			}
			if rewards == 0 && refunds < deductions {
				if err := UserService.AddScoreTx(ctx, topic.UserId, topic.BountyScore,
					constants.SourceTypeQaBountyRefund, strconv.FormatInt(topic.Id, 10), locales.Get("topic.bounty_refund")); err != nil {
					return err
				}
			}
		}

		// Previous soft-delete releases already decremented topic_count. Only a
		// row that was still visible can contribute another decrement here.
		if wasVisible {
			if err := UserService.DecrTopicCount(ctx, topic.UserId); err != nil {
				return err
			}
		}
		return hardDeleteTopicGraph(ctx, &topic)
	})
	if err != nil {
		return err
	}
	if deletedTopic == nil {
		return nil
	}

	s.InvalidateListCaches()
	if s.tagsCache != nil {
		s.tagsCache.invalidate(topicId)
	}
	if err := SearchDeleteService.ProcessEntity(constants.EntityTopic, topicId); err != nil {
		slog.Error("topic index delete failed; durable retry queued", slog.Int64("topicId", topicId), slog.Any("err", err))
	}
	ForumRealtimeService.ForgetTopic(topicId)
	// Media rows were queued in the same transaction as the physical delete.
	// Try them immediately; the scheduler will retry any transient failures.
	go func() {
		if err := StorageDeleteService.ProcessPending(100); err != nil {
			slog.Warn("immediate topic media cleanup incomplete", slog.Int64("topicId", topicId), slog.Any("err", err))
		}
	}()
	if emitDeleteEvent {
		event.Send(event.TopicDeleteEvent{
			UserId:       deletedTopic.UserId,
			TopicId:      deletedTopic.Id,
			TopicTitle:   deletedTopic.GetTitle(),
			DeleteUserId: deleteUserId,
		})
	}
	return nil
}

// Undelete 取消删除
func (s *topicService) Undelete(id int64) error {
	topic := s.Get(id)
	if topic == nil {
		return errors.New(locales.Get("common.not_found"))
	}
	restored := false
	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		result := ctx.Tx.Model(&models.Topic{}).
			Where("id = ? AND status = ?", id, constants.StatusDeleted).
			UpdateColumn("status", constants.StatusOk)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		restored = true

		// Recreate escrow only when a delete actually left a refund outstanding.
		// A previously paid answer must not charge the topic owner again.
		if topic.Type == constants.TopicTypeQA && topic.BountyScore > 0 {
			deductions, refunds, rewards, err := qaBountyLedger(ctx.Tx, topic.Id)
			if err != nil {
				return err
			}
			if rewards == 0 && refunds >= deductions {
				if err := UserService.DecrScoreTx(ctx, topic.UserId, topic.BountyScore,
					constants.SourceTypeQaBounty, strconv.FormatInt(topic.Id, 10), locales.Get("topic.bounty_deduct")); err != nil {
					return err
				}
			}
		}
		if err := UserService.IncrTopicCount(ctx, topic.UserId); err != nil {
			return err
		}
		if err := TopicTagService.UndeleteByTopicId(ctx, id); err != nil {
			return err
		}
		if err := AttachmentService.RestoreByTopicId(ctx, id); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !restored {
		return nil
	}
	s.InvalidateListCaches()
	if s.tagsCache != nil {
		s.tagsCache.invalidate(id)
	}
	SearchDeleteService.RefreshTopicIndex(id, s.Get(id))
	return nil
}

// 更新
func (s *topicService) Edit(userId, topicId int64, form req.EditTopicReq) error {
	if len(form.Title) == 0 {
		return errors.New(locales.Get("topic.title_required"))
	}

	if strs.RuneLen(form.Title) > 128 {
		return errors.New(locales.Get("topic.title_too_long"))
	}

	category := repositories.CategoryRepository.Get(sqls.DB(), form.CategoryId)
	if category == nil || category.Status != constants.StatusOk {
		return errors.New(locales.Get("topic.category_not_found"))
	}
	topic := repositories.TopicRepository.Get(sqls.DB(), topicId)
	if topic == nil {
		return errors.New(locales.Get("common.not_found"))
	}
	// 编辑时附件数量校验（仅帖子类型）
	if topic.Type == constants.TopicTypeTopic && form.AttachmentIds != nil {
		attCfg := SysConfigService.GetAttachmentConfig()
		if !attCfg.Enabled {
			return errors.New(locales.Get("attachment.disabled"))
		}
		if len(form.AttachmentIds) > attCfg.MaxCount {
			return errors.New(locales.Getf("attachment.too_many", attCfg.MaxCount))
		}
	}
	if !category.Type.Supports(topic.Type) {
		return errors.New(locales.Get("topic.category_type_mismatch"))
	}
	if CategoryService.IsAdminOnlyPost(category) {
		user := UserService.Get(userId)
		if !PermissionService.HasPermission(user, permissions.PermissionTopicAnnouncementCreate.Code) {
			return errs.NoPermission()
		}
	}

	hideContent := form.HideContent

	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		var (
			tagIds []int64
			err    error
		)

		// Serialize edit with permanent delete. Reading the topic before opening
		// this transaction is only a preflight check; without this root row lock a
		// concurrent delete could commit first and this edit could recreate tags or
		// attachments for a topic row that no longer exists.
		var lockedTopic models.Topic
		query := ctx.Tx.Where("id = ?", topicId)
		if ctx.Tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err = query.Take(&lockedTopic).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("common.not_found"))
			}
			return err
		}
		if !category.Type.Supports(lockedTopic.Type) {
			return errors.New(locales.Get("topic.category_type_mismatch"))
		}
		if lockedTopic.Type == constants.TopicTypeQA {
			// QA topics ignore hide-content changes.
			hideContent = lockedTopic.HideContent
		}

		// Queue the topic's pre-edit media in the same transaction. After commit
		// the delete worker re-checks live references, so media still present in
		// the edited topic/attachments is kept while removed inline images and
		// detached attachments are physically reclaimed.
		oldMediaTargets, mediaErr := collectTopicStorageDeleteTargets(ctx.Tx, &lockedTopic, nil)
		if mediaErr != nil {
			return mediaErr
		}
		if err = StorageDeleteService.EnqueueTargets(ctx.Tx, oldMediaTargets); err != nil {
			return err
		}

		if err = repositories.TopicRepository.Updates(ctx.Tx, topicId, map[string]interface{}{
			"category_id":  form.CategoryId,
			"title":        form.Title,
			"summary":      BuildTopicSummary(lockedTopic.Type, lockedTopic.ContentType, form.Content),
			"content":      form.Content,
			"hide_content": hideContent,
		}); err != nil {
			return err
		}

		// 创建帖子对应标签
		if tagIds, err = repositories.TagRepository.GetOrCreates(ctx.Tx, form.Tags); err != nil {
			return err
		}

		// 先删掉所有的标签
		if err := TopicTagService.HardDeleteTopicTags(ctx, topicId); err != nil {
			return err
		}
		// 然后重新添加标签
		if err := repositories.TopicTagRepository.AddTopicTags(ctx.Tx, topicId, tagIds); err != nil {
			return err
		}

		// 附件全量替换（仅当请求中带 attachmentIds 时，同一事务内执行避免 SQLite 卡住）
		if form.AttachmentIds != nil {
			if err := AttachmentService.ReplaceTopicAttachments(ctx, topicId, userId, form.AttachmentIds); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.InvalidateListCaches()
	if s.tagsCache != nil {
		s.tagsCache.invalidate(topicId)
	}
	// 添加索引
	SearchDeleteService.RefreshTopicIndex(topicId, s.Get(topicId))

	event.Send(event.TopicUpdateEvent{
		UserId:  userId,
		TopicId: topicId,
	})

	// Most edit-time media deletes complete immediately. Transient object
	// storage failures remain in the durable outbox for the scheduler.
	go func() {
		if err := StorageDeleteService.ProcessPending(100); err != nil {
			slog.Warn("immediate edited-topic media cleanup incomplete", slog.Int64("topicId", topicId), slog.Any("err", err))
		}
	}()

	return nil
}

// 推荐
func (s *topicService) SetRecommend(topicId int64, recommend bool) error {
	topic := s.Get(topicId)
	if topic == nil || topic.Status != constants.StatusOk {
		return errors.New(locales.Get("topic.topic_not_found"))
	}
	if topic.Recommend == recommend { // 推荐状态没变更
		return nil
	}
	if recommend {
		if err := s.Updates(topicId, map[string]interface{}{
			"recommend":      recommend,
			"recommend_time": dates.NowTimestamp(),
		}); err != nil {
			return err
		}
	} else {
		if err := s.UpdateColumn(topicId, "recommend", recommend); err != nil {
			return err
		}
	}

	// 发送事件
	event.Send(event.TopicRecommendEvent{
		TopicId:   topicId,
		Recommend: recommend,
	})

	// 添加索引
	SearchDeleteService.RefreshTopicIndex(topicId, s.Get(topicId))

	return nil
}

// GetTopicTags 话题的标签。
func (s *topicService) GetTopicTags(topicId int64) []models.Tag {
	return s.GetTopicTagsMap([]int64{topicId})[topicId]
}

// GetTopicTagsMap 批量获取话题标签，避免列表渲染时逐条查询数据库。
func (s *topicService) GetTopicTagsMap(topicIds []int64) map[int64][]models.Tag {
	result := make(map[int64][]models.Tag, len(topicIds))
	if len(topicIds) == 0 {
		return result
	}

	uniqueTopicIds := make([]int64, 0, len(topicIds))
	seenTopicIds := make(map[int64]struct{}, len(topicIds))
	for _, topicId := range topicIds {
		if topicId <= 0 {
			continue
		}
		if _, exists := seenTopicIds[topicId]; exists {
			continue
		}
		seenTopicIds[topicId] = struct{}{}
		uniqueTopicIds = append(uniqueTopicIds, topicId)
	}
	if len(uniqueTopicIds) == 0 {
		return result
	}

	missing := uniqueTopicIds
	if s.tagsCache != nil {
		var cached map[int64][]models.Tag
		cached, missing = s.tagsCache.getMany(uniqueTopicIds)
		for id, tags := range cached {
			result[id] = tags
		}
	}
	if len(missing) == 0 {
		return result
	}

	loaded := make(map[int64][]models.Tag, len(missing))
	for _, id := range missing {
		loaded[id] = nil
	}
	topicTags := repositories.TopicTagRepository.FindByTopicIds(sqls.DB(), missing, constants.StatusOk)
	if len(topicTags) > 0 {
		tagIds := make([]int64, 0, len(topicTags))
		seenTagIds := make(map[int64]struct{}, len(topicTags))
		for _, topicTag := range topicTags {
			if _, exists := seenTagIds[topicTag.TagId]; exists {
				continue
			}
			seenTagIds[topicTag.TagId] = struct{}{}
			tagIds = append(tagIds, topicTag.TagId)
		}

		tags := repositories.TagRepository.GetTagInIds(sqls.DB(), tagIds)
		tagsById := make(map[int64]models.Tag, len(tags))
		for _, tag := range tags {
			tagsById[tag.Id] = tag
		}
		for _, topicTag := range topicTags {
			if tag, exists := tagsById[topicTag.TagId]; exists {
				loaded[topicTag.TopicId] = append(loaded[topicTag.TopicId], tag)
			}
		}
	}
	if s.tagsCache != nil {
		s.tagsCache.putMany(missing, loaded)
	}
	for id, tags := range loaded {
		result[id] = cloneTags(tags)
	}
	return result
}

// GetTopics 帖子列表（最新、推荐、关注、节点）
func (s *topicService) GetTopics(user *models.User, categoryId, cursor int64, qaStatus, sort string) (topics []models.Topic, nextCursor int64, hasMore bool) {
	limit := constants.TopicListPageSize
	if categoryId == constants.CategoryIdFollow {
		if user != nil {
			return s._GetFollowTopics(user.Id, cursor)
		}
		return
	} else {
		return s._GetCategoryTopics(categoryId, cursor, limit, qaStatus, sort)
	}
}

// _GetCategoryTopics 帖子列表（最新、推荐、节点）
func (s *topicService) _GetCategoryTopics(categoryId, cursor int64, limit int, qaStatus, sort string) (topics []models.Topic, nextCursor int64, hasMore bool) {
	cacheKey := ""
	if cursor == 0 && s.listCache != nil {
		cacheKey = fmt.Sprintf("topics:%d:%d:%s:%s", categoryId, limit, qaStatus, sort)
		if cachedTopics, cachedCursor, cachedHasMore, ok := s.listCache.get(cacheKey); ok {
			return cachedTopics, cachedCursor, cachedHasMore
		}
		// Collapse a burst at cache expiry into one database query. The second
		// lookup handles a response populated while this goroutine was waiting.
		loadLock := s.listCache.loadLock(cacheKey)
		loadLock.Lock()
		defer loadLock.Unlock()
		if cachedTopics, cachedCursor, cachedHasMore, ok := s.listCache.get(cacheKey); ok {
			return cachedTopics, cachedCursor, cachedHasMore
		}
	}

	query := sqls.DB().Model(&models.Topic{}).Select(topicListSelectColumns)
	if categoryId > 0 {
		categoryIds := CategoryService.GetCategoryIdsForList(categoryId)
		if len(categoryIds) > 0 {
			query = query.Where("category_id IN ?", categoryIds)
		} else {
			query = query.Where("category_id = ?", categoryId)
		}
	}
	if categoryId == constants.CategoryIdRecommend {
		query = query.Where("recommend = ?", true)
	}
	if qaStatus != "" {
		query = query.Where("type = ? AND qa_status = ?", constants.TopicTypeQA, qaStatus)
	}

	if sort == "latestPublish" {
		if cursor > 0 {
			query = query.Where("id < ?", cursor)
		}
		query = query.Where("status = ?", constants.StatusOk).Order("id DESC").Limit(limit + 1)
	} else {
		if cursor > 0 {
			if cursorTime, cursorId, ok := decodeTimelineCursor(cursor); ok {
				query = query.Where("(last_comment_time < ? OR (last_comment_time = ? AND id < ?))", cursorTime, cursorTime, cursorId)
			} else {
				query = query.Where("last_comment_time < ?", cursor)
			}
		}
		query = query.Where("status = ?", constants.StatusOk).
			Order("last_comment_time DESC").Order("id DESC").Limit(limit + 1)
	}
	_ = query.Find(&topics).Error
	hasMore = len(topics) > limit
	if hasMore {
		topics = topics[:limit]
	}
	s.hydrateTweetContents(topics)

	if len(topics) > 0 {
		last := topics[len(topics)-1]
		if sort == "latestPublish" {
			nextCursor = last.Id
		} else {
			nextCursor = encodeTimelineCursor(last.LastCommentTime, last.Id)
		}
	} else {
		nextCursor = cursor
	}
	if cacheKey != "" {
		s.listCache.put(cacheKey, topics, nextCursor, hasMore)
	}
	return
}

// _GetFollowTopics 关注帖子列表
func (s *topicService) _GetFollowTopics(userId int64, cursor int64) (topics []models.Topic, nextCursor int64, hasMore bool) {
	limit := constants.TopicListPageSize
	query := sqls.DB().Model(&models.UserFeed{}).
		Where("user_id = ? AND data_type = ?", userId, constants.EntityTopic)
	if cursor > 0 {
		if cursorTime, cursorId, ok := decodeTimelineCursor(cursor); ok {
			query = query.Where("(create_time < ? OR (create_time = ? AND id < ?))", cursorTime, cursorTime, cursorId)
		} else {
			// 兼容升级前客户端保存的秒级时间游标。
			query = query.Where("create_time < ?", cursor)
		}
	}
	query = query.Order("create_time DESC").Order("id DESC").Limit(limit + 1)
	var userFeeds []models.UserFeed
	_ = query.Find(&userFeeds).Error
	hasMore = len(userFeeds) > limit
	if hasMore {
		userFeeds = userFeeds[:limit]
	}
	if len(userFeeds) > 0 {
		last := userFeeds[len(userFeeds)-1]
		nextCursor = encodeTimelineCursor(last.CreateTime, last.Id)
	} else {
		nextCursor = cursor
	}

	var topicIds []int64
	for _, item := range userFeeds {
		topicIds = append(topicIds, item.DataId)
	}
	topics = TopicService.GetTopicListByIds(topicIds)

	return
}

// 指定标签下话题列表
func (s *topicService) GetTagTopics(tagId, cursor int64) (topics []models.Topic, nextCursor int64, hasMore bool) {
	limit := constants.TopicListPageSize
	query := sqls.DB().Model(&models.TopicTag{}).
		Where("tag_id = ? AND status = ?", tagId, constants.StatusOk)
	if cursor > 0 {
		if cursorTime, cursorId, ok := decodeTimelineCursor(cursor); ok {
			query = query.Where("(last_comment_time < ? OR (last_comment_time = ? AND id < ?))", cursorTime, cursorTime, cursorId)
		} else {
			// 兼容升级前客户端保存的秒级时间游标。
			query = query.Where("last_comment_time < ?", cursor)
		}
	}
	query = query.Order("last_comment_time DESC").Order("id DESC").Limit(limit + 1)
	var topicTags []models.TopicTag
	_ = query.Find(&topicTags).Error
	hasMore = len(topicTags) > limit
	if hasMore {
		topicTags = topicTags[:limit]
	}
	if len(topicTags) > 0 {
		last := topicTags[len(topicTags)-1]
		nextCursor = encodeTimelineCursor(last.LastCommentTime, last.Id)

		var topicIds []int64
		for _, topicTag := range topicTags {
			topicIds = append(topicIds, topicTag.TopicId)
		}

		topicsMap := s.GetTopicListInIds(topicIds)
		if topicsMap != nil {
			for _, topicTag := range topicTags {
				if topic, found := topicsMap[topicTag.TopicId]; found {
					topics = append(topics, topic)
				}
			}
		}
	} else {
		nextCursor = cursor
	}
	return
}

// hydrateTweetContents loads full short-update text only for tweet rows. Regular
// topic list queries never touch the longtext content column, while the existing
// tweet API continues to return the complete post instead of a truncated summary.
func (s *topicService) hydrateTweetContents(topics []models.Topic) {
	if len(topics) == 0 {
		return
	}
	tweetIds := make([]int64, 0, len(topics))
	for _, topic := range topics {
		if constants.IsTweetTopicType(topic.Type) {
			tweetIds = append(tweetIds, topic.Id)
		}
	}
	if len(tweetIds) == 0 {
		return
	}

	type tweetContentRow struct {
		Id      int64
		Content string
	}
	var rows []tweetContentRow
	if err := sqls.DB().Model(&models.Topic{}).Select("id, content").
		Where("id IN ? AND type = ?", tweetIds, constants.TopicTypeTweet).
		Find(&rows).Error; err != nil {
		return
	}
	contentById := make(map[int64]string, len(rows))
	for _, row := range rows {
		contentById[row.Id] = row.Content
	}
	for i := range topics {
		if content, ok := contentById[topics[i].Id]; ok {
			topics[i].Content = content
		}
	}
}

func (s *topicService) GetTopicListByIds(topicIds []int64) (topics []models.Topic) {
	topicsMap := s.GetTopicListInIds(topicIds)
	for _, topicId := range topicIds {
		if topic, found := topicsMap[topicId]; found {
			topics = append(topics, topic)
		}
	}
	return
}

// GetTopicListInIds returns only list fields, preserving the full-content
// methods for detail/edit/search paths.
func (s *topicService) GetTopicListInIds(topicIds []int64) map[int64]models.Topic {
	if len(topicIds) == 0 {
		return nil
	}
	var topics []models.Topic
	_ = sqls.DB().Model(&models.Topic{}).Select(topicListSelectColumns).
		Where("id IN ? AND status = ?", topicIds, constants.StatusOk).Find(&topics).Error
	s.hydrateTweetContents(topics)
	result := make(map[int64]models.Topic, len(topics))
	for _, topic := range topics {
		result[topic.Id] = topic
	}
	return result
}

func (s *topicService) GetTopicByIds(topicIds []int64) (topics []models.Topic) {
	topicsMap := s.GetTopicInIds(topicIds)
	for _, topicId := range topicIds {
		topic, found := topicsMap[topicId]
		if found {
			topics = append(topics, topic)
		}
	}
	return
}

// GetTopicInIds 根据编号批量获取主题
func (s *topicService) GetTopicInIds(topicIds []int64) map[int64]models.Topic {
	if len(topicIds) == 0 {
		return nil
	}
	var topics []models.Topic
	sqls.DB().Where("id in (?)", topicIds).Find(&topics)

	topicsMap := make(map[int64]models.Topic, len(topics))
	for _, topic := range topics {
		topicsMap[topic.Id] = topic
	}
	return topicsMap
}

// 浏览数+1
func (s *topicService) IncrViewCount(topicId int64) {
	ViewCountService.IncrTopic(topicId)
}

// 当帖子被评论的时候，更新最后回复时间、回复数量+1
func (s *topicService) onComment(tx *gorm.DB, topicId int64, comment *models.Comment) error {
	result := tx.Model(&models.Topic{}).Where("id = ? AND status = ?", topicId, constants.StatusOk).Updates(map[string]interface{}{
		"last_comment_time":    comment.CreateTime,
		"last_comment_user_id": comment.UserId,
		"comment_count":        gorm.Expr("comment_count + 1"),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New(locales.Get("common.not_found"))
	}
	if err := tx.Exec("update t_topic_tag set last_comment_time = ?, last_comment_user_id = ? where topic_id = ?",
		comment.CreateTime, comment.UserId, topicId).Error; err != nil {
		return err
	}
	return nil
}

func (s *topicService) ScanByUser(userId int64, callback func(topics []models.Topic)) {
	var cursor int64 = 0
	for {
		list := repositories.TopicRepository.Find(sqls.DB(), sqls.NewCnd().
			Eq("user_id", userId).Gt("id", cursor).Asc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

func (s *topicService) Scan(callback func(topics []models.Topic)) {
	var cursor int64 = 0
	for {
		list := repositories.TopicRepository.Find(sqls.DB(), sqls.NewCnd().
			Gt("id", cursor).Asc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

// 倒序扫描
func (s *topicService) ScanDesc(callback func(topics []models.Topic)) {
	var cursor int64 = math.MaxInt64
	for {
		list := repositories.TopicRepository.Find(sqls.DB(), sqls.NewCnd().
			Lt("id", cursor).Desc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

// 倒序扫描
func (s *topicService) ScanDescWithDate(dateFrom, dateTo int64, callback func(topics []models.Topic)) {
	var cursor int64 = math.MaxInt64
	for {
		list := repositories.TopicRepository.Find(sqls.DB(), sqls.NewCnd().
			Cols("id", "status", "create_time", "update_time").
			Lt("id", cursor).Gte("create_time", dateFrom).Lt("create_time", dateTo).Desc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

func (s *topicService) GetUserTopics(userId, cursor int64) (topics []models.Topic, nextCursor int64, hasMore bool) {
	limit := constants.TopicListPageSize
	query := sqls.DB().Model(&models.Topic{}).Select(topicListSelectColumns).
		Where("status = ?", constants.StatusOk)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	_ = query.Order("id DESC").Limit(limit + 1).Find(&topics).Error
	hasMore = len(topics) > limit
	if hasMore {
		topics = topics[:limit]
	}
	s.hydrateTweetContents(topics)
	if len(topics) > 0 {
		nextCursor = topics[len(topics)-1].Id
	} else {
		nextCursor = cursor
	}
	return
}

func (s *topicService) GetRecentTopics(limit int) []models.Topic {
	if limit <= 0 {
		return nil
	}
	var topics []models.Topic
	_ = sqls.DB().Model(&models.Topic{}).Select(topicListSelectColumns).
		Where("status = ?", constants.StatusOk).Order("id DESC").Limit(limit).Find(&topics).Error
	s.hydrateTweetContents(topics)
	return topics
}

func (s *topicService) GetRecentTopicsByUser(userId int64, limit int) []models.Topic {
	if userId <= 0 || limit <= 0 {
		return nil
	}
	var topics []models.Topic
	_ = sqls.DB().Model(&models.Topic{}).Select(topicListSelectColumns).
		Where("user_id = ? AND status = ?", userId, constants.StatusOk).
		Order("id DESC").Limit(limit).Find(&topics).Error
	return topics
}

func (s *topicService) GetStickyTopics(categoryId int64, limit int, qaStatus string) []models.Topic {
	cacheKey := fmt.Sprintf("sticky:%d:%d:%s", categoryId, limit, qaStatus)
	if s.listCache != nil {
		if topics, _, _, ok := s.listCache.get(cacheKey); ok {
			return topics
		}
		loadLock := s.listCache.loadLock(cacheKey)
		loadLock.Lock()
		defer loadLock.Unlock()
		if topics, _, _, ok := s.listCache.get(cacheKey); ok {
			return topics
		}
	}

	query := sqls.DB().Model(&models.Topic{}).Select(topicListSelectColumns).
		Where("sticky = ? AND status = ?", true, constants.StatusOk)
	if categoryId > 0 {
		categoryIds := CategoryService.GetCategoryIdsForList(categoryId)
		if len(categoryIds) > 0 {
			query = query.Where("category_id IN ?", categoryIds)
		} else {
			query = query.Where("category_id = ?", categoryId)
		}
	}
	if qaStatus != "" {
		query = query.Where("type = ? AND qa_status = ?", constants.TopicTypeQA, qaStatus)
	}
	var topics []models.Topic
	_ = query.Order("sticky_time DESC").Limit(limit).Find(&topics).Error
	s.hydrateTweetContents(topics)
	if s.listCache != nil {
		s.listCache.put(cacheKey, topics, 0, false)
	}
	return topics
}

func (s *topicService) SetSticky(topicId int64, sticky bool) error {
	topic := s.Get(topicId)
	if topic == nil || topic.Status != constants.StatusOk {
		return errors.New(locales.Get("topic.topic_not_found"))
	}
	if topic.Sticky == sticky {
		return nil
	}
	if sticky {
		return s.Updates(topicId, map[string]interface{}{
			"sticky":      true,
			"sticky_time": dates.NowTimestamp(),
		})
	} else {
		return s.Updates(topicId, map[string]interface{}{
			"sticky": false,
		})
	}
}

func (s *topicService) AcceptAnswer(topicId, commentId, userId int64, isAdmin bool) error {
	if topicId <= 0 || commentId <= 0 {
		return errors.New(locales.Get("common.not_found"))
	}

	now := dates.NowTimestamp()
	awardedBounty := 0
	didAccept := false
	var acceptedComment models.Comment
	var lockedTopic models.Topic
	if err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		// Use the same root -> comment lock order as comment deletion. The previous
		// comment -> topic order could deadlock when accepting an answer raced with
		// deleting that answer.
		rootQuery := ctx.Tx.Where("id = ? AND status = ?", topicId, constants.StatusOk)
		if ctx.Tx.Dialector.Name() != "sqlite" {
			rootQuery = rootQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := rootQuery.Take(&lockedTopic).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("common.not_found"))
			}
			return err
		}
		if lockedTopic.Type != constants.TopicTypeQA {
			return errors.New(locales.Get("topic.type_not_supported"))
		}
		if lockedTopic.UserId != userId && !isAdmin {
			return errors.New(locales.Get("topic.no_permission"))
		}
		if lockedTopic.AcceptedCommentId == commentId && lockedTopic.QaStatus == constants.QaStatusSolved {
			// Idempotent retry: do not emit a second event or pay the bounty again.
			return nil
		}
		if lockedTopic.AcceptedCommentId > 0 || lockedTopic.QaStatus == constants.QaStatusSolved {
			return errors.New(locales.Get("topic.answer_already_accepted"))
		}

		answerQuery := ctx.Tx.Where("id = ? AND status = ? AND entity_type = ? AND entity_id = ?",
			commentId, constants.StatusOk, constants.EntityTopic, lockedTopic.Id)
		if ctx.Tx.Dialector.Name() != "sqlite" {
			answerQuery = answerQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := answerQuery.Take(&acceptedComment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("common.not_found"))
			}
			return err
		}

		result := ctx.Tx.Model(&models.Topic{}).
			Where("id = ? AND status = ? AND accepted_comment_id = 0 AND qa_status = ?",
				lockedTopic.Id, constants.StatusOk, constants.QaStatusUnsolved).
			Updates(map[string]interface{}{
				"accepted_comment_id": acceptedComment.Id,
				"qa_status":           constants.QaStatusSolved,
				"solved_at":           now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New(locales.Get("topic.answer_already_accepted"))
		}

		if lockedTopic.BountyScore > 0 && acceptedComment.UserId != lockedTopic.UserId {
			// A bounty belongs to the topic, not to an acceptance attempt. If an
			// administrator reopens a solved topic, never pay the same escrow twice.
			var rewardCount int64
			if err := ctx.Tx.Model(&models.UserScoreLog{}).
				Where("source_type = ? AND source_id = ? AND type = ?",
					constants.SourceTypeQaBounty, strconv.FormatInt(lockedTopic.Id, 10), constants.ScoreTypeIncr).
				Count(&rewardCount).Error; err != nil {
				return err
			}
			if rewardCount == 0 {
				if err := UserService.AddScoreTx(ctx, acceptedComment.UserId, lockedTopic.BountyScore,
					constants.SourceTypeQaBounty, strconv.FormatInt(lockedTopic.Id, 10), locales.Get("topic.bounty_reward")); err != nil {
					return err
				}
				awardedBounty = lockedTopic.BountyScore
			}
		}
		didAccept = true
		return nil
	}); err != nil {
		return err
	}
	if !didAccept {
		return nil
	}

	s.InvalidateListCaches()
	SearchDeleteService.RefreshTopicIndex(lockedTopic.Id, s.Get(lockedTopic.Id))

	event.Send(event.QaAnswerAcceptedEvent{
		UserId:      acceptedComment.UserId,
		TopicId:     lockedTopic.Id,
		CommentId:   acceptedComment.Id,
		BountyScore: awardedBounty,
		CreateTime:  now,
	})
	return nil
}

func (s *topicService) UnacceptAnswer(topicId, userId int64, isAdmin bool) error {
	topic := s.Get(topicId)
	if topic == nil || topic.Status != constants.StatusOk {
		return errors.New(locales.Get("common.not_found"))
	}
	if topic.Type != constants.TopicTypeQA {
		return errors.New(locales.Get("topic.type_not_supported"))
	}
	if topic.UserId != userId && !isAdmin {
		return errors.New(locales.Get("topic.no_permission"))
	}
	if topic.AcceptedCommentId == 0 && topic.QaStatus == constants.QaStatusUnsolved {
		return nil
	}
	// A paid bounty is an irreversible transfer. A self-answer or another
	// acceptance that did not pay anyone may still be undone safely.
	if topic.BountyScore > 0 {
		paid, err := hasQaBountyReward(sqls.DB(), topic.Id)
		if err != nil {
			return err
		}
		if paid {
			return errors.New(locales.Get("topic.bounty_unaccept_forbidden"))
		}
	}

	return s.Updates(topic.Id, map[string]interface{}{
		"accepted_comment_id": 0,
		"qa_status":           constants.QaStatusUnsolved,
		"solved_at":           0,
	})
}

func (s *topicService) ForceSetQaStatus(topicId int64, qaStatus constants.QaStatus) error {
	topic := s.Get(topicId)
	if topic == nil || topic.Status != constants.StatusOk {
		return errors.New(locales.Get("common.not_found"))
	}
	if topic.Type != constants.TopicTypeQA {
		return errors.New(locales.Get("topic.type_not_supported"))
	}

	columns := map[string]interface{}{
		"qa_status": qaStatus,
	}
	if qaStatus == constants.QaStatusSolved {
		columns["solved_at"] = dates.NowTimestamp()
	} else {
		columns["solved_at"] = 0
		columns["accepted_comment_id"] = 0
	}
	return s.Updates(topic.Id, columns)
}
