package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sashaoxel/alfa-hack/backend/internal/config"
)

func TestParse_Defaults(t *testing.T) {
	c, err := config.Parse(map[string]string{})
	require.NoError(t, err)
	require.Equal(t, ":8080", c.HTTPAddr)
	require.Equal(t, ":9090", c.GRPCAddr)
	require.Equal(t, config.MLClientGRPC, c.MLClient)
	require.True(t, c.DemoMode)
	require.Equal(t, 12*time.Hour, c.SessionTTL)
	require.Equal(t, 15*time.Second, c.SSEHeartbeat)
	require.False(t, c.CookieSecure)
}

func TestParse_DemoGeneratesSecret(t *testing.T) {
	c, err := config.Parse(map[string]string{})
	require.NoError(t, err)
	require.True(t, c.JWTSecretGenerated)
	require.GreaterOrEqual(t, len(c.JWTSecret), 32)

	other, err := config.Parse(map[string]string{})
	require.NoError(t, err)
	require.NotEqual(t, c.JWTSecret, other.JWTSecret)
}

func TestParse_NonDemoRequiresSecret(t *testing.T) {
	_, err := config.Parse(map[string]string{"DEMO_MODE": "false"})
	require.ErrorContains(t, err, "JWT_SECRET")

	c, err := config.Parse(map[string]string{"DEMO_MODE": "false", "JWT_SECRET": "0123456789abcdef"})
	require.NoError(t, err)
	require.False(t, c.JWTSecretGenerated)
}

func TestParse_Validation(t *testing.T) {
	cases := map[string]map[string]string{
		"ml client":    {"ML_CLIENT": "mock"},
		"log format":   {"LOG_FORMAT": "xml"},
		"short secret": {"JWT_SECRET": "short"},
		"bad ttl":      {"SESSION_TTL": "-1h"},
		"bad bool":     {"DEMO_MODE": "maybe"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse(env)
			require.Error(t, err)
		})
	}
}

func TestParse_Overrides(t *testing.T) {
	c, err := config.Parse(map[string]string{
		"HTTP_ADDR": ":18080", "ML_CLIENT": "fake", "COOKIE_SECURE": "true",
		"JWT_SECRET": "0123456789abcdef", "SSE_HEARTBEAT": "5s",
	})
	require.NoError(t, err)
	require.Equal(t, ":18080", c.HTTPAddr)
	require.Equal(t, config.MLClientFake, c.MLClient)
	require.True(t, c.CookieSecure)
	require.Equal(t, 5*time.Second, c.SSEHeartbeat)
}
