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

// hardDeleteArticleGraph permanently removes an article and all database rows
// owned by that article. Audit/accounting records are intentionally retained.
// Media targets are written to the durable storage-delete outbox in the same
// transaction, so the database commit is never allowed to forget what objects
// still need to be reclaimed.
func hardDeleteArticleGraph(ctx *sqls.TxContext, article *models.Article) error {
	if ctx == nil || ctx.Tx == nil || article == nil || article.Id <= 0 {
		return nil
	}
	tx := ctx.Tx

	commentIDs, publishedByUser, err := articleCommentDeletePlan(tx, article.Id)
	if err != nil {
		return err
	}
	mediaTargets, err := collectArticleStorageDeleteTargets(tx, article, commentIDs)
	if err != nil {
		return err
	}
	if err := StorageDeleteService.EnqueueTargets(tx, mediaTargets); err != nil {
		return err
	}

	if err := tx.Where("entity_type = ? AND entity_id = ?", constants.EntityArticle, article.Id).
		Delete(&models.UserLike{}).Error; err != nil {
		return err
	}
	if err := tx.Where("entity_type = ? AND entity_id = ?", constants.EntityArticle, article.Id).
		Delete(&models.Favorite{}).Error; err != nil {
		return err
	}
	if err := tx.Where("data_type = ? AND data_id = ?", constants.EntityArticle, article.Id).
		Delete(&models.UserReport{}).Error; err != nil {
		return err
	}
	if err := tx.Where("data_type = ? AND data_id = ?", constants.EntityArticle, article.Id).
		Delete(&models.UserFeed{}).Error; err != nil {
		return err
	}

	if err := forEachInt64Batch(commentIDs, 500, func(batch []int64) error {
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

	if err := tx.Where("article_id = ?", article.Id).Delete(&models.ArticleTag{}).Error; err != nil {
		return err
	}
	if err := hardDeleteArticleMessages(tx, article.Id); err != nil {
		return err
	}

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

	if err := SearchDeleteService.Enqueue(tx, constants.EntityArticle, article.Id); err != nil {
		return err
	}
	return tx.Delete(&models.Article{}, "id = ?", article.Id).Error
}

func articleCommentDeletePlan(tx *gorm.DB, articleID int64) ([]int64, map[int64]int64, error) {
	publishedByUser := make(map[int64]int64)
	if articleID <= 0 {
		return nil, publishedByUser, nil
	}

	var roots []models.Comment
	if err := tx.Select("id, user_id, status").Where(
		"entity_type = ? AND entity_id = ?", constants.EntityArticle, articleID,
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

// hardDeleteArticleMessages removes user-facing notifications that would point
// to the now-missing article. Audit logs are not stored in this table and are
// deliberately unaffected.
func hardDeleteArticleMessages(tx *gorm.DB, articleID int64) error {
	if articleID <= 0 {
		return nil
	}
	needle := strconv.FormatInt(articleID, 10)
	var cursor int64
	for {
		var candidates []models.Message
		if err := tx.Select("id, extra_data").Where("id > ?", cursor).Where(
			"(extra_data LIKE ? AND extra_data LIKE ?) OR (extra_data LIKE ? AND extra_data LIKE ?)",
			"%\"entityType\":\""+constants.EntityArticle+"\"%",
			"%\"entityId\":"+needle+"%",
			"%\"rootEntityType\":\""+constants.EntityArticle+"\"%",
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
			entityMatch := fmt.Sprint(payload["entityType"]) == constants.EntityArticle && jsonValueEqualsInt64(payload["entityId"], articleID)
			rootMatch := fmt.Sprint(payload["rootEntityType"]) == constants.EntityArticle && fmt.Sprint(payload["rootEntityId"]) == needle
			if entityMatch || rootMatch {
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
