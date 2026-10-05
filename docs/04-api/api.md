# API для фронтенда

Источник правды — `.proto` в `/proto` ([ADR-0001](../03-architecture/adr/0001-monorepo-contract-first.md)). Здесь — обзор, конвенции и то, что не видно из proto (SSE, загрузка файлов).

## Конвенции

| Тема | Правило |
|---|---|
| Базовый путь | `/api/v1`. nginx проксирует `/api` в core, origin один |
| Формат | JSON через protojson: поля в `lowerCamelCase`, enum строками, `EmitUnpopulated: true` (нулевые значения приходят явно) |
| ⚠️ `int64` | protojson сериализует `int64` **строкой**: `"amountKop": "200000000"`. На фронте `useMoney()` приводит к `number` (до 9·10¹⁵ коп. безопасно) |
| Деньги | Всегда копейки, поле с суффиксом `Kop`. Форматирование только на фронте |
| Ставки | Базисные пункты, суффикс `Bp` (`1650` = 16,50%) |
| Доли | `double` 0..1 (`approvalProb: 0.78`, `paymentToRevenue: 0.056`) |
| Даты | `"2026-10-06"` (date), `"2026-10-06T10:12:00Z"` (timestamp) |
| Авторизация | Cookie `session` (httpOnly). Мутирующие запросы шлют заголовок `X-Requested-With: fetch` |
| Идемпотентность | `Idempotency-Key` для `submit`, `sign/confirm`, `consents` |
| Ошибки | `google.rpc.Status` → HTTP-код от gateway. Тело `{code, message, details[]}`. В `details` — `ErrorInfo.reason`: `OTP_INVALID`, `OTP_EXPIRED`, `STOP_FACTOR`, `VALIDATION`, `LLM_UNAVAILABLE`… |
| Пагинация | `pageSize`, `pageToken` → `nextPageToken` |

## Эндпоинты

### Auth

| Метод | Путь | RPC | Описание |
|---|---|---|---|
| POST | `/auth/otp` | `AuthService.RequestOtp` | `{phone}` → `{otpId, ttlSeconds, demoCode?}` |
| POST | `/auth/verify` | `AuthService.VerifyOtp` | `{otpId, code}` → Set-Cookie + `{user}` |
| POST | `/auth/logout` | `AuthService.Logout` | Сброс cookie |
| GET | `/me` | `AuthService.GetMe` | Пользователь + бизнес |
| GET | `/auth/demo-personas` | `AuthService.ListDemoPersonas` | Только `DEMO_MODE`: чипы на экране входа |

### Главная, профиль, финансы

| Метод | Путь | RPC | Описание |
|---|---|---|---|
| GET | `/dashboard` | `ClientService.GetDashboard` | Всё для главной одним запросом |
| GET | `/profile` | `ClientService.GetProfile` | Данные бизнеса (для заявки, правой панели чата) |
| GET | `/finance/cashflow?horizonDays=60` | `CashflowService.GetCashflow` | Факт 90 дней + прогноз + разрывы + регулярные платежи |

### Данные и согласия

| Метод | Путь | RPC | Описание |
|---|---|---|---|
| GET | `/consents` | `ConsentService.ListDataSources` | Источники: статус, что используем, ожидаемый эффект |
| POST | `/consents/{source}` | `ConsentService.GrantConsent` | → `{before, after}` лимит и ставка |
| DELETE | `/consents/{source}` | `ConsentService.RevokeConsent` | |

### Офферы

| Метод | Путь | RPC | Описание |
|---|---|---|---|
| GET | `/offers?purpose=&amountKop=&termMonths=` | `OfferService.ListOffers` | Ранжированные офферы под цель (или проактивные без цели) |
| POST | `/offers/simulate` | `OfferService.Simulate` | Калькулятор: `{productId, amountKop, termMonths, scheduleType}` |
| GET | `/offers/catalog` | `OfferService.GetCatalog` | Каталог продуктов |
| GET | `/offers/gov-support` | `OfferService.GetGovSupport` | Подходящие программы и экономия (P1) |

### Заявки

| Метод | Путь | RPC | Описание |
|---|---|---|---|
| POST | `/applications` | `ApplicationService.CreateApplication` | Черновик из `{productId, amountKop, termMonths, purpose, origin}` с автозаполнением |
| GET | `/applications` | `ApplicationService.ListApplications` | |
| GET | `/applications/{id}` | `ApplicationService.GetApplication` | Заявка + таймлайн + решение |
| PATCH | `/applications/{id}` | `ApplicationService.UpdateApplication` | Поля формы (`updateMask`) |
| POST | `/applications/{id}/submit` | `ApplicationService.SubmitApplication` | → 202, дальше статусы по SSE |
| POST | `/applications/{id}/accept-counter-offer` | `ApplicationService.AcceptCounterOffer` | |
| POST | `/applications/{id}/cancel` | `ApplicationService.CancelApplication` | |
| POST | `/applications/{id}/sign/otp` | `ApplicationService.RequestSignOtp` | |
| POST | `/applications/{id}/sign/confirm` | `ApplicationService.ConfirmSign` | `{otpId, code}` |
| GET | `/applications/{id}/schedule` | `ApplicationService.GetSchedule` | График платежей |

### Документы (ручные хендлеры)

| Метод | Путь | Описание |
|---|---|---|
| POST | `/documents` | `multipart/form-data`: `file`, `kind`, `applicationId?` → `{documentId, extractStatus}` |
| GET | `/documents` | `DocumentService.ListDocuments` (gateway) |
| GET | `/documents/{id}` | `DocumentService.GetDocument` (gateway): метаданные + `extracted` |
| GET | `/documents/{id}/content` | Скачивание файла (ручной хендлер) |

### Ассистент

| Метод | Путь | Описание |
|---|---|---|
| POST | `/chat/sessions` | `ChatService.CreateSession` → `{sessionId}` |
| GET | `/chat/sessions` | `ChatService.ListSessions` |
| GET | `/chat/sessions/{id}/messages` | `ChatService.GetMessages`: история с виджетами |
| POST | `/chat/sessions/{id}/messages` | **SSE**: тело `{text}` или `{action: {type, payload}}` (быстрый ответ, действие из виджета) |

### События и сервисное

| Метод | Путь | Описание |
|---|---|---|
| GET | `/events` | **SSE**: события клиента |
| POST | `/demo/reset` | Только `DEMO_MODE`: сброс демо-мира |
| GET | `/healthz`, `/readyz` | Проверки (readyz проверяет БД и ml) |

## Стрим ассистента

`POST /api/v1/chat/sessions/{id}/messages`, ответ `Content-Type: text/event-stream`. Payload каждого события — protojson соответствующего варианта `AssistantEvent`.

```
event: message_start
data: {"messageId":"0192f…"}

event: tool_status
data: {"callId":"c1","tool":"find_offers","label":"Подбираю варианты под оборудование…","state":"STATE_STARTED"}

event: context_update
data: {"needs":{"goal":"PURPOSE_EQUIPMENT","goalText":"вторая кофемашина + запасы","amountKop":"200000000","termMonths":36}}

event: tool_status
data: {"callId":"c1","tool":"find_offers","state":"STATE_DONE","durationMs":84}

event: widget
data: {"widgetId":"w1","offerCards":{"offers":[{"productId":"equipment_loan","badge":"BADGE_BEST", "...": "..."}]}}

event: text_delta
data: {"text":"Для оборудования лучше всего подойдёт "}

event: text_delta
data: {"text":"**кредит на оборудование**: ставка 16,5%, платёж около 71 400 ₽ в месяц."}

event: message_end
data: {"messageId":"0192f…","trace":{"model":"qwen3:8b","tools":[{"name":"find_offers","durationMs":84}],"ttfeMs":420,"totalMs":3900}}
```

### Типы виджетов

| Тип (`oneof` в `Widget`) | Payload | Компонент |
|---|---|---|
| `offerCards` | `offers[]` | `OfferCards.vue` (и на экране «Кредиты») |
| `offerComparison` | `offers[]`, `rows[]` (параметр × вариант) | `OfferComparison.vue` |
| `loanSimulator` | `SimulateResponse` + границы слайдеров | `LoanSimulator.vue`: двигаешь, фронт сам зовёт `/offers/simulate` |
| `cashflowChart` | `Cashflow` | `CashflowChart.vue` |
| `applicationDraft` | `applicationId`, `autofillRatio`, ключевые поля | `ApplicationDraft.vue` → «Перейти к заявке» |
| `consentRequest` | `DataSource` + ожидаемый эффект | `ConsentRequest.vue` → кнопка «Подключить» |
| `decisionFactors` | `factors[]`, `tips[]` | `DecisionFactors.vue` |
| `govSupport` | программа, ставка, экономия | `GovSupport.vue` |
| `metricsCard` | ключевые метрики | `MetricsCard.vue` |
| `quickReplies` | `options[]` | `QuickReplies.vue` → отправляет `{action:{type:"quick_reply"}}` |

## Поток событий клиента

`GET /api/v1/events`, `text/event-stream`. Heartbeat — комментарий `: ping` каждые 15 с. Поддерживается `Last-Event-ID`: события хранятся в памяти, последние 100 на клиента.

| event | data | Реакция фронта |
|---|---|---|
| `application.status` | `{applicationId, statusFrom, statusTo, at, decision?}` | Обновить таймлайн и карточку, тост |
| `document.extracted` | `{documentId, applicationId, fields, supplierCheck}` | Заполнить поля заявки |
| `offers.updated` | `{reason: "consent_granted"}` | Инвалидировать query офферов и дашборда |
| `dashboard.invalidate` | `{reason: "disbursed"}` | Перезапросить дашборд (остаток вырос) |
| `notification` | `{title, body, link}` | Тост |

## Примеры ответов

### `GET /api/v1/dashboard` (сокращено)

```json
{
  "profile": { "name": "ИП Смирнов А. И.", "brand": "Кофейня «Зерно»", "okvedMain": "56.30", "businessAgeMonths": 31 },
  "metrics": {
    "revenue30dKop": "128450000", "revenueGrowthQoq": 0.18,
    "revenueSeries": [{ "month": "2026-04", "amountKop": "98000000" }],
    "balanceKop": "64230000", "creditLoad": 0.12
  },
  "health": { "score": 814, "label": "Отличное", "tips": ["Подключите данные кассы, чтобы повысить лимит"] },
  "banner": { "title": "Вам доступно до 3 500 000 ₽", "text": "Выручка выросла на 18% за квартал…", "maxAmountKop": "350000000" },
  "cashGap": { "date": "2026-10-29", "daysAhead": 23, "amountKop": "18000000", "causes": ["Квартальная закупка зерна 450 000 ₽", "Авансовый платёж по УСН 96 000 ₽ (28.10)", "Конец сезона веранды: выручка −15%"], "suggestedProductId": "overdraft", "suggestedAmountKop": "22000000" },
  "topOffers": [ { "productId": "biz_loan", "maxAmountKop": "350000000", "rateBp": 1790, "approvalProb": 0.92 } ],
  "applications": [ { "applicationId": "…", "number": "48217", "productName": "Кредит на оборудование", "status": "APPLICATION_STATUS_SCORING" } ],
  "dataBoost": { "source": "ofd", "title": "Подключите данные кассы", "maxAmountDeltaKop": "60000000", "rateDeltaBp": -50 }
}
```

### `POST /api/v1/offers/simulate`

```json
// запрос
{ "productId": "equipment_loan", "amountKop": "200000000", "termMonths": 36, "scheduleType": "SCHEDULE_TYPE_ANNUITY" }
// ответ
{
  "offer": {
    "productId": "equipment_loan", "amountKop": "200000000", "termMonths": 36, "rateBp": 1650,
    "monthlyPaymentKop": "7080877", "overpaymentKop": "54911572", "paymentToRevenue": 0.055,
    "approvalProb": 0.78,
    "factors": [
      { "code": "revenue_growth_qoq", "title": "Выручка растёт 3 месяца подряд", "impact": 0.21, "direction": "DIRECTION_POSITIVE" },
      { "code": "business_age_months", "title": "Бизнесу 2,5 года", "impact": 0.08, "direction": "DIRECTION_POSITIVE" }
    ],
    "tips": [
      { "code": "reduce_amount", "title": "Уменьшить сумму до 1 600 000 ₽", "approvalProbAfter": 0.91 },
      { "code": "connect_ofd", "title": "Подключить данные кассы", "approvalProbAfter": 0.84, "rateBpAfter": 1600 }
    ]
  },
  "schedule": [ { "n": 1, "date": "2026-11-06", "paymentKop": "7080877", "principalKop": "4330877", "interestKop": "2750000", "balanceKop": "195669123" } ]
}
```

> Заметка: в макете платёж 71 400 ₽, а аннуитет 2 млн / 16,5% / 36 мес даёт ~70 809 ₽. Именно поэтому **все цифры считает калькулятор**, а не рисует дизайн или LLM.
