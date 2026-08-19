package migrations

import (
	"fmt"
	"strings"

	"bbs-go/internal/models"
	"bbs-go/internal/services"

	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

const summaryBackfillBatchSize = 200

type summaryUpdate struct {
	id      int64
	summary string
}

// migrate_content_summaries backfills rows created before topic.summary and
// automatic article summaries existed. It advances by primary key and commits
// in small batches to avoid a long-running table lock on established forums.
func migrate_content_summaries() error {
	if err := backfillTopicSummaries(sqls.DB()); err != nil {
		return err
	}
	return backfillArticleSummaries(sqls.DB())
}

func backfillTopicSummaries(db *gorm.DB) error {
	var cursor int64
	for {
		var rows []models.Topic
		if err := db.Model(&models.Topic{}).
			Select("id, type, content_type, content").
			Where("id > ? AND COALESCE(summary, '') = ''", cursor).
			Order("id ASC").Limit(summaryBackfillBatchSize).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}

		updates := make([]summaryUpdate, 0, len(rows))
		for _, row := range rows {
			updates = append(updates, summaryUpdate{
				id:      row.Id,
				summary: services.BuildTopicSummary(row.Type, row.ContentType, row.Content),
			})
		}
		if err := updateSummaryBatch(db, "t_topic", updates); err != nil {
			return err
		}
		cursor = rows[len(rows)-1].Id
	}
}

func backfillArticleSummaries(db *gorm.DB) error {
	var cursor int64
	for {
		var rows []models.Article
		if err := db.Model(&models.Article{}).
			Select("id, content_type, content").
			Where("id > ? AND COALESCE(summary, '') = ''", cursor).
			Order("id ASC").Limit(summaryBackfillBatchSize).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}

		updates := make([]summaryUpdate, 0, len(rows))
		for _, row := range rows {
			updates = append(updates, summaryUpdate{
				id:      row.Id,
				summary: services.BuildArticleSummary(row.ContentType, row.Content),
			})
		}
		if err := updateSummaryBatch(db, "t_article", updates); err != nil {
			return err
		}
		cursor = rows[len(rows)-1].Id
	}
}

// updateSummaryBatch reduces a 200-row backfill from 200 UPDATE round trips to
// one portable CASE expression. Both MySQL and SQLite support this syntax.
func updateSummaryBatch(db *gorm.DB, table string, updates []summaryUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	if table != "t_topic" && table != "t_article" {
		return fmt.Errorf("unsupported summary table: %s", table)
	}

	var query strings.Builder
	query.WriteString("UPDATE ")
	query.WriteString(table)
	query.WriteString(" SET summary = CASE id ")

	args := make([]any, 0, len(updates)*3)
	for _, update := range updates {
		query.WriteString("WHEN ? THEN ? ")
		args = append(args, update.id, update.summary)
	}
	query.WriteString("ELSE summary END WHERE id IN (")
	for i, update := range updates {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteByte('?')
		args = append(args, update.id)
	}
	query.WriteByte(')')
	return db.Exec(query.String(), args...).Error
}
