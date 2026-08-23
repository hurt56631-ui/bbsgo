package api

import (
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/req"
	"bbs-go/internal/pkg/common"
	"bbs-go/internal/pkg/idcodec"
	"bbs-go/internal/spam"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"bbs-go/internal/pkg/ginx"
	"bbs-go/internal/pkg/params"

	"bbs-go/internal/handlers/render"
	"bbs-go/internal/services"
	"github.com/mlogclub/simple/web"
)

func CommentComments(ctx *gin.Context) {
	var (
		cursor, _     = params.GetInt64(ctx, "cursor")
		entityType, _ = params.Get(ctx, "entityType")
		entityId      = common.GetID(ctx, "entityId")
		currentUser   = common.GetCurrentUser(ctx)
		sortMode      = strings.ToLower(strings.TrimSpace(ctx.Query("sort")))
	)
	onlyOwner := ctx.Query("onlyOwner") == "1" || strings.EqualFold(strings.TrimSpace(ctx.Query("onlyOwner")), "true")
	ownerUserID := int64(0)
	if onlyOwner {
		switch entityType {
		case constants.EntityTopic:
			if topic := services.TopicService.Get(entityId); topic != nil {
				ownerUserID = topic.UserId
			}
		case constants.EntityArticle:
			if article := services.ArticleService.Get(entityId); article != nil {
				ownerUserID = article.UserId
			}
		}
	}
	if onlyOwner && ownerUserID <= 0 {
		ginx.WriteJSON(ctx, ginx.CursorData([]any{}, strconv.FormatInt(cursor, 10), false))
		return
	}

	comments, cursor, hasMore := services.CommentService.GetCommentsSortedByUser(
		entityType, entityId, cursor, sortMode, ownerUserID,
	)
	ginx.WriteJSON(ctx, ginx.CursorData(render.BuildComments(comments, currentUser, !onlyOwner, false), strconv.FormatInt(cursor, 10), hasMore))

}

func CommentReplies(ctx *gin.Context) {
	var (
		cursor, _    = params.GetInt64(ctx, "cursor")
		commentId, _ = params.GetInt64(ctx, "commentId")
	)
	currentUser := common.GetCurrentUser(ctx)
	comments, cursor, hasMore := services.CommentService.GetReplies(commentId, cursor, 10)
	ginx.WriteJSON(ctx, ginx.CursorData(render.BuildComments(comments, currentUser, false, true), strconv.FormatInt(cursor, 10), hasMore))

}

func CommentCreate(ctx *gin.Context) {
	user := common.GetCurrentUser(ctx)
	if err := services.UserService.CheckPostStatus(user); err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}
	var body req.CreateCommentReq
	if err := ginx.Bind(ctx, &body); err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}
	body.UserAgent = web.GetUserAgent(ctx.Request)
	body.Ip = web.GetRequestIP(ctx.Request)
	if err := spam.CheckComment(user, body); err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}

	comment, err := services.CommentService.Publish(user.Id, body)
	if err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}

	ginx.WriteJSON(ctx, render.BuildComment(comment))

}

func CommentWatch(ctx *gin.Context) {
	var body struct {
		TopicID string `json:"topicId" form:"topicId"`
		WatchID string `json:"watchId" form:"watchId"`
		Active  bool   `json:"active" form:"active"`
	}
	if err := ginx.Bind(ctx, &body); err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}
	topicID := idcodec.Decode(body.TopicID)
	if topicID <= 0 {
		ginx.WriteJSON(ctx, false)
		return
	}
	if body.Active {
		topic := services.TopicService.Get(topicID)
		if topic == nil || topic.Status == constants.StatusDeleted {
			// A stale/background Activity may resume after the topic was
			// permanently removed. Return false so Android closes the stale
			// detail page instead of believing a new realtime lease exists.
			ginx.WriteJSON(ctx, false)
			return
		}
		if topic.Status != constants.StatusOk {
			// Review/hidden states are not equivalent to physical deletion. Do
			// not create a realtime lease, but keep the detail page intact.
			ginx.WriteJSON(ctx, true)
			return
		}
	}
	user := common.GetCurrentUser(ctx)
	if user == nil || user.AuthSource != services.TalkamiAuthSource || !user.ExternalUID.Valid {
		// Anonymous/web-only viewers have no WuKongIM uid. Keep the endpoint a no-op
		// so normal forum reading still works without a realtime IM session.
		ginx.WriteJSON(ctx, true)
		return
	}
	uid := strings.TrimSpace(user.ExternalUID.String)
	if uid != "" {
		services.ForumRealtimeService.WatchTopic(topicID, uid, body.WatchID, body.Active)
	}
	ginx.WriteJSON(ctx, true)
}

func CommentRemove(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}
	user := common.GetCurrentUser(ctx)
	if err := services.CommentService.DeleteByUser(user, id); err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}
	ginx.WriteJSON(ctx, nil)

}
