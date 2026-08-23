package eventhandler

import (
	"bbs-go/internal/models"
	"log/slog"
	"reflect"

	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/services"
	"github.com/mlogclub/simple/sqls"
)

func init() {
	event.RegHandler(reflect.TypeOf(event.TopicCreateEvent{}), handleTopicCreateEvent)
}

func handleTopicCreateEvent(i interface{}) {
	e := i.(event.TopicCreateEvent)

	// Event handlers run asynchronously. A topic can be physically deleted before
	// this handler gets a worker; never recreate follower feeds for a dead topic.
	topic := services.TopicService.Get(e.TopicId)
	if topic == nil || topic.Status != constants.StatusOk {
		return
	}

	err := services.UserFeedService.FanOutFromFollowers(
		e.UserId, e.TopicId, constants.EntityTopic, e.CreateTime,
	)
	if err != nil {
		slog.Error("fan-out topic feed failed", slog.Int64("topicId", e.TopicId), slog.Any("err", err))
		return
	}

	// The event worker is asynchronous. A permanent delete can commit after the
	// preflight read but before FanOutFromFollowers finishes, leaving freshly
	// inserted feed rows after the delete transaction already purged its copy.
	// Re-check and compensate so a deleted topic cannot reappear in home feeds.
	topic = services.TopicService.Get(e.TopicId)
	if topic == nil || topic.Status != constants.StatusOk {
		if cleanupErr := sqls.DB().Where("data_type = ? AND data_id = ?", constants.EntityTopic, e.TopicId).
			Delete(&models.UserFeed{}).Error; cleanupErr != nil {
			slog.Error("cleanup late topic feed failed", slog.Int64("topicId", e.TopicId), slog.Any("err", cleanupErr))
		}
	}
}
