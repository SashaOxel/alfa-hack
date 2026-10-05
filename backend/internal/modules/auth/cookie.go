package auth

import (
	"context"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	// CookieName — имя сессионной cookie.
	CookieName = "session"
	cookiePath = "/api"

	// SetCookieMetadataKey — gRPC-заголовок, который gateway превращает в Set-Cookie.
	SetCookieMetadataKey = "x-set-cookie"
)

// SessionCookie — cookie сессии: HttpOnly, SameSite=Strict (CSRF-защита, ADR-0008).
func SessionCookie(token string, expires time.Time, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     cookiePath,
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// ClearCookie сбрасывает сессию (Logout).
func ClearCookie(secure bool) *http.Cookie {
	c := SessionCookie("", time.Unix(0, 0), secure)
	c.MaxAge = -1
	return c
}

// SetCookie вызывается из gRPC-метода: gateway отправит cookie клиенту заголовком Set-Cookie.
func SetCookie(ctx context.Context, c *http.Cookie) error {
	return grpc.SetHeader(ctx, metadata.Pairs(SetCookieMetadataKey, c.String()))
}

// TokenFromCookieHeader достаёт значение сессионной cookie из заголовка Cookie.
func TokenFromCookieHeader(header string) (string, bool) {
	req := http.Request{Header: http.Header{"Cookie": {header}}}
	c, err := req.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}
