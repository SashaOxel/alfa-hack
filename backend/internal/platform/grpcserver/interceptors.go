package grpcserver

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	"github.com/sashaoxel/alfa-hack/backend/internal/modules/auth"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/apperr"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/requestid"
)

type interceptors struct {
	log    *slog.Logger
	tokens TokenParser
	public map[string]struct{}
}

// --- recover ---------------------------------------------------------------

func (i *interceptors) recoverPanic(ctx context.Context, method string, err *error) {
	if r := recover(); r != nil {
		i.log.ErrorContext(ctx, "panic in handler",
			"method", method, "request_id", requestid.From(ctx), "panic", r, "stack", string(debug.Stack()))
		*err = apperr.Internal()
	}
}

func (i *interceptors) unaryRecover(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (resp any, err error) {
	defer i.recoverPanic(ctx, info.FullMethod, &err)
	return h(ctx, req)
}

func (i *interceptors) streamRecover(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) (err error) {
	defer i.recoverPanic(ss.Context(), info.FullMethod, &err)
	return h(srv, ss)
}

// --- request-id ------------------------------------------------------------

func withRequestID(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(requestid.MetadataKey); len(v) > 0 && requestid.Valid(v[0]) {
			return requestid.With(ctx, v[0])
		}
	}
	return requestid.With(ctx, requestid.New())
}

func (i *interceptors) unaryRequestID(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	return h(withRequestID(ctx), req)
}

func (i *interceptors) streamRequestID(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	return h(srv, &ctxStream{ServerStream: ss, ctx: withRequestID(ss.Context())})
}

// --- логирование -----------------------------------------------------------

func (i *interceptors) logCall(ctx context.Context, method string, started time.Time, err error) {
	level := slog.LevelInfo
	code := apperr.Code(err)
	switch code {
	case codes.OK, codes.Canceled, codes.NotFound, codes.InvalidArgument, codes.Unauthenticated, codes.AlreadyExists:
	default:
		level = slog.LevelError
	}
	attrs := []any{
		"method", method,
		"grpc_code", code.String(),
		"duration_ms", time.Since(started).Milliseconds(),
		"request_id", requestid.From(ctx),
	}
	if cid := auth.ClientID(ctx); cid != "" {
		attrs = append(attrs, "client_id", cid)
	}
	if err != nil && level == slog.LevelError {
		attrs = append(attrs, "error", err.Error())
	}
	i.log.Log(ctx, level, "grpc call", attrs...)
}

// Личность появляется в контексте только внутри цепочки (auth — последний interceptor),
// поэтому логируем через общий указатель на контекст, который обновляет auth.
type callState struct{ ctx context.Context }

type stateKey struct{}

func withState(ctx context.Context) (context.Context, *callState) {
	st := &callState{ctx: ctx}
	return context.WithValue(ctx, stateKey{}, st), st
}

func (i *interceptors) unaryLog(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	started := time.Now()
	ctx, st := withState(ctx)
	resp, err := h(ctx, req)
	i.logCall(st.ctx, info.FullMethod, started, err)
	return resp, err
}

func (i *interceptors) streamLog(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	started := time.Now()
	ctx, st := withState(ss.Context())
	err := h(srv, &ctxStream{ServerStream: ss, ctx: ctx})
	i.logCall(st.ctx, info.FullMethod, started, err)
	return err
}

// --- аутентификация --------------------------------------------------------

// authenticate возвращает контекст с личностью. Для публичных методов отсутствие
// сессии не ошибка, но валидная сессия всё равно подхватывается.
func (i *interceptors) authenticate(ctx context.Context, method string) (context.Context, error) {
	_, public := i.public[method]
	var token string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for _, c := range md.Get("cookie") {
			if t, ok := auth.TokenFromCookieHeader(c); ok {
				token = t
				break
			}
		}
	}
	if token == "" {
		if public {
			return ctx, nil
		}
		return ctx, apperr.New(codes.Unauthenticated, apperr.ReasonUnauthenticated, "требуется вход")
	}
	id, err := i.tokens.Parse(token)
	if err != nil {
		if public {
			return ctx, nil
		}
		return ctx, apperr.New(codes.Unauthenticated, apperr.ReasonUnauthenticated, "сессия недействительна, войдите снова")
	}
	ctx = auth.WithIdentity(ctx, id)
	if st, ok := ctx.Value(stateKey{}).(*callState); ok {
		st.ctx = ctx
	}
	return ctx, nil
}

func (i *interceptors) unaryAuth(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	ctx, err := i.authenticate(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return h(ctx, req)
}

// streamAuth после успешной проверки сразу отправляет заголовки стрима (непустые: x-request-id).
// HTTP-слой (SSE) по ним узнаёт, что доступ разрешён, и отвечает 200 до первого события.
// Если заголовков нет, стрим закрылся отказом: клиент читает статус через Recv и отдаёт HTTP 401.
func (i *interceptors) streamAuth(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	ctx, err := i.authenticate(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	if err := ss.SendHeader(metadata.Pairs(requestid.MetadataKey, requestid.From(ctx))); err != nil {
		return err
	}
	return h(srv, &ctxStream{ServerStream: ss, ctx: ctx})
}
