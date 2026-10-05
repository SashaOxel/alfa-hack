// Package mlclient — клиент к ml-сервису (Python, только инференс).
// Реализации: grpc (боевая) и fake (детерминированные скоры, пока модели не обучены, и для тестов).
package mlclient

import (
	"context"

	mlv1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/ml/v1"
)

// Client — всё, что core просит у моделей. Бизнес-правил здесь нет (ADR-0003).
type Client interface {
	// Score считает скор по бандлу для каждого сценария (минимум один).
	Score(ctx context.Context, bundle *mlv1.ClientDataBundle, scenarios []*mlv1.Scenario) (*mlv1.ScoreResponse, error)
	// Forecast строит прогноз остатка (p10/p50/p90) и находит регулярные платежи.
	Forecast(ctx context.Context, req *mlv1.ForecastRequest) (*mlv1.ForecastResponse, error)
	// Categorize классифицирует пачку транзакций.
	Categorize(ctx context.Context, items []*mlv1.TxInput) (*mlv1.CategorizeResponse, error)
	// ModelInfo — версии и метрики моделей; используется и как проверка готовности.
	ModelInfo(ctx context.Context) (*mlv1.GetModelInfoResponse, error)
}
