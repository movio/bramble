package bramble

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	t.Run("no interface provided", func(t *testing.T) {
		cfg := new(Config)
		cfg.GatewayPort = 8082
		cfg.PrivatePort = 8083
		cfg.MetricsPort = 8084
		gAddress := cfg.GatewayAddress()
		require.Equal(t, ":8082", gAddress)
		pAddress := cfg.PrivateAddress()
		require.Equal(t, ":8083", pAddress)
		mAddress := cfg.MetricAddress()
		require.Equal(t, ":8084", mAddress)
	})
	t.Run("network address provided", func(t *testing.T) {
		cfg := new(Config)
		cfg.GatewayListenAddress = "0.0.0.0:8082"
		cfg.GatewayPort = 0
		cfg.PrivateListenAddress = "127.0.0.1:8084"
		cfg.PrivatePort = 8083
		cfg.MetricsListenAddress = ""
		cfg.MetricsPort = 8084
		gAddress := cfg.GatewayAddress()
		require.Equal(t, "0.0.0.0:8082", gAddress)
		pAddress := cfg.PrivateAddress()
		require.Equal(t, "127.0.0.1:8084", pAddress)
		mAddress := cfg.MetricAddress()
		require.Equal(t, ":8084", mAddress)
	})
	t.Run("private http address for plugin services", func(t *testing.T) {
		cfg := new(Config)
		cfg.PrivatePort = 8083
		require.Equal(t, "http://localhost:8083/plugin", cfg.PrivateHttpAddress("plugin"))
		cfg.PrivateListenAddress = "127.0.0.1:8084"
		require.Equal(t, "http://127.0.0.1:8084/plugin", cfg.PrivateHttpAddress("plugin"))
	})
}
