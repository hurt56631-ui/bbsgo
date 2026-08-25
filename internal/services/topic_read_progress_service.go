package services

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/locales"
	"errors"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm/clause"
)

var TopicReadProgressService = new(topicReadProgressService)

type topicReadProgressService struct{}

func (s *topicReadProgressService) Get(userID, topicID int64) *models.TopicReadProgress {
	if userID <= 0 || topicID <= 0 {
		return nil
	}
	var progress models.TopicReadProgress
	if err := sqls.DB().Where("user_id = ? AND topic_id = ?", userID, topicID).Take(&progress).Error; err != nil {
		return nil
	}
	return &progress
}

// Save stores two related pieces of state:
//   - LastCommentId / ReadCommentCount: monotonic furthest-read progress used for unread state.
//   - AnchorCommentId / AnchorOffsetDp / ScrollProgress: the most recent resume position.
//
// Keeping them separate prevents a user who scrolls back to reread an older section from making
// already-read replies become unread again, while still allowing "continue reading" to reopen at
// the exact place they last left. Writes are serialized with a row lock so concurrent devices or
// delayed mobile requests cannot move the durable furthest-read marker backwards.
func (s *topicReadProgressService) Save(
	userID, topicID, lastCommentID, anchorCommentID int64,
	anchorOffsetDp, scrollProgress, scrollPercent int, updateResumeFields bool,
) (*models.TopicReadProgress, error) {
	if userID <= 0 || topicID <= 0 {
		return nil, errors.New(locales.Get("comment.invalid_params"))
	}
	// Treat malformed negative client coordinates as the beginning instead of
	// ever persisting negative ids into the canonical progress row. Official
	// clients already clamp these values, but the API remains defensive.
	if lastCommentID < 0 {
		lastCommentID = 0
	}
	if anchorCommentID < 0 {
		anchorCommentID = 0
		anchorOffsetDp = 0
	}
	topic := TopicService.Get(topicID)
	if topic == nil || topic.Status != constants.StatusOk {
		return nil, errors.New(locales.Get("common.not_found"))
	}

	if scrollProgress < 0 {
		scrollProgress = 0
	} else if scrollProgress > 10000 {
		scrollProgress = 10000
	}
	if scrollPercent < 0 {
		scrollPercent = 0
	} else if scrollPercent > 100 {
		scrollPercent = 100
	}
	if anchorOffsetDp < -4096 {
		anchorOffsetDp = -4096
	} else if anchorOffsetDp > 4096 {
		anchorOffsetDp = 4096
	}

	readCommentCount := int64(0)
	if lastCommentID > 0 {
		valid, count, err := validateTopicCommentAndCount(topicID, lastCommentID)
		if err != nil {
			return nil, err
		}
		if !valid {
			lastCommentID = 0
		} else {
			readCommentCount = count
		}
	}

	if anchorCommentID > 0 {
		valid, _, err := validateTopicCommentAndCount(topicID, anchorCommentID)
		if err != nil {
			return nil, err
		}
		if !valid {
			anchorCommentID = 0
			anchorOffsetDp = 0
		}
	}

	now := dates.NowTimestamp()
	candidate := &models.TopicReadProgress{
		UserId:           userID,
		TopicId:          topicID,
		LastCommentId:    lastCommentID,
		ReadCommentCount: readCommentCount,
		AnchorCommentId:  anchorCommentID,
		AnchorOffsetDp:   anchorOffsetDp,
		ScrollProgress:   scrollProgress,
		ScrollPercent:    scrollPercent,
		LastReadTime:     now,
		CreateTime:       now,
		UpdateTime:       now,
	}

	var saved models.TopicReadProgress
	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		tx := ctx.Tx

		// Ensure the row exists without racing another device creating it at the same time.
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "topic_id"}},
			DoNothing: true,
		}).Create(candidate).Error; err != nil {
			return err
		}

		query := tx.Where("user_id = ? AND topic_id = ?", userID, topicID)
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&saved).Error; err != nil {
			return err
		}

		furthestCommentID := saved.LastCommentId
		furthestReadCount := saved.ReadCommentCount
		if lastCommentID > furthestCommentID {
			furthestCommentID = lastCommentID
			furthestReadCount = readCommentCount
		} else if lastCommentID == furthestCommentID && readCommentCount > furthestReadCount {
			furthestReadCount = readCommentCount
		}

		updates := map[string]interface{}{
			"last_comment_id":    furthestCommentID,
			"read_comment_count": furthestReadCount,
			"scroll_percent":     scrollPercent,
			"last_read_time":     now,
			"update_time":        now,
		}
		if updateResumeFields {
			updates["anchor_comment_id"] = anchorCommentID
			updates["anchor_offset_dp"] = anchorOffsetDp
			updates["scroll_progress"] = scrollProgress
		}
		if err := tx.Model(&saved).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND topic_id = ?", userID, topicID).Take(&saved).Error
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func validateTopicCommentAndCount(topicID, commentID int64) (bool, int64, error) {
	var validCount int64
	if err := sqls.DB().Model(&models.Comment{}).Where(
		"id = ? AND entity_type = ? AND entity_id = ? AND status = ?",
		commentID, constants.EntityTopic, topicID, constants.StatusOk,
	).Count(&validCount).Error; err != nil {
		return false, 0, err
	}
	if validCount == 0 {
		return false, 0, nil
	}

	var readCommentCount int64
	if err := sqls.DB().Model(&models.Comment{}).Where(
		"entity_type = ? AND entity_id = ? AND status = ? AND id <= ?",
		constants.EntityTopic, topicID, constants.StatusOk, commentID,
	).Count(&readCommentCount).Error; err != nil {
		return false, 0, err
	}
	return true, readCommentCount, nil
}
