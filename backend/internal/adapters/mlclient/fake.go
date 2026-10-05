package mlclient

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mlv1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/ml/v1"
)

const fakeVersion = "fake-0"

// Fake — детерминированная замена ml-сервиса без моделей. Цифры правдоподобны и реагируют на вход
// (долговая нагрузка, рост выручки, размер платежа), чтобы на нём можно было строить движок офферов
// и экраны. Настоящие значения даёт обученная модель.
//
// Допущение: Contribution.Shap > 0 повышает риск (PD), < 0 снижает. Согласовать с ML-2.
type Fake struct{}

func NewFake() *Fake { return &Fake{} }

var _ Client = (*Fake)(nil)

func (*Fake) ModelInfo(context.Context) (*mlv1.GetModelInfoResponse, error) {
	return &mlv1.GetModelInfoResponse{
		BundleVersion: "1.0",
		Models: []*mlv1.ModelInfo{
			{Name: "pd", Version: fakeVersion, TrainedAt: "1970-01-01T00:00:00Z"},
			{Name: "forecast", Version: fakeVersion, TrainedAt: "1970-01-01T00:00:00Z"},
			{Name: "categorizer", Version: fakeVersion, TrainedAt: "1970-01-01T00:00:00Z"},
		},
	}, nil
}

// --- Score -------------------------------------------------------------------

type bundleStats struct {
	hasData         bool
	avgRevenueKop   int64
	debtServiceRate float64 // (обслуживание долга + МФО) / выручка
	growthQoQ       float64
	lowBalanceShare float64 // доля дней с низким остатком в месяце
	ageMonths       int32
}

func statsOf(b *mlv1.ClientDataBundle) bundleStats {
	s := bundleStats{ageMonths: b.GetProfile().GetBusinessAgeMonths()}
	months := b.GetMonths()
	if len(months) == 0 {
		return s
	}
	last := months[max(0, len(months)-6):]
	var revenue, debt int64
	var lowDays int32
	for _, m := range last {
		revenue += m.GetRevenueKop()
		debt += m.GetDebtServiceKop() + m.GetMfoKop()
		lowDays += m.GetLowBalanceDays()
	}
	s.hasData = revenue > 0
	s.avgRevenueKop = revenue / int64(len(last))
	if revenue > 0 {
		s.debtServiceRate = float64(debt) / float64(revenue)
	}
	s.lowBalanceShare = float64(lowDays) / float64(len(last)) / 30
	if len(months) >= 6 {
		var recent, prev int64
		for _, m := range months[len(months)-3:] {
			recent += m.GetRevenueKop()
		}
		for _, m := range months[len(months)-6 : len(months)-3] {
			prev += m.GetRevenueKop()
		}
		if prev > 0 {
			s.growthQoQ = float64(recent)/float64(prev) - 1
		}
	}
	return s
}

func (f *Fake) Score(_ context.Context, bundle *mlv1.ClientDataBundle, scenarios []*mlv1.Scenario) (*mlv1.ScoreResponse, error) {
	if len(scenarios) == 0 {
		return nil, status.Error(codes.InvalidArgument, "mlclient/fake: нужен хотя бы один сценарий")
	}
	st := statsOf(bundle)
	results := make([]*mlv1.ScoreResult, 0, len(scenarios))
	for _, sc := range scenarios {
		results = append(results, scoreScenario(st, sc))
	}
	return &mlv1.ScoreResponse{Results: results, ModelVersion: fakeVersion}, nil
}

func scoreScenario(st bundleStats, sc *mlv1.Scenario) *mlv1.ScoreResult {
	var contribs []*mlv1.Contribution
	add := func(feature string, value, shap float64) {
		contribs = append(contribs, &mlv1.Contribution{Feature: feature, Value: value, Shap: shap})
	}

	pd := 0.15 // нет данных — осторожный скор
	var paymentToRevenue float64
	if st.hasData {
		growth := clamp(st.growthQoQ, 0, 0.3)
		age := math.Min(float64(st.ageMonths), 36)
		pd = 0.02 + 0.30*st.debtServiceRate + 0.10*st.lowBalanceShare - 0.02*growth/0.3 - 0.01*age/36
		add("debt_service_ratio", st.debtServiceRate, 4*(st.debtServiceRate-0.1))
		add("revenue_growth_qoq", st.growthQoQ, -2*st.growthQoQ)
		add("business_age_months", float64(st.ageMonths), -0.8*math.Min(float64(st.ageMonths), 60)/60)
		add("low_balance_share", st.lowBalanceShare, 3*st.lowBalanceShare)
		if sc.MonthlyPaymentKop != nil && st.avgRevenueKop > 0 {
			paymentToRevenue = float64(sc.GetMonthlyPaymentKop()) / float64(st.avgRevenueKop)
			pd += 0.5 * math.Max(0, paymentToRevenue-0.10)
			add("payment_to_revenue", paymentToRevenue, 3*(paymentToRevenue-0.1))
		}
	}
	pd = clamp(pd, 0.005, 0.60)

	approval := clamp(1-pd*4, 0.02, 0.98)
	if limit, amount := sc.GetLimitKop(), sc.GetAmountKop(); limit > 0 && amount > limit {
		approval *= float64(limit) / float64(amount)
	}

	sort.SliceStable(contribs, func(i, j int) bool {
		return math.Abs(contribs[i].Shap) > math.Abs(contribs[j].Shap)
	})
	if len(contribs) > 10 {
		contribs = contribs[:10]
	}
	return &mlv1.ScoreResult{
		ScenarioId:    sc.GetId(),
		Pd:            pd,
		Grade:         gradeOf(pd),
		HealthScore:   int32(clamp(math.Round(1000*(1-pd*3)), 0, 1000)),
		ApprovalProb:  approval,
		Contributions: contribs,
	}
}

func gradeOf(pd float64) string {
	switch {
	case pd < 0.03:
		return "A"
	case pd < 0.06:
		return "B"
	case pd < 0.10:
		return "C"
	case pd < 0.18:
		return "D"
	}
	return "E"
}

// --- Forecast ----------------------------------------------------------------

const dateLayout = "2006-01-02"

var recurringCategories = []string{"rent", "payroll", "taxes", "debt_service"}

func (f *Fake) Forecast(_ context.Context, req *mlv1.ForecastRequest) (*mlv1.ForecastResponse, error) {
	asOf, err := time.Parse(dateLayout, req.GetAsOf())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "mlclient/fake: as_of: %v", err)
	}
	if req.GetHorizonDays() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "mlclient/fake: horizon_days должен быть положительным")
	}

	history := req.GetHistory()
	window := history[max(0, len(history)-60):]
	var inSum, outSum float64
	nets := make([]float64, 0, len(window))
	for _, p := range window {
		inSum += float64(p.GetInflowKop())
		outSum += float64(p.GetOutflowKop())
		nets = append(nets, float64(p.GetInflowKop()-p.GetOutflowKop()))
	}
	var meanIn, meanOut, sigma float64
	if n := float64(len(window)); n > 0 {
		meanIn, meanOut = inSum/n, outSum/n
		mean := (inSum - outSum) / n
		var ss float64
		for _, v := range nets {
			ss += (v - mean) * (v - mean)
		}
		sigma = math.Sqrt(ss / n)
	}
	var balance float64
	if len(history) > 0 {
		balance = float64(history[len(history)-1].GetBalanceKop())
	}

	points := make([]*mlv1.ForecastPoint, 0, req.GetHorizonDays())
	for i := int32(1); i <= req.GetHorizonDays(); i++ {
		balance += meanIn - meanOut
		spread := 1.28 * sigma * math.Sqrt(float64(i))
		points = append(points, &mlv1.ForecastPoint{
			Date:          asOf.AddDate(0, 0, int(i)).Format(dateLayout),
			InflowP50Kop:  int64(meanIn),
			OutflowP50Kop: int64(meanOut),
			BalanceP10Kop: int64(balance - spread),
			BalanceP50Kop: int64(balance),
			BalanceP90Kop: int64(balance + spread),
		})
	}

	return &mlv1.ForecastResponse{
		Points:       points,
		Recurring:    fakeRecurring(history),
		ModelVersion: fakeVersion,
		BacktestWape: 0.15,
	}, nil
}

// fakeRecurring считает регулярным последний платёж категории, повторяющийся раз в месяц.
func fakeRecurring(history []*mlv1.DailyPoint) []*mlv1.RecurringPayment {
	var out []*mlv1.RecurringPayment
	for _, cat := range recurringCategories {
		for i := len(history) - 1; i >= 0; i-- {
			amount := history[i].GetOutflowByCategory()[cat]
			if amount <= 0 {
				continue
			}
			last, err := time.Parse(dateLayout, history[i].GetDate())
			if err != nil {
				break
			}
			out = append(out, &mlv1.RecurringPayment{
				Category:  cat,
				AmountKop: amount,
				Period:    "monthly",
				NextDate:  last.AddDate(0, 1, 0).Format(dateLayout),
			})
			break
		}
	}
	return out
}

// --- Categorize --------------------------------------------------------------

// Категории совпадают с полями MonthAggregate (bundle.proto); настоящий набор задаёт D5.
var outflowKeywords = []struct {
	category string
	words    []string
}{
	{"rent", []string{"аренд"}},
	{"payroll", []string{"зарплат", "заработн", "аванс сотруд"}},
	{"taxes", []string{"налог", "усн", "ндфл", "страховые взнос", "енп"}},
	{"debt_service", []string{"кредит", "погашение", "лизинг"}},
	{"mfo", []string{"займ", "мфо", "микрофинанс"}},
	{"owner_withdrawals", []string{"личные нужды", "снятие владельц"}},
}

var inflowByChannel = map[string]string{
	"acquiring": "revenue_acquiring",
	"sbp":       "revenue_sbp",
	"transfer":  "revenue_b2b",
}

func (*Fake) Categorize(_ context.Context, items []*mlv1.TxInput) (*mlv1.CategorizeResponse, error) {
	out := make([]*mlv1.TxCategory, 0, len(items))
	for _, it := range items {
		cat, conf := categorize(it)
		out = append(out, &mlv1.TxCategory{Id: it.GetId(), Category: cat, Confidence: conf})
	}
	return &mlv1.CategorizeResponse{Items: out, ModelVersion: fakeVersion}, nil
}

func categorize(it *mlv1.TxInput) (string, float64) {
	text := strings.ToLower(it.GetPurpose() + " " + it.GetCounterpartyName())
	if it.GetDirection() == "in" {
		if strings.Contains(text, "маркетплейс") || strings.Contains(text, "wildberries") || strings.Contains(text, "ozon") {
			return "revenue_marketplace", 0.9
		}
		if strings.Contains(text, "кредит") || strings.Contains(text, "займ") {
			return "loans_received", 0.85
		}
		if cat, ok := inflowByChannel[it.GetChannel()]; ok {
			return cat, 0.9
		}
		return "revenue_b2b", 0.6
	}
	if it.GetChannel() == "cash" {
		return "cash_out", 0.9
	}
	for _, rule := range outflowKeywords {
		for _, w := range rule.words {
			if strings.Contains(text, w) {
				return rule.category, 0.9
			}
		}
	}
	return "suppliers", 0.6
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
