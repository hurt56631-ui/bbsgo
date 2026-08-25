package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

func migrate_topic_read_progress_v2() error {
	// Existing installations already have t_topic_read_progress from v27.
	// AutoMigrate adds only the new resume-anchor/high-precision scroll fields.
	return sqls.DB().AutoMigrate(&models.TopicReadProgress{})
}
