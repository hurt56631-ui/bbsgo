package services

import (
	"bbs-go/internal/cache"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/req"
	"bbs-go/internal/permissions"
	"bbs-go/internal/pkg/errs"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/pkg/iplocator"
	"bbs-go/internal/pkg/locales"
	"errors"
	"log/slog"
	"strings"

	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/common/jsons"
	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/sqls"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bbs-go/internal/models"
	"bbs-go/internal/repositories"
)

var CommentService = newCommentService()

func newCommentService() *commentService {
	return &commentService{}
}

type commentService struct {
}

const hotCommentCursorIDMask int64 = 1<<32 - 1

// encodeHotCommentCursor stores the ranking value used for the last row together
// with its id. Looking the score up again by id is not stable because likes and
// replies can change between page requests.
func encodeHotCommentCursor(hotScore, id int64) int64 {
	if hotScore < 0 || hotScore >= 1<<31-1 || id <= 0 || id > hotCommentCursorIDMask {
		return id
	}
	return ((hotScore + 1) << 32) | id
}

func decodeHotCommentCursor(cursor int64) (hotScore, id int64, ok bool) {
	if cursor <= hotCommentCursorIDMask {
		return 0, 0, false
	}
	hotScore = (cursor >> 32) - 1
	id = cursor & hotCommentCursorIDMask
	if hotScore < 0 || id <= 0 {
		return 0, 0, false
	}
	return hotScore, id, true
}

func (s *commentService) Get(id int64) *models.Comment {
	return repositories.CommentRepository.Get(sqls.DB(), id)
}

func (s *commentService) Take(where ...interface{}) *models.Comment {
	return repositories.CommentRepository.Take(sqls.DB(), where...)
}

func (s *commentService) Find(cnd *sqls.Cnd) []models.Comment {
	return repositories.CommentRepository.Find(sqls.DB(), cnd)
}

func (s *commentService) FindOne(cnd *sqls.Cnd) *models.Comment {
	return repositories.CommentRepository.FindOne(sqls.DB(), cnd)
}

func (s *commentService) FindPageByParams(params *params.QueryParams) (list []models.Comment, paging *sqls.Paging) {
	return repositories.CommentRepository.FindPageByParams(sqls.DB(), params)
}

func (s *commentService) FindPageByCnd(cnd *sqls.Cnd) (list []models.Comment, paging *sqls.Paging) {
	return repositories.CommentRepository.FindPageByCnd(sqls.DB(), cnd)
}

func (s *commentService) Count(cnd *sqls.Cnd) int64 {
	return repositories.CommentRepository.Count(sqls.DB(), cnd)
}

func (s *commentService) Create(t *models.Comment) error {
	return repositories.CommentRepository.Create(sqls.DB(), t)
}

func (s *commentService) Update(t *models.Comment) error {
	return repositories.CommentRepository.Update(sqls.DB(), t)
}

func (s *commentService) Updates(id int64, columns map[string]interface{}) error {
	return repositories.CommentRepository.Updates(sqls.DB(), id, columns)
}

func (s *commentService) UpdateColumn(id int64, name string, value interface{}) error {
	return repositories.CommentRepository.UpdateColumn(sqls.DB(), id, name, value)
}

func (s *commentService) Delete(id int64) error {
	var rootType string
	userDecrements := make(map[int64]int64)
	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		tx := ctx.Tx
		comment, err := s.lockCommentForDelete(tx, id)
		if err != nil {
			return err
		}
		if comment == nil || comment.Status == constants.StatusDeleted {
			return nil
		}

		mediaTargets, mediaErr := collectCommentStorageDeleteTargets(tx, comment,
			comment.EntityType == constants.EntityTopic || comment.EntityType == constants.EntityArticle)
		if mediaErr != nil {
			return mediaErr
		}
		if err := StorageDeleteService.EnqueueTargets(tx, mediaTargets); err != nil {
			return err
		}

		result := tx.Model(&models.Comment{}).
			Where("id = ? AND status <> ?", id, constants.StatusDeleted).
			UpdateColumn("status", constants.StatusDeleted)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		// Only published comments contributed to counters.
		if comment.Status != constants.StatusOk {
			return nil
		}
		if comment.UserId > 0 {
			userDecrements[comment.UserId]++
		}

		// A deleted top-level comment hides its reply thread. Soft-delete the
		// direct replies in the same transaction so user comment counters and the
		// visible comment tree do not drift apart. Nested quote replies are stored
		// with the same parent EntityId, so one query covers the whole thread.
		if comment.EntityType == constants.EntityTopic || comment.EntityType == constants.EntityArticle {
			if err := s.deleteVisibleChildReplies(tx, comment.Id, userDecrements); err != nil {
				return err
			}
		}

		switch comment.EntityType {
		case constants.EntityTopic:
			rootType = constants.EntityTopic
			var topic struct {
				AcceptedCommentId int64
				BountyScore       int
			}
			if err := tx.Model(&models.Topic{}).Select("accepted_comment_id, bounty_score").
				Where("id = ?", comment.EntityId).Take(&topic).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			topicUpdates := map[string]interface{}{
				"comment_count": gorm.Expr("CASE WHEN comment_count > 0 THEN comment_count - 1 ELSE 0 END"),
			}
			if topic.AcceptedCommentId == comment.Id {
				// The accepted row must never keep pointing at a deleted comment. Keep
				// the question solved only if a bounty was actually paid; otherwise it
				// can safely return to the unsolved queue.
				topicUpdates["accepted_comment_id"] = 0
				paid, err := hasQaBountyReward(tx, comment.EntityId)
				if err != nil {
					return err
				}
				if !paid {
					topicUpdates["qa_status"] = constants.QaStatusUnsolved
					topicUpdates["solved_at"] = 0
				}
			}
			if err := tx.Model(&models.Topic{}).Where("id = ?", comment.EntityId).Updates(topicUpdates).Error; err != nil {
				return err
			}
			if err := refreshTopicLastComment(tx, comment.EntityId); err != nil {
				return err
			}
		case constants.EntityArticle:
			rootType = constants.EntityArticle
			if err := tx.Model(&models.Article{}).Where("id = ?", comment.EntityId).
				UpdateColumn("comment_count", gorm.Expr("CASE WHEN comment_count > 0 THEN comment_count - 1 ELSE 0 END")).Error; err != nil {
				return err
			}
		case constants.EntityComment:
			// Keep sibling replies but remove a now-dangling quote reference to this
			// deleted child. lockCommentForDelete holds the parent row before the
			// target row, so a concurrent quote publish is serialized with this update.
			if err := tx.Model(&models.Comment{}).Where(
				"entity_type = ? AND entity_id = ? AND quote_id = ? AND status = ?",
				constants.EntityComment, comment.EntityId, comment.Id, constants.StatusOk,
			).UpdateColumn("quote_id", 0).Error; err != nil {
				return err
			}

			var parent struct {
				EntityType string
				EntityId   int64
			}
			if err := tx.Model(&models.Comment{}).Select("entity_type, entity_id").
				Where("id = ?", comment.EntityId).Take(&parent).Error; err == nil {
				rootType = parent.EntityType
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Model(&models.Comment{}).Where("id = ?", comment.EntityId).Updates(map[string]interface{}{
				"comment_count": gorm.Expr("CASE WHEN comment_count > 0 THEN comment_count - 1 ELSE 0 END"),
				"hot_score":     gorm.Expr("CASE WHEN hot_score >= 2 THEN hot_score - 2 ELSE 0 END"),
			}).Error; err != nil {
				return err
			}
		}

		for uid, n := range userDecrements {
			if uid <= 0 || n <= 0 {
				continue
			}
			if err := tx.Model(&models.User{}).Where("id = ?", uid).
				UpdateColumn("comment_count", gorm.Expr("CASE WHEN comment_count >= ? THEN comment_count - ? ELSE 0 END", n, n)).Error; err != nil {
				return err
			}
		}

		ctx.RegisterCallback(func() {
			for uid := range userDecrements {
				cache.UserCache.Invalidate(uid)
				UserService.invalidateInfoCache(uid)
			}
			switch rootType {
			case constants.EntityTopic:
				TopicService.InvalidateListCaches()
			case constants.EntityArticle:
				ArticleService.InvalidateListCache()
			default:
				TopicService.InvalidateListCaches()
				ArticleService.InvalidateListCache()
			}
		})
		return nil
	})
	if err != nil {
		return err
	}
	go func() {
		if err := StorageDeleteService.ProcessPending(50); err != nil {
			slog.Warn("immediate comment media cleanup incomplete", slog.Int64("commentId", id), slog.Any("err", err))
		}
	}()
	return nil
}

// lockCommentForDelete serializes comment deletion with comment/reply creation.
// Publish uses root -> parent locking, so deletion takes the same root-first
// order before locking the target comment. This prevents a reply/quote from
// being inserted after a top-level floor has already been cascade-deleted.
func (s *commentService) lockCommentForDelete(tx *gorm.DB, id int64) (*models.Comment, error) {
	if tx == nil || id <= 0 {
		return nil, nil
	}

	var snapshot models.Comment
	if err := tx.Select("id, entity_type, entity_id, status").First(&snapshot, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	rootType := snapshot.EntityType
	rootID := snapshot.EntityId
	parentCommentID := int64(0)
	if snapshot.EntityType == constants.EntityComment {
		parentCommentID = snapshot.EntityId
		var parent struct {
			EntityType string
			EntityId   int64
		}
		if err := tx.Model(&models.Comment{}).Select("entity_type, entity_id").
			Where("id = ?", snapshot.EntityId).Take(&parent).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			rootType, rootID = "", 0
		} else {
			rootType, rootID = parent.EntityType, parent.EntityId
		}
	}

	if rootID > 0 {
		var rootModel interface{}
		switch rootType {
		case constants.EntityTopic:
			rootModel = &models.Topic{}
		case constants.EntityArticle:
			rootModel = &models.Article{}
		}
		if rootModel != nil {
			var found struct{ Id int64 }
			query := tx.Model(rootModel).Select("id").Where("id = ?", rootID)
			if tx.Dialector.Name() != "sqlite" {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.Take(&found).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}
	}

	// Nested reply creation takes root -> parent. Take the same parent lock
	// before locking the child being deleted, so quote validation cannot race
	// with child deletion and create a fresh dangling quote_id.
	if parentCommentID > 0 {
		var found struct{ Id int64 }
		query := tx.Model(&models.Comment{}).Select("id").Where("id = ?", parentCommentID)
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&found).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	var comment models.Comment
	query := tx
	if tx.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&comment, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &comment, nil
}

func (s *commentService) deleteVisibleChildReplies(tx *gorm.DB, parentCommentId int64, userDecrements map[int64]int64) error {
	if parentCommentId <= 0 {
		return nil
	}

	// Aggregate by author instead of loading every reply ID into Go and issuing
	// one enormous IN (...). A classroom floor can have tens of thousands of
	// replies; the indexed predicate + set-based UPDATE keeps memory bounded and
	// avoids oversized SQL/parameter lists while preserving per-user counters.
	type authorCount struct {
		UserId int64 `gorm:"column:user_id"`
		Count  int64 `gorm:"column:reply_count"`
	}
	var counts []authorCount
	if err := tx.Model(&models.Comment{}).
		Select("user_id, COUNT(*) AS reply_count").
		Where("entity_type = ? AND entity_id = ? AND status = ? AND user_id > 0",
			constants.EntityComment, parentCommentId, constants.StatusOk).
		Group("user_id").Scan(&counts).Error; err != nil {
		return err
	}
	for _, item := range counts {
		if item.UserId > 0 && item.Count > 0 {
			userDecrements[item.UserId] += item.Count
		}
	}

	return tx.Model(&models.Comment{}).Where(
		"entity_type = ? AND entity_id = ? AND status = ?",
		constants.EntityComment, parentCommentId, constants.StatusOk,
	).UpdateColumn("status", constants.StatusDeleted).Error
}

func (s *commentService) DeleteByUser(user *models.User, id int64) error {
	if user == nil {
		return errs.NotLogin()
	}
	comment := s.Get(id)
	if comment == nil || comment.Status == constants.StatusDeleted {
		return errors.New(locales.Get("comment.not_found"))
	}
	if !PermissionService.CanManageOwnedResource(user, comment.UserId, permissions.PermissionCommentDelete.Code) {
		return errs.NoPermission()
	}
	return s.Delete(id)
}

// Publish 发表评论
func (s *commentService) Publish(userId int64, form req.CreateCommentReq) (*models.Comment, error) {
	form.Content = strings.TrimSpace(form.Content)
	entityId := form.DecodedEntityId()
	if strs.IsBlank(form.EntityType) {
		return nil, errors.New(locales.Get("comment.invalid_params"))
	}
	if entityId <= 0 {
		return nil, errors.New(locales.Get("comment.invalid_params"))
	}
	if strs.IsBlank(form.Content) {
		return nil, errors.New(locales.Get("comment.content_required"))
	}

	comment := &models.Comment{
		UserId:      userId,
		EntityType:  form.EntityType,
		EntityId:    entityId,
		Content:     form.Content,
		ContentType: constants.ContentTypeText,
		QuoteId:     form.QuoteId,
		Status:      constants.StatusOk,
		UserAgent:   form.UserAgent,
		Ip:          form.Ip,
		IpLocation:  iplocator.IpLocation(form.Ip),
		CreateTime:  dates.NowTimestamp(),
	}

	imageList := form.ParsedImageList()
	if len(imageList) > 0 {
		imageListStr, err := jsons.ToStr(imageList)
		if err == nil {
			comment.ImageList = imageListStr
		} else {
			slog.Error(err.Error(), slog.Any("err", err))
		}
	}

	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		tx := ctx.Tx
		if err := s.validatePublishTarget(tx, comment); err != nil {
			return err
		}
		if err := repositories.CommentRepository.Create(tx, comment); err != nil {
			return err
		}

		switch form.EntityType {
		case constants.EntityTopic:
			if err := TopicService.onComment(tx, entityId, comment); err != nil {
				return err
			}
		case constants.EntityArticle:
			// Articles are a separate content stream. Keep the counter in sync so
			// the native Android article list shows the new reply immediately.
			result := tx.Model(&models.Article{}).Where("id = ? AND status = ?", entityId, constants.StatusOk).
				UpdateColumn("comment_count", gorm.Expr("comment_count + 1"))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return errors.New(locales.Get("common.not_found"))
			}
		case constants.EntityComment: // 二级评论
			if err := s.onComment(tx, comment); err != nil {
				return err
			}
		}
		return UserService.IncrCommentCount(ctx, userId)
	})

	if err != nil {
		return nil, err
	}

	switch form.EntityType {
	case constants.EntityTopic:
		TopicService.InvalidateListCaches()
	case constants.EntityArticle:
		ArticleService.InvalidateListCache()
	case constants.EntityComment:
		// A nested reply may belong to either a topic or an article. Invalidating
		// both tiny first-page caches is cheaper and safer than another lookup.
		TopicService.InvalidateListCaches()
		ArticleService.InvalidateListCache()
	}

	// 发送事件
	event.Send(event.CommentCreateEvent{
		UserId:    userId,
		CommentId: comment.Id,
	})

	return comment, nil
}

// onComment 评论被回复（二级评论）
func (s *commentService) onComment(tx *gorm.DB, comment *models.Comment) error {
	result := tx.Model(&models.Comment{}).
		Where("id = ? AND status = ?", comment.EntityId, constants.StatusOk).
		Updates(map[string]interface{}{
			"comment_count": gorm.Expr("comment_count + 1"),
			"hot_score":     gorm.Expr("hot_score + 2"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New(locales.Get("common.not_found"))
	}
	return nil
}

func (s *commentService) validatePublishTarget(tx *gorm.DB, comment *models.Comment) error {
	if comment == nil || comment.EntityId <= 0 {
		return errors.New(locales.Get("comment.invalid_params"))
	}

	lockEntityExists := func(model interface{}, id int64) error {
		var found struct{ Id int64 }
		query := tx.Model(model).Select("id").Where("id = ? AND status = ?", id, constants.StatusOk)
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		err := query.Take(&found).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(locales.Get("common.not_found"))
		}
		return err
	}

	switch comment.EntityType {
	case constants.EntityTopic:
		if comment.QuoteId > 0 {
			return errors.New(locales.Get("comment.invalid_params"))
		}
		// Serialize comment creation with physical topic deletion. If a comment
		// wins the row lock first, delete waits and then sees this new row; if the
		// delete wins first, this transaction observes the missing topic and rolls
		// back without leaving an orphan comment.
		return lockEntityExists(&models.Topic{}, comment.EntityId)
	case constants.EntityArticle:
		if comment.QuoteId > 0 {
			return errors.New(locales.Get("comment.invalid_params"))
		}
		return lockEntityExists(&models.Article{}, comment.EntityId)
	case constants.EntityComment:
		// Read the parent once without a lock only to discover its root. Then lock
		// root -> parent in that order, matching hard-delete's root-first order and
		// avoiding a parent/root deadlock.
		var snapshot models.Comment
		if err := tx.Select("id, entity_type, entity_id, status").
			First(&snapshot, "id = ? AND status = ?", comment.EntityId, constants.StatusOk).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("common.not_found"))
			}
			return err
		}

		switch snapshot.EntityType {
		case constants.EntityTopic:
			if err := lockEntityExists(&models.Topic{}, snapshot.EntityId); err != nil {
				return err
			}
		case constants.EntityArticle:
			if err := lockEntityExists(&models.Article{}, snapshot.EntityId); err != nil {
				return err
			}
		default:
			return errors.New(locales.Get("comment.invalid_params"))
		}

		var parent models.Comment
		parentQuery := tx.Select("id, entity_type, entity_id, status")
		if tx.Dialector.Name() != "sqlite" {
			parentQuery = parentQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := parentQuery.First(&parent, "id = ? AND status = ?", comment.EntityId, constants.StatusOk).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("common.not_found"))
			}
			return err
		}
		if parent.EntityType != snapshot.EntityType || parent.EntityId != snapshot.EntityId {
			return errors.New(locales.Get("comment.invalid_params"))
		}

		if comment.QuoteId <= 0 {
			return nil
		}
		var quote models.Comment
		if err := tx.Select("id, entity_type, entity_id, status").
			First(&quote, "id = ? AND status = ?", comment.QuoteId, constants.StatusOk).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("common.not_found"))
			}
			return err
		}
		if quote.Id != parent.Id &&
			(quote.EntityType != constants.EntityComment || quote.EntityId != parent.Id) {
			return errors.New(locales.Get("comment.invalid_params"))
		}
		return nil
	default:
		return errors.New(locales.Get("comment.invalid_params"))
	}
}

func refreshTopicLastComment(tx *gorm.DB, topicId int64) error {
	var latest struct {
		UserId     int64
		CreateTime int64
	}
	err := tx.Model(&models.Comment{}).
		Select("user_id, create_time").
		Where("entity_type = ? AND entity_id = ? AND status = ?", constants.EntityTopic, topicId, constants.StatusOk).
		Order("create_time DESC").Order("id DESC").
		Take(&latest).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		latest.UserId = 0
		latest.CreateTime = 0
	}

	if err := tx.Model(&models.Topic{}).Where("id = ?", topicId).Updates(map[string]interface{}{
		"last_comment_time":    latest.CreateTime,
		"last_comment_user_id": latest.UserId,
	}).Error; err != nil {
		return err
	}
	return tx.Model(&models.TopicTag{}).Where("topic_id = ?", topicId).Updates(map[string]interface{}{
		"last_comment_time":    latest.CreateTime,
		"last_comment_user_id": latest.UserId,
	}).Error
}

// // 统计数量
// func (s *commentService) Count(entityType string, entityId int64) int64 {
// 	var count int64 = 0
// 	sqls.DB().Model(&model.Comment{}).Where("entity_type = ? and entity_id = ?", entityType, entityId).Count(&count)
// 	return count
// }

// GetComments 列表。保留旧入口，默认使用倒序，兼容现有调用。
func (s *commentService) GetComments(entityType string, entityId int64, cursor int64) (comments []models.Comment, nextCursor int64, hasMore bool) {
	return s.GetCommentsSorted(entityType, entityId, cursor, "desc")
}

// GetCommentsSorted 支持三种排序：
// asc  正序，最早评论在前；desc 倒序，最新评论在前；hot 热门。
// 热门游标保存上一条评论的 (hot_score, id) 排序边界，避免 OFFSET
// 在高楼层评论区越来越慢，也不依赖翻页时锚点的当前分数。
func (s *commentService) GetCommentsSorted(entityType string, entityId int64, cursor int64, sortMode string) (comments []models.Comment, nextCursor int64, hasMore bool) {
	return s.GetCommentsSortedByUser(entityType, entityId, cursor, sortMode, 0)
}

// GetCommentsSortedByUser keeps filtering on the database side. This is used by
// "only owner" so a 20-item page is not fetched and then mostly discarded on
// the client, which would break cursor pagination on long threads.
func (s *commentService) GetCommentsSortedByUser(entityType string, entityId int64, cursor int64, sortMode string, userId int64) (comments []models.Comment, nextCursor int64, hasMore bool) {
	const limit = 20
	var acceptedComment *models.Comment
	var acceptedCommentId int64

	if entityType == constants.EntityTopic {
		if topic := TopicService.Get(entityId); topic != nil && topic.AcceptedCommentId > 0 {
			acceptedCommentId = topic.AcceptedCommentId
			acceptedComment = repositories.CommentRepository.FindOne(sqls.DB(), sqls.NewCnd().
				Eq("id", acceptedCommentId).
				Eq("entity_type", entityType).
				Eq("entity_id", entityId).
				Eq("status", constants.StatusOk))
			if acceptedComment == nil {
				acceptedCommentId = 0
			}
		}
	}

	if userId > 0 && acceptedComment != nil && acceptedComment.UserId != userId {
		acceptedComment = nil
		acceptedCommentId = 0
	}

	normalLimit := limit
	if cursor <= 0 && acceptedComment != nil {
		normalLimit = limit - 1
	}
	fetchLimit := normalLimit + 1

	query := sqls.DB().Model(&models.Comment{}).
		Where("entity_type = ? AND entity_id = ? AND status = ?", entityType, entityId, constants.StatusOk)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if acceptedCommentId > 0 {
		query = query.Where("id <> ?", acceptedCommentId)
	}

	switch sortMode {
	case "asc", "positive", "oldest":
		if cursor > 0 {
			query = query.Where("id > ?", cursor)
		}
		query = query.Order("id ASC").Limit(fetchLimit)
	case "hot", "popular":
		sortMode = "hot"
		if cursor > 0 {
			anchorScore, anchorID, encoded := decodeHotCommentCursor(cursor)
			if !encoded {
				// Backward compatibility for clients holding a cursor produced by an
				// older server, where the cursor was only the comment id.
				var anchor models.Comment
				if err := sqls.DB().Select("id, hot_score").
					Where("id = ? AND entity_type = ? AND entity_id = ?", cursor, entityType, entityId).
					Take(&anchor).Error; err != nil {
					return nil, cursor, false
				}
				anchorScore, anchorID = anchor.HotScore, anchor.Id
			}
			query = query.Where("(hot_score < ? OR (hot_score = ? AND id < ?))", anchorScore, anchorScore, anchorID)
		}
		query = query.
			Order("hot_score DESC").
			Order("id DESC").
			Limit(fetchLimit)
	default:
		sortMode = "desc"
		if cursor > 0 {
			query = query.Where("id < ?", cursor)
		}
		query = query.Order("id DESC").Limit(fetchLimit)
	}

	if err := query.Find(&comments).Error; err != nil {
		return nil, cursor, false
	}

	hasMore = len(comments) > normalLimit
	if hasMore {
		comments = comments[:normalLimit]
	}
	if cursor <= 0 && acceptedComment != nil {
		comments = append([]models.Comment{*acceptedComment}, comments...)
	}

	normalCount := len(comments)
	if cursor <= 0 && acceptedComment != nil && normalCount > 0 {
		normalCount--
	}
	if normalCount <= 0 {
		return comments, cursor, false
	}

	last := comments[len(comments)-1]
	// 第一项可能是置顶的采纳回答，但最后一项始终是普通游标项。
	if sortMode == "hot" {
		nextCursor = encodeHotCommentCursor(last.HotScore, last.Id)
	} else {
		nextCursor = last.Id
	}
	return comments, nextCursor, hasMore
}

// GetByIds 根据编号批量获取评论。
func (s *commentService) GetByIds(ids []int64) map[int64]models.Comment {
	result := make(map[int64]models.Comment, len(ids))
	for _, comment := range repositories.CommentRepository.FindByIds(sqls.DB(), ids) {
		result[comment.Id] = comment
	}
	return result
}

// GetTopRepliesByCommentIds 批量获取每个一级评论的前 limit 条二级回复。
func (s *commentService) GetTopRepliesByCommentIds(commentIds []int64, limit int) map[int64][]models.Comment {
	result := make(map[int64][]models.Comment, len(commentIds))
	replies, err := repositories.CommentRepository.FindTopRepliesByCommentIds(sqls.DB(), commentIds, limit)
	if err != nil {
		slog.Error("batch load comment replies failed", slog.Any("err", err))
		return result
	}
	for _, reply := range replies {
		result[reply.EntityId] = append(result[reply.EntityId], reply)
	}
	return result
}

// GetReplies 二级回复列表
func (s *commentService) GetReplies(commentId int64, cursor int64, limit int) (comments []models.Comment, nextCursor int64, hasMore bool) {
	if limit <= 0 {
		limit = 10
	}
	// Fetch one extra row so an exactly-full final page does not incorrectly
	// advertise hasMore=true and trigger a redundant empty request.
	cnd := sqls.NewCnd().Eq("entity_type", constants.EntityComment).Eq("entity_id", commentId).Eq("status", constants.StatusOk).Asc("id").Limit(limit + 1)
	if cursor > 0 {
		cnd.Gt("id", cursor)
	}
	comments = s.Find(cnd)
	if len(comments) > limit {
		hasMore = true
		comments = comments[:limit]
	}
	if len(comments) > 0 {
		nextCursor = comments[len(comments)-1].Id
	} else {
		nextCursor = cursor
	}
	return
}

// ScanByUser 按照用户扫描数据
func (s *commentService) ScanByUser(userId int64, callback func(comments []models.Comment)) {
	var cursor int64 = 0
	for {
		list := repositories.CommentRepository.Find(sqls.DB(), sqls.NewCnd().
			Eq("user_id", userId).Gt("id", cursor).Asc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

// ScanByUser 按照用户扫描数据
func (s *commentService) Scan(callback func(comments []models.Comment)) {
	var cursor int64 = 0
	for {
		logrus.Info("scan comments, cursor:" + cast.ToString(cursor))
		list := repositories.CommentRepository.Find(sqls.DB(), sqls.NewCnd().
			Gt("id", cursor).Asc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

func (s *commentService) IsCommented(userId int64, entityType string, entityId int64) bool {
	return s.FindOne(sqls.NewCnd().Where("user_id = ? and entity_id = ? and entity_type = ? and status = ?", userId, entityId, entityType, constants.StatusOk)) != nil
}
