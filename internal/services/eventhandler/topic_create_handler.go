package eventhandler

import (
	"log/slog"
	"reflect"

	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/services"
)

func init() {
	event.RegHandler(reflect.TypeOf(event.TopicCreateEvent{}), handleTopicCreateEvent)
}

func handleTopicCreateEvent(i interface{}) {
	e := i.(event.TopicCreateEvent)

	err := services.UserFeedService.FanOutFromFollowers(
		e.UserId, e.TopicId, constants.EntityTopic, e.CreateTime,
	)
	if err != nil {
		slog.Error("fan-out topic feed failed", slog.Int64("topicId", e.TopicId), slog.Any("err", err))
	}
}
