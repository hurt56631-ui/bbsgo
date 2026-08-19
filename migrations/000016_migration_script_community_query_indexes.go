package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

type communityIndexSpec struct {
	model interface{}
	name  string
	sql   string
}

func migrate_community_query_indexes() error {
	return sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		tx := ctx.Tx

		// 历史版本没有收藏唯一约束。先保留每组最早的一条，再创建唯一索引，
		// 避免已有重复数据导致升级失败。
		if err := tx.Exec(`
DELETE FROM t_favorite
WHERE id NOT IN (
    SELECT keep_id FROM (
        SELECT MIN(id) AS keep_id
        FROM t_favorite
        GROUP BY user_id, entity_type, entity_id
    ) AS favorite_keep_rows
)`).Error; err != nil {
			return err
		}

		indexes := []communityIndexSpec{
			{&models.Favorite{}, "uk_favorite_user_entity", "CREATE UNIQUE INDEX uk_favorite_user_entity ON t_favorite (user_id, entity_type, entity_id)"},
			{&models.Topic{}, "idx_topic_status_last_comment_id", "CREATE INDEX idx_topic_status_last_comment_id ON t_topic (status, last_comment_time, id)"},
			{&models.Topic{}, "idx_topic_category_status_last_comment_id", "CREATE INDEX idx_topic_category_status_last_comment_id ON t_topic (category_id, status, last_comment_time, id)"},
			{&models.Topic{}, "idx_topic_category_status_id", "CREATE INDEX idx_topic_category_status_id ON t_topic (category_id, status, id)"},
			{&models.Topic{}, "idx_topic_user_status_id", "CREATE INDEX idx_topic_user_status_id ON t_topic (user_id, status, id)"},
			{&models.Topic{}, "idx_topic_recommend_status_last_comment_id", "CREATE INDEX idx_topic_recommend_status_last_comment_id ON t_topic (recommend, status, last_comment_time, id)"},
			{&models.TopicTag{}, "idx_topic_tag_tag_status_time_id", "CREATE INDEX idx_topic_tag_tag_status_time_id ON t_topic_tag (tag_id, status, last_comment_time, id)"},
			{&models.TopicTag{}, "idx_topic_tag_topic_status_id", "CREATE INDEX idx_topic_tag_topic_status_id ON t_topic_tag (topic_id, status, id)"},
			{&models.Comment{}, "idx_comment_entity_status_id", "CREATE INDEX idx_comment_entity_status_id ON t_comment (entity_type, entity_id, status, id)"},
			{&models.UserFeed{}, "idx_user_feed_user_type_time_id", "CREATE INDEX idx_user_feed_user_type_time_id ON t_user_feed (user_id, data_type, create_time, id)"},
			{&models.Message{}, "idx_message_user_status_id", "CREATE INDEX idx_message_user_status_id ON t_message (user_id, status, id)"},
		}

		for _, index := range indexes {
			if err := ensureCommunityIndex(tx, index); err != nil {
				return err
			}
		}
		return nil
	})
}

func ensureCommunityIndex(tx *gorm.DB, index communityIndexSpec) error {
	if tx.Migrator().HasIndex(index.model, index.name) {
		return nil
	}
	return tx.Exec(index.sql).Error
}
