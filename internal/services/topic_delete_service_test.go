package services

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/config"
	"errors"
	"strconv"
	"testing"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

func setupTopicDeleteServiceTestDB(t *testing.T) {
	t.Helper()
	config.Instance = &config.Config{Language: config.DefaultLanguage}
	db := setupTestDB(t)
	if err := db.AutoMigrate(
		&models.Topic{}, &models.TopicTag{}, &models.Attachment{}, &models.AttachmentDownloadLog{},
		&models.Comment{}, &models.UserLike{}, &models.Favorite{}, &models.UserFeed{}, &models.UserReport{},
		&models.Vote{}, &models.VoteOption{}, &models.VoteRecord{}, &models.Message{}, &models.StorageDeleteTask{}, &models.SearchDeleteTask{},
	); err != nil {
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

func countRows(t *testing.T, model any, where string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := sqls.DB().Model(model).Where(where, args...).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestTopicService_DeletePhysicallyRemovesOwnedGraphAndIsIdempotent(t *testing.T) {
	setupTopicDeleteServiceTestDB(t)
	author, topic, _, attachment := createBountyTopicFixture(t, 80)
	commenter := mustCreateUser(t, dates.NowTimestamp()+1)
	replier := mustCreateUser(t, dates.NowTimestamp()+2)
	if err := sqls.DB().Model(&models.User{}).Where("id IN ?", []int64{commenter.Id, replier.Id}).
		UpdateColumn("comment_count", 1).Error; err != nil {
		t.Fatalf("prepare comment counters: %v", err)
	}

	root := &models.Comment{UserId: commenter.Id, EntityType: constants.EntityTopic, EntityId: topic.Id, Content: "root", ContentType: constants.ContentTypeText, Status: constants.StatusOk, CreateTime: dates.NowTimestamp() + 3}
	if err := sqls.DB().Create(root).Error; err != nil {
		t.Fatalf("create root comment: %v", err)
	}
	reply := &models.Comment{UserId: replier.Id, EntityType: constants.EntityComment, EntityId: root.Id, Content: "reply", ContentType: constants.ContentTypeText, Status: constants.StatusOk, CreateTime: dates.NowTimestamp() + 4}
	if err := sqls.DB().Create(reply).Error; err != nil {
		t.Fatalf("create reply: %v", err)
	}
	if err := sqls.DB().Create(&models.UserLike{UserId: replier.Id, EntityType: constants.EntityTopic, EntityId: topic.Id}).Error; err != nil {
		t.Fatalf("create topic like: %v", err)
	}
	if err := sqls.DB().Create(&models.UserLike{UserId: author.Id, EntityType: constants.EntityComment, EntityId: root.Id}).Error; err != nil {
		t.Fatalf("create comment like: %v", err)
	}
	if err := sqls.DB().Create(&models.Favorite{UserId: commenter.Id, EntityType: constants.EntityTopic, EntityId: topic.Id}).Error; err != nil {
		t.Fatalf("create favorite: %v", err)
	}
	if err := sqls.DB().Create(&models.UserFeed{UserId: commenter.Id, DataId: topic.Id, DataType: constants.EntityTopic, AuthorId: author.Id}).Error; err != nil {
		t.Fatalf("create feed: %v", err)
	}
	if err := sqls.DB().Create(&models.UserReport{UserId: replier.Id, DataId: root.Id, DataType: constants.EntityComment}).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	if err := sqls.DB().Create(&models.UserReport{UserId: commenter.Id, DataId: topic.Id, DataType: constants.EntityTopic}).Error; err != nil {
		t.Fatalf("create topic report: %v", err)
	}
	if err := sqls.DB().Create(&models.Message{UserId: author.Id, Type: 0, ExtraData: `{"rootEntityType":"topic","rootEntityId":"` + strconv.FormatInt(topic.Id, 10) + `"}`, Status: 0, CreateTime: dates.NowTimestamp()}).Error; err != nil {
		t.Fatalf("create topic comment message: %v", err)
	}
	if err := sqls.DB().Create(&models.Message{UserId: author.Id, Type: 2, ExtraData: `{"topicId":` + strconv.FormatInt(topic.Id, 10) + `,"likeUserId":1}`, Status: 0, CreateTime: dates.NowTimestamp()}).Error; err != nil {
		t.Fatalf("create topic like message: %v", err)
	}
	unrelatedTopicID := topic.Id*10 + 7
	if err := sqls.DB().Create(&models.Message{UserId: author.Id, Type: 2, ExtraData: `{"topicId":` + strconv.FormatInt(unrelatedTopicID, 10) + `,"likeUserId":1}`, Status: 0, CreateTime: dates.NowTimestamp()}).Error; err != nil {
		t.Fatalf("create unrelated topic message: %v", err)
	}
	if err := sqls.DB().Create(&models.AttachmentDownloadLog{UserId: commenter.Id, AttachmentId: attachment.Id}).Error; err != nil {
		t.Fatalf("create attachment log: %v", err)
	}
	vote := &models.Vote{TopicId: topic.Id, UserId: author.Id, Type: constants.VoteTypeSingle, Title: "vote", OptionCount: 1, VoteNum: 1, CreateTime: dates.NowTimestamp()}
	if err := sqls.DB().Create(vote).Error; err != nil {
		t.Fatalf("create vote: %v", err)
	}
	option := &models.VoteOption{VoteId: vote.Id, Content: "A", SortNo: 1, CreateTime: dates.NowTimestamp()}
	if err := sqls.DB().Create(option).Error; err != nil {
		t.Fatalf("create option: %v", err)
	}
	if err := sqls.DB().Create(&models.VoteRecord{UserId: commenter.Id, VoteId: vote.Id, OptionIds: strconv.FormatInt(option.Id, 10), CreateTime: dates.NowTimestamp()}).Error; err != nil {
		t.Fatalf("create vote record: %v", err)
	}

	if err := TopicService.Delete(topic.Id, author.Id, nil); err != nil {
		t.Fatalf("delete topic: %v", err)
	}
	if err := TopicService.Delete(topic.Id, author.Id, nil); err != nil {
		t.Fatalf("duplicate delete: %v", err)
	}

	var gotAuthor, gotCommenter, gotReplier models.User
	_ = sqls.DB().First(&gotAuthor, author.Id).Error
	_ = sqls.DB().First(&gotCommenter, commenter.Id).Error
	_ = sqls.DB().First(&gotReplier, replier.Id).Error
	if gotAuthor.Score != 100 || gotAuthor.TopicCount != 0 {
		t.Fatalf("unexpected author balances score=%d topics=%d", gotAuthor.Score, gotAuthor.TopicCount)
	}
	if gotCommenter.CommentCount != 0 || gotReplier.CommentCount != 0 {
		t.Fatalf("comment counters not repaired commenter=%d replier=%d", gotCommenter.CommentCount, gotReplier.CommentCount)
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

	checks := []struct {
		name  string
		model any
		where string
		args  []any
	}{
		{"topic", &models.Topic{}, "id = ?", []any{topic.Id}},
		{"topic tags", &models.TopicTag{}, "topic_id = ?", []any{topic.Id}},
		{"comments", &models.Comment{}, "id IN ?", []any{[]int64{root.Id, reply.Id}}},
		{"topic likes", &models.UserLike{}, "entity_type = ? AND entity_id = ?", []any{constants.EntityTopic, topic.Id}},
		{"comment likes", &models.UserLike{}, "entity_type = ? AND entity_id IN ?", []any{constants.EntityComment, []int64{root.Id, reply.Id}}},
		{"favorites", &models.Favorite{}, "entity_type = ? AND entity_id = ?", []any{constants.EntityTopic, topic.Id}},
		{"feeds", &models.UserFeed{}, "data_type = ? AND data_id = ?", []any{constants.EntityTopic, topic.Id}},
		{"comment reports", &models.UserReport{}, "data_type = ? AND data_id IN ?", []any{constants.EntityComment, []int64{root.Id, reply.Id}}},
		{"topic reports", &models.UserReport{}, "data_type = ? AND data_id = ?", []any{constants.EntityTopic, topic.Id}},
		{"topic comment messages", &models.Message{}, "extra_data LIKE ?", []any{"%\"rootEntityId\":\"" + strconv.FormatInt(topic.Id, 10) + "\"%"}},
		{"topic direct messages", &models.Message{}, "extra_data LIKE ?", []any{"%\"topicId\":" + strconv.FormatInt(topic.Id, 10) + ",%"}},
		{"attachments", &models.Attachment{}, "topic_id = ?", []any{topic.Id}},
		{"attachment logs", &models.AttachmentDownloadLog{}, "attachment_id = ?", []any{attachment.Id}},
		{"votes", &models.Vote{}, "topic_id = ?", []any{topic.Id}},
		{"vote options", &models.VoteOption{}, "vote_id = ?", []any{vote.Id}},
		{"vote records", &models.VoteRecord{}, "vote_id = ?", []any{vote.Id}},
	}
	for _, check := range checks {
		if got := countRows(t, check.model, check.where, check.args...); got != 0 {
			t.Fatalf("%s not physically deleted: %d rows remain", check.name, got)
		}
	}
	if got := countRows(t, &models.Message{}, "extra_data LIKE ?", "%\"topicId\":"+strconv.FormatInt(unrelatedTopicID, 10)+"%"); got != 1 {
		t.Fatalf("unrelated topic message was removed: remaining=%d", got)
	}
}

func TestTopicService_DeletePurgesLegacySoftDeletedRowWithoutDoubleDecrement(t *testing.T) {
	setupTopicDeleteServiceTestDB(t)
	user, topic, _, _ := createBountyTopicFixture(t, 80)
	if err := sqls.DB().Model(&models.Topic{}).Where("id = ?", topic.Id).
		UpdateColumn("status", constants.StatusDeleted).Error; err != nil {
		t.Fatalf("mark legacy topic deleted: %v", err)
	}
	if err := sqls.DB().Model(&models.User{}).Where("id = ?", user.Id).
		UpdateColumn("topic_count", 0).Error; err != nil {
		t.Fatalf("prepare legacy topic count: %v", err)
	}
	if err := sqls.DB().Create(&models.UserScoreLog{
		UserId: user.Id, SourceType: constants.SourceTypeQaBountyRefund,
		SourceId: strconv.FormatInt(topic.Id, 10), Description: "legacy refund",
		Type: constants.ScoreTypeIncr, Score: topic.BountyScore, CreateTime: dates.NowTimestamp(),
	}).Error; err != nil {
		t.Fatalf("create legacy refund log: %v", err)
	}
	if err := sqls.DB().Model(&models.User{}).Where("id = ?", user.Id).
		UpdateColumn("score", 100).Error; err != nil {
		t.Fatalf("prepare legacy refunded score: %v", err)
	}

	if err := TopicService.Delete(topic.Id, user.Id, nil); err != nil {
		t.Fatalf("purge legacy delete: %v", err)
	}
	if got := countRows(t, &models.Topic{}, "id = ?", topic.Id); got != 0 {
		t.Fatalf("legacy soft-deleted topic remains: %d", got)
	}
	var gotUser models.User
	_ = sqls.DB().First(&gotUser, user.Id).Error
	if gotUser.TopicCount != 0 || gotUser.Score != 100 {
		t.Fatalf("legacy purge changed counters/score: topics=%d score=%d", gotUser.TopicCount, gotUser.Score)
	}
	var refunds int64
	_ = sqls.DB().Model(&models.UserScoreLog{}).
		Where("source_type = ? AND source_id = ? AND type = ?", constants.SourceTypeQaBountyRefund, strconv.FormatInt(topic.Id, 10), constants.ScoreTypeIncr).
		Count(&refunds).Error
	if refunds != 1 {
		t.Fatalf("legacy purge duplicated refund: %d", refunds)
	}
}

func TestTopicService_UndeleteCannotRestoreNewHardDelete(t *testing.T) {
	setupTopicDeleteServiceTestDB(t)
	user, topic, _, _ := createBountyTopicFixture(t, 80)
	if err := TopicService.Delete(topic.Id, user.Id, nil); err != nil {
		t.Fatalf("delete topic: %v", err)
	}
	if err := TopicService.Undelete(topic.Id); err == nil {
		t.Fatalf("hard-deleted topic must not be restorable")
	}
}

func TestTopicService_DeleteDoesNotRefundAlreadyPaidBounty(t *testing.T) {
	setupTopicDeleteServiceTestDB(t)
	author, topic, _, _ := createBountyTopicFixture(t, 80)
	answerer := mustCreateUser(t, dates.NowTimestamp()+1)
	answer := mustCreateComment(t, &models.Comment{
		UserId: answerer.Id, EntityType: constants.EntityTopic, EntityId: topic.Id,
		Content: "paid answer", ContentType: constants.ContentTypeText, Status: constants.StatusOk, CreateTime: dates.NowTimestamp() + 2,
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
	if err := sqls.DB().First(&gotTopic, topic.Id).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("topic should be physically deleted, err=%v", err)
	}
}
