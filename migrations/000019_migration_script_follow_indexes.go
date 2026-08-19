package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

// migrate_follow_indexes supports both follower fan-out scans and normal
// follow/fan list pagination. The old unique (user_id, other_id) index cannot
// efficiently serve WHERE other_id = ? ORDER BY id or user_id + id cursors.
func migrate_follow_indexes() error {
	return sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		indexes := []communityIndexSpec{
			{
				&models.UserFollow{},
				"idx_user_follow_other_id_id_user_id",
				"CREATE INDEX idx_user_follow_other_id_id_user_id ON t_user_follow (other_id, id, user_id)",
			},
			{
				&models.UserFollow{},
				"idx_user_follow_user_id_id_other_id",
				"CREATE INDEX idx_user_follow_user_id_id_other_id ON t_user_follow (user_id, id, other_id)",
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
