// Package apperr строит gRPC-ошибки в формате API: google.rpc.Status + ErrorInfo.reason.
// Gateway отдаёт их как {code, message, details[]} с HTTP-кодом по коду gRPC (docs/04-api/api.md).
package apperr

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Domain — значение ErrorInfo.domain.
const Domain = "alfa-hack"

// Коды причин (ErrorInfo.reason): по ним фронт различает ошибки одного HTTP-кода.
const (
	ReasonUnauthenticated = "UNAUTHENTICATED"
	ReasonCSRF            = "CSRF"
	ReasonValidation      = "VALIDATION"
	ReasonNotFound        = "NOT_FOUND"
	ReasonInternal        = "INTERNAL"
	ReasonOTPInvalid      = "OTP_INVALID"
	ReasonOTPExpired      = "OTP_EXPIRED"
	ReasonOTPRateLimited  = "OTP_RATE_LIMITED"
	ReasonStopFactor      = "STOP_FACTOR"
	ReasonLLMUnavailable  = "LLM_UNAVAILABLE"
)

// New возвращает gRPC-ошибку с кодом, причиной и сообщением для пользователя.
func New(code codes.Code, reason, msg string) error {
	st := status.New(code, msg)
	withInfo, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: Domain})
	if err != nil {
		return st.Err()
	}
	return withInfo.Err()
}

// Validation — ошибка проверки входа с указанием поля.
func Validation(field, msg string) error {
	st := status.New(codes.InvalidArgument, msg)
	withDetails, err := st.WithDetails(
		&errdetails.ErrorInfo{Reason: ReasonValidation, Domain: Domain},
		&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: field, Description: msg},
		}},
	)
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

// Internal скрывает детали от клиента: причина остаётся только в логе.
func Internal() error {
	return New(codes.Internal, ReasonInternal, "внутренняя ошибка")
}

// Reason достаёт ErrorInfo.reason; пустая строка, если её нет.
func Reason(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

// Code возвращает код gRPC; для не-gRPC ошибок — Unknown, для nil — OK.
func Code(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	var s interface{ GRPCStatus() *status.Status }
	if errors.As(err, &s) {
		return s.GRPCStatus().Code()
	}
	return codes.Unknown
}
