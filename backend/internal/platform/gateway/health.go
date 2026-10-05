package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// ReadyCheck — проверка готовности зависимости (БД, ml). Должна уложиться в таймаут контекста.
type ReadyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

const readyTimeout = 2 * time.Second

func healthz(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func readyz(checks []ReadyCheck) runtime.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		results := make(map[string]string, len(checks))
		ready := true
		for _, c := range checks {
			if err := c.Check(ctx); err != nil {
				ready = false
				results[c.Name] = err.Error()
				continue
			}
			results[c.Name] = "ok"
		}

		names := make([]string, 0, len(results))
		for n := range results {
			names = append(names, n)
		}
		sort.Strings(names) // стабильный вывод для людей и тестов

		status, code := "ok", http.StatusOK
		if !ready {
			status, code = "unavailable", http.StatusServiceUnavailable
		}
		body, _ := json.Marshal(map[string]any{"status": status, "checks": results, "order": names})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
	}
}
