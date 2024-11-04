package bramble

import (
	"flag"
	"io"
	log "log/slog"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Verbose() {
		blackhole := log.New(log.NewTextHandler(io.Discard, nil))
		log.SetDefault(blackhole)
	}
	os.Exit(m.Run())
}
