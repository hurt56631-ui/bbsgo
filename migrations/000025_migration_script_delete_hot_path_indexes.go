package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

// migrate_delete_hot_path_indexes adds indexes used specifically by physical
// cleanup. They are safe for existing installations and prevent a large topic
// deletion from scanning every attachment purchase log, vote/report row, or
// every reply in a very large floor when clearing dangling quote references.
func migrate_delete_hot_path_indexes() error {
	db := sqls.DB()
	if err := ensureCommunityIndex(db, communityIndexSpec{
		model: &models.AttachmentDownloadLog{},
		name:  "idx_attachment_download_log_attachment_id",
		sql:   "CREATE INDEX idx_attachment_download_log_attachment_id ON t_attachment_download_log (attachment_id)",
	}); err != nil {
		return err
	}
	if err := ensureCommunityIndex(db, communityIndexSpec{
		model: &models.Vote{},
		name:  "idx_vote_topic_id",
		sql:   "CREATE INDEX idx_vote_topic_id ON t_vote (topic_id)",
	}); err != nil {
		return err
	}
	if err := ensureCommunityIndex(db, communityIndexSpec{
		model: &models.UserReport{},
		name:  "idx_user_report_data",
		sql:   "CREATE INDEX idx_user_report_data ON t_user_report (data_type(32), data_id)",
	}); err != nil {
		return err
	}
	return ensureCommunityIndex(db, communityIndexSpec{
		model: &models.Comment{},
		name:  "idx_comment_parent_quote_status",
		sql:   "CREATE INDEX idx_comment_parent_quote_status ON t_comment (entity_type, entity_id, quote_id, status)",
	})
}
