package services

import (
	"bbs-go/internal/models/constants"

	"github.com/mlogclub/simple/sqls"
)

// PurgeDeletedEntityMessages is a compensation path for asynchronous event
// handlers. A comment-created worker can pass its initial root check, then lose
// a race with permanent topic/article deletion and insert a notification after
// the delete transaction already cleaned messages. Re-running the root cleanup
// removes those late rows. Topic-delete notices are intentionally preserved.
func PurgeDeletedEntityMessages(entityType string, entityID int64) error {
	if entityID <= 0 {
		return nil
	}
	switch entityType {
	case constants.EntityTopic:
		return deleteTopicMessages(sqls.DB(), entityID, true)
	case constants.EntityArticle:
		return hardDeleteArticleMessages(sqls.DB(), entityID)
	default:
		return nil
	}
}
