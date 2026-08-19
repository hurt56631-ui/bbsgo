package eventhandler

import (
	"log/slog"
	"reflect"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/services"
)

const (
	followHistoryBackfillLimit = 100
	followBackfillBatchSize    = 500
)

func init() {
	event.RegHandler(reflect.TypeOf(event.FollowEvent{}), handleFollowEvent)
}

func handleFollowEvent(i interface{}) {
	e := i.(event.FollowEvent)

	// Backfill only the latest public topics. Scanning an author's entire history
	// made one follow request generate thousands of writes for active accounts.
	topics := services.TopicService.GetRecentTopicsByUser(e.OtherId, followHistoryBackfillLimit)
	feeds := make([]models.UserFeed, 0, len(topics))
	for _, topic := range topics {
		feeds = append(feeds, models.UserFeed{
			UserId:     e.UserId,
			DataType:   constants.EntityTopic,
			DataId:     topic.Id,
			AuthorId:   topic.UserId,
			CreateTime: topic.CreateTime,
		})
	}
	if err := services.UserFeedService.CreateInBatches(feeds, followBackfillBatchSize); err != nil {
		slog.Error("backfill followed user feed failed", slog.Int64("userId", e.UserId), slog.Int64("otherId", e.OtherId), slog.Any("err", err))
	}
}
