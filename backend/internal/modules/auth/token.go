package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken — токен не подписан нашим ключом, просрочен или повреждён.
var ErrInvalidToken = errors.New("auth: invalid token")

type claims struct {
	ClientID string `json:"cid"`
	jwt.RegisteredClaims
}

// Tokens выпускает и проверяет сессионные JWT (HS256, sub=user_id, cid=client_id).
type Tokens struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokens(secret string, ttl time.Duration) *Tokens {
	return &Tokens{secret: []byte(secret), ttl: ttl, now: time.Now}
}

// Issue выпускает токен и возвращает время его истечения.
func (t *Tokens) Issue(id Identity) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		ClientID: id.ClientID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	})
	s, err := tok.SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return s, exp, nil
}

// Parse проверяет подпись, алгоритм и срок; возвращает личность из токена.
func (t *Tokens) Parse(token string) (Identity, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if c.Subject == "" || c.ClientID == "" {
		return Identity{}, fmt.Errorf("%w: empty subject or client", ErrInvalidToken)
	}
	return Identity{UserID: c.Subject, ClientID: c.ClientID}, nil
}
