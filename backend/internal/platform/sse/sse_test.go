package sse_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	corev1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/core/v1"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/sse"
)

func TestWriter_Frames(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.New(rec)
	require.NoError(t, err)

	require.NoError(t, w.Event("42", "text_delta", &corev1.TextDelta{Text: "привет"}))
	require.NoError(t, w.Ping())

	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))

	body := rec.Body.String()
	require.Contains(t, body, "id: 42\nevent: text_delta\ndata: {")
	require.Contains(t, body, "привет")
	require.True(t, strings.HasSuffix(body, "}\n\n: ping\n\n"), body)
}

func TestWriter_SanitizesFields(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.New(rec)
	require.NoError(t, err)
	require.NoError(t, w.Event("1\nevent: evil", "x\r\ndata: y", &corev1.TextDelta{}))
	// инъекция полей = строка, начинающаяся с имени поля; перевод строки в значении её бы создал
	var events, datas int
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		switch {
		case strings.HasPrefix(line, "event: "):
			events++
		case strings.HasPrefix(line, "data: "):
			datas++
		}
	}
	require.Equal(t, 1, events)
	require.Equal(t, 1, datas)
}

func TestOneofName(t *testing.T) {
	ev := &corev1.AssistantEvent{Event: &corev1.AssistantEvent_TextDelta{TextDelta: &corev1.TextDelta{Text: "x"}}}
	require.Equal(t, "text_delta", sse.OneofName(ev, "event"))
	require.Equal(t, "", sse.OneofName(&corev1.AssistantEvent{}, "event"))
	require.Equal(t, "", sse.OneofName(ev, "nope"))
}

func TestPump_EventsHeartbeatAndEOF(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.New(rec)
	require.NoError(t, err)

	step := 0
	recv := func() (*corev1.TextDelta, error) {
		step++
		switch step {
		case 1:
			return &corev1.TextDelta{Text: "a"}, nil
		case 2:
			time.Sleep(60 * time.Millisecond) // пауза дольше heartbeat → в потоке появится ping
			return &corev1.TextDelta{Text: "b"}, nil
		}
		return nil, io.EOF
	}
	err = sse.Pump(context.Background(), w, 20*time.Millisecond, recv,
		func(*corev1.TextDelta) (string, string) { return "", "text_delta" })
	require.NoError(t, err)

	body := rec.Body.String()
	require.Equal(t, 2, strings.Count(body, "event: text_delta"))
	require.Contains(t, body, ": ping")
}

func TestPump_ReturnsStreamError(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.New(rec)
	require.NoError(t, err)

	boom := errors.New("boom")
	err = sse.Pump(context.Background(), w, time.Second,
		func() (*corev1.TextDelta, error) { return nil, boom },
		func(*corev1.TextDelta) (string, string) { return "", "x" })
	require.ErrorIs(t, err, boom)
}

func TestPump_StopsOnContextCancel(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.New(rec)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	done := make(chan error, 1)
	go func() {
		done <- sse.Pump(ctx, w, time.Hour,
			func() (*corev1.TextDelta, error) { <-block; return nil, io.EOF },
			func(*corev1.TextDelta) (string, string) { return "", "x" })
	}()
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Pump не завершился после отмены контекста")
	}
}
