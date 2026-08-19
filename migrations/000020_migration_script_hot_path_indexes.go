package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

// migrate_hot_path_indexes adds the remaining indexes used by the two most
// common topic feeds. They are intentionally separate from the reply-time
// indexes because MySQL cannot efficiently satisfy ORDER BY id from an index
// whose second column is last_comment_time.
func migrate_hot_path_indexes() error {
	return sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		indexes := []communityIndexSpec{
			{
				&models.Topic{},
				"idx_topic_status_id",
				"CREATE INDEX idx_topic_status_id ON t_topic (status, id)",
			},
			{
				&models.Topic{},
				"idx_topic_type_qa_status_state_time_id",
				"CREATE INDEX idx_topic_type_qa_status_state_time_id ON t_topic (type, qa_status, status, last_comment_time, id)",
			},
			{
				&models.Topic{},
				"idx_topic_category_type_qa_state_time_id",
				"CREATE INDEX idx_topic_category_type_qa_state_time_id ON t_topic (category_id, type, qa_status, status, last_comment_time, id)",
			},
		}
		for _, index := range indexes {
			if err := ensureCommunityIndex(ctx.Tx, index); err != nil {
				return err
			}
		}
		return nil
	})
}
