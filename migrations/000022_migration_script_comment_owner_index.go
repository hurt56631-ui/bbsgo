package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

// migrate_comment_owner_index keeps the Tieba-style "author only" filter on
// the same keyset-pagination path as the normal comment feed. Without the user
// column in the composite index, a very hot topic can make MySQL scan many
// unrelated floors before finding the author's next page.
func migrate_comment_owner_index() error {
	return ensureCommunityIndex(sqls.DB(), communityIndexSpec{
		model: &models.Comment{},
		name:  "idx_comment_entity_status_user_id",
		sql:   "CREATE INDEX idx_comment_entity_status_user_id ON t_comment (entity_type, entity_id, status, user_id, id)",
	})
}
