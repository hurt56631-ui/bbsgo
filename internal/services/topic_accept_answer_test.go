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

func setupTopicAcceptAnswerTestDB(t *testing.T) {
	t.Helper()
	config.Instance = &config.Config{Language: config.DefaultLanguage}
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Topic{}, &models.Comment{}); err != nil {
		t.Fatalf("auto migrate topic answer: %v", err)
	}
}

func TestTopicService_AcceptAnswerPaysBountyOnlyOnce(t *testing.T) {
	setupTopicAcceptAnswerTestDB(t)
	now := dates.NowTimestamp()
	author := mustCreateUser(t, now)
	answerer := mustCreateUser(t, now+1)
	otherAnswerer := mustCreateUser(t, now+2)
	topic := &models.Topic{
		Type:        constants.TopicTypeQA,
		QaStatus:    constants.QaStatusUnsolved,
		BountyScore: 20,
		UserId:      author.Id,
		Title:       "question",
		Status:      constants.StatusOk,
		CreateTime:  now,
	}
	if err := sqls.DB().Create(topic).Error; err != nil {
		t.Fatalf("create topic: %v", err)
	}
	answer := mustCreateComment(t, &models.Comment{
		UserId:      answerer.Id,
		EntityType:  constants.EntityTopic,
		EntityId:    topic.Id,
		Content:     "answer",
		ContentType: constants.ContentTypeText,
	})
	otherAnswer := mustCreateComment(t, &models.Comment{
		UserId:      otherAnswerer.Id,
		EntityType:  constants.EntityTopic,
		EntityId:    topic.Id,
		Content:     "other",
		ContentType: constants.ContentTypeText,
	})

	if err := TopicService.AcceptAnswer(topic.Id, answer.Id, author.Id, false); err != nil {
		t.Fatalf("accept answer: %v", err)
	}
	if err := TopicService.AcceptAnswer(topic.Id, answer.Id, author.Id, false); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	var gotAnswerer models.User
	_ = sqls.DB().First(&gotAnswerer, answerer.Id).Error
	if gotAnswerer.Score != 20 {
		t.Fatalf("expected one bounty payout, got %d", gotAnswerer.Score)
	}
	var rewardLogs int64
	if err := sqls.DB().Model(&models.UserScoreLog{}).
		Where("source_type = ? AND source_id = ? AND type = ?", constants.SourceTypeQaBounty, topicIdString(topic.Id), constants.ScoreTypeIncr).
		Count(&rewardLogs).Error; err != nil {
		t.Fatalf("count reward logs: %v", err)
	}
	if rewardLogs != 1 {
		t.Fatalf("expected one reward log, got %d", rewardLogs)
	}
	if err := TopicService.UnacceptAnswer(topic.Id, author.Id, false); err == nil {
		t.Fatalf("expected paid bounty unaccept to be rejected")
	}

	if err := TopicService.ForceSetQaStatus(topic.Id, constants.QaStatusUnsolved); err != nil {
		t.Fatalf("admin reopen: %v", err)
	}
	if err := TopicService.AcceptAnswer(topic.Id, otherAnswer.Id, author.Id, false); err != nil {
		t.Fatalf("accept after admin reopen: %v", err)
	}
	var gotOther models.User
	_ = sqls.DB().First(&gotOther, otherAnswerer.Id).Error
	if gotOther.Score != 0 {
		t.Fatalf("same topic bounty paid twice: %d", gotOther.Score)
	}
}

func topicIdString(id int64) string {
	return strconv.FormatInt(id, 10)
}
