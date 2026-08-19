package render

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/resp"
	"bbs-go/internal/pkg/markdown"
	"bbs-go/internal/services"
	"html"
	"strconv"

	"github.com/mlogclub/simple/web"
)

type commentBuildContext struct {
	liked   map[int64]struct{}
	replies map[int64][]models.Comment
	quotes  map[int64]models.Comment
	users   map[int64]models.User
}

func BuildComment(comment *models.Comment) *resp.CommentResponse {
	if comment == nil {
		return nil
	}
	items := BuildComments([]models.Comment{*comment}, nil, true, true)
	if len(items) == 0 {
		return nil
	}
	return &items[0]
}

func BuildComments(comments []models.Comment, currentUser *models.User, isBuildReplies, isBuildQuote bool) []resp.CommentResponse {
	if len(comments) == 0 {
		return nil
	}

	ctx := prepareCommentBuildContext(comments, currentUser, isBuildReplies, isBuildQuote)
	ret := make([]resp.CommentResponse, 0, len(comments))
	for i := range comments {
		item := doBuildComment(&comments[i], ctx, isBuildReplies, isBuildQuote)
		if item != nil {
			ret = append(ret, *item)
		}
	}
	return ret
}

func prepareCommentBuildContext(comments []models.Comment, currentUser *models.User, isBuildReplies, isBuildQuote bool) *commentBuildContext {
	ctx := &commentBuildContext{
		liked:   make(map[int64]struct{}),
		replies: make(map[int64][]models.Comment),
		quotes:  make(map[int64]models.Comment),
		users:   make(map[int64]models.User),
	}

	allVisibleComments := make([]models.Comment, 0, len(comments)*2)
	allVisibleComments = append(allVisibleComments, comments...)

	if isBuildReplies {
		parentIds := make([]int64, 0, len(comments))
		for _, comment := range comments {
			if comment.CommentCount > 0 {
				parentIds = append(parentIds, comment.Id)
			}
		}
		ctx.replies = services.CommentService.GetTopRepliesByCommentIds(parentIds, 3)
		for _, replies := range ctx.replies {
			allVisibleComments = append(allVisibleComments, replies...)
		}
	}

	quoteIds := make([]int64, 0)
	seenQuoteIds := make(map[int64]struct{})
	collectQuote := func(comment models.Comment) {
		if comment.QuoteId <= 0 {
			return
		}
		if _, exists := seenQuoteIds[comment.QuoteId]; exists {
			return
		}
		seenQuoteIds[comment.QuoteId] = struct{}{}
		quoteIds = append(quoteIds, comment.QuoteId)
	}
	if isBuildQuote {
		for _, comment := range comments {
			collectQuote(comment)
		}
	}
	// 一级评论内嵌的二级回复始终需要构建引用内容。
	for _, replies := range ctx.replies {
		for _, reply := range replies {
			collectQuote(reply)
		}
	}
	ctx.quotes = services.CommentService.GetByIds(quoteIds)

	userIds := make([]int64, 0, len(allVisibleComments)+len(ctx.quotes))
	seenUserIds := make(map[int64]struct{}, len(allVisibleComments)+len(ctx.quotes))
	collectUser := func(comment models.Comment) {
		if comment.UserId <= 0 {
			return
		}
		if _, exists := seenUserIds[comment.UserId]; exists {
			return
		}
		seenUserIds[comment.UserId] = struct{}{}
		userIds = append(userIds, comment.UserId)
	}
	for _, comment := range allVisibleComments {
		collectUser(comment)
	}
	for _, comment := range ctx.quotes {
		collectUser(comment)
	}
	ctx.users = services.UserService.GetMap(userIds)

	if currentUser != nil && len(allVisibleComments) > 0 {
		commentIds := make([]int64, 0, len(allVisibleComments))
		seenCommentIds := make(map[int64]struct{}, len(allVisibleComments))
		for _, comment := range allVisibleComments {
			if _, exists := seenCommentIds[comment.Id]; exists {
				continue
			}
			seenCommentIds[comment.Id] = struct{}{}
			commentIds = append(commentIds, comment.Id)
		}
		for _, id := range services.UserLikeService.IsLiked(currentUser.Id, constants.EntityComment, commentIds) {
			ctx.liked[id] = struct{}{}
		}
	}

	return ctx
}

// doBuildComment 渲染评论。
func doBuildComment(comment *models.Comment, ctx *commentBuildContext, isBuildReplies, isBuildQuote bool) *resp.CommentResponse {
	if comment == nil {
		return nil
	}

	var user *models.User
	if ctx != nil {
		if loaded, ok := ctx.users[comment.UserId]; ok {
			copy := loaded
			user = &copy
		}
	}

	ret := &resp.CommentResponse{
		Id:           comment.Id,
		User:         BuildUserInfoDefaultIfNullWithUser(comment.UserId, user),
		EntityType:   comment.EntityType,
		EntityId:     comment.EntityId,
		QuoteId:      comment.QuoteId,
		LikeCount:    comment.LikeCount,
		CommentCount: comment.CommentCount,
		ContentType:  comment.ContentType,
		IpLocation:   comment.IpLocation,
		Status:       comment.Status,
		CreateTime:   comment.CreateTime,
	}
	if ctx != nil {
		_, ret.Liked = ctx.liked[comment.Id]
	}

	if comment.Status == constants.StatusOk {
		if comment.ContentType == constants.ContentTypeMarkdown {
			content := markdown.ToHTML(comment.Content)
			ret.Content = handleHtmlContent(content)
		} else if comment.ContentType == constants.ContentTypeHtml {
			ret.Content = handleHtmlContent(comment.Content)
		} else {
			ret.Content = html.EscapeString(comment.Content)
		}
		ret.ImageList = BuildImageList(comment.ImageList)
	} else {
		ret.Content = "内容已删除"
	}

	if isBuildReplies && comment.CommentCount > 0 && ctx != nil {
		replies := ctx.replies[comment.Id]
		replyResults := make([]resp.CommentResponse, 0, len(replies))
		for i := range replies {
			if reply := doBuildComment(&replies[i], ctx, false, true); reply != nil {
				replyResults = append(replyResults, *reply)
			}
		}
		var nextCursor int64
		if len(replies) > 0 {
			nextCursor = replies[len(replies)-1].Id
		}
		ret.Replies = &web.CursorResult{
			Results: replyResults,
			Cursor:  strconv.FormatInt(nextCursor, 10),
			HasMore: comment.CommentCount > int64(len(replies)),
		}
	}

	if isBuildQuote && comment.QuoteId > 0 && ctx != nil {
		if quote, exists := ctx.quotes[comment.QuoteId]; exists {
			ret.Quote = doBuildComment(&quote, ctx, false, false)
		}
	}

	return ret
}
