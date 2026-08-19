package migrations

import (
	"bbs-go/internal/models"

	"github.com/mlogclub/simple/sqls"
)

func migrate_article_and_token_indexes() error {
	return sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		tx := ctx.Tx

		// Old versions did not enforce one tag relation per article. Keep the
		// earliest row so the unique index can be created on existing databases.
		if err := tx.Exec(`
DELETE FROM t_article_tag
WHERE id NOT IN (
    SELECT keep_id FROM (
        SELECT MIN(id) AS keep_id
        FROM t_article_tag
        GROUP BY article_id, tag_id
    ) AS article_tag_keep_rows
)`).Error; err != nil {
			return err
		}

		indexes := []communityIndexSpec{
			{&models.Article{}, "idx_article_status_id", "CREATE INDEX idx_article_status_id ON t_article (status, id)"},
			{&models.Article{}, "idx_article_user_status_id", "CREATE INDEX idx_article_user_status_id ON t_article (user_id, status, id)"},
			{&models.ArticleTag{}, "uk_article_tag_article_tag", "CREATE UNIQUE INDEX uk_article_tag_article_tag ON t_article_tag (article_id, tag_id)"},
			{&models.ArticleTag{}, "idx_article_tag_article_status_id", "CREATE INDEX idx_article_tag_article_status_id ON t_article_tag (article_id, status, id)"},
			{&models.ArticleTag{}, "idx_article_tag_tag_status_id", "CREATE INDEX idx_article_tag_tag_status_id ON t_article_tag (tag_id, status, id)"},
			{&models.UserToken{}, "idx_user_token_expired_status_id", "CREATE INDEX idx_user_token_expired_status_id ON t_user_token (expired_at, status, id)"},
		}
		for _, index := range indexes {
			if err := ensureCommunityIndex(tx, index); err != nil {
				return err
			}
		}
		return nil
	})
}
