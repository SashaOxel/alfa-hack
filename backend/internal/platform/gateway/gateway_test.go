package gateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	corev1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/core/v1"
	"github.com/sashaoxel/alfa-hack/backend/internal/modules/auth"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/apperr"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/gateway"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/grpcserver"
)

const secret = "test-secret-0123456789"

// --- заглушки сервисов ------------------------------------------------------------------------------

type authSvc struct {
	corev1.UnimplementedAuthServiceServer
	tokens *auth.Tokens
}

func (s authSvc) VerifyOtp(ctx context.Context, _ *corev1.VerifyOtpRequest) (*corev1.VerifyOtpResponse, error) {
	tok, exp, err := s.tokens.Issue(auth.Identity{UserID: "u-1", ClientID: "c-1"})
	if err != nil {
		return nil, err
	}
	if err := auth.SetCookie(ctx, auth.SessionCookie(tok, exp, false)); err != nil {
		return nil, err
	}
	return &corev1.VerifyOtpResponse{User: &corev1.User{UserId: "u-1"}}, nil
}

func (authSvc) GetMe(ctx context.Context, _ *corev1.GetMeRequest) (*corev1.GetMeResponse, error) {
	return &corev1.GetMeResponse{User: &corev1.User{UserId: auth.UserID(ctx), BusinessName: auth.ClientID(ctx)}}, nil
}

func (authSvc) RequestOtp(context.Context, *corev1.RequestOtpRequest) (*corev1.RequestOtpResponse, error) {
	return nil, apperr.New(codes.ResourceExhausted, apperr.ReasonOTPRateLimited, "слишком часто")
}

type offerSvc struct {
	corev1.UnimplementedOfferServiceServer
}

func (offerSvc) ListOffers(context.Context, *corev1.ListOffersRequest) (*corev1.ListOffersResponse, error) {
	return &corev1.ListOffersResponse{Offers: []*corev1.Offer{{ProductId: "equipment_loan", AmountKop: 200000000, Badge: corev1.Badge_BADGE_BEST}}}, nil
}

type chatSvc struct {
	corev1.UnimplementedChatServiceServer
}

func (chatSvc) SendMessage(req *corev1.SendMessageRequest, stream corev1.ChatService_SendMessageServer) error {
	if req.Text == "boom" {
		return apperr.New(codes.Unavailable, apperr.ReasonLLMUnavailable, "модель недоступна")
	}
	for _, ev := range []*corev1.AssistantEvent{
		{Event: &corev1.AssistantEvent_MessageStart{MessageStart: &corev1.MessageStart{MessageId: "m1"}}},
		{Event: &corev1.AssistantEvent_TextDelta{TextDelta: &corev1.TextDelta{Text: req.SessionId + ":" + req.Text}}},
		{Event: &corev1.AssistantEvent_MessageEnd{MessageEnd: &corev1.MessageEnd{MessageId: "m1"}}},
	} {
		if err := stream.Send(ev); err != nil {
			return err
		}
	}
	return nil
}

type eventsSvc struct {
	corev1.UnimplementedEventsServiceServer
	lastID chan string
}

func (s eventsSvc) Stream(req *corev1.StreamRequest, stream corev1.EventsService_StreamServer) error {
	s.lastID <- req.LastEventId
	err := stream.Send(&corev1.ClientEvent{EventId: "7", Event: &corev1.ClientEvent_ApplicationStatus{
		ApplicationStatus: &corev1.ApplicationStatusChanged{ApplicationId: "a-1", StatusTo: corev1.ApplicationStatus_APPLICATION_STATUS_SCORING},
	}})
	if err != nil {
		return err
	}
	<-stream.Context().Done() // стрим живёт, пока клиент подключён
	return nil
}

// --- стенд ------------------------------------------------------------------------------------------

type env struct {
	url    string
	tokens *auth.Tokens
	lastID chan string
	ready  *readyFlag
}

type readyFlag struct{ err error }

func newEnv(t *testing.T) *env {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := auth.NewTokens(secret, time.Hour)
	e := &env{tokens: tokens, lastID: make(chan string, 4), ready: &readyFlag{}}

	srv := grpcserver.New(grpcserver.Options{
		Logger: log, Tokens: tokens,
		PublicMethods: []string{corev1.AuthService_RequestOtp_FullMethodName, corev1.AuthService_VerifyOtp_FullMethodName},
	})
	corev1.RegisterAuthServiceServer(srv, authSvc{tokens: tokens})
	corev1.RegisterOfferServiceServer(srv, offerSvc{})
	corev1.RegisterChatServiceServer(srv, chatSvc{})
	corev1.RegisterEventsServiceServer(srv, eventsSvc{lastID: e.lastID})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	gw, err := gateway.New(context.Background(), gateway.Options{
		Logger: log, GRPCAddr: lis.Addr().String(), SSEHeartbeat: 30 * time.Millisecond,
		Ready: []gateway.ReadyCheck{{Name: "ml", Check: func(context.Context) error { return e.ready.err }}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gw.Close() })

	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	e.url = ts.URL
	return e
}

func (e *env) cookie(t *testing.T) *http.Cookie {
	t.Helper()
	tok, exp, err := e.tokens.Issue(auth.Identity{UserID: "u-1", ClientID: "c-1"})
	require.NoError(t, err)
	return auth.SessionCookie(tok, exp, false)
}

func (e *env) do(t *testing.T, method, path, body string, mod func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, e.url+path, strings.NewReader(body))
	require.NoError(t, err)
	if mod != nil {
		mod(req)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c); r.Header.Set("X-Requested-With", "fetch") }
}

func decode(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
	return m
}

// reasonOf достаёт ErrorInfo.reason из тела ошибки {code, message, details[]}.
func reasonOf(t *testing.T, body map[string]any) string {
	t.Helper()
	details, _ := body["details"].([]any)
	for _, d := range details {
		if m, ok := d.(map[string]any); ok && m["reason"] != nil {
			return m["reason"].(string)
		}
	}
	return ""
}

// --- REST -------------------------------------------------------------------------------------------

func TestREST_RequiresSession(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodGet, "/api/v1/me", "", nil)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.NotEmpty(t, resp.Header.Get("X-Request-Id"))
	body := decode(t, resp)
	require.Equal(t, apperr.ReasonUnauthenticated, reasonOf(t, body))
	require.NotEmpty(t, body["message"])
}

func TestREST_RejectsGarbageAndForeignCookie(t *testing.T) {
	e := newEnv(t)
	for name, c := range map[string]*http.Cookie{
		"garbage": {Name: "session", Value: "not.a.jwt"},
		"foreign": func() *http.Cookie {
			tok, exp, _ := auth.NewTokens("another-secret-key-1", time.Hour).Issue(auth.Identity{UserID: "u", ClientID: "c"})
			return auth.SessionCookie(tok, exp, false)
		}(),
	} {
		resp := e.do(t, http.MethodGet, "/api/v1/me", "", withCookie(c))
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
	}
}

func TestREST_ClientIDComesFromJWT(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodGet, "/api/v1/me?clientId=evil", "", withCookie(e.cookie(t)))

	require.Equal(t, http.StatusOK, resp.StatusCode)
	user := decode(t, resp)["user"].(map[string]any)
	require.Equal(t, "u-1", user["userId"])
	require.Equal(t, "c-1", user["businessName"], "client_id должен быть из токена, а не из запроса")
}

func TestREST_CSRFHeaderRequiredForMutations(t *testing.T) {
	e := newEnv(t)

	resp := e.do(t, http.MethodPost, "/api/v1/auth/verify", `{"otpId":"x","code":"1234"}`, nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Equal(t, apperr.ReasonCSRF, reasonOf(t, decode(t, resp)))

	resp = e.do(t, http.MethodPost, "/api/v1/auth/verify", `{"otpId":"x","code":"1234"}`,
		func(r *http.Request) { r.Header.Set("X-Requested-With", "fetch") })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// GET без заголовка разрешён
	resp = e.do(t, http.MethodGet, "/api/v1/offers", "", withCookie(e.cookie(t)))
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestREST_SetCookieFromGRPC(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodPost, "/api/v1/auth/verify", `{"otpId":"x","code":"1234"}`,
		func(r *http.Request) { r.Header.Set("X-Requested-With", "fetch") })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cookies := resp.Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "session", cookies[0].Name)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
	require.Equal(t, "/api", cookies[0].Path)
	require.Empty(t, resp.Header.Get("Grpc-Metadata-X-Set-Cookie"), "служебный заголовок не должен утекать клиенту")

	// выданной cookie достаточно для защищённого метода
	me := e.do(t, http.MethodGet, "/api/v1/me", "", withCookie(cookies[0]))
	require.Equal(t, http.StatusOK, me.StatusCode)
}

func TestREST_JSONConventions(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodGet, "/api/v1/offers", "", withCookie(e.cookie(t)))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	offer := decode(t, resp)["offers"].([]any)[0].(map[string]any)
	require.Equal(t, "200000000", offer["amountKop"], "int64 — строкой")
	require.Equal(t, "BADGE_BEST", offer["badge"], "enum — строкой")
	require.Equal(t, "equipment_loan", offer["productId"], "lowerCamelCase")
	require.Contains(t, offer, "rateBp", "нулевые значения приходят явно (EmitUnpopulated)")
	require.EqualValues(t, 0, offer["rateBp"], "int32 — числом")
}

func TestREST_ErrorBodyAndStatusMapping(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodPost, "/api/v1/auth/otp", `{"phone":"+79000000001"}`,
		func(r *http.Request) { r.Header.Set("X-Requested-With", "fetch") })

	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	body := decode(t, resp)
	require.Equal(t, apperr.ReasonOTPRateLimited, reasonOf(t, body))
	require.EqualValues(t, codes.ResourceExhausted, body["code"])
}

func TestREST_UnimplementedIs501(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodGet, "/api/v1/consents", "", withCookie(e.cookie(t)))
	require.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}

func TestREST_RequestIDPropagation(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodGet, "/api/v1/me", "", func(r *http.Request) { r.Header.Set("X-Request-Id", "my-trace-1") })
	require.Equal(t, "my-trace-1", resp.Header.Get("X-Request-Id"))

	resp = e.do(t, http.MethodGet, "/api/v1/me", "", func(r *http.Request) { r.Header.Set("X-Request-Id", "bad id with spaces") })
	require.NotContains(t, resp.Header.Get("X-Request-Id"), "bad")
	require.NotEmpty(t, resp.Header.Get("X-Request-Id"))
}

// --- health -----------------------------------------------------------------------------------------

func TestHealth(t *testing.T) {
	e := newEnv(t)
	require.Equal(t, http.StatusOK, e.do(t, http.MethodGet, "/healthz", "", nil).StatusCode)

	resp := e.do(t, http.MethodGet, "/readyz", "", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "ok", decode(t, resp)["status"])

	e.ready.err = errors.New("ml недоступен")
	resp = e.do(t, http.MethodGet, "/readyz", "", nil)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body := decode(t, resp)
	require.Equal(t, "unavailable", body["status"])
	require.Equal(t, "ml недоступен", body["checks"].(map[string]any)["ml"])
}

// --- SSE --------------------------------------------------------------------------------------------

type sseFrame struct{ id, event, data string }

// readFrames читает SSE-кадры, пока stop не вернёт true; комментарии (`: ping`) приходят как event="ping".
func readFrames(t *testing.T, r io.Reader, stop func([]sseFrame) bool) []sseFrame {
	t.Helper()
	var (
		frames []sseFrame
		cur    sseFrame
		sc     = bufio.NewScanner(r)
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur != (sseFrame{}) {
					frames = append(frames, cur)
					cur = sseFrame{}
				}
				if stop(frames) {
					return
				}
			case strings.HasPrefix(line, ": "):
				cur.event = strings.TrimPrefix(line, ": ")
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("SSE: не дождались кадров, получено: %+v", frames)
	}
	return frames
}

func names(frames []sseFrame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = f.event
	}
	return out
}

func TestSSE_ChatStream(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodPost, "/api/v1/chat/sessions/s-9/messages", `{"text":"привет"}`, withCookie(e.cookie(t)))

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	require.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	frames := readFrames(t, resp.Body, func(f []sseFrame) bool { return len(f) >= 3 })
	require.Equal(t, []string{"message_start", "text_delta", "message_end"}, names(frames))
	require.Contains(t, frames[1].data, `"s-9:привет"`, "session_id берётся из пути")
}

func TestSSE_ChatBodyCannotOverrideSessionID(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodPost, "/api/v1/chat/sessions/s-9/messages", `{"sessionId":"other","text":"x"}`, withCookie(e.cookie(t)))
	frames := readFrames(t, resp.Body, func(f []sseFrame) bool { return len(f) >= 3 })
	require.Contains(t, frames[1].data, `"s-9:x"`)
}

func TestSSE_ChatRequiresSessionAsHTTP401(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodPost, "/api/v1/chat/sessions/s-9/messages", `{"text":"x"}`,
		func(r *http.Request) { r.Header.Set("X-Requested-With", "fetch") })

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "отказ — обычный HTTP-ответ, а не пустой SSE")
	require.NotEqual(t, "text/event-stream", resp.Header.Get("Content-Type"))
}

func TestSSE_ChatBadBody(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodPost, "/api/v1/chat/sessions/s-9/messages", `{"text":`, withCookie(e.cookie(t)))
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, apperr.ReasonValidation, reasonOf(t, decode(t, resp)))
}

func TestSSE_ChatErrorAfterStartBecomesErrorEvent(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodPost, "/api/v1/chat/sessions/s-9/messages", `{"text":"boom"}`, withCookie(e.cookie(t)))

	require.Equal(t, http.StatusOK, resp.StatusCode, "стрим уже открыт, HTTP-код не меняется")
	frames := readFrames(t, resp.Body, func(f []sseFrame) bool { return len(f) >= 1 })
	require.Equal(t, []string{"error"}, names(frames))
	require.Contains(t, frames[0].data, apperr.ReasonLLMUnavailable)
}

func TestSSE_ClientEvents(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodGet, "/api/v1/events", "", func(r *http.Request) {
		withCookie(e.cookie(t))(r)
		r.Header.Set("Last-Event-ID", "5")
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case id := <-e.lastID:
		require.Equal(t, "5", id, "Last-Event-ID доходит до сервиса")
	case <-time.After(2 * time.Second):
		t.Fatal("сервис не получил Last-Event-ID")
	}

	frames := readFrames(t, resp.Body, func(f []sseFrame) bool {
		return len(f) >= 2 // событие + heartbeat в паузе
	})
	require.Equal(t, "7", frames[0].id)
	require.Equal(t, "application.status", frames[0].event, "application_status → application.status")
	require.Contains(t, frames[0].data, `"applicationId":"a-1"`)
	require.Equal(t, "ping", frames[1].event)
}

func TestSSE_ClientEventsRequireSession(t *testing.T) {
	e := newEnv(t)
	resp := e.do(t, http.MethodGet, "/api/v1/events", "", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
