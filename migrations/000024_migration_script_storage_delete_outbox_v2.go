package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

// migrate_storage_delete_outbox_v2 upgrades early builds of migration 23 that
// used a 1024-character object key and a composite due index. 700 characters
// keeps the utf8mb4 unique key below the InnoDB 3072-byte index ceiling, while
// a next_retry_time-only index matches the retry worker's actual query.
func migrate_storage_delete_outbox_v2() error {
	db := sqls.DB()
	if db.Migrator().HasIndex(&models.StorageDeleteTask{}, "idx_storage_delete_due") {
		if err := db.Migrator().DropIndex(&models.StorageDeleteTask{}, "idx_storage_delete_due"); err != nil {
			return err
		}
	}
	return db.AutoMigrate(&models.StorageDeleteTask{})
}
