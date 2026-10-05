// Package auth — личность клиента в контексте запроса, JWT-сессия и cookie (ADR-0008).
// OTP-флоу и gRPC-сервис AuthService добавляются поверх этих примитивов (задача J2).
package auth

import "context"

// Identity — кто делает запрос. Заполняется только interceptor-ом из проверенного JWT.
type Identity struct {
	UserID   string
	ClientID string // → bank.clients
}

type ctxKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// ClientID — единственный источник client_id для бизнес-логики (никогда не из запроса).
// Пустая строка — запрос не аутентифицирован (метод публичный).
func ClientID(ctx context.Context) string {
	id, _ := IdentityFrom(ctx)
	return id.ClientID
}

// UserID возвращает user_id из контекста или пустую строку.
func UserID(ctx context.Context) string {
	id, _ := IdentityFrom(ctx)
	return id.UserID
}
