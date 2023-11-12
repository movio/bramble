package bramble

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// can only run one test at a time that takes over the logrus output
var logrusLock = sync.Mutex{}

func collectLogEvent(t *testing.T, f func()) map[string]interface{} {
	t.Helper()
	r, w := io.Pipe()
	defer r.Close()
	logger := log.StandardLogger()
	prevOut := logger.Out
	prevFmt := logger.Formatter
	logrusLock.Lock()
	defer logrusLock.Unlock()
	logger.SetOutput(w)
	logger.SetFormatter(&log.JSONFormatter{})
	t.Cleanup(func() {
		logger.SetOutput(prevOut)
		logger.SetFormatter(prevFmt)
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

func TestDropsField(t *testing.T) {
	AddField(context.TODO(), "val", "test")
	assert.True(t, true)
}

func TestEventLogOnFinish(t *testing.T) {
	ctx, _ := startEvent(context.TODO(), "test")
	output := collectEventFromContext(ctx, t, func(*event) {
		AddField(ctx, "val", "test")
	})

	assert.Equal(t, "test", output["val"])
}

func TestAddMultipleToEventOnContext(t *testing.T) {
	ctx, _ := startEvent(context.TODO(), "test")
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
	ctx, _ := startEvent(context.TODO(), "test")
	output := collectEventFromContext(ctx, t, func(*event) {
		time.Sleep(time.Microsecond)
	})

	if ts, ok := output["timestamp"].(string); ok {
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
