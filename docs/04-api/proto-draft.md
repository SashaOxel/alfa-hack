# Черновики .proto

> Стартовая точка для `/proto`. После того как файлы появятся в `/proto`, **источник правды — они**, этот документ не обновляется.

## Раскладка

```
proto/
├── third_party/                 # вендоренные googleapis: google/api/{annotations,http}.proto, google/rpc/status.proto
└── alfa/
    ├── core/v1/                 # публичный API (через grpc-gateway)
    │   ├── auth.proto
    │   ├── client.proto         # dashboard, profile
    │   ├── cashflow.proto
    │   ├── consent.proto
    │   ├── offer.proto
    │   ├── application.proto
    │   ├── document.proto
    │   ├── chat.proto           # сессии, история + AssistantEvent (payload SSE)
    │   └── events.proto         # ClientEvent (payload SSE /events)
    └── ml/v1/                   # внутренний: core → ml
        ├── bundle.proto         # ClientDataBundle — КОНТРАКТ данных для моделей
        ├── scoring.proto
        ├── forecast.proto
        └── categorize.proto
```

## Генерация (protoc, без buf)

Решение и список инструментов — в [ADR-0001](../03-architecture/adr/0001-monorepo-contract-first.md). Поскольку managed mode buf нет, в каждом файле явно указываем `go_package`:

```proto
option go_package = "github.com/sashaoxel/alfa-hack/backend/gen/alfa/core/v1;corev1";
```

```bash
# scripts/proto-tools.env — версии закреплены, чтобы сгенерированный код не «дёргался» в диффах
PROTOC_VERSION=...
PROTOC_GEN_GO_VERSION=...
PROTOC_GEN_GO_GRPC_VERSION=...
GRPC_GATEWAY_VERSION=...
GNOSTIC_VERSION=...
```

```bash
# scripts/gen-proto.sh (суть), запускается через `make proto` из корня репозитория
set -euo pipefail
CORE=$(find proto/alfa/core -name '*.proto')
ML=$(find proto/alfa/ml -name '*.proto')

# 1. Go: сообщения + gRPC + gateway (core и ml-клиент)
protoc -I proto -I proto/third_party \
  --go_out=backend/gen          --go_opt=paths=source_relative \
  --go-grpc_out=backend/gen     --go-grpc_opt=paths=source_relative \
  --grpc-gateway_out=backend/gen --grpc-gateway_opt=paths=source_relative \
  $CORE $ML

# 2. Python: только ML-контракт (grpcio-tools приносит свой protoc)
uv run --project ml python -m grpc_tools.protoc -I proto \
  --python_out=ml/gen --pyi_out=ml/gen --grpc_python_out=ml/gen \
  $ML

# 3. OpenAPI v3 (только публичный API) → TypeScript-типы
protoc -I proto -I proto/third_party \
  --openapi_out=frontend/src/api --openapi_opt=enum_type=string \
  $CORE
npx --prefix frontend openapi-typescript frontend/src/api/openapi.yaml -o frontend/src/api/schema.d.ts
```

> Go-код для `google/api/annotations.proto` не генерируем: он уже есть в модуле `google.golang.org/genproto/googleapis/api`. Python-сервису googleapis не нужны: ML-протоколы их не импортируют.

**Валидация** — в Go, в сервисном слое. В черновиках ниже ограничения указаны комментариями `// валидация: …`.

---

## alfa/core/v1/auth.proto

```proto
syntax = "proto3";
package alfa.core.v1;

import "google/api/annotations.proto";

service AuthService {
  rpc RequestOtp(RequestOtpRequest) returns (RequestOtpResponse) {
    option (google.api.http) = { post: "/api/v1/auth/otp" body: "*" };
  }
  rpc VerifyOtp(VerifyOtpRequest) returns (VerifyOtpResponse) {
    option (google.api.http) = { post: "/api/v1/auth/verify" body: "*" };
  }
  rpc Logout(LogoutRequest) returns (LogoutResponse) {
    option (google.api.http) = { post: "/api/v1/auth/logout" body: "*" };
  }
  rpc GetMe(GetMeRequest) returns (GetMeResponse) {
    option (google.api.http) = { get: "/api/v1/me" };
  }
  rpc ListDemoPersonas(ListDemoPersonasRequest) returns (ListDemoPersonasResponse) {
    option (google.api.http) = { get: "/api/v1/auth/demo-personas" };
  }
}

message RequestOtpRequest {
  string phone = 1;                  // валидация: ^\+7\d{10}$
}
message RequestOtpResponse {
  string otp_id = 1;
  int32 ttl_seconds = 2;
  string demo_code = 3;              // заполнен только в DEMO_MODE
}
message VerifyOtpRequest {
  string otp_id = 1;                 // валидация: uuid
  string code = 2;                   // валидация: 4 цифры
}
message VerifyOtpResponse { User user = 1; }
// Cookie ставится через grpc.SetHeader(ctx, "x-set-cookie", …) +
// runtime.WithForwardResponseOption, который превращает его в Set-Cookie.

message User {
  string user_id = 1;
  string phone = 2;
  string business_name = 3;          // «ИП Смирнов А.»
  string brand = 4;                  // «Кофейня «Зерно»»
}
message LogoutRequest {}
message LogoutResponse {}
message GetMeRequest {}
message GetMeResponse { User user = 1; }
message ListDemoPersonasRequest {}
message ListDemoPersonasResponse {
  repeated DemoPersona personas = 1;
}
message DemoPersona { string phone = 1; string title = 2; string subtitle = 3; }
```

## alfa/core/v1/offer.proto

```proto
syntax = "proto3";
package alfa.core.v1;

import "google/api/annotations.proto";

service OfferService {
  rpc ListOffers(ListOffersRequest) returns (ListOffersResponse) {
    option (google.api.http) = { get: "/api/v1/offers" };
  }
  rpc Simulate(SimulateRequest) returns (SimulateResponse) {
    option (google.api.http) = { post: "/api/v1/offers/simulate" body: "*" };
  }
  rpc GetCatalog(GetCatalogRequest) returns (GetCatalogResponse) {
    option (google.api.http) = { get: "/api/v1/offers/catalog" };
  }
  rpc GetGovSupport(GetGovSupportRequest) returns (GetGovSupportResponse) {
    option (google.api.http) = { get: "/api/v1/offers/gov-support" };
  }
}

enum Purpose {
  PURPOSE_UNSPECIFIED = 0;
  PURPOSE_EQUIPMENT = 1;
  PURPOSE_VEHICLE = 2;
  PURPOSE_WORKING_CAPITAL = 3;
  PURPOSE_INVENTORY_SEASON = 4;
  PURPOSE_CASH_GAP = 5;
  PURPOSE_TENDER = 6;
  PURPOSE_REFINANCE = 7;
}

enum ScheduleType {
  SCHEDULE_TYPE_UNSPECIFIED = 0;
  SCHEDULE_TYPE_ANNUITY = 1;
  SCHEDULE_TYPE_DIFFERENTIATED = 2;
  SCHEDULE_TYPE_SEASONAL = 3;
}

enum Badge {
  BADGE_UNSPECIFIED = 0;
  BADGE_BEST = 1;           // «Лучший вариант»
  BADGE_ALTERNATIVE = 2;
  BADGE_GOV_SUPPORT = 3;    // «Господдержка»
  BADGE_PREAPPROVED = 4;
}

enum Direction {
  DIRECTION_UNSPECIFIED = 0;
  DIRECTION_POSITIVE = 1;
  DIRECTION_NEGATIVE = 2;
}

message ListOffersRequest {
  Purpose purpose = 1;
  optional int64 amount_kop = 2;
  optional int32 term_months = 3;
  int32 limit = 4;                        // по умолчанию 3
}
message ListOffersResponse {
  repeated Offer offers = 1;
  repeated UnavailableProduct unavailable = 2;   // что не подошло и почему
}

message Offer {
  string offer_id = 1;
  string product_id = 2;
  string product_name = 3;
  Badge badge = 4;
  int64 max_amount_kop = 5;
  int64 amount_kop = 6;
  int32 term_months = 7;
  int32 rate_bp = 8;
  int64 monthly_payment_kop = 9;
  int64 overpayment_kop = 10;
  double payment_to_revenue = 11;
  double approval_prob = 12;
  string decision_time = 13;              // «за 1 час»
  repeated string fit_reasons = 14;       // «Целевое использование под оборудование»
  repeated Factor factors = 15;           // SHAP → человеческие формулировки
  repeated Tip tips = 16;                 // «как повысить шанс»
  optional int64 savings_vs_market_kop = 17;
  repeated string signals = 18;           // коды сработавших сигналов (для режима прозрачности)
  ScheduleType schedule_type = 19;
}

message Factor {
  string code = 1;
  string title = 2;
  double impact = 3;                      // нормированный вклад 0..1
  Direction direction = 4;
}

message Tip {
  string code = 1;                        // reduce_amount | extend_term | connect_ofd | repay_tax_debt | add_umbrella_guarantee …
  string title = 2;
  double approval_prob_after = 3;
  optional int32 rate_bp_after = 4;
  optional int64 max_amount_kop_after = 5;
  string action_link = 6;                 // deeplink во фронт: /finance/consents?source=ofd
}

message UnavailableProduct { string product_id = 1; string product_name = 2; repeated string reasons = 3; }

message SimulateRequest {
  string product_id = 1;                  // валидация: есть в каталоге
  int64 amount_kop = 2;                   // валидация: в границах продукта
  int32 term_months = 3;                  // валидация: в границах продукта
  ScheduleType schedule_type = 4;
}
message SimulateResponse {
  Offer offer = 1;
  repeated SchedulePayment schedule = 2;
  repeated string warnings = 3;           // «Платёж превышает 30% свободного потока»
}
message SchedulePayment {
  int32 n = 1;
  string date = 2;
  int64 payment_kop = 3;
  int64 principal_kop = 4;
  int64 interest_kop = 5;
  int64 balance_kop = 6;
}

message GetCatalogRequest {}
message GetCatalogResponse { repeated Product products = 1; }
message Product {
  string product_id = 1;
  string name = 2;
  string kind = 3;
  int64 min_amount_kop = 4;
  int64 max_amount_kop = 5;
  int32 min_term_months = 6;
  int32 max_term_months = 7;
  int32 base_rate_bp = 8;
  string decision_time = 9;
  string description = 10;
}

message GetGovSupportRequest {}
message GetGovSupportResponse { repeated GovProgramMatch matches = 1; }
message GovProgramMatch {
  string program_id = 1;
  string name = 2;
  bool eligible = 3;
  repeated string reasons = 4;
  int32 rate_bp = 5;
  int64 savings_vs_market_kop = 6;
}
```

## alfa/core/v1/application.proto (фрагмент)

```proto
service ApplicationService {
  rpc CreateApplication(CreateApplicationRequest) returns (Application) {
    option (google.api.http) = { post: "/api/v1/applications" body: "*" };
  }
  rpc GetApplication(GetApplicationRequest) returns (Application) {
    option (google.api.http) = { get: "/api/v1/applications/{application_id}" };
  }
  rpc UpdateApplication(UpdateApplicationRequest) returns (Application) {
    option (google.api.http) = { patch: "/api/v1/applications/{application_id}" body: "form" };
  }
  rpc SubmitApplication(SubmitApplicationRequest) returns (Application) {
    option (google.api.http) = { post: "/api/v1/applications/{application_id}/submit" body: "*" };
  }
  rpc RequestSignOtp(RequestSignOtpRequest) returns (RequestOtpResponse) {
    option (google.api.http) = { post: "/api/v1/applications/{application_id}/sign/otp" body: "*" };
  }
  rpc ConfirmSign(ConfirmSignRequest) returns (Application) {
    option (google.api.http) = { post: "/api/v1/applications/{application_id}/sign/confirm" body: "*" };
  }
  // ListApplications, AcceptCounterOffer, CancelApplication, GetSchedule — аналогично
}

enum ApplicationStatus {
  APPLICATION_STATUS_UNSPECIFIED = 0;
  APPLICATION_STATUS_DRAFT = 1;
  APPLICATION_STATUS_SUBMITTED = 2;
  APPLICATION_STATUS_DATA_CHECK = 3;
  APPLICATION_STATUS_SCORING = 4;
  APPLICATION_STATUS_DECISION = 5;
  APPLICATION_STATUS_APPROVED = 6;
  APPLICATION_STATUS_COUNTER_OFFER = 7;
  APPLICATION_STATUS_MANUAL_REVIEW = 8;
  APPLICATION_STATUS_DECLINED = 9;
  APPLICATION_STATUS_SIGNED = 10;
  APPLICATION_STATUS_DISBURSED = 11;
  APPLICATION_STATUS_CANCELLED = 12;
}

message Application {
  string application_id = 1;
  string number = 2;                           // «48217»
  string product_id = 3;
  string product_name = 4;
  ApplicationStatus status = 5;
  int64 amount_kop = 6;
  int32 term_months = 7;
  ScheduleType schedule_type = 8;
  int32 rate_bp = 9;
  int64 monthly_payment_kop = 10;
  ApplicationForm form = 11;
  double autofill_ratio = 12;                  // 0.8 → «заполнено 80%»
  Decision decision = 13;
  repeated TimelineStep timeline = 14;
  repeated string document_ids = 15;
}

message ApplicationForm {
  FormField organization_name = 1;
  FormField inn = 2;
  FormField okved = 3;
  FormField registration_date = 4;
  FormField avg_monthly_revenue = 5;
  FormField employees = 6;
  FormField purpose_text = 7;
  FormField supplier_name = 8;
  FormField supplier_inn = 9;
}
message FormField {
  string value = 1;
  FieldSource source = 2;                      // откуда значение — бейдж в UI
  bool editable = 3;
}
enum FieldSource {
  FIELD_SOURCE_UNSPECIFIED = 0;
  FIELD_SOURCE_USER = 1;
  FIELD_SOURCE_EGR = 2;                        // «авто» из ЕГРИП/ЕГРЮЛ
  FIELD_SOURCE_STATEMENT = 3;                  // «по выписке»
  FIELD_SOURCE_DOCUMENT = 4;                   // «из счёта»
  FIELD_SOURCE_ASSISTANT = 5;                  // «заполнено ИИ»
}

message Decision {
  string outcome = 1;                          // approved | counter_offer | manual_review | declined
  int64 approved_amount_kop = 2;
  int32 rate_bp = 3;
  int64 monthly_payment_kop = 4;
  repeated Factor reasons = 5;
  repeated Tip tips = 6;
  repeated string conditions = 7;
  string model_version = 8;
}

message TimelineStep {
  string code = 1;                             // submitted | data_check | scoring | decision | signing | disbursement
  string title = 2;
  string state = 3;                            // done | current | pending | failed
  string at = 4;
  string note = 5;                             // «Автоматически, 2 мин.», «Балл 814 из 1000»
}
```

## alfa/core/v1/chat.proto (события ассистента)

```proto
message AssistantEvent {
  oneof event {
    MessageStart message_start = 1;
    TextDelta text_delta = 2;
    ToolStatus tool_status = 3;
    Widget widget = 4;
    ContextUpdate context_update = 5;
    MessageEnd message_end = 6;
    AssistantError error = 7;
  }
}

message MessageStart { string message_id = 1; }
message TextDelta { string text = 1; }
message ToolStatus {
  string call_id = 1;
  string tool = 2;
  string label = 3;                            // «Смотрю обороты за 12 месяцев…»
  enum State { STATE_UNSPECIFIED = 0; STATE_STARTED = 1; STATE_DONE = 2; STATE_FAILED = 3; }
  State state = 4;
  int32 duration_ms = 5;
}
message ContextUpdate { Needs needs = 1; }
message Needs {
  Purpose goal = 1;
  string goal_text = 2;
  optional int64 amount_kop = 3;
  optional int32 term_months = 4;
  optional bool has_collateral = 5;
  string urgency = 6;
  optional int64 max_payment_kop = 7;
}

message Widget {
  string widget_id = 1;
  oneof kind {
    OfferCardsWidget offer_cards = 2;
    OfferComparisonWidget offer_comparison = 3;
    SimulateResponse loan_simulator = 4;
    Cashflow cashflow_chart = 5;
    ApplicationDraftWidget application_draft = 6;
    ConsentRequestWidget consent_request = 7;
    DecisionFactorsWidget decision_factors = 8;
    GovProgramMatch gov_support = 9;
    QuickRepliesWidget quick_replies = 10;
  }
}
message OfferCardsWidget { repeated Offer offers = 1; }
message OfferComparisonWidget { repeated Offer offers = 1; repeated ComparisonRow rows = 2; }
message ComparisonRow { string title = 1; repeated string values = 2; int32 best_index = 3; }
message ApplicationDraftWidget { string application_id = 1; double autofill_ratio = 2; string summary = 3; }
message ConsentRequestWidget { string source = 1; string title = 2; string benefit = 3; }
message DecisionFactorsWidget { repeated Factor factors = 1; repeated Tip tips = 2; }
message QuickRepliesWidget { repeated string options = 1; }

message MessageEnd { string message_id = 1; Trace trace = 2; }
message Trace {
  string model = 1;
  repeated ToolTrace tools = 2;
  int32 ttfe_ms = 3;
  int32 total_ms = 4;
  int32 prompt_tokens = 5;
  int32 completion_tokens = 6;
  repeated string data_sources = 7;
  bool guardrail_regenerated = 8;
}
message ToolTrace { string name = 1; string args_json = 2; int32 duration_ms = 3; string result_summary = 4; }
message AssistantError { string code = 1; string message = 2; }
```

---

## alfa/ml/v1/bundle.proto — контракт данных для моделей

Самый важный внутренний контракт. Go собирает его из Postgres, Python строит из него признаки. Версия контракта — в `bundle_version`. Добавление полей — нормально, удаление и переименование — только через PR с ревью обеих сторон.

```proto
syntax = "proto3";
package alfa.ml.v1;

message ClientDataBundle {
  string bundle_version = 1;               // "1.0"
  string as_of = 2;                        // "2026-10-06"
  Profile profile = 3;
  repeated MonthAggregate months = 4;      // до 24 мес, по возрастанию
  repeated DailyPoint daily = 5;           // до 365 дней
  Counterparties counterparties = 6;
  repeated Debt debts = 7;
  ExternalData external = 8;               // только источники, разрешённые маской
  repeated string sources_present = 9;     // ["bank_tx", "egr", "fssp", "ofd", …]
}

message Profile {
  string okved_main = 1;
  repeated string okved_extra = 2;
  string legal_form = 3;                   // IP | OOO
  int32 business_age_months = 4;
  string region_code = 5;
  string tax_regime = 6;
  optional int32 employees = 7;
  string msp_category = 8;
  int32 months_with_bank = 9;
}

message MonthAggregate {
  string month = 1;                        // "2026-09"
  int64 inflow_total_kop = 2;              // обороты
  int64 revenue_kop = 3;                   // выручка всего
  int64 revenue_acquiring_kop = 4;
  int64 revenue_sbp_kop = 5;
  int64 revenue_b2b_kop = 6;
  int64 revenue_marketplace_kop = 7;
  int64 revenue_gov_kop = 8;
  int64 loans_received_kop = 9;
  int64 outflow_total_kop = 10;
  int64 suppliers_kop = 11;
  int64 payroll_kop = 12;
  int64 rent_kop = 13;
  int64 taxes_kop = 14;
  int64 debt_service_kop = 15;             // наш банк + распознанные в выписке
  int64 mfo_kop = 16;
  int64 owner_withdrawals_kop = 17;
  int64 cash_out_kop = 18;
  int64 balance_end_kop = 19;
  int64 balance_min_kop = 20;
  int32 low_balance_days = 21;
  int32 tx_count = 22;
}

message DailyPoint {
  string date = 1;
  int64 inflow_kop = 2;
  int64 outflow_kop = 3;
  int64 balance_kop = 4;
  map<string, int64> inflow_by_category = 5;
  map<string, int64> outflow_by_category = 6;
}

message Counterparties {
  double customer_hhi = 1;
  double top_customer_share = 2;
  double supplier_hhi = 3;
  int32 active_customers_90d = 4;
  bool has_marketplace_payouts = 5;
  bool has_gov_customers = 6;
  bool has_equipment_supplier_prepay = 7;
}

message Debt {
  string kind = 1;                         // loan | overdraft | leasing | card | mfo
  bool is_own_bank = 2;
  int64 balance_kop = 3;
  int64 monthly_payment_kop = 4;
  int32 max_overdue_days = 5;
  string source = 6;                       // own | bki | detected
}

message ExternalData {
  // Tier 2 — без согласия
  optional Egr egr = 1;
  optional FnsOpen fns_open = 2;
  optional bool fns_blocked = 3;
  optional Fssp fssp = 4;
  optional Arbitr arbitr = 5;
  optional bool bankrupt = 6;
  optional Eis eis = 7;
  optional string zsk_level = 8;           // low | medium | high
  // Tier 3 — только по согласию
  optional Bki bki = 20;
  optional TaxSecret tax_secret = 21;
  optional Ofd ofd = 22;
  optional Marketplace marketplace = 23;
}

message Egr { bool mass_address = 1; bool unreliable = 2; int32 okved_changes_12m = 3; }
message FnsOpen { int64 tax_debt_kop = 1; int64 taxes_paid_year_kop = 2; }
message Fssp { int32 count = 1; int64 total_kop = 2; }
message Arbitr { int32 as_defendant_12m = 1; int64 claims_kop = 2; }
message Eis { int32 contracts_12m = 1; int64 contracts_sum_kop = 2; int64 active_contracts_kop = 3; bool in_rnp = 4; }
message Bki { int32 loans_open = 1; int64 total_debt_kop = 2; int64 monthly_payments_kop = 3; int32 max_overdue_days_24m = 4; int32 inquiries_30d = 5; int32 bureau_score = 6; }
message TaxSecret { int64 declared_income_year_kop = 1; int64 ens_balance_kop = 2; }
message Ofd { int64 revenue_30d_kop = 1; int64 avg_check_kop = 2; int32 points_of_sale = 3; double cash_share = 4; }
message Marketplace { int64 revenue_30d_kop = 1; double rating = 2; double buyout_rate = 3; int32 platforms = 4; }
```

## alfa/ml/v1/scoring.proto

```proto
syntax = "proto3";
package alfa.ml.v1;

import "alfa/ml/v1/bundle.proto";

service ScoringService {
  rpc Score(ScoreRequest) returns (ScoreResponse);
}

message ScoreRequest {
  ClientDataBundle bundle = 1;
  repeated Scenario scenarios = 2;         // минимум один; пустой продукт = клиентский скор
}

message Scenario {
  string id = 1;
  optional string product_kind = 2;        // loan | overdraft | leasing | …
  optional int64 amount_kop = 3;
  optional int32 term_months = 4;
  optional int64 monthly_payment_kop = 5;  // считает Go
  optional int64 limit_kop = 6;            // лимит, рассчитанный Go
  Assumptions assume = 7;                  // what-if
}

message Assumptions {
  repeated string assume_sources = 1;      // «как если бы подключили ofd»: априорные значения, без реальных данных
  bool tax_debt_repaid = 2;
  bool fssp_repaid = 3;
  bool mfo_closed = 4;
  optional double revenue_multiplier = 5;
}

message ScoreResponse {
  repeated ScoreResult results = 1;
  string model_version = 2;
}

message ScoreResult {
  string scenario_id = 1;
  double pd = 2;
  string grade = 3;                        // A..E
  int32 health_score = 4;                  // 0..1000
  double approval_prob = 5;
  repeated Contribution contributions = 6; // топ-10 по |shap|
}

message Contribution {
  string feature = 1;                      // код признака; тексты — в config/feature_texts.yaml (читает Go)
  double value = 2;
  double shap = 3;
}
```

## alfa/ml/v1/forecast.proto и categorize.proto

```proto
service ForecastService {
  rpc Forecast(ForecastRequest) returns (ForecastResponse);
}
message ForecastRequest {
  string as_of = 1;
  repeated DailyPoint history = 2;         // 180–365 дней
  int32 horizon_days = 3;
  string okved_main = 4;
}
message ForecastResponse {
  repeated ForecastPoint points = 1;
  repeated RecurringPayment recurring = 2;
  string model_version = 3;
  double backtest_wape = 4;
}
message ForecastPoint {
  string date = 1;
  int64 inflow_p50_kop = 2;
  int64 outflow_p50_kop = 3;
  int64 balance_p10_kop = 4;
  int64 balance_p50_kop = 5;
  int64 balance_p90_kop = 6;
}
message RecurringPayment {
  string category = 1;                     // taxes | rent | payroll | supplier_bulk | debt_service
  string counterparty = 2;
  int64 amount_kop = 3;
  string period = 4;                       // biweekly | monthly | quarterly
  string next_date = 5;
}

service CategorizeService {
  rpc Categorize(CategorizeRequest) returns (CategorizeResponse);
}
message CategorizeRequest { repeated TxInput items = 1; }
message TxInput {
  string id = 1;
  string direction = 2;
  int64 amount_kop = 3;
  string counterparty_name = 4;
  string counterparty_inn = 5;
  string purpose = 6;
  string channel = 7;
}
message CategorizeResponse { repeated TxCategory items = 1; string model_version = 2; }
message TxCategory { string id = 1; string category = 2; double confidence = 3; }
```
