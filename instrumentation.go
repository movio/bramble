package bramble

import (
	"context"
	log "log/slog"
	"sync"
	"time"
)

const eventKey contextKey = "instrumentation"

type event struct {
	nameFunc  EventNameFunc
	timestamp time.Time
	fields    EventFields
	fieldLock sync.Mutex
	writeLock sync.Once
}

// EventFields contains fields to be logged for the event
type EventFields map[string]interface{}

// EventNameFunc constructs a name for the event from the provided fields
type EventNameFunc func(EventFields) string

func newEvent(name EventNameFunc) *event {
	return &event{
		nameFunc:  name,
		timestamp: time.Now(),
		fields:    EventFields{},
	}
}

func startEvent(ctx context.Context, name EventNameFunc) (context.Context, *event) {
	ev := newEvent(name)
	return context.WithValue(ctx, eventKey, ev), ev
}

func (e *event) addField(name string, value interface{}) {
	e.fieldLock.Lock()
	e.fields[name] = value
	e.fieldLock.Unlock()
}

func (e *event) addFields(fields EventFields) {
	e.fieldLock.Lock()
	for k, v := range fields {
		e.fields[k] = v
	}
	e.fieldLock.Unlock()
}

func (e *event) finish() {
	e.writeLock.Do(func() {
		attrs := make([]any, 0, len(e.fields))
		for k, v := range e.fields {
			attrs = append(attrs, log.Any(k, v))
		}
		log.With(
			"duration", time.Since(e.timestamp).String(),
		).Info(e.nameFunc(e.fields), attrs...)
	})
}

// AddField adds the given field to the event contained in the context (if any)
func AddField(ctx context.Context, name string, value interface{}) {
	if e := getEvent(ctx); e != nil {
		e.addField(name, value)
	}
}

// AddFields adds the given fields to the event contained in the context (if any)
func AddFields(ctx context.Context, fields EventFields) {
	if e := getEvent(ctx); e != nil {
		e.addFields(fields)
	}
}

func getEvent(ctx context.Context) *event {
	if e := ctx.Value(eventKey); e != nil {
		if e, ok := e.(*event); ok {
			return e
		}
	}
	return nil
}
