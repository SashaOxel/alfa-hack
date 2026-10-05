// Package gateway — HTTP-слой core: grpc-gateway (REST), ручные SSE-хендлеры, health и middleware (ADR-0002).
// Все пути ведут в gRPC-сервер через loopback-соединение, поэтому auth, request-id и логирование
// работают в одних и тех же interceptors.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	corev1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/core/v1"
	"github.com/sashaoxel/alfa-hack/backend/internal/modules/auth"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/requestid"
)

type Options struct {
	Logger *slog.Logger
	// GRPCAddr — адрес gRPC-сервера core (loopback), например 127.0.0.1:9090.
	GRPCAddr string
	// SSEHeartbeat — период комментария `: ping` в SSE.
	SSEHeartbeat time.Duration
	// Ready — проверки для /readyz (БД, ml…).
	Ready []ReadyCheck
}

// Gateway — http.Handler плюс освобождение соединения с gRPC.
type Gateway struct {
	http.Handler
	conn *grpc.ClientConn
}

func (g *Gateway) Close() error { return g.conn.Close() }

// New собирает HTTP-обработчик. Соединение с gRPC устанавливается лениво.
func New(ctx context.Context, o Options) (*Gateway, error) {
	conn, err := grpc.NewClient(o.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("gateway: dial %s: %w", o.GRPCAddr, err)
	}

	mux := runtime.NewServeMux(
		// JSON как в docs/04-api/api.md: lowerCamelCase, enum строками, нулевые значения явно.
		// int64 protojson отдаёт строкой — это часть контракта.
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{EmitUnpopulated: true},
		}),
		runtime.WithIncomingHeaderMatcher(incomingHeader),
		runtime.WithOutgoingHeaderMatcher(outgoingHeader),
		runtime.WithForwardResponseOption(forwardSetCookie),
	)

	register := []func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error{
		corev1.RegisterAuthServiceHandler,
		corev1.RegisterClientServiceHandler,
		corev1.RegisterCashflowServiceHandler,
		corev1.RegisterConsentServiceHandler,
		corev1.RegisterOfferServiceHandler,
		corev1.RegisterApplicationServiceHandler,
		corev1.RegisterDocumentServiceHandler,
		corev1.RegisterChatServiceHandler,
		corev1.RegisterDemoServiceHandler,
	}
	for _, reg := range register {
		if err := reg(ctx, mux, conn); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("gateway: register handler: %w", err)
		}
	}

	h := &sseHandlers{
		log:       o.Logger,
		chat:      corev1.NewChatServiceClient(conn),
		events:    corev1.NewEventsServiceClient(conn),
		heartbeat: o.SSEHeartbeat,
	}
	for _, r := range []struct {
		method, path string
		fn           runtime.HandlerFunc
	}{
		{http.MethodPost, "/api/v1/chat/sessions/{session_id}/messages", h.chatMessages},
		{http.MethodGet, "/api/v1/events", h.clientEvents},
		{http.MethodGet, "/healthz", healthz},
		{http.MethodGet, "/readyz", readyz(o.Ready)},
	} {
		if err := mux.HandlePath(r.method, r.path, r.fn); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("gateway: handle %s: %w", r.path, err)
		}
	}

	return &Gateway{Handler: chain(mux, o.Logger), conn: conn}, nil
}

// incomingHeader решает, какие HTTP-заголовки попадают в gRPC-метаданные.
func incomingHeader(key string) (string, bool) {
	switch http.CanonicalHeaderKey(key) {
	case "Cookie", "X-Requested-With", "Idempotency-Key", requestid.Header:
		return strings.ToLower(key), true
	}
	return runtime.DefaultHeaderMatcher(key)
}

// outgoingHeader не пропускает служебные заголовки наружу как Grpc-Metadata-*.
func outgoingHeader(key string) (string, bool) {
	if strings.EqualFold(key, auth.SetCookieMetadataKey) {
		return "", false
	}
	return runtime.MetadataHeaderPrefix + key, true
}

// forwardSetCookie превращает gRPC-заголовок x-set-cookie (auth.SetCookie) в HTTP Set-Cookie.
func forwardSetCookie(ctx context.Context, w http.ResponseWriter, _ proto.Message) error {
	md, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		return nil
	}
	for _, c := range md.HeaderMD.Get(auth.SetCookieMetadataKey) {
		w.Header().Add("Set-Cookie", c)
	}
	return nil
}

// outgoingContext переносит нужные заголовки HTTP-запроса в исходящие метаданные gRPC
// (для ручных хендлеров, которые вызывают gRPC-клиент сами).
func outgoingContext(r *http.Request) context.Context {
	md := metadata.MD{}
	for _, h := range []string{"Cookie", "X-Requested-With", "Idempotency-Key"} {
		if v := r.Header.Values(h); len(v) > 0 {
			md.Set(strings.ToLower(h), v...)
		}
	}
	if id := requestid.From(r.Context()); id != "" {
		md.Set(requestid.MetadataKey, id)
	}
	return metadata.NewOutgoingContext(r.Context(), md)
}
