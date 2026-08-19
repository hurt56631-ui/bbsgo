package render

import (
	"bbs-go/internal/cache"
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/resp"
	"bbs-go/internal/pkg/html"
	"bbs-go/internal/pkg/markdown"
	"bbs-go/internal/pkg/text"
	"bbs-go/internal/services"

	"github.com/mlogclub/simple/common/strs"
)

func BuildArticle(article *models.Article, currentUser *models.User) *resp.ArticleResponse {
	if article == nil {
		return nil
	}

	rsp := &resp.ArticleResponse{}
	rsp.Id = article.Id
	rsp.Title = article.Title
	rsp.Summary = article.Summary
	rsp.SourceUrl = article.SourceUrl
	rsp.ViewCount = article.ViewCount
	rsp.CommentCount = article.CommentCount
	rsp.LikeCount = article.LikeCount
	rsp.CreateTime = article.CreateTime
	rsp.Status = article.Status

	rsp.User = BuildUserInfoDefaultIfNull(article.UserId)

	tagIds := cache.ArticleTagCache.Get(article.Id)
	tags := cache.TagCache.GetList(tagIds)
	rsp.Tags = BuildTags(tags)

	if article.ContentType == constants.ContentTypeMarkdown {
		content := markdown.ToHTML(article.Content)
		rsp.Content, rsp.Toc = handleTopicHtmlContent(content)
	} else if article.ContentType == constants.ContentTypeHtml {
		rsp.Content, rsp.Toc = handleTopicHtmlContent(article.Content)
	}

	rsp.Cover = BuildImage(article.Cover)

	if currentUser != nil {
		rsp.Favorited = services.FavoriteService.IsFavorited(currentUser.Id, constants.EntityArticle, article.Id)
	}

	return rsp
}

func BuildSimpleArticle(article *models.Article) *resp.ArticleSimpleResponse {
	return buildSimpleArticleWithRelations(article, nil, false, nil, false)
}

func buildSimpleArticleWithRelations(article *models.Article, tags []models.Tag, tagsLoaded bool, user *models.User, userLoaded bool) *resp.ArticleSimpleResponse {
	if article == nil {
		return nil
	}

	rsp := &resp.ArticleSimpleResponse{}
	rsp.Id = article.Id
	rsp.Title = article.Title
	rsp.Summary = article.Summary
	rsp.SourceUrl = article.SourceUrl
	rsp.ViewCount = article.ViewCount
	rsp.CommentCount = article.CommentCount
	rsp.LikeCount = article.LikeCount
	rsp.CreateTime = article.CreateTime
	rsp.Status = article.Status

	if userLoaded {
		rsp.User = BuildUserInfoDefaultIfNullWithUser(article.UserId, user)
	} else {
		rsp.User = BuildUserInfoDefaultIfNull(article.UserId)
	}

	if !tagsLoaded {
		tagIds := cache.ArticleTagCache.Get(article.Id)
		tags = cache.TagCache.GetList(tagIds)
	}
	rsp.Tags = BuildTags(tags)

	// Compatibility fallback for rows created before the summary migration.
	// List queries no longer select content, so this branch is normally skipped.
	if strs.IsBlank(rsp.Summary) && strs.IsNotBlank(article.Content) {
		if article.ContentType == constants.ContentTypeMarkdown {
			rsp.Summary = markdown.GetSummary(article.Content, constants.SummaryLen)
		} else if article.ContentType == constants.ContentTypeHtml {
			rsp.Summary = text.GetSummary(html.GetHtmlText(article.Content), constants.SummaryLen)
		}
	}

	rsp.Cover = BuildImage(article.Cover)
	return rsp
}

func BuildSimpleArticles(articles []models.Article) []resp.ArticleSimpleResponse {
	if len(articles) == 0 {
		return nil
	}
	articleIds := make([]int64, 0, len(articles))
	userIds := make([]int64, 0, len(articles))
	for _, article := range articles {
		articleIds = append(articleIds, article.Id)
		userIds = append(userIds, article.UserId)
	}
	tagsByArticleId := services.ArticleService.GetArticleTagsMap(articleIds)
	usersById := services.UserService.GetMap(userIds)

	responses := make([]resp.ArticleSimpleResponse, 0, len(articles))
	for i := range articles {
		article := &articles[i]
		var user *models.User
		if loaded, ok := usersById[article.UserId]; ok {
			copy := loaded
			user = &copy
		}
		responses = append(responses, *buildSimpleArticleWithRelations(article, tagsByArticleId[article.Id], true, user, true))
	}
	return responses
}
