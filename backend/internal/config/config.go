// Package config читает конфигурацию core из переменных окружения.
// Список переменных и значения по умолчанию — docs/06-dev/local-setup.md.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	MLClientGRPC = "grpc"
	MLClientFake = "fake"
)

type Config struct {
	HTTPAddr  string `env:"HTTP_ADDR" envDefault:":8080"`
	GRPCAddr  string `env:"GRPC_ADDR" envDefault:":9090"`
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"text"` // text | json

	MLAddr   string `env:"ML_ADDR" envDefault:"ml:50051"`
	MLClient string `env:"ML_CLIENT" envDefault:"grpc"` // grpc | fake

	DemoMode bool `env:"DEMO_MODE" envDefault:"true"`

	JWTSecret    string        `env:"JWT_SECRET"`
	SessionTTL   time.Duration `env:"SESSION_TTL" envDefault:"12h"`
	CookieSecure bool          `env:"COOKIE_SECURE" envDefault:"false"` // true за TLS-терминатором

	SSEHeartbeat time.Duration `env:"SSE_HEARTBEAT" envDefault:"15s"`

	// JWTSecretGenerated — секрет сгенерирован при старте (только DEMO_MODE): сессии не переживут рестарт.
	JWTSecretGenerated bool `env:"-"`
}

// Load читает окружение процесса.
func Load() (Config, error) {
	return parse(env.Options{})
}

// Parse читает конфигурацию из переданной карты (для тестов).
func Parse(environ map[string]string) (Config, error) {
	return parse(env.Options{Environment: environ})
}

func parse(opts env.Options) (Config, error) {
	var c Config
	if err := env.ParseWithOptions(&c, opts); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := c.finalize(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c *Config) finalize() error {
	switch c.MLClient {
	case MLClientGRPC, MLClientFake:
	default:
		return fmt.Errorf("config: ML_CLIENT=%q: допустимо %q или %q", c.MLClient, MLClientGRPC, MLClientFake)
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("config: LOG_FORMAT=%q: допустимо text или json", c.LogFormat)
	}
	if c.SessionTTL <= 0 {
		return errors.New("config: SESSION_TTL должен быть положительным")
	}
	if c.SSEHeartbeat <= 0 {
		return errors.New("config: SSE_HEARTBEAT должен быть положительным")
	}

	if c.JWTSecret == "" {
		if !c.DemoMode {
			return errors.New("config: JWT_SECRET обязателен вне DEMO_MODE")
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return fmt.Errorf("config: generate jwt secret: %w", err)
		}
		c.JWTSecret = hex.EncodeToString(buf)
		c.JWTSecretGenerated = true
	}
	if len(c.JWTSecret) < 16 {
		return errors.New("config: JWT_SECRET короче 16 символов")
	}
	return nil
}
