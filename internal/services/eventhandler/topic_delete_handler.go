package eventhandler

import (
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/pkg/locales"
	"bbs-go/internal/pkg/msg"
	"bbs-go/internal/services"
	"reflect"
)

func init() {
	event.RegHandler(reflect.TypeOf(event.TopicDeleteEvent{}), handleTopicDeleteEvent)
}

func handleTopicDeleteEvent(i interface{}) {
	e := i.(event.TopicDeleteEvent)

	// The topic and its user-feed rows have already been physically removed in
	// the delete transaction. Keep notification/audit side effects asynchronous.
	sendTopicDeleteMsg(e)

	services.OperateLogService.AddOperateLog(e.DeleteUserId, constants.OpTypeDelete, constants.EntityTopic,
		e.TopicId, "", nil)
}

func sendTopicDeleteMsg(e event.TopicDeleteEvent) {
	if e.UserId <= 0 || e.UserId == e.DeleteUserId {
		return
	}
	quoteContent := "《" + e.TopicTitle + "》"
	services.MessageService.SendMsg(0, e.UserId, msg.TypeTopicDelete,
		locales.Get("message.topic_delete_msg_title"), "", quoteContent,
		&msg.TopicDeleteExtraData{
			TopicId:      e.TopicId,
			DeleteUserId: e.DeleteUserId,
		},
	)
}
