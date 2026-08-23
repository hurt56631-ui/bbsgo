package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

// migrate_storage_delete_outbox creates the durable retry queue used when a
// deleted topic/comment owns files in local disk or object storage.
func migrate_storage_delete_outbox() error {
	return sqls.DB().AutoMigrate(&models.StorageDeleteTask{})
}
