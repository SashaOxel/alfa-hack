// Package grpcserver собирает gRPC-сервер core с цепочкой interceptors:
// recover → request-id → логирование → аутентификация.
package grpcserver

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	"github.com/sashaoxel/alfa-hack/backend/internal/modules/auth"
)

// TokenParser проверяет сессионный токен. Реализует *auth.Tokens.
type TokenParser interface {
	Parse(token string) (auth.Identity, error)
}

type Options struct {
	Logger *slog.Logger
	Tokens TokenParser
	// PublicMethods — полные имена методов (/alfa.core.v1.AuthService/RequestOtp), доступные без сессии.
	PublicMethods []string
}

// New создаёт сервер; сервисы регистрирует вызывающий.
func New(o Options) *grpc.Server {
	i := &interceptors{
		log:    o.Logger,
		tokens: o.Tokens,
		public: make(map[string]struct{}, len(o.PublicMethods)),
	}
	for _, m := range o.PublicMethods {
		i.public[m] = struct{}{}
	}
	return grpc.NewServer(
		grpc.ChainUnaryInterceptor(i.unaryRecover, i.unaryRequestID, i.unaryLog, i.unaryAuth),
		grpc.ChainStreamInterceptor(i.streamRecover, i.streamRequestID, i.streamLog, i.streamAuth),
	)
}

// ctxStream подменяет контекст серверного стрима (interceptor-ы кладут в него request-id и личность).
type ctxStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *ctxStream) Context() context.Context { return s.ctx }
