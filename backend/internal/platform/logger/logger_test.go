package logger_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sashaoxel/alfa-hack/backend/internal/platform/logger"
)

func TestMaskString(t *testing.T) {
	cases := map[string]string{
		"+7 900 000-00-01":            "+7 *** ***-**-01",
		"89000000001":                 "+7 *** ***-**-01",
		"+7(900)123-45-67":            "+7 *** ***-**-67",
		"звонил +79001234567 вчера":   "звонил +7 *** ***-**-67 вчера",
		"без телефона, сумма 2000000": "без телефона, сумма 2000000",
	}
	for in, want := range cases {
		require.Equal(t, want, logger.MaskString(in), in)
	}
}

func TestLogger_MasksPII(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, "text", "info")
	log.Info("login", "phone", "+79001234567", "inn", "7707083893", "note", "тел +7 900 111-22-33", "client_id", "c-1")
	out := buf.String()

	require.NotContains(t, out, "9001234567")
	require.NotContains(t, out, "7707083893")
	require.NotContains(t, out, "111-22-33")
	require.Contains(t, out, "client_id=c-1")
}

func TestLogger_Level(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, "json", "warn")
	log.Info("hidden")
	log.Warn("shown")
	require.NotContains(t, buf.String(), "hidden")
	require.Contains(t, buf.String(), "shown")
}
