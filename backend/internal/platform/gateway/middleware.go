package gateway

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/sashaoxel/alfa-hack/backend/internal/platform/apperr"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/requestid"
)

// chain: recover → request-id → access log → CSRF → mux.
func chain(next http.Handler, log *slog.Logger) http.Handler {
	return recoverMW(log, requestIDMW(accessLogMW(log, csrfMW(next))))
}

func recoverMW(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.ErrorContext(r.Context(), "panic in http handler",
					"path", r.URL.Path, "request_id", requestid.From(r.Context()), "panic", rec, "stack", string(debug.Stack()))
				writeError(w, apperr.Internal())
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestIDMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestid.Header)
		if !requestid.Valid(id) {
			id = requestid.New()
		}
		w.Header().Set(requestid.Header, id)
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}

// statusWriter запоминает код ответа. Unwrap нужен http.ResponseController, чтобы SSE мог делать Flush.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func accessLogMW(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		level := slog.LevelInfo
		switch {
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
			level = slog.LevelDebug
		case sw.status >= 500:
			level = slog.LevelError
		}
		// query в лог не пишем: там могут быть персональные данные
		log.Log(r.Context(), level, "http request",
			"method", r.Method, "path", r.URL.Path, "http_status", sw.status,
			"duration_ms", time.Since(started).Milliseconds(), "bytes", sw.bytes,
			"request_id", requestid.From(r.Context()))
	})
}

// csrfMW требует заголовок X-Requested-With для мутирующих запросов к API: вместе с SameSite=Strict
// он закрывает CSRF (форма с чужого сайта не может его выставить).
func csrfMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("X-Requested-With") == "" {
				writeError(w, apperr.New(codes.PermissionDenied, apperr.ReasonCSRF, "не хватает заголовка X-Requested-With"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// writeError пишет ошибку в том же формате, что и gateway: тело — google.rpc.Status.
func writeError(w http.ResponseWriter, err error) {
	st := status.Convert(err)
	body, mErr := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(st.Proto())
	if mErr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(runtime.HTTPStatusFromCode(st.Code()))
	_, _ = w.Write(body)
}
