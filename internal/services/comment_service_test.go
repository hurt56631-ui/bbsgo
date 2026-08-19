package services

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/req"
	"bbs-go/internal/permissions"
	"bbs-go/internal/pkg/config"
	"bbs-go/internal/repositories"
	"testing"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
)

func setupCommentServiceTestDB(t *testing.T) {
	t.Helper()
	config.Instance = &config.Config{Language: config.DefaultLanguage}
	db := setupTestDB(t)
	if err := db.AutoMigrate(
		&models.Comment{}, &models.Topic{}, &models.TopicTag{}, &models.Article{},
		&models.Role{}, &models.UserRole{}, &models.Permission{}, &models.RolePermission{},
		&models.UserScoreLog{},
	); err != nil {
		t.Fatalf("auto migrate comment: %v", err)
	}
	PermissionService.ClearCache()
}

func mustCreateComment(t *testing.T, comment *models.Comment) *models.Comment {
	t.Helper()
	if comment.Status == 0 {
		comment.Status = constants.StatusOk
	}
	if err := repositories.CommentRepository.Create(sqls.DB(), comment); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	return comment
}

func TestCommentService_DeleteByUserRejectsNonAuthorWithoutPermission(t *testing.T) {
	setupCommentServiceTestDB(t)
	comment := mustCreateComment(t, &models.Comment{
		UserId:      10,
		EntityType:  constants.EntityTopic,
		EntityId:    20,
		Content:     "hello",
		ContentType: constants.ContentTypeText,
	})

	regularUser := &models.User{Roles: ""}
	if err := CommentService.DeleteByUser(regularUser, comment.Id); err == nil {
		t.Fatalf("expected permission error for regular user")
	}

	got := CommentService.Get(comment.Id)
	if got == nil {
		t.Fatalf("expected comment to still exist")
	}
	if got.Status != constants.StatusOk {
		t.Fatalf("expected comment status ok, got %d", got.Status)
	}
}

func TestCommentService_DeleteByUserAllowsAuthor(t *testing.T) {
	setupCommentServiceTestDB(t)
	comment := mustCreateComment(t, &models.Comment{
		UserId:      10,
		EntityType:  constants.EntityTopic,
		EntityId:    20,
		Content:     "hello",
		ContentType: constants.ContentTypeText,
	})

	author := &models.User{Model: models.Model{Id: 10}}
	if err := CommentService.DeleteByUser(author, comment.Id); err != nil {
		t.Fatalf("delete by author: %v", err)
	}

	got := CommentService.Get(comment.Id)
	if got == nil {
		t.Fatalf("expected comment to still exist")
	}
	if got.Status != constants.StatusDeleted {
		t.Fatalf("expected comment status deleted, got %d", got.Status)
	}
}

func TestCommentService_DeleteByUserAllowsCommentDeletePermission(t *testing.T) {
	setupCommentServiceTestDB(t)
	comment := mustCreateComment(t, &models.Comment{
		UserId:      10,
		EntityType:  constants.EntityTopic,
		EntityId:    20,
		Content:     "hello",
		ContentType: constants.ContentTypeText,
	})
	now := dates.NowTimestamp()
	moderator := mustCreateUser(t, now)
	role := mustCreateRole(t, "comment-moderator", constants.StatusOk)
	permission := mustCreatePermission(t, permissions.PermissionCommentDelete.Code, constants.StatusOk)
	mustAssignRole(t, moderator, role)
	mustGrantPermission(t, role, permission)

	if err := CommentService.DeleteByUser(moderator, comment.Id); err != nil {
		t.Fatalf("delete with comment permission: %v", err)
	}

	got := CommentService.Get(comment.Id)
	if got == nil {
		t.Fatalf("expected comment to still exist")
	}
	if got.Status != constants.StatusDeleted {
		t.Fatalf("expected comment status deleted, got %d", got.Status)
	}
}

func TestCommentService_DeleteByUserAllowsOwner(t *testing.T) {
	setupCommentServiceTestDB(t)
	comment := mustCreateComment(t, &models.Comment{
		UserId:      10,
		EntityType:  constants.EntityComment,
		EntityId:    20,
		Content:     "reply",
		ContentType: constants.ContentTypeText,
	})

	ownerUser := &models.User{Roles: constants.RoleOwner}
	if err := CommentService.DeleteByUser(ownerUser, comment.Id); err != nil {
		t.Fatalf("delete by owner: %v", err)
	}

	got := CommentService.Get(comment.Id)
	if got == nil {
		t.Fatalf("expected comment to still exist")
	}
	if got.Status != constants.StatusDeleted {
		t.Fatalf("expected comment status deleted, got %d", got.Status)
	}
}

func TestCommentService_PublishRejectsMissingTarget(t *testing.T) {
	setupCommentServiceTestDB(t)
	user := mustCreateUser(t, dates.NowTimestamp())

	_, err := CommentService.Publish(user.Id, req.CreateCommentReq{
		EntityType: constants.EntityTopic,
		EntityId:   "999999",
		Content:    "orphan",
	})
	if err == nil {
		t.Fatalf("expected missing topic to be rejected")
	}
	if got := CommentService.Count(sqls.NewCnd()); got != 0 {
		t.Fatalf("expected no orphan comments, got %d", got)
	}
}

func TestCommentService_DeleteMaintainsCountersAndAcceptedState(t *testing.T) {
	setupCommentServiceTestDB(t)
	now := dates.NowTimestamp()
	author := mustCreateUser(t, now)
	commenter := mustCreateUser(t, now+1)
	topic := &models.Topic{
		Type:              constants.TopicTypeQA,
		QaStatus:          constants.QaStatusSolved,
		UserId:            author.Id,
		Title:             "question",
		Status:            constants.StatusOk,
		CommentCount:      1,
		LastCommentUserId: commenter.Id,
		LastCommentTime:   now + 2,
		CreateTime:        now,
	}
	if err := sqls.DB().Create(topic).Error; err != nil {
		t.Fatalf("create topic: %v", err)
	}
	comment := mustCreateComment(t, &models.Comment{
		UserId:      commenter.Id,
		EntityType:  constants.EntityTopic,
		EntityId:    topic.Id,
		Content:     "answer",
		ContentType: constants.ContentTypeText,
		CreateTime:  now + 2,
	})
	if err := sqls.DB().Model(&models.Topic{}).Where("id = ?", topic.Id).
		UpdateColumn("accepted_comment_id", comment.Id).Error; err != nil {
		t.Fatalf("set accepted comment: %v", err)
	}
	if err := sqls.DB().Model(&models.User{}).Where("id = ?", commenter.Id).
		UpdateColumn("comment_count", 1).Error; err != nil {
		t.Fatalf("set user comment count: %v", err)
	}

	if err := CommentService.Delete(comment.Id); err != nil {
		t.Fatalf("delete comment: %v", err)
	}

	var gotTopic models.Topic
	if err := sqls.DB().First(&gotTopic, topic.Id).Error; err != nil {
		t.Fatalf("load topic: %v", err)
	}
	if gotTopic.CommentCount != 0 || gotTopic.LastCommentTime != 0 || gotTopic.LastCommentUserId != 0 {
		t.Fatalf("topic counters not repaired: %+v", gotTopic)
	}
	if gotTopic.AcceptedCommentId != 0 || gotTopic.QaStatus != constants.QaStatusUnsolved {
		t.Fatalf("accepted state not repaired: accepted=%d status=%s", gotTopic.AcceptedCommentId, gotTopic.QaStatus)
	}
	var gotUser models.User
	if err := sqls.DB().First(&gotUser, commenter.Id).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if gotUser.CommentCount != 0 {
		t.Fatalf("expected user comment count 0, got %d", gotUser.CommentCount)
	}
}

func TestCommentService_DeleteTopLevelCascadesVisibleReplies(t *testing.T) {
	setupCommentServiceTestDB(t)
	now := dates.NowTimestamp()
	author := mustCreateUser(t, now)
	replier := mustCreateUser(t, now+1)
	topic := &models.Topic{
		Type:         constants.TopicTypeTopic,
		UserId:       author.Id,
		Title:        "thread",
		Status:       constants.StatusOk,
		CommentCount: 1,
		CreateTime:   now,
	}
	if err := sqls.DB().Create(topic).Error; err != nil {
		t.Fatalf("create topic: %v", err)
	}
	parent := mustCreateComment(t, &models.Comment{
		UserId:       author.Id,
		EntityType:   constants.EntityTopic,
		EntityId:     topic.Id,
		Content:      "parent",
		ContentType:  constants.ContentTypeText,
		CommentCount: 1,
		HotScore:     2,
		CreateTime:   now + 2,
	})
	reply := mustCreateComment(t, &models.Comment{
		UserId:      replier.Id,
		EntityType:  constants.EntityComment,
		EntityId:    parent.Id,
		Content:     "reply",
		ContentType: constants.ContentTypeText,
		CreateTime:  now + 3,
	})
	if err := sqls.DB().Model(&models.User{}).Where("id IN ?", []int64{author.Id, replier.Id}).
		UpdateColumn("comment_count", 1).Error; err != nil {
		t.Fatalf("seed user comment counts: %v", err)
	}

	if err := CommentService.Delete(parent.Id); err != nil {
		t.Fatalf("delete parent: %v", err)
	}

	gotParent := CommentService.Get(parent.Id)
	gotReply := CommentService.Get(reply.Id)
	if gotParent == nil || gotReply == nil {
		t.Fatalf("comments missing after soft delete")
	}
	if gotParent.Status != constants.StatusDeleted || gotReply.Status != constants.StatusDeleted {
		t.Fatalf("expected parent and reply deleted, parent=%d reply=%d", gotParent.Status, gotReply.Status)
	}
	var gotAuthor, gotReplier models.User
	if err := sqls.DB().First(&gotAuthor, author.Id).Error; err != nil {
		t.Fatalf("load author: %v", err)
	}
	if err := sqls.DB().First(&gotReplier, replier.Id).Error; err != nil {
		t.Fatalf("load replier: %v", err)
	}
	if gotAuthor.CommentCount != 0 || gotReplier.CommentCount != 0 {
		t.Fatalf("user counters not decremented: author=%d replier=%d", gotAuthor.CommentCount, gotReplier.CommentCount)
	}
}

func TestCommentService_DeleteNestedReplyMaintainsParentHotScore(t *testing.T) {
	setupCommentServiceTestDB(t)
	parent := mustCreateComment(t, &models.Comment{
		UserId:       1,
		EntityType:   constants.EntityTopic,
		EntityId:     10,
		Content:      "parent",
		ContentType:  constants.ContentTypeText,
		CommentCount: 1,
		HotScore:     2,
	})
	child := mustCreateComment(t, &models.Comment{
		UserId:      2,
		EntityType:  constants.EntityComment,
		EntityId:    parent.Id,
		Content:     "child",
		ContentType: constants.ContentTypeText,
	})

	if err := CommentService.Delete(child.Id); err != nil {
		t.Fatalf("delete child: %v", err)
	}
	got := CommentService.Get(parent.Id)
	if got == nil {
		t.Fatalf("parent missing")
	}
	if got.CommentCount != 0 || got.HotScore != 0 {
		t.Fatalf("parent counters not repaired: replies=%d hot=%d", got.CommentCount, got.HotScore)
	}
}

func TestCommentService_HotCommentsUseStableKeyset(t *testing.T) {
	setupCommentServiceTestDB(t)
	topic := &models.Topic{
		Type:       constants.TopicTypeTopic,
		UserId:     1,
		Title:      "hot",
		Status:     constants.StatusOk,
		CreateTime: dates.NowTimestamp(),
	}
	if err := sqls.DB().Create(topic).Error; err != nil {
		t.Fatalf("create topic: %v", err)
	}
	for i := 0; i < 25; i++ {
		mustCreateComment(t, &models.Comment{
			UserId:      int64(i + 1),
			EntityType:  constants.EntityTopic,
			EntityId:    topic.Id,
			Content:     "hot",
			ContentType: constants.ContentTypeText,
			HotScore:    int64(25 - i),
			CreateTime:  int64(i + 1),
		})
	}

	first, cursor, more := CommentService.GetCommentsSorted(constants.EntityTopic, topic.Id, 0, "hot")
	if len(first) != 20 || !more || cursor <= 0 {
		t.Fatalf("unexpected first page len=%d more=%v cursor=%d", len(first), more, cursor)
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].HotScore < first[i].HotScore {
			t.Fatalf("hot order is not descending at %d", i)
		}
	}
	seen := map[int64]struct{}{}
	for _, item := range first {
		seen[item.Id] = struct{}{}
	}
	anchor := first[len(first)-1]
	encodedScore, encodedID, ok := decodeHotCommentCursor(cursor)
	if !ok || encodedScore != anchor.HotScore || encodedID != anchor.Id {
		t.Fatalf("invalid hot cursor score=%d id=%d ok=%v", encodedScore, encodedID, ok)
	}
	// The boundary row can be deleted or have its score changed while the client
	// is between pages. The cursor must retain the original ranking boundary.
	if err := sqls.DB().Model(&models.Comment{}).Where("id = ?", anchor.Id).Updates(map[string]interface{}{
		"status":    constants.StatusDeleted,
		"hot_score": anchor.HotScore + 1000,
	}).Error; err != nil {
		t.Fatalf("change cursor anchor: %v", err)
	}
	second, _, more := CommentService.GetCommentsSorted(constants.EntityTopic, topic.Id, cursor, "hot")
	if len(second) != 5 || more {
		t.Fatalf("unexpected second page len=%d more=%v", len(second), more)
	}
	for _, item := range second {
		if _, ok := seen[item.Id]; ok {
			t.Fatalf("duplicate comment across pages: %d", item.Id)
		}
	}
}
