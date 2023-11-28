package bramble

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	t.Run("port provided", func(t *testing.T) {
		cfg := &Config{
			GatewayPort: 8082,
			PrivatePort: 8083,
			MetricsPort: 8084,
		}
		require.Equal(t, ":8082", cfg.GatewayAddress())
		require.Equal(t, ":8083", cfg.PrivateAddress())
		require.Equal(t, ":8084", cfg.MetricAddress())
	})
	t.Run("address provided and prefered over port", func(t *testing.T) {
		cfg := &Config{
			GatewayListenAddress: "0.0.0.0:8082",
			GatewayPort:          0,
			PrivateListenAddress: "127.0.0.1:8084",
			PrivatePort:          8083,
			MetricsListenAddress: "",
			MetricsPort:          8084,
		}
		require.Equal(t, "0.0.0.0:8082", cfg.GatewayAddress())
		require.Equal(t, "127.0.0.1:8084", cfg.PrivateAddress())
		require.Equal(t, ":8084", cfg.MetricAddress())
	})
	t.Run("private http address for plugin services", func(t *testing.T) {
		cfg := &Config{
			PrivatePort: 8083,
		}
		require.Equal(t, "http://localhost:8083/plugin", cfg.PrivateHttpAddress("plugin"))
		cfg.PrivateListenAddress = "127.0.0.1:8084"
		require.Equal(t, "http://127.0.0.1:8084/plugin", cfg.PrivateHttpAddress("plugin"))
	})
}

func TestParseExampleConfig(t *testing.T) {
	cfg, err := GetConfig([]string{"config.json.example"})
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, cfg.DefaultTimeouts.ReadTimeoutDuration)
	require.Equal(t, 120*time.Second, cfg.DefaultTimeouts.IdleTimeoutDuration)
	require.Equal(t, 20*time.Second, cfg.GatewayTimeouts.WriteTimeoutDuration)
	require.Equal(t, 10*time.Second, cfg.PrivateTimeouts.WriteTimeoutDuration)
}
