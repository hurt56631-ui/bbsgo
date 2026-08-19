package render

import (
	"html"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/resp"
	"bbs-go/internal/pkg/common"
	"bbs-go/internal/pkg/idcodec"
	"bbs-go/internal/pkg/markdown"
	"bbs-go/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/mlogclub/simple/common/strs"
)

func BuildTopic(ctx *gin.Context, topic *models.Topic) *resp.TopicResponse {
	rsp := _buildTopic(topic, true)
	if rsp == nil {
		return nil
	}

	if currentUser := common.GetCurrentUser(ctx); currentUser != nil {
		rsp.Liked = services.UserLikeService.Exists(currentUser.Id, constants.EntityTopic, topic.Id)
		rsp.Favorited = services.FavoriteService.IsFavorited(currentUser.Id, constants.EntityTopic, topic.Id)
	}

	if vote := services.VoteService.Get(topic.VoteId); vote != nil {
		rsp.Vote = BuildVote(ctx, vote)
	}

	// 附件仅在帖子详情接口返回。
	list := services.AttachmentService.ListByTopicId(topic.Id)
	if len(list) > 0 {
		var currentUser *models.User
		if u := common.GetCurrentUser(ctx); u != nil {
			currentUser = u
		}
		rsp.Attachments = BuildAttachmentResponses(list, currentUser)
	}

	return rsp
}

// BuildAttachmentResponses 将附件列表转为 AttachmentResponse 列表；currentUser 为 nil 时 downloaded 均为 false（如编辑表单）
func BuildAttachmentResponses(list []models.Attachment, currentUser *models.User) []resp.AttachmentResponse {
	if len(list) == 0 {
		return nil
	}
	atts := make([]resp.AttachmentResponse, 0, len(list))
	downloadedMap := make(map[string]bool)
	if currentUser != nil {
		attachmentIds := make([]string, 0, len(list))
		for _, att := range list {
			attachmentIds = append(attachmentIds, att.Id)
		}
		for _, attachmentId := range services.AttachmentService.FindDownloadedAttachmentIds(currentUser.Id, attachmentIds) {
			downloadedMap[attachmentId] = true
		}
	}

	for _, att := range list {
		atts = append(atts, resp.AttachmentResponse{
			Id:            att.Id,
			FileName:      att.FileName,
			FileSize:      att.FileSize,
			DownloadScore: att.DownloadScore,
			DownloadCount: att.DownloadCount,
			Downloaded:    downloadedMap[att.Id],
		})
	}
	return atts
}

func BuildSimpleTopic(topic *models.Topic) *resp.TopicResponse {
	buildContent := constants.IsTweetTopicType(topic.Type)
	return _buildTopic(topic, buildContent)
}

func BuildSimpleTopics(ctx *gin.Context, topics []models.Topic) []resp.TopicResponse {
	if len(topics) == 0 {
		return nil
	}

	topicIds := make([]int64, 0, len(topics))
	userIds := make([]int64, 0, len(topics))
	categoryIds := make([]int64, 0, len(topics))
	voteIds := make([]int64, 0, len(topics))
	for _, topic := range topics {
		topicIds = append(topicIds, topic.Id)
		userIds = append(userIds, topic.UserId)
		if topic.CategoryId > 0 {
			categoryIds = append(categoryIds, topic.CategoryId)
		}
		if topic.VoteId > 0 {
			voteIds = append(voteIds, topic.VoteId)
		}
	}

	liked := make(map[int64]bool)
	if currentUser := common.GetCurrentUser(ctx); currentUser != nil {
		for _, topicId := range services.UserLikeService.IsLiked(currentUser.Id, constants.EntityTopic, topicIds) {
			liked[topicId] = true
		}
	}

	tagsByTopicId := services.TopicService.GetTopicTagsMap(topicIds)
	usersById := services.UserService.GetMap(userIds)
	categoriesById := services.CategoryService.GetMapWithParents(categoryIds)
	votesById := BuildVotes(ctx, voteIds)

	responses := make([]resp.TopicResponse, 0, len(topics))
	for i := range topics {
		topic := &topics[i]

		var user *models.User
		if loaded, ok := usersById[topic.UserId]; ok {
			copy := loaded
			user = &copy
		}

		var category *models.Category
		if loaded, ok := categoriesById[topic.CategoryId]; ok {
			copy := loaded
			category = &copy
		}

		item := _buildTopicWithRelations(
			topic,
			constants.IsTweetTopicType(topic.Type),
			tagsByTopicId[topic.Id],
			true,
			user,
			true,
			category,
			true,
			categoriesById,
		)
		item.Liked = liked[topic.Id]
		item.Vote = votesById[topic.VoteId]
		responses = append(responses, *item)
	}
	return responses
}

func _buildTopic(topic *models.Topic, buildContent bool) *resp.TopicResponse {
	return _buildTopicWithRelations(topic, buildContent, nil, false, nil, false, nil, false, nil)
}

func _buildTopicWithRelations(
	topic *models.Topic,
	buildContent bool,
	tags []models.Tag,
	tagsLoaded bool,
	user *models.User,
	userLoaded bool,
	category *models.Category,
	categoryLoaded bool,
	categories map[int64]models.Category,
) *resp.TopicResponse {
	if topic == nil {
		return nil
	}

	rsp := &resp.TopicResponse{
		Id:                idcodec.Encode(topic.Id),
		Type:              topic.Type,
		QaStatus:          topic.QaStatus,
		AcceptedCommentId: topic.AcceptedCommentId,
		SolvedAt:          topic.SolvedAt,
		BountyScore:       topic.BountyScore,
		Title:             topic.Title,
		LastCommentTime:   topic.LastCommentTime,
		CreateTime:        topic.CreateTime,
		ViewCount:         topic.ViewCount,
		CommentCount:      topic.CommentCount,
		LikeCount:         topic.LikeCount,
		Recommend:         topic.Recommend,
		RecommendTime:     topic.RecommendTime,
		Sticky:            topic.Sticky,
		StickyTime:        topic.StickyTime,
		Status:            topic.Status,
		IpLocation:        topic.IpLocation,
	}

	if userLoaded {
		rsp.User = BuildUserInfoDefaultIfNullWithUser(topic.UserId, user)
	} else {
		rsp.User = BuildUserInfoDefaultIfNull(topic.UserId)
	}

	if buildContent {
		if constants.IsTweetTopicType(topic.Type) {
			content := topic.Content
			if strs.IsBlank(content) {
				content = topic.Summary
			}
			if strs.IsBlank(content) {
				rsp.Content = "分享图片"
			} else {
				rsp.Content = html.EscapeString(content)
			}
		} else {
			contentHTML := topic.Content
			if topic.ContentType == constants.ContentTypeMarkdown {
				contentHTML = markdown.ToHTML(topic.Content)
			}
			rsp.Content, rsp.Toc = handleTopicHtmlContent(contentHTML)
		}
	} else {
		rsp.Summary = topic.Summary
		if strs.IsBlank(rsp.Summary) && strs.IsNotBlank(topic.Content) {
			rsp.Summary = services.BuildTopicSummary(topic.Type, topic.ContentType, topic.Content)
		}
	}

	if constants.IsTweetTopicType(topic.Type) {
		if strs.IsBlank(rsp.Summary) {
			rsp.Summary = topic.Summary
		}
	}

	// Images may be attached to normal posts and Q&A posts as well as tweets.
	// The old tweet-only condition stored those images correctly but omitted
	// imageList from the detail response, so native clients had nothing to render.
	rsp.ImageList = BuildImageList(topic.ImageList)

	if topic.CategoryId > 0 {
		if categoryLoaded {
			rsp.Category = BuildCategoryFromMap(category, categories)
		} else {
			rsp.Category = BuildCategory(services.CategoryService.Get(topic.CategoryId))
		}
	}

	if !tagsLoaded {
		tags = services.TopicService.GetTopicTags(topic.Id)
	}
	rsp.Tags = BuildTags(tags)
	return rsp
}
