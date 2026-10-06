package mlclient_test

import (
	"context"
	"math"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	mlv1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/ml/v1"
	"github.com/sashaoxel/alfa-hack/backend/internal/adapters/mlclient"
)

func months(n int, revenue, debt int64) []*mlv1.MonthAggregate {
	out := make([]*mlv1.MonthAggregate, n)
	for i := range out {
		out[i] = &mlv1.MonthAggregate{RevenueKop: revenue, DebtServiceKop: debt}
	}
	return out
}

func score(t *testing.T, b *mlv1.ClientDataBundle, sc *mlv1.Scenario) *mlv1.ScoreResult {
	t.Helper()
	resp, err := mlclient.NewFake().Score(context.Background(), b, []*mlv1.Scenario{sc})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	return resp.Results[0]
}

func TestFake_Score_ReactsToDebtLoad(t *testing.T) {
	profile := &mlv1.Profile{BusinessAgeMonths: 30}
	healthy := score(t, &mlv1.ClientDataBundle{Profile: profile, Months: months(12, 100_000_00, 5_000_00)}, &mlv1.Scenario{Id: "s"})
	indebted := score(t, &mlv1.ClientDataBundle{Profile: profile, Months: months(12, 100_000_00, 45_000_00)}, &mlv1.Scenario{Id: "s"})

	require.Equal(t, "s", healthy.ScenarioId)
	require.Less(t, healthy.Pd, indebted.Pd)
	require.Greater(t, healthy.HealthScore, indebted.HealthScore)
	require.Greater(t, healthy.ApprovalProb, indebted.ApprovalProb)
	require.Less(t, healthy.Grade, indebted.Grade) // A..E лексикографически
}

func TestFake_Score_ReactsToPaymentAndLimit(t *testing.T) {
	b := &mlv1.ClientDataBundle{Profile: &mlv1.Profile{BusinessAgeMonths: 30}, Months: months(12, 100_000_00, 5_000_00)}
	base := score(t, b, &mlv1.Scenario{Id: "s"})

	pay := int64(40_000_00) // 40% выручки
	heavy := score(t, b, &mlv1.Scenario{Id: "s", MonthlyPaymentKop: &pay})
	require.Greater(t, heavy.Pd, base.Pd)

	amount, limit := int64(2_000_000_00), int64(1_000_000_00)
	overLimit := score(t, b, &mlv1.Scenario{Id: "s", AmountKop: &amount, LimitKop: &limit})
	require.InDelta(t, base.ApprovalProb/2, overLimit.ApprovalProb, 1e-9)
}

func TestFake_Score_InvariantsAndDeterminism(t *testing.T) {
	b := &mlv1.ClientDataBundle{Profile: &mlv1.Profile{BusinessAgeMonths: 12}, Months: months(12, 80_000_00, 10_000_00)}
	a1 := score(t, b, &mlv1.Scenario{Id: "s"})
	a2 := score(t, b, &mlv1.Scenario{Id: "s"})
	require.Equal(t, a1.Pd, a2.Pd)

	require.False(t, math.IsNaN(a1.Pd) || math.IsNaN(a1.ApprovalProb))
	require.GreaterOrEqual(t, a1.Pd, 0.005)
	require.LessOrEqual(t, a1.Pd, 0.60)
	require.GreaterOrEqual(t, a1.HealthScore, int32(0))
	require.LessOrEqual(t, a1.HealthScore, int32(1000))
	require.NotEmpty(t, a1.Contributions)
	for i := 1; i < len(a1.Contributions); i++ { // отсортированы по |shap|
		require.GreaterOrEqual(t, abs(a1.Contributions[i-1].Shap), abs(a1.Contributions[i].Shap))
	}
}

func TestFake_Score_NoDataIsCautious(t *testing.T) {
	r := score(t, &mlv1.ClientDataBundle{}, &mlv1.Scenario{Id: "s"})
	require.InDelta(t, 0.15, r.Pd, 1e-9)
	require.Equal(t, "D", r.Grade)
}

func TestFake_Score_RequiresScenario(t *testing.T) {
	_, err := mlclient.NewFake().Score(context.Background(), &mlv1.ClientDataBundle{}, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestFake_Forecast(t *testing.T) {
	history := make([]*mlv1.DailyPoint, 90)
	for i := range history {
		history[i] = &mlv1.DailyPoint{
			Date: "2026-07-08", InflowKop: 50_000_00, OutflowKop: 40_000_00, BalanceKop: int64(100_000_00 + i*10_000_00),
		}
	}
	history[80].Date = "2026-09-26"
	history[80].OutflowByCategory = map[string]int64{"rent": 120_000_00}

	resp, err := mlclient.NewFake().Forecast(context.Background(),
		&mlv1.ForecastRequest{AsOf: "2026-10-06", History: history, HorizonDays: 30})
	require.NoError(t, err)
	require.Len(t, resp.Points, 30)
	require.Equal(t, "2026-10-07", resp.Points[0].Date)
	require.Equal(t, "2026-11-05", resp.Points[29].Date)
	for _, p := range resp.Points {
		require.LessOrEqual(t, p.BalanceP10Kop, p.BalanceP50Kop)
		require.LessOrEqual(t, p.BalanceP50Kop, p.BalanceP90Kop)
	}
	require.Len(t, resp.Recurring, 1)
	require.Equal(t, "rent", resp.Recurring[0].Category)
	require.Equal(t, "2026-10-26", resp.Recurring[0].NextDate)

	_, err = mlclient.NewFake().Forecast(context.Background(), &mlv1.ForecastRequest{AsOf: "вчера", HorizonDays: 5})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = mlclient.NewFake().Forecast(context.Background(), &mlv1.ForecastRequest{AsOf: "2026-10-06"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// Допустимые коды категорий — docs/05-ml/features.md#категории-транзакций.
var (
	inCategories = []string{
		"revenue_acquiring", "revenue_sbp", "revenue_b2b", "revenue_marketplace", "revenue_gov",
		"loan_received", "own_transfer_in", "refund_in", "other_in",
	}
	outCategories = []string{
		"suppliers", "payroll", "taxes", "rent", "debt_service", "leasing", "mfo", "owner_withdrawal",
		"cash_out", "own_transfer_out", "bank_fees", "marketing", "utilities", "other_out",
	}
)

func TestFake_Categorize(t *testing.T) {
	cases := []struct {
		item *mlv1.TxInput
		want string
	}{
		// поступления
		{&mlv1.TxInput{Direction: "in", Channel: "acquiring"}, "revenue_acquiring"},
		{&mlv1.TxInput{Direction: "in", Channel: "sbp"}, "revenue_sbp"},
		{&mlv1.TxInput{Direction: "in", Channel: "transfer"}, "revenue_b2b"},
		{&mlv1.TxInput{Direction: "in", Channel: "transfer", CounterpartyName: "ООО Wildberries"}, "revenue_marketplace"},
		{&mlv1.TxInput{Direction: "in", Channel: "transfer", CounterpartyName: "УФК по Иркутской области"}, "revenue_gov"},
		{&mlv1.TxInput{Direction: "in", Purpose: "Предоставление кредита по договору 5"}, "loan_received"},
		{&mlv1.TxInput{Direction: "in", Purpose: "Перевод между своими счетами"}, "own_transfer_in"},
		{&mlv1.TxInput{Direction: "in", Purpose: "Возврат средств по счёту 7", Channel: "transfer"}, "refund_in"},
		{&mlv1.TxInput{Direction: "in", Purpose: "Непонятный платёж"}, "other_in"},
		// списания
		{&mlv1.TxInput{Direction: "out", Purpose: "Арендная плата за октябрь"}, "rent"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Уплата налога УСН"}, "taxes"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Заработная плата за сентябрь"}, "payroll"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Оплата по счёту 15"}, "suppliers"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Погашение кредита по договору 12"}, "debt_service"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Лизинговый платёж по договору 3"}, "leasing"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Погашение займа МФО Быстроденьги"}, "mfo"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Перевод на личные нужды"}, "owner_withdrawal"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Перевод между своими счетами"}, "own_transfer_out"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Комиссия за обслуживание счёта"}, "bank_fees"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Оплата рекламы в Яндекс Директ"}, "marketing"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Коммунальные услуги за сентябрь"}, "utilities"},
		{&mlv1.TxInput{Direction: "out", Purpose: "Прочее"}, "other_out"},
		// наличные: без признаков — cash_out, но «на личные нужды» — изъятие прибыли
		{&mlv1.TxInput{Direction: "out", Channel: "cash"}, "cash_out"},
		{&mlv1.TxInput{Direction: "out", Channel: "cash", Purpose: "Снятие на личные нужды"}, "owner_withdrawal"},
	}
	items := make([]*mlv1.TxInput, len(cases))
	for i, c := range cases {
		c.item.Id = strconv.Itoa(i)
		items[i] = c.item
	}
	resp, err := mlclient.NewFake().Categorize(context.Background(), items)
	require.NoError(t, err)
	require.Len(t, resp.Items, len(cases))
	for i, c := range cases {
		require.Equal(t, c.item.Id, resp.Items[i].Id)
		require.Equal(t, c.want, resp.Items[i].Category, "%s %s", c.item.Direction, c.item.Purpose)
	}
}

// Любой ответ fake обязан быть из официального набора: иначе BuildBundle суммирует по несуществующим ключам.
func TestFake_Categorize_OnlyKnownCategories(t *testing.T) {
	purposes := []string{"", "кредит", "лизинг займ", "возврат кредита", "налог аренда", "wildberries", "рекламa", "???"}
	channels := []string{"", "acquiring", "sbp", "transfer", "cash", "card"}

	var items []*mlv1.TxInput
	for _, dir := range []string{"in", "out", ""} {
		for _, p := range purposes {
			for _, ch := range channels {
				items = append(items, &mlv1.TxInput{Id: "x", Direction: dir, Purpose: p, Channel: ch})
			}
		}
	}
	resp, err := mlclient.NewFake().Categorize(context.Background(), items)
	require.NoError(t, err)
	for i, got := range resp.Items {
		in := items[i].Direction == "in"
		allowed := outCategories
		if in {
			allowed = inCategories
		}
		require.Contains(t, allowed, got.Category, "direction=%q purpose=%q channel=%q", items[i].Direction, items[i].Purpose, items[i].Channel)
		require.Greater(t, got.Confidence, 0.0)
		require.LessOrEqual(t, got.Confidence, 1.0)
	}
}

// --- gRPC-клиент против сервера на bufconn --------------------------------------------------------

type scoringServer struct {
	mlv1.UnimplementedScoringServiceServer
	fake *mlclient.Fake
}

func (s scoringServer) Score(ctx context.Context, r *mlv1.ScoreRequest) (*mlv1.ScoreResponse, error) {
	return s.fake.Score(ctx, r.Bundle, r.Scenarios)
}

type metaServer struct {
	mlv1.UnimplementedMetaServiceServer
	fake *mlclient.Fake
}

func (s metaServer) GetModelInfo(ctx context.Context, _ *mlv1.GetModelInfoRequest) (*mlv1.GetModelInfoResponse, error) {
	return s.fake.ModelInfo(ctx)
}

func TestGRPC_ClientAgainstServer(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	fake := mlclient.NewFake()
	mlv1.RegisterScoringServiceServer(srv, scoringServer{fake: fake})
	mlv1.RegisterMetaServiceServer(srv, metaServer{fake: fake})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	c, err := mlclient.NewGRPC("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	info, err := c.ModelInfo(context.Background())
	require.NoError(t, err)
	require.Equal(t, "1.0", info.BundleVersion)

	b := &mlv1.ClientDataBundle{Profile: &mlv1.Profile{BusinessAgeMonths: 24}, Months: months(12, 100_000_00, 5_000_00)}
	resp, err := c.Score(context.Background(), b, []*mlv1.Scenario{{Id: "x"}})
	require.NoError(t, err)
	require.Equal(t, "x", resp.Results[0].ScenarioId)

	// сервис Forecast не зарегистрирован: ошибка оборачивается и сохраняет код
	_, err = c.Forecast(context.Background(), &mlv1.ForecastRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
