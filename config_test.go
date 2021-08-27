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
	t.Run("network interface provided", func(t *testing.T) {
		cfg := new(Config)
		cfg.GatewayInterface = "0.0.0.0"
		cfg.GatewayPort = 8082
		cfg.PrivateInterface = "127.0.0.1"
		cfg.PrivatePort = 8083
		cfg.MetricsInterface = "192.0.0.1"
		cfg.MetricsPort = 8084
		gAddress := cfg.GatewayAddress()
		require.Equal(t, "0.0.0.0:8082", gAddress)
		pAddress := cfg.PrivateAddress()
		require.Equal(t, "127.0.0.1:8083", pAddress)
		mAddress := cfg.MetricAddress()
		require.Equal(t, "192.0.0.1:8084", mAddress)
	})
}
