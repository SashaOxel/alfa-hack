// Package sse — запись Server-Sent Events поверх http.ResponseWriter (ADR-0002).
package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var marshal = protojson.MarshalOptions{EmitUnpopulated: true}

// Writer пишет события в ответ; безопасен для параллельных вызовов (события + heartbeat).
type Writer struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

// New отправляет заголовки и сразу сбрасывает буфер: клиент видит открытое соединение.
// Заголовок X-Accel-Buffering отключает буферизацию в nginx.
func New(w http.ResponseWriter) (*Writer, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &Writer{w: w, rc: http.NewResponseController(w)}
	if err := s.rc.Flush(); err != nil {
		return nil, fmt.Errorf("sse: flush: %w", err)
	}
	return s, nil
}

// Event пишет событие: id (необязательно), имя и protojson-данные одной строкой.
func (s *Writer) Event(id, name string, data proto.Message) error {
	b, err := marshal.Marshal(data)
	if err != nil {
		return fmt.Errorf("sse: marshal %s: %w", name, err)
	}
	var frame []byte
	if id != "" {
		frame = fmt.Appendf(frame, "id: %s\n", sanitize(id))
	}
	frame = fmt.Appendf(frame, "event: %s\ndata: %s\n\n", sanitize(name), b)
	return s.write(frame)
}

// Ping — комментарий-heartbeat, держит соединение живым через прокси.
func (s *Writer) Ping() error {
	return s.write([]byte(": ping\n\n"))
}

func (s *Writer) write(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(b); err != nil {
		return err
	}
	return s.rc.Flush()
}

// sanitize убирает переводы строк из id и имени события (защита от инъекции полей).
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' && s[i] != '\r' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// Pump читает сообщения через recv и пишет их как события, а в паузах шлёт heartbeat.
// Возвращает nil на io.EOF и при отмене ctx; любая другая ошибка recv возвращается вызывающему.
// meta определяет id и имя события по сообщению.
func Pump[T proto.Message](ctx context.Context, w *Writer, heartbeat time.Duration, recv func() (T, error), meta func(T) (id, name string)) error {
	type result struct {
		msg T
		err error
	}
	ch := make(chan result)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			msg, err := recv()
			select {
			case ch <- result{msg, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := w.Ping(); err != nil {
				return nil // клиент ушёл
			}
		case r := <-ch:
			if r.err != nil {
				if errors.Is(r.err, io.EOF) || ctx.Err() != nil {
					return nil
				}
				return r.err
			}
			id, name := meta(r.msg)
			if err := w.Event(id, name, r.msg); err != nil {
				return nil // клиент ушёл
			}
			tick.Reset(heartbeat)
		}
	}
}

// OneofName возвращает имя заполненного варианта oneof (например, "text_delta" для AssistantEvent).
func OneofName(msg proto.Message, oneof protoreflect.Name) string {
	m := msg.ProtoReflect()
	od := m.Descriptor().Oneofs().ByName(oneof)
	if od == nil {
		return ""
	}
	if fd := m.WhichOneof(od); fd != nil {
		return string(fd.Name())
	}
	return ""
}
