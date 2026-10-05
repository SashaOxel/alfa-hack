# Сервисы и модули

Граница ответственности ([ADR-0003](adr/0003-go-logic-python-models.md)):

| | Go `core` | Python `ml` (рантайм) | Python офлайн |
|---|---|---|---|
| Доступ к БД | ✅ единственный | ❌ | ✅ datagen пишет `bank.*` |
| Бизнес-правила (стоп-факторы, лимиты, ставки, графики) | ✅ | ❌ | ❌ |
| ИИ-агент (LLM-вызовы, инструменты, RAG, guardrails) | ✅ | ❌ | eval-скрипты, промпты (файлы) |
| Признаки из данных клиента → модель → предсказание | ❌ (только собирает `ClientDataBundle`) | ✅ | ✅ обучение на тех же функциях |
| Состояние | Postgres | нет (stateless) | parquet, artifacts |

---

## `core` (Go)

### Структура пакетов

```
backend/
├── cmd/core/main.go               # точка входа: конфиг → app.Run()
├── internal/
│   ├── app/                       # сборка зависимостей (DI руками), запуск gRPC + HTTP + River
│   ├── config/                    # env → struct (caarlos0/env)
│   ├── platform/                  # общая инфраструктура
│   │   ├── db/                    # пул pgx, транзакции
│   │   ├── grpcserver/            # interceptors: auth, request-id, logging, recover
│   │   ├── gateway/               # grpc-gateway mux + кастомные HTTP-роуты (SSE, upload)
│   │   ├── sse/                   # writer, heartbeat, Last-Event-ID
│   │   └── logger/
│   ├── modules/                   # бизнес-модули: внутри service.go / handler.go / repo.go / model.go
│   │   ├── auth/
│   │   ├── client/
│   │   ├── consent/
│   │   ├── offers/
│   │   ├── cashflow/
│   │   ├── application/
│   │   ├── document/
│   │   ├── assistant/
│   │   ├── notify/
│   │   └── audit/
│   ├── adapters/                  # всё внешнее за интерфейсами
│   │   ├── bankdata/              # чтение bank.* + сборка ClientDataBundle
│   │   ├── mlclient/              # gRPC-клиент к ml
│   │   ├── llm/                   # провайдеры LLM + маскирование PII
│   │   ├── sms/                   # mock SMS
│   │   └── storage/               # файлы (локальный volume)
│   └── db/                        # сгенерировано sqlc
├── queries/                       # *.sql для sqlc
├── migrations/                    # goose
├── gen/                           # сгенерировано protoc (go, grpc, gateway) — make proto
└── sqlc.yaml
```

Правило зависимостей: `transport → modules → adapters`. Модули общаются через Go-интерфейсы, а не через gRPC. Модуль не лезет в таблицы другого модуля, а вызывает его сервис.

### Модули

| Модуль | Отвечает за | Публичные RPC (через gateway) | Зависит от | Владелец |
|---|---|---|---|---|
| `auth` | OTP (генерация, TTL, попытки), выпуск и проверка JWT в cookie, logout | `RequestOtp`, `VerifyOtp`, `Logout`, `Me` | `sms`, Postgres | Junior (с ревью) |
| `client` | Профиль бизнеса, **дашборд одним запросом**: метрики (выручка, рост, остаток, нагрузка, sparkline), здоровье, проактивные офферы, алерт разрыва, заявки | `GetProfile`, `GetDashboard` | `bankdata`, `offers`, `cashflow`, `application` | Fullstack |
| `consent` | Согласия на источники Tier 3: выдать, отозвать, список. Маска источников для `bankdata` | `ListDataSources`, `GrantConsent`, `RevokeConsent` | Postgres | Junior |
| `offers` | **Движок подбора** (см. [offer-engine.md](../05-ml/offer-engine.md)): каталог, сигналы, стоп-факторы (expr), лимиты, ценообразование, калькуляторы (аннуитет, дифференцированный, сезонный), ранжирование, what-if-сценарии, господдержка | `ListOffers`, `Simulate`, `GetCatalog` | `bankdata`, `mlclient`, `consent` | Fullstack |
| `cashflow` | Ряд денежного потока, вызов `ml.Forecast`, **детекция разрыва** и причин, размер рекомендованного овердрафта | `GetCashflow` | `bankdata`, `mlclient` | Fullstack (ML помогает с интерпретацией) |
| `application` | Заявки: черновик с автозаполнением, валидация, state machine, пайплайн на River (проверка → скоринг → решение → выдача), подписание по OTP, события и таймлайн | `CreateApplication`, `UpdateApplication`, `SubmitApplication`, `GetApplication`, `ListApplications`, `RequestSignOtp`, `ConfirmSign`, `GetSchedule` | `offers`, `mlclient`, `bankdata`, `auth`, `notify` | Fullstack + Junior |
| `document` | Загрузка файлов, хранение, извлечение данных из счёта (текст → LLM structured output), проверка поставщика по mock ЕГРЮЛ | `ListDocuments`, `GetDocument` + HTTP `POST /documents` | `storage`, `llm`, `bankdata` | Junior (загрузка), Fullstack (извлечение) |
| `assistant` | **ИИ-агент** (см. [ai-assistant.md](ai-assistant.md)): сессии, история, сборка контекста, цикл tool calling, инструменты-обёртки над модулями, RAG по базе знаний, guardrails, стрим событий | `CreateSession`, `ListSessions`, `GetMessages` + SSE `POST /chat/sessions/{id}/messages` | `offers`, `client`, `cashflow`, `application`, `consent`, `llm`, pgvector | Fullstack (код), ML-LLM (промпты, инструменты, eval) |
| `notify` | In-process pub/sub по `client_id` → SSE `/events`. Уведомления (заявка одобрена и т.п.) | SSE `GET /events` | — | Junior |
| `audit` | Журнал действий: вызовы инструментов агента, смены статусов, выдачи согласий | — (внутренний) | Postgres | Junior |

### Адаптеры

| Адаптер | Интерфейс (суть) | Реализации |
|---|---|---|
| `bankdata` | `Profile(clientID)`, `MonthlyAggregates(...)`, `DailyBalances(...)`, `Transactions(...)`, `ExtSnapshot(clientID, source)`, **`BuildBundle(clientID, consentMask, asOf) → ClientDataBundle`** | `postgres` (схема `bank`, наполняется datagen) |
| `mlclient` | `Score(bundle, scenarios)`, `Forecast(series, horizon)`, `Categorize(txs)` | gRPC; `fake` для тестов и для работы фронта, пока ML не готов |
| `llm` | `ChatStream(ctx, req) → stream`, `Chat(ctx, req)`, `Embed(ctx, texts)` | `openai_compat` (Ollama, vLLM, LM Studio, OpenAI-совместимые API организаторов), `gigachat` (если понадобится); декоратор `masking` |
| `sms` | `Send(phone, text)` | `mock`: пишет в лог и в таблицу `core.sms_outbox`, фронт показывает тост |
| `storage` | `Put`, `Get`, `Delete` | `localfs` (volume) |

> 💡 `mlclient.fake` — важная вещь для параллельной работы. Fullstack с первого дня строит экраны и движок офферов на фейковых скорах, не дожидаясь моделей.

### Фоновые задачи (River)

| Задача | Когда | Что делает |
|---|---|---|
| `application.check_data` | после `Submit` | Стоп-факторы (ЗСК, блокировки, банкротство, возраст бизнеса), полнота данных |
| `application.score` | после `check_data` | `ml.Score` с параметрами заявки → `ml_score_log` |
| `application.decide` | после `score` | Политика решения: одобрить / встречное предложение / ручная проверка / отказ. Причины из SHAP |
| `application.disburse` | после `ConfirmSign` | Вставляет транзакцию выдачи в `bank.transactions`, обязательство в `bank.loans`. Дашборд это видит |
| `document.extract` | после загрузки | Извлечение реквизитов из счёта |
| `kb.reindex` | старт приложения, если изменился хэш `/kb` | Эмбеддинги базы знаний → pgvector |

Для демо у каждого шага пайплайна есть искусственная задержка `PIPELINE_STEP_DELAY` (по умолчанию 3–5 с): таймлайн должен «прожиться» на глазах у жюри.

---

## `ml` (Python, инференс)

Stateless gRPC-сервис. Знает только модели. Не знает про продукты, ставки, БД и LLM.

```
ml/
├── pyproject.toml                 # uv workspace
├── packages/
│   ├── mlcore/                    # ОБЩИЙ код обучения и инференса: bundle → признаки, утилиты
│   ├── ml_service/                # gRPC-сервер: servicers, загрузка артефактов
│   ├── datagen/                   # офлайн: синтетика + адаптеры датасетов
│   └── training/                  # офлайн: обучение, калибровка, отчёты, model cards
├── eval/                          # офлайн: eval LLM-агента и моделей
├── notebooks/
├── gen/                           # сгенерировано grpcio-tools (python) — make proto
└── artifacts/                     # модели (gitignored, монтируется в контейнер)
```

### RPC

| RPC | Вход | Выход | Модель |
|---|---|---|---|
| `Scoring.Score` | `ClientDataBundle` + список `Scenario` (продукт, сумма, срок, платёж, патч допущений) | Для каждого сценария: `pd`, `grade` (A–E), `health_score` (0–1000), `approval_prob`, топ-факторы SHAP (признак, значение, вклад, направление), `model_version` | CatBoost PD + калибровка + approval-модель |
| `Forecast.Forecast` | Дневные ряды поступлений и списаний по категориям (180–365 дней), горизонт | Прогноз остатка p10 / p50 / p90 по дням, найденные регулярные платежи (категория, сумма, периодичность, следующая дата) | Сезонная декомпозиция + регулярные платежи |
| `Categorize.Categorize` | Пачка транзакций (назначение, контрагент, ИНН, сумма, направление) | Категория + уверенность | Правила + TF-IDF/LogReg |
| `Meta.GetModelInfo` | — | Версии, метрики, дата обучения | — (для режима прозрачности и питча) |

**Ключевое правило:** код `ClientDataBundle → признаки` лежит в `mlcore` и используется **и при обучении, и при инференсе**. Так нет расхождения train/serve. Обучающий пайплайн собирает те же `ClientDataBundle` из parquet, а контрактный тест сверяет бандлы, собранные Go из Postgres и Python из parquet, для каждой персоны ([features.md](../05-ml/features.md#контракт-clientdatabundle)).

### Бюджет латентности
`Score` на 10 сценариев — < 100 мс, `Forecast` — < 300 мс. Модели загружаются в память при старте.

---

## `web` (Vue)

```
frontend/src/
├── api/              # сгенерированные типы (openapi-typescript) + клиент openapi-fetch
├── composables/      # useChatStream (SSE), useEvents (SSE), useMoney, useDebounce…
├── stores/           # Pinia: session, ui (режим прозрачности)
├── views/            # Login, Home, Assistant, Credits, ApplicationWizard, ApplicationCard, Applications, Documents, Finance
├── components/       # дизайн-система: Card, Badge, Button, Slider, Stepper, ProgressBar, Money…
├── widgets/          # ВИДЖЕТЫ АССИСТЕНТА: OfferCards, OfferComparison, LoanSimulator, CashflowChart,
│                     #   ApplicationDraft, ConsentRequest, QuickReplies, DecisionFactors, GovSupport
│   └── registry.ts   # type → component
└── router/
```

Виджеты ассистента переиспользуются на обычных экранах: `OfferCards` в чате и на «Кредитах» — один и тот же компонент.

---

## `datagen` / `training` / `eval` (Python, офлайн)

| Пакет | Что делает | Команда |
|---|---|---|
| `datagen` | Синтетика (персоны + фон) или импорт датасета организаторов через адаптер → parquet + загрузка в `bank.*` + прогон категоризации | `make data` / `make import DATASET=…` |
| `training` | Обучение PD, approval, категоризатора и прогноза → `artifacts/<model>/<version>/` + model card | `make train` |
| `eval` | Golden-диалоги к ассистенту через REST core, метрики tool-calling и галлюцинаций цифр. Сравнение LLM-моделей | `make eval-llm` |

Подробнее: [data-generation.md](../05-ml/data-generation.md), [models.md](../05-ml/models.md).
