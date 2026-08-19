package services

import (
	"strings"

	"bbs-go/internal/models/constants"
	htmlutil "bbs-go/internal/pkg/html"
	"bbs-go/internal/pkg/markdown"
	"bbs-go/internal/pkg/text"
)

// BuildTopicSummary creates the stable text used by topic list APIs. It is
// intentionally generated on writes so list requests never need to load and
// parse the longtext content column.
func BuildTopicSummary(topicType constants.TopicType, contentType constants.ContentType, content string) string {
	content = strings.TrimSpace(content)
	if constants.IsTweetTopicType(topicType) {
		if content == "" {
			return "分享图片"
		}
		return text.GetSummary(content, constants.SummaryLen)
	}
	return BuildContentSummary(contentType, content)
}

// BuildArticleSummary creates a compact plain-text article summary.
func BuildArticleSummary(contentType constants.ContentType, content string) string {
	return BuildContentSummary(contentType, content)
}

// BuildContentSummary normalizes markdown/html/text into a bounded plain-text
// summary. Keeping this in one place ensures publish, edit and migrations
// produce identical output.
func BuildContentSummary(contentType constants.ContentType, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	switch contentType {
	case constants.ContentTypeMarkdown:
		return markdown.GetSummary(content, constants.SummaryLen)
	case constants.ContentTypeHtml:
		return text.GetSummary(htmlutil.GetHtmlText(content), constants.SummaryLen)
	default:
		return text.GetSummary(content, constants.SummaryLen)
	}
}
