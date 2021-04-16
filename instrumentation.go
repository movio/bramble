package bramble

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const eventKey contextKey = "instrumentation"

type event struct {
	name      string
	timestamp time.Time
	fields    EventFields
	fieldLock sync.Mutex
	writeLock sync.Once
}

// EventFields contains fields to be logged for the event
type EventFields map[string]interface{}

func newEvent(name string) *event {
	return &event{
		name:      name,
		timestamp: time.Now(),
		fields:    EventFields{},
	}
}

func startEvent(ctx context.Context, name string) (context.Context, *event) {
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
		log.WithFields(log.Fields{
			"timestamp": e.timestamp.Format(time.RFC3339Nano),
			"duration":  time.Since(e.timestamp).String(),
		}).WithFields(log.Fields(e.fields)).Info(e.name)
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
