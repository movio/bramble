package bramble

import (
	"context"
	"encoding/json"
	"io"
	log "log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// can only run one test at a time that takes over the log output
var logLock = sync.Mutex{}

func collectLogEvent(t *testing.T, f func()) map[string]interface{} {
	t.Helper()
	r, w := io.Pipe()
	defer r.Close()
	prevlogger := log.Default()
	logLock.Lock()
	defer logLock.Unlock()
	log.SetDefault(log.New(log.NewJSONHandler(w, nil)))
	t.Cleanup(func() {
		logLock.Lock()
		defer logLock.Unlock()
		log.SetDefault(prevlogger)
	})

	go func() {
		defer w.Close()
		f()
	}()

	var obj map[string]interface{}
	err := json.NewDecoder(r).Decode(&obj)
	assert.NoError(t, err)

	return obj
}

func collectEventFromContext(ctx context.Context, t *testing.T, f func(*event)) map[string]interface{} {
	t.Helper()
	return collectLogEvent(t, func() {
		e := getEvent(ctx)
		f(e)
		if e != nil {
			e.finish()
		}
	})
}

func testEventName(EventFields) string {
	return "test"
}

func TestDropsField(t *testing.T) {
	AddField(context.TODO(), "val", "test")
	assert.True(t, true)
}

func TestEventLogOnFinish(t *testing.T) {
	ctx, _ := startEvent(context.TODO(), testEventName)
	output := collectEventFromContext(ctx, t, func(*event) {
		AddField(ctx, "val", "test")
	})

	assert.Equal(t, "test", output["val"])
}

func TestAddMultipleToEventOnContext(t *testing.T) {
	ctx, _ := startEvent(context.TODO(), testEventName)
	output := collectEventFromContext(ctx, t, func(*event) {
		AddFields(ctx, EventFields{
			"gizmo":   "foo",
			"gimmick": "bar",
		})
	})

	assert.Equal(t, "foo", output["gizmo"])
	assert.Equal(t, "bar", output["gimmick"])
}

func TestEventMeasurement(t *testing.T) {
	start := time.Now()
	ctx, _ := startEvent(context.TODO(), testEventName)
	output := collectEventFromContext(ctx, t, func(*event) {
		time.Sleep(time.Microsecond)
	})

	if ts, ok := output["time"].(string); ok {
		timestamp, err := time.Parse(time.RFC3339Nano, ts)
		assert.NoError(t, err)
		assert.WithinDuration(t, start, timestamp, time.Second)
	} else {
		assert.Fail(t, "missing timestamp")
	}
	if dur, ok := output["duration"].(string); ok {
		duration, err := time.ParseDuration(dur)
		assert.NoError(t, err)
		assert.True(t, duration > 0)
	} else {
		assert.Fail(t, "missing duration")
	}
}
