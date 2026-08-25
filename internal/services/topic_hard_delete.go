package services

import (
	"bbs-go/internal/cache"
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

// hardDeleteTopicGraph permanently removes the topic-owned database graph.
// Accounting/audit ledgers (score/operate logs) are intentionally retained.
// User-facing notifications pointing at this topic are removed. Forum-owned
// media is enqueued in the same transaction and physically deleted immediately
// after commit. The durable delete worker checks surviving references first so
// a shared object is removed only after its last live owner disappears.
func hardDeleteTopicGraph(ctx *sqls.TxContext, topic *models.Topic) error {
	if ctx == nil || ctx.Tx == nil || topic == nil || topic.Id <= 0 {
		return nil
	}
	tx := ctx.Tx

	commentIds, publishedByUser, err := topicCommentDeletePlan(tx, topic.Id)
	if err != nil {
		return err
	}
	mediaTargets, err := collectTopicStorageDeleteTargets(tx, topic, commentIds)
	if err != nil {
		return err
	}
	if err := StorageDeleteService.EnqueueTargets(tx, mediaTargets); err != nil {
		return err
	}

	// Likes and reports can point at the topic itself or at any comment in its
	// two-level thread. Remove them before deleting the comments.
	if err := tx.Where("entity_type = ? AND entity_id = ?", constants.EntityTopic, topic.Id).
		Delete(&models.UserLike{}).Error; err != nil {
		return err
	}
	if err := tx.Where("entity_type = ? AND entity_id = ?", constants.EntityTopic, topic.Id).
		Delete(&models.Favorite{}).Error; err != nil {
		return err
	}
	if err := tx.Where("data_type = ? AND data_id = ?", constants.EntityTopic, topic.Id).
		Delete(&models.UserReport{}).Error; err != nil {
		return err
	}
	if err := tx.Where("data_type = ? AND data_id = ?", constants.EntityTopic, topic.Id).
		Delete(&models.UserFeed{}).Error; err != nil {
		return err
	}
	if err := tx.Where("topic_id = ?", topic.Id).Delete(&models.TopicReadProgress{}).Error; err != nil {
		return err
	}

	if err := forEachInt64Batch(commentIds, 500, func(batch []int64) error {
		if err := tx.Where("entity_type = ? AND entity_id IN ?", constants.EntityComment, batch).
			Delete(&models.UserLike{}).Error; err != nil {
			return err
		}
		if err := tx.Where("data_type = ? AND data_id IN ?", constants.EntityComment, batch).
			Delete(&models.UserReport{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", batch).Delete(&models.Comment{}).Error
	}); err != nil {
		return err
	}

	if err := TopicTagService.HardDeleteTopicTags(ctx, topic.Id); err != nil {
		return err
	}

	if err := hardDeleteTopicVotes(tx, topic.Id); err != nil {
		return err
	}
	if err := hardDeleteTopicAttachments(tx, topic.Id); err != nil {
		return err
	}
	if err := hardDeleteTopicMessages(tx, topic.Id); err != nil {
		return err
	}

	// Only published comments incremented user.comment_count. Older soft-deleted
	// or review rows are still physically removed but must not decrement twice.
	for userID, count := range publishedByUser {
		if userID <= 0 || count <= 0 {
			continue
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).UpdateColumn(
			"comment_count",
			gorm.Expr("CASE WHEN comment_count >= ? THEN comment_count - ? ELSE 0 END", count, count),
		).Error; err != nil {
			return err
		}
		uid := userID
		ctx.RegisterCallback(func() {
			cache.UserCache.Invalidate(uid)
			UserService.invalidateInfoCache(uid)
		})
	}

	if err := SearchDeleteService.Enqueue(tx, constants.EntityTopic, topic.Id); err != nil {
		return err
	}
	return tx.Delete(&models.Topic{}, "id = ?", topic.Id).Error
}

func topicCommentDeletePlan(tx *gorm.DB, topicID int64) ([]int64, map[int64]int64, error) {
	publishedByUser := make(map[int64]int64)
	if topicID <= 0 {
		return nil, publishedByUser, nil
	}

	var roots []models.Comment
	if err := tx.Select("id, user_id, status").Where(
		"entity_type = ? AND entity_id = ?", constants.EntityTopic, topicID,
	).Find(&roots).Error; err != nil {
		return nil, nil, err
	}

	rootIDs := make([]int64, 0, len(roots))
	allIDs := make([]int64, 0, len(roots))
	for _, comment := range roots {
		rootIDs = append(rootIDs, comment.Id)
		allIDs = append(allIDs, comment.Id)
		if comment.Status == constants.StatusOk && comment.UserId > 0 {
			publishedByUser[comment.UserId]++
		}
	}

	if len(rootIDs) == 0 {
		return allIDs, publishedByUser, nil
	}

	if err := forEachInt64Batch(rootIDs, 500, func(batch []int64) error {
		var replies []models.Comment
		if err := tx.Select("id, user_id, status").Where(
			"entity_type = ? AND entity_id IN ?", constants.EntityComment, batch,
		).Find(&replies).Error; err != nil {
			return err
		}
		for _, reply := range replies {
			allIDs = append(allIDs, reply.Id)
			if reply.Status == constants.StatusOk && reply.UserId > 0 {
				publishedByUser[reply.UserId]++
			}
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return allIDs, publishedByUser, nil
}

func forEachInt64Batch(ids []int64, size int, fn func([]int64) error) error {
	if len(ids) == 0 || fn == nil {
		return nil
	}
	if size <= 0 {
		size = 500
	}
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		if err := fn(ids[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func hardDeleteTopicVotes(tx *gorm.DB, topicID int64) error {
	var voteIDs []int64
	if err := tx.Model(&models.Vote{}).Where("topic_id = ?", topicID).Pluck("id", &voteIDs).Error; err != nil {
		return err
	}
	return forEachInt64Batch(voteIDs, 500, func(batch []int64) error {
		if err := tx.Where("vote_id IN ?", batch).Delete(&models.VoteRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("vote_id IN ?", batch).Delete(&models.VoteOption{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", batch).Delete(&models.Vote{}).Error
	})
}

func hardDeleteTopicAttachments(tx *gorm.DB, topicID int64) error {
	var attachmentIDs []string
	if err := tx.Model(&models.Attachment{}).Where("topic_id = ?", topicID).
		Pluck("id", &attachmentIDs).Error; err != nil {
		return err
	}
	if err := forEachStringBatch(attachmentIDs, 500, func(batch []string) error {
		return tx.Where("attachment_id IN ?", batch).Delete(&models.AttachmentDownloadLog{}).Error
	}); err != nil {
		return err
	}
	return tx.Where("topic_id = ?", topicID).Delete(&models.Attachment{}).Error
}

func forEachStringBatch(values []string, size int, fn func([]string) error) error {
	if len(values) == 0 || fn == nil {
		return nil
	}
	if size <= 0 {
		size = 500
	}
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		if err := fn(values[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// hardDeleteTopicMessages removes user-facing notifications whose target is the
// deleted topic. Score/audit logs are intentionally retained, but stale notices
// would otherwise keep dead links and copied topic text after a hard delete.
func hardDeleteTopicMessages(tx *gorm.DB, topicID int64) error {
	return deleteTopicMessages(tx, topicID, false)
}

func deleteTopicMessages(tx *gorm.DB, topicID int64, preserveDeleteNotice bool) error {
	if topicID <= 0 {
		return nil
	}

	needle := strconv.FormatInt(topicID, 10)
	var cursor int64
	for {
		// Page through candidates so a topic with a very large notification
		// history does not load every message or build one huge DELETE IN list.
		var candidates []models.Message
		if err := tx.Select("id, type, extra_data").Where("id > ?", cursor).Where(
			"extra_data LIKE ? OR extra_data LIKE ?",
			"%\"topicId\":"+needle+"%",
			"%\"rootEntityId\":\""+needle+"\"%",
		).Order("id ASC").Limit(500).Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		cursor = candidates[len(candidates)-1].Id
		ids := make([]int64, 0, len(candidates))
		for _, message := range candidates {
			var payload map[string]interface{}
			decoder := json.NewDecoder(strings.NewReader(message.ExtraData))
			decoder.UseNumber()
			if err := decoder.Decode(&payload); err != nil {
				continue
			}
			// Topic-delete notices are intentionally emitted after the physical
			// delete commits. A late comment-event compensation must not erase that
			// audit/user notice. Detect it from its dedicated extra-data field rather
			// than importing the msg package into services (avoids package coupling).
			if preserveDeleteNotice && jsonValueEqualsInt64(payload["topicId"], topicID) {
				if _, ok := payload["deleteUserId"]; ok {
					continue
				}
			}
			if jsonValueEqualsInt64(payload["topicId"], topicID) ||
				(fmt.Sprint(payload["rootEntityType"]) == constants.EntityTopic && fmt.Sprint(payload["rootEntityId"]) == needle) {
				ids = append(ids, message.Id)
			}
		}
		if len(ids) > 0 {
			if err := tx.Where("id IN ?", ids).Delete(&models.Message{}).Error; err != nil {
				return err
			}
		}
		if len(candidates) < 500 {
			return nil
		}
	}
}

func jsonValueEqualsInt64(value interface{}, expected int64) bool {
	switch v := value.(type) {
	case float64:
		return int64(v) == expected && v == float64(expected)
	case string:
		return v == strconv.FormatInt(expected, 10)
	case json.Number:
		n, err := v.Int64()
		return err == nil && n == expected
	default:
		return fmt.Sprint(v) == strconv.FormatInt(expected, 10)
	}
}
