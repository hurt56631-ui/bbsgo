package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

// migrate_search_delete_outbox creates the durable retry queue for external
// search-index cleanup. It is safe on existing databases and idempotent under
// the migration runner's retry behavior.
func migrate_search_delete_outbox() error {
	return sqls.DB().AutoMigrate(&models.SearchDeleteTask{})
}
