package event

import (
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"runtime/debug"
	"sync"

	"github.com/panjf2000/ants/v2"
)

var (
	m         sync.RWMutex
	eventPool *ants.PoolWithFunc
	handlers  map[reflect.Type][]func(i any)
	// wg        sync.WaitGroup
)

func init() {
	var err error
	workers := runtime.GOMAXPROCS(0) * 2
	if workers < 8 {
		workers = 8
	}
	if workers > 32 {
		workers = 32
	}
	eventPool, err = ants.NewPoolWithFunc(workers, dispatch, ants.WithMaxBlockingTasks(4096))
	if err != nil {
		slog.Error(err.Error(), slog.Any("err", err))
	}
	handlers = make(map[reflect.Type][]func(i any))
}

func dispatch(i any) {
	handlerList := getHandlerList(i)
	if len(handlerList) == 0 {
		return
	}
	for _, handler := range handlerList {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Error("event handler panic",
						slog.String("type", eventTypeName(i)),
						slog.Any("panic", recovered),
						slog.String("stack", string(debug.Stack())))
				}
			}()
			handler(i)
		}()
	}
}

func Send(e any) {
	if e == nil {
		slog.Warn("ignore nil event")
		return
	}
	if eventPool == nil {
		// Pool construction should not fail in normal operation. Falling back to
		// direct dispatch keeps business events functional instead of panicking.
		dispatch(e)
		return
	}
	if err := eventPool.Invoke(e); err != nil {
		// Pool overload or shutdown must not silently discard business events.
		// Direct dispatch preserves correctness; each handler has panic isolation.
		slog.Error("submit event failed, falling back to direct dispatch",
			slog.String("type", eventTypeName(e)), slog.Any("err", err))
		dispatch(e)
	} else {
		// wg.Add(len(getHandlerList(e)))
		// wg.Wait()
	}
}

func RegHandler(t reflect.Type, handler func(i any)) {
	if t == nil || handler == nil {
		slog.Warn("ignore invalid event handler registration")
		return
	}
	m.Lock()
	defer m.Unlock()

	handlerList := handlers[t]
	handlerList = append(handlerList, handler)
	handlers[t] = handlerList
}

func getHandlerList(i any) []func(i any) {
	if i == nil {
		return nil
	}
	m.RLock()
	defer m.RUnlock()

	t := reflect.TypeOf(i)
	handlerList, ok := handlers[t]
	if ok {
		// Return a copy so a late registration cannot race with dispatch.
		handlersCopy := make([]func(any), len(handlerList))
		copy(handlersCopy, handlerList)
		return handlersCopy
	} else {
		slog.Error("没找到任务处理器", slog.String("type", eventTypeName(i)))
		return nil
	}
}

func eventTypeName(i any) string {
	if i == nil {
		return "<nil>"
	}
	if t := reflect.TypeOf(i); t != nil {
		return t.String()
	}
	return fmt.Sprintf("%T", i)
}
