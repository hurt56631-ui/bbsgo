package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

func migrate_topic_read_progress() error {
	return sqls.DB().AutoMigrate(&models.TopicReadProgress{})
}
