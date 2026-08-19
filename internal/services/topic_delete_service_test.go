package services

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/config"
	"strconv"
	"testing"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
)

func setupTopicDeleteServiceTestDB(t *testing.T) {
	t.Helper()
	config.Instance = &config.Config{Language: config.DefaultLanguage}
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Topic{}, &models.TopicTag{}, &models.Attachment{}, &models.Comment{}); err != nil {
		t.Fatalf("auto migrate topic delete: %v", err)
	}
}

func createBountyTopicFixture(t *testing.T, score int) (*models.User, *models.Topic, *models.TopicTag, *models.Attachment) {
	t.Helper()
	now := dates.NowTimestamp()
	user := mustCreateUser(t, now)
	if err := sqls.DB().Model(&models.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"score": score, "topic_count": 1,
	}).Error; err != nil {
		t.Fatalf("prepare user: %v", err)
	}
	topic := &models.Topic{
		Type:        constants.TopicTypeQA,
		QaStatus:    constants.QaStatusUnsolved,
		BountyScore: 20,
		UserId:      user.Id,
		Title:       "bounty",
		Status:      constants.StatusOk,
		CreateTime:  now,
	}
	if err := sqls.DB().Create(topic).Error; err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := sqls.DB().Create(&models.UserScoreLog{
		UserId: user.Id, SourceType: constants.SourceTypeQaBounty, SourceId: strconv.FormatInt(topic.Id, 10),
		Description: "escrow", Type: constants.ScoreTypeDecr, Score: -topic.BountyScore, CreateTime: now,
	}).Error; err != nil {
		t.Fatalf("create escrow log: %v", err)
	}
	tag := &models.TopicTag{TopicId: topic.Id, TagId: 1, Status: int64(constants.StatusOk), CreateTime: now}
	if err := sqls.DB().Create(tag).Error; err != nil {
		t.Fatalf("create topic tag: %v", err)
	}
	attachment := &models.Attachment{
		Id: "attachment-1", TopicId: topic.Id, UserId: user.Id, FileName: "a.txt",
		Status: constants.StatusOk, CreateTime: now, UpdateTime: now,
	}
	if err := sqls.DB().Create(attachment).Error; err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	return user, topic, tag, attachment
}

func TestTopicService_DeleteAndUndeleteAreIdempotent(t *testing.T) {
	setupTopicDeleteServiceTestDB(t)
	user, topic, tag, attachment := createBountyTopicFixture(t, 80)

	if err := TopicService.Delete(topic.Id, user.Id, nil); err != nil {
		t.Fatalf("delete topic: %v", err)
	}
	if err := TopicService.Delete(topic.Id, user.Id, nil); err != nil {
		t.Fatalf("duplicate delete: %v", err)
	}

	var gotUser models.User
	_ = sqls.DB().First(&gotUser, user.Id).Error
	if gotUser.Score != 100 || gotUser.TopicCount != 0 {
		t.Fatalf("unexpected delete balances score=%d topics=%d", gotUser.Score, gotUser.TopicCount)
	}
	var refunds int64
	if err := sqls.DB().Model(&models.UserScoreLog{}).
		Where("source_type = ? AND source_id = ? AND type = ?", constants.SourceTypeQaBountyRefund, strconv.FormatInt(topic.Id, 10), constants.ScoreTypeIncr).
		Count(&refunds).Error; err != nil {
		t.Fatalf("count refunds: %v", err)
	}
	if refunds != 1 {
		t.Fatalf("expected one refund, got %d", refunds)
	}
	var gotTopic models.Topic
	var gotTag models.TopicTag
	var gotAttachment models.Attachment
	_ = sqls.DB().First(&gotTopic, topic.Id).Error
	_ = sqls.DB().First(&gotTag, tag.Id).Error
	_ = sqls.DB().First(&gotAttachment, "id = ?", attachment.Id).Error
	if gotTopic.Status != constants.StatusDeleted || gotTag.Status != int64(constants.StatusDeleted) || gotAttachment.Status != constants.StatusDeleted {
		t.Fatalf("delete state incomplete topic=%d tag=%d attachment=%d", gotTopic.Status, gotTag.Status, gotAttachment.Status)
	}

	if err := TopicService.Undelete(topic.Id); err != nil {
		t.Fatalf("undelete topic: %v", err)
	}
	if err := TopicService.Undelete(topic.Id); err != nil {
		t.Fatalf("duplicate undelete: %v", err)
	}
	_ = sqls.DB().First(&gotUser, user.Id).Error
	_ = sqls.DB().First(&gotTopic, topic.Id).Error
	_ = sqls.DB().First(&gotTag, tag.Id).Error
	_ = sqls.DB().First(&gotAttachment, "id = ?", attachment.Id).Error
	if gotUser.Score != 80 || gotUser.TopicCount != 1 {
		t.Fatalf("unexpected restore balances score=%d topics=%d", gotUser.Score, gotUser.TopicCount)
	}
	if gotTopic.Status != constants.StatusOk || gotTag.Status != int64(constants.StatusOk) || gotAttachment.Status != constants.StatusOk {
		t.Fatalf("restore state incomplete topic=%d tag=%d attachment=%d", gotTopic.Status, gotTag.Status, gotAttachment.Status)
	}
}

func TestTopicService_UndeleteRollsBackWhenBountyCannotBeReEscrowed(t *testing.T) {
	setupTopicDeleteServiceTestDB(t)
	user, topic, _, _ := createBountyTopicFixture(t, 0)
	if err := TopicService.Delete(topic.Id, user.Id, nil); err != nil {
		t.Fatalf("delete topic: %v", err)
	}
	if err := sqls.DB().Model(&models.User{}).Where("id = ?", user.Id).UpdateColumn("score", 0).Error; err != nil {
		t.Fatalf("spend refunded score: %v", err)
	}

	if err := TopicService.Undelete(topic.Id); err == nil {
		t.Fatalf("expected insufficient score error")
	}
	var gotTopic models.Topic
	var gotUser models.User
	_ = sqls.DB().First(&gotTopic, topic.Id).Error
	_ = sqls.DB().First(&gotUser, user.Id).Error
	if gotTopic.Status != constants.StatusDeleted {
		t.Fatalf("topic restore should have rolled back, status=%d", gotTopic.Status)
	}
	if gotUser.TopicCount != 0 || gotUser.Score != 0 {
		t.Fatalf("user state should remain deleted score=%d topics=%d", gotUser.Score, gotUser.TopicCount)
	}
}

func TestTopicService_DeleteDoesNotRefundAlreadyPaidBounty(t *testing.T) {
	setupTopicDeleteServiceTestDB(t)
	author, topic, _, _ := createBountyTopicFixture(t, 80)
	answerer := mustCreateUser(t, dates.NowTimestamp()+1)
	answer := mustCreateComment(t, &models.Comment{
		UserId: answerer.Id, EntityType: constants.EntityTopic, EntityId: topic.Id,
		Content: "paid answer", ContentType: constants.ContentTypeText, CreateTime: dates.NowTimestamp() + 2,
	})
	if err := TopicService.AcceptAnswer(topic.Id, answer.Id, author.Id, false); err != nil {
		t.Fatalf("accept answer: %v", err)
	}
	if err := CommentService.Delete(answer.Id); err != nil {
		t.Fatalf("delete accepted comment: %v", err)
	}
	if err := TopicService.Delete(topic.Id, author.Id, nil); err != nil {
		t.Fatalf("delete solved topic: %v", err)
	}

	var gotAuthor, gotAnswerer models.User
	_ = sqls.DB().First(&gotAuthor, author.Id).Error
	_ = sqls.DB().First(&gotAnswerer, answerer.Id).Error
	if gotAuthor.Score != 80 {
		t.Fatalf("paid bounty was incorrectly refunded to author: %d", gotAuthor.Score)
	}
	if gotAnswerer.Score != 20 {
		t.Fatalf("answerer reward changed unexpectedly: %d", gotAnswerer.Score)
	}
	var gotTopic models.Topic
	_ = sqls.DB().First(&gotTopic, topic.Id).Error
	if gotTopic.AcceptedCommentId != 0 || gotTopic.QaStatus != constants.QaStatusSolved {
		t.Fatalf("paid deleted answer should leave solved ledger state, accepted=%d status=%s", gotTopic.AcceptedCommentId, gotTopic.QaStatus)
	}
}
