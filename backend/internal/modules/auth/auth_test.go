package auth_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/sashaoxel/alfa-hack/backend/internal/modules/auth"
)

var id = auth.Identity{UserID: "u-1", ClientID: "c-1"}

func TestTokens_RoundTrip(t *testing.T) {
	tk := auth.NewTokens("0123456789abcdef", time.Hour)
	tok, exp, err := tk.Issue(id)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), exp, 5*time.Second)

	got, err := tk.Parse(tok)
	require.NoError(t, err)
	require.Equal(t, id, got)
}

func TestTokens_Rejects(t *testing.T) {
	tk := auth.NewTokens("0123456789abcdef", time.Hour)
	good, _, err := tk.Issue(id)
	require.NoError(t, err)

	expired, _, err := auth.NewTokens("0123456789abcdef", -time.Minute).Issue(id)
	require.NoError(t, err)

	otherKey, _, err := auth.NewTokens("another-secret-key", time.Hour).Issue(id)
	require.NoError(t, err)

	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "u-1", "cid": "c-1", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	noClient, _, err := tk.Issue(auth.Identity{UserID: "u-1"})
	require.NoError(t, err)

	for name, tok := range map[string]string{
		"expired": expired, "wrong key": otherKey, "alg none": none, "no client": noClient,
		"garbage": "abc.def.ghi", "empty": "", "tampered": good + "x",
	} {
		_, err := tk.Parse(tok)
		require.ErrorIs(t, err, auth.ErrInvalidToken, name)
	}
}

func TestContext(t *testing.T) {
	ctx := context.Background()
	require.Empty(t, auth.ClientID(ctx))

	ctx = auth.WithIdentity(ctx, id)
	require.Equal(t, "c-1", auth.ClientID(ctx))
	require.Equal(t, "u-1", auth.UserID(ctx))
}

func TestSessionCookie(t *testing.T) {
	c := auth.SessionCookie("tok", time.Now().Add(time.Hour), true)
	require.True(t, c.HttpOnly)
	require.True(t, c.Secure)
	require.Equal(t, http.SameSiteStrictMode, c.SameSite)
	require.Equal(t, "/api", c.Path)

	require.Negative(t, auth.ClearCookie(false).MaxAge)
}

func TestTokenFromCookieHeader(t *testing.T) {
	tok, ok := auth.TokenFromCookieHeader("theme=dark; session=abc; x=y")
	require.True(t, ok)
	require.Equal(t, "abc", tok)

	_, ok = auth.TokenFromCookieHeader("theme=dark")
	require.False(t, ok)
	_, ok = auth.TokenFromCookieHeader("session=")
	require.False(t, ok)
}
