package mlclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mlv1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/ml/v1"
)

// Бюджеты латентности из docs/03-architecture/services.md (с запасом на сеть).
const (
	scoreTimeout     = 500 * time.Millisecond
	forecastTimeout  = 1500 * time.Millisecond
	categorizeTimout = 3 * time.Second
	infoTimeout      = 1 * time.Second
)

// GRPC — клиент к ml по gRPC.
type GRPC struct {
	conn       *grpc.ClientConn
	scoring    mlv1.ScoringServiceClient
	forecast   mlv1.ForecastServiceClient
	categorize mlv1.CategorizeServiceClient
	meta       mlv1.MetaServiceClient
}

// NewGRPC создаёт клиент; соединение устанавливается лениво, ml может подняться позже core.
// Дополнительные опции нужны тестам (bufconn).
func NewGRPC(addr string, opts ...grpc.DialOption) (*GRPC, error) {
	if len(opts) == 0 {
		opts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("mlclient: dial %s: %w", addr, err)
	}
	return &GRPC{
		conn:       conn,
		scoring:    mlv1.NewScoringServiceClient(conn),
		forecast:   mlv1.NewForecastServiceClient(conn),
		categorize: mlv1.NewCategorizeServiceClient(conn),
		meta:       mlv1.NewMetaServiceClient(conn),
	}, nil
}

func (c *GRPC) Close() error { return c.conn.Close() }

func (c *GRPC) Score(ctx context.Context, bundle *mlv1.ClientDataBundle, scenarios []*mlv1.Scenario) (*mlv1.ScoreResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, scoreTimeout)
	defer cancel()
	resp, err := c.scoring.Score(ctx, &mlv1.ScoreRequest{Bundle: bundle, Scenarios: scenarios})
	if err != nil {
		return nil, fmt.Errorf("mlclient: score: %w", err)
	}
	return resp, nil
}

func (c *GRPC) Forecast(ctx context.Context, req *mlv1.ForecastRequest) (*mlv1.ForecastResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, forecastTimeout)
	defer cancel()
	resp, err := c.forecast.Forecast(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("mlclient: forecast: %w", err)
	}
	return resp, nil
}

func (c *GRPC) Categorize(ctx context.Context, items []*mlv1.TxInput) (*mlv1.CategorizeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, categorizeTimout)
	defer cancel()
	resp, err := c.categorize.Categorize(ctx, &mlv1.CategorizeRequest{Items: items})
	if err != nil {
		return nil, fmt.Errorf("mlclient: categorize: %w", err)
	}
	return resp, nil
}

func (c *GRPC) ModelInfo(ctx context.Context) (*mlv1.GetModelInfoResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, infoTimeout)
	defer cancel()
	resp, err := c.meta.GetModelInfo(ctx, &mlv1.GetModelInfoRequest{})
	if err != nil {
		return nil, fmt.Errorf("mlclient: model info: %w", err)
	}
	return resp, nil
}
