package services

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/search"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var SearchDeleteService = &searchDeleteService{}

type searchDeleteService struct{}

// Enqueue records the external search cleanup in the same SQL transaction as
// the physical entity delete. If the SQL delete rolls back, the outbox row rolls
// back too; if it commits, search cleanup remains retryable across restarts.
func (s *searchDeleteService) Enqueue(tx *gorm.DB, entityType string, entityID int64) error {
	if tx == nil || entityID <= 0 {
		return nil
	}
	if entityType != constants.EntityTopic && entityType != constants.EntityArticle {
		return fmt.Errorf("unsupported search delete entity type %q", entityType)
	}
	now := dates.NowTimestamp()
	task := models.SearchDeleteTask{
		EntityType:    entityType,
		EntityId:      entityID,
		NextRetryTime: now,
		CreateTime:    now,
		UpdateTime:    now,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&task).Error
}

// RefreshTopicIndex writes the current topic document, then verifies the SQL
// root still exists. This closes the edit/delete race where a late index update
// could recreate a document after physical deletion already removed it.
func (s *searchDeleteService) RefreshTopicIndex(topicID int64, topic *models.Topic) {
	if topicID <= 0 {
		return
	}
	if topic != nil {
		search.UpdateTopicIndex(topic)
	}
	var count int64
	if err := sqls.DB().Model(&models.Topic{}).Where("id = ? AND status <> ?", topicID, constants.StatusDeleted).Count(&count).Error; err != nil {
		slog.Warn("verify topic after search refresh failed", slog.Int64("topicId", topicID), slog.Any("err", err))
		return
	}
	if count > 0 {
		return
	}
	if err := s.Enqueue(sqls.DB(), constants.EntityTopic, topicID); err != nil {
		slog.Warn("queue corrective topic search delete failed", slog.Int64("topicId", topicID), slog.Any("err", err))
		return
	}
	if err := s.ProcessEntity(constants.EntityTopic, topicID); err != nil {
		slog.Warn("corrective topic search delete incomplete", slog.Int64("topicId", topicID), slog.Any("err", err))
	}
}

// RefreshArticleIndex is the article equivalent of RefreshTopicIndex.
func (s *searchDeleteService) RefreshArticleIndex(articleID int64, article *models.Article) {
	if articleID <= 0 {
		return
	}
	if article != nil {
		search.UpdateArticleIndex(article)
	}
	var count int64
	if err := sqls.DB().Model(&models.Article{}).Where("id = ? AND status <> ?", articleID, constants.StatusDeleted).Count(&count).Error; err != nil {
		slog.Warn("verify article after search refresh failed", slog.Int64("articleId", articleID), slog.Any("err", err))
		return
	}
	if count > 0 {
		return
	}
	if err := s.Enqueue(sqls.DB(), constants.EntityArticle, articleID); err != nil {
		slog.Warn("queue corrective article search delete failed", slog.Int64("articleId", articleID), slog.Any("err", err))
		return
	}
	if err := s.ProcessEntity(constants.EntityArticle, articleID); err != nil {
		slog.Warn("corrective article search delete incomplete", slog.Int64("articleId", articleID), slog.Any("err", err))
	}
}

func (s *searchDeleteService) ProcessEntity(entityType string, entityID int64) error {
	if entityID <= 0 {
		return nil
	}
	var task models.SearchDeleteTask
	err := sqls.DB().Where("entity_type = ? AND entity_id = ?", entityType, entityID).Take(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.deleteIndex(task); err != nil {
		s.recordFailure(&task, err)
		return err
	}
	return sqls.DB().Delete(&models.SearchDeleteTask{}, "id = ?", task.Id).Error
}

func (s *searchDeleteService) ProcessPending(limit int) error {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	now := dates.NowTimestamp()
	var tasks []models.SearchDeleteTask
	if err := sqls.DB().Where("next_retry_time <= ?", now).Order("id ASC").Limit(limit).Find(&tasks).Error; err != nil {
		return err
	}
	var firstErr error
	for i := range tasks {
		task := tasks[i]
		if err := s.deleteIndex(task); err != nil {
			s.recordFailure(&task, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := sqls.DB().Delete(&models.SearchDeleteTask{}, "id = ?", task.Id).Error; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *searchDeleteService) deleteIndex(task models.SearchDeleteTask) error {
	switch task.EntityType {
	case constants.EntityTopic:
		return search.DeleteTopicIndex(task.EntityId)
	case constants.EntityArticle:
		return search.DeleteArticleIndex(task.EntityId)
	default:
		return fmt.Errorf("unsupported search delete entity type %q", task.EntityType)
	}
}

func (s *searchDeleteService) recordFailure(task *models.SearchDeleteTask, err error) {
	if task == nil || task.Id <= 0 {
		return
	}
	attempt := task.AttemptCount + 1
	delay := time.Minute
	for i := 1; i < attempt && delay < time.Hour; i++ {
		delay *= 2
		if delay > time.Hour {
			delay = time.Hour
		}
	}
	now := dates.NowTimestamp()
	message := ""
	if err != nil {
		message = err.Error()
	}
	if len(message) > 4000 {
		message = message[:4000]
	}
	_ = sqls.DB().Model(&models.SearchDeleteTask{}).Where("id = ?", task.Id).Updates(map[string]any{
		"attempt_count":   attempt,
		"next_retry_time": now + int64(delay/time.Millisecond),
		"last_error":      message,
		"update_time":     now,
	}).Error
	slog.Warn("delete search document failed; queued for retry",
		slog.Int64("taskId", task.Id), slog.String("entityType", task.EntityType),
		slog.Int64("entityId", task.EntityId), slog.Any("err", err))
}
