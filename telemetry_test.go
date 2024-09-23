package bramble

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResources(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	_, err := resources(cfg)
	require.NoError(t, err)
}
