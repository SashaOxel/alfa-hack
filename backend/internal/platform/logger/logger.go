// Package logger настраивает slog. PII (телефоны, ИНН, ФИО) в логи не попадает:
// значения известных полей и телефоны внутри строк маскируются.
package logger

import (
	"io"
	"log/slog"
	"regexp"
	"strings"
)

// phoneRe ловит телефон в свободном тексте: +7 900 000-00-01, 89000000001, +7(900)000-00-01.
var phoneRe = regexp.MustCompile(`(?:\+7|8)[\s\-()]*\d{3}[\s\-()]*\d{3}[\s\-]*\d{2}[\s\-]*\d{2}`)

// sensitiveKeys — поля, значения которых скрываются целиком.
var sensitiveKeys = map[string]struct{}{
	"phone": {}, "inn": {}, "full_name": {}, "owner_full_name": {},
	"fio": {}, "code": {}, "otp": {}, "token": {}, "password": {}, "secret": {},
}

// New создаёт логгер: format — text | json, level — debug | info | warn | error.
func New(w io.Writer, format, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level), ReplaceAttr: maskAttr}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.ToLower(s))); err != nil {
		return slog.LevelInfo
	}
	return l
}

func maskAttr(_ []string, a slog.Attr) slog.Attr {
	if _, ok := sensitiveKeys[strings.ToLower(a.Key)]; ok {
		a.Value = slog.StringValue("***")
		return a
	}
	if a.Value.Kind() == slog.KindString {
		a.Value = slog.StringValue(MaskString(a.Value.String()))
	}
	return a
}

// MaskString заменяет телефоны в строке: +7 900 000-00-01 → +7 *** ***-**-01.
func MaskString(s string) string {
	return phoneRe.ReplaceAllStringFunc(s, func(m string) string {
		digits := make([]byte, 0, 11)
		for i := 0; i < len(m); i++ {
			if m[i] >= '0' && m[i] <= '9' {
				digits = append(digits, m[i])
			}
		}
		return "+7 *** ***-**-" + string(digits[len(digits)-2:])
	})
}
