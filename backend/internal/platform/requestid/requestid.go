// Package requestid хранит идентификатор запроса в контексте. Он попадает в каждую запись лога.
package requestid

import (
	"context"

	"github.com/google/uuid"
)

const (
	// Header — HTTP-заголовок запроса и ответа.
	Header = "X-Request-Id"
	// MetadataKey — ключ gRPC-метаданных.
	MetadataKey = "x-request-id"
)

type ctxKey struct{}

// New генерирует идентификатор (uuid v7: сортируется по времени).
func New() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From возвращает идентификатор запроса или пустую строку.
func From(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Valid — приходящий снаружи id принимаем только безопасной длины и алфавита (защита логов от инъекций).
func Valid(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
