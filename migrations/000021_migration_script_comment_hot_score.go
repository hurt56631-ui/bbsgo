package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

// migrate_comment_hot_score persists the lightweight ranking expression used
// by the hot-comment feed. This lets MySQL satisfy the ranking from an index
// instead of calculating and filesorting every comment on each request.
func migrate_comment_hot_score() error {
	db := sqls.DB()
	if !db.Migrator().HasColumn(&models.Comment{}, "HotScore") {
		if err := db.Migrator().AddColumn(&models.Comment{}, "HotScore"); err != nil {
			return err
		}
	}

	const batchSize = 2000
	var cursor int64
	for {
		ids := make([]int64, 0, batchSize)
		if err := db.Model(&models.Comment{}).Where(
			"id > ? AND hot_score <> like_count * 3 + comment_count * 2", cursor,
		).Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		if err := db.Model(&models.Comment{}).Where("id IN ?", ids).
			UpdateColumn("hot_score", gorm.Expr("like_count * 3 + comment_count * 2")).Error; err != nil {
			return err
		}
		cursor = ids[len(ids)-1]
	}

	if err := ensureCommunityIndex(db, communityIndexSpec{
		model: &models.Comment{},
		name:  "idx_comment_entity_status_hot_id",
		sql:   "CREATE INDEX idx_comment_entity_status_hot_id ON t_comment (entity_type, entity_id, status, hot_score, id)",
	}); err != nil {
		return err
	}
	return ensureCommunityIndex(db, communityIndexSpec{
		model: &models.UserScoreLog{},
		name:  "idx_user_score_source_id_type",
		sql:   "CREATE INDEX idx_user_score_source_id_type ON t_user_score_log (source_type, source_id, type)",
	})
}
