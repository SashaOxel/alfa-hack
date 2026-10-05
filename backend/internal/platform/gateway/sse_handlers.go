package gateway

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	corev1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/core/v1"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/apperr"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/sse"
)

// maxChatBody — предел тела POST /chat/.../messages (текст сообщения + payload действия).
const maxChatBody = 64 << 10

type sseHandlers struct {
	log       *slog.Logger
	chat      corev1.ChatServiceClient
	events    corev1.EventsServiceClient
	heartbeat time.Duration
}

// sseStream — то, что нужно от gRPC-стрима для SSE.
type sseStream struct {
	header func() (metadata.MD, error)
	recv   func() (proto.Message, error)
	// meta определяет id и имя SSE-события по сообщению.
	meta func(proto.Message) (id, name string)
	// onErr превращает ошибку после старта стрима в финальное событие; nil — просто закрыть.
	onErr func(error) proto.Message
}

// chatMessages: POST /api/v1/chat/sessions/{session_id}/messages → SSE с AssistantEvent.
// Тело — {text} или {action:{type,payload}}; session_id берётся из пути.
func (h *sseHandlers) chatMessages(w http.ResponseWriter, r *http.Request, params map[string]string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxChatBody))
	if err != nil {
		writeError(w, apperr.Validation("body", "тело запроса слишком большое или повреждено"))
		return
	}
	req := &corev1.SendMessageRequest{}
	if err := protojson.Unmarshal(body, req); err != nil {
		writeError(w, apperr.Validation("body", "некорректный JSON: "+err.Error()))
		return
	}
	req.SessionId = params["session_id"] // из пути, тело его не определяет

	stream, err := h.chat.SendMessage(outgoingContext(r), req)
	if err != nil {
		writeError(w, err)
		return
	}
	h.serve(w, r, sseStream{
		header: stream.Header,
		recv:   func() (proto.Message, error) { return stream.Recv() },
		meta:   func(m proto.Message) (string, string) { return "", sse.OneofName(m, "event") },
		onErr: func(err error) proto.Message {
			// HTTP-код уже отправлен: ошибку сообщаем событием error
			return &corev1.AssistantEvent{Event: &corev1.AssistantEvent_Error{Error: &corev1.AssistantError{
				Code: errorCode(err), Message: status.Convert(err).Message(),
			}}}
		},
	})
}

// clientEvents: GET /api/v1/events → SSE с ClientEvent. Last-Event-ID передаётся в сервис.
func (h *sseHandlers) clientEvents(w http.ResponseWriter, r *http.Request, _ map[string]string) {
	req := &corev1.StreamRequest{LastEventId: r.Header.Get("Last-Event-ID")}
	stream, err := h.events.Stream(outgoingContext(r), req)
	if err != nil {
		writeError(w, err)
		return
	}
	h.serve(w, r, sseStream{
		header: stream.Header,
		recv:   func() (proto.Message, error) { return stream.Recv() },
		meta: func(m proto.Message) (string, string) {
			// application_status → application.status
			return m.(*corev1.ClientEvent).GetEventId(), strings.Replace(sse.OneofName(m, "event"), "_", ".", 1)
		},
	})
}

// serve ждёт заголовки стрима: отказ interceptor-а (нет сессии) приходит именно здесь и
// становится обычным HTTP-ответом (401), а не пустым SSE. Дальше события идут через sse.Pump.
func (h *sseHandlers) serve(w http.ResponseWriter, r *http.Request, s sseStream) {
	md, err := s.header()
	if err != nil {
		writeError(w, err)
		return
	}
	if len(md) == 0 {
		// стрим закрылся без заголовков (trailers-only): причина отказа — в статусе, его отдаёт Recv
		_, err := s.recv()
		if err == nil {
			err = apperr.Internal()
		}
		writeError(w, err)
		return
	}
	sw, err := sse.New(w)
	if err != nil {
		h.log.ErrorContext(r.Context(), "sse: start", "error", err)
		return
	}
	err = sse.Pump(r.Context(), sw, h.heartbeat, s.recv, s.meta)
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	h.log.WarnContext(r.Context(), "sse: stream failed", "error", err)
	if s.onErr != nil {
		ev := s.onErr(err)
		_, name := s.meta(ev)
		_ = sw.Event("", name, ev)
	}
}

func errorCode(err error) string {
	if r := apperr.Reason(err); r != "" {
		return r
	}
	return status.Convert(err).Code().String()
}
