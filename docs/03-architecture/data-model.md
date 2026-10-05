# Модель данных

Один PostgreSQL 16 (+ pgvector), три схемы с явными владельцами.

| Схема | Что это | Пишет | Читает |
|---|---|---|---|
| `bank` | **Эмуляция данных банка и внешних источников**: клиенты, счета, транзакции, остатки, кредиты, снимки внешних источников | `datagen` (офлайн). Исключение: core при выдаче кредита (эмуляция АБС) | `core` (адаптер `bankdata`) |
| `core` | Данные кабинета: пользователи, OTP, согласия, заявки, документы, уведомления, аудит, лог скоринга, таблицы River | `core` | `core` |
| `assistant` | Сессии чата, сообщения, база знаний с эмбеддингами | `core` (модуль `assistant`) | `core` |

Python-сервис `ml` в БД **не ходит** ([ADR-0003](adr/0003-go-logic-python-models.md)).

## Соглашения

- **Деньги — `bigint` в копейках** (`amount_kop`). Никаких `numeric` / `float` в Go-коде для сумм.
- **Ставки — `int` в базисных пунктах** (`rate_bp`: 1650 = 16,50%).
- Время — `timestamptz` (UTC). Бизнес-даты операций — `date`.
- Идентификаторы — `uuid` (v7, сортируемые). Человекочитаемый номер заявки — отдельная sequence (`№ 48217`).
- Статусы — Postgres `enum` либо `text` + `CHECK`. Выбираем `text` + `CHECK`: проще миграции.
- Гибкие части (снимки внешних источников, форма заявки, виджеты сообщений) — `jsonb` со схемой в коде.

## Схема `bank`

```mermaid
erDiagram
  clients ||--o{ accounts : has
  clients ||--o{ transactions : has
  accounts ||--o{ transactions : has
  accounts ||--o{ daily_balances : has
  clients ||--o{ loans : has
  clients ||--o{ ext_snapshots : has
  clients ||--o{ ext_daily : has

  clients {
    uuid client_id PK
    text inn UK
    text ogrn
    text legal_form "IP | OOO"
    text name
    text okved_main
    text_arr okved_extra
    text region_code
    date registered_at
    text tax_regime "USN6 | USN15 | PSN | AUSN | OSN | ESHN"
    int employees
    text msp_category "micro | small | medium"
    text owner_full_name
    text owner_phone
    text persona_code "null для фона"
  }
  accounts {
    uuid account_id PK
    uuid client_id FK
    text bank_name
    bool is_own_bank "false = счёт в другом банке (Open API)"
    date opened_at
  }
  transactions {
    bigint tx_id PK
    uuid account_id FK
    uuid client_id FK
    date booked_date
    timestamptz ts
    bigint amount_kop "всегда > 0"
    text direction "in | out"
    text counterparty_name
    text counterparty_inn
    text purpose "назначение платежа"
    text channel "acquiring | sbp | transfer | cash | card"
    text category "предсказание категоризатора"
    real category_conf
    text category_true "только синтетика, для оценки"
  }
  daily_balances {
    uuid account_id PK
    date date PK
    bigint balance_kop
  }
  loans {
    uuid loan_id PK
    uuid client_id FK
    text lender_name
    text lender_inn
    bool is_own_bank
    text kind "loan | overdraft | leasing | card | mfo"
    bigint principal_kop
    bigint balance_kop
    bigint monthly_payment_kop
    date opened_at
    date maturity_at
    int max_overdue_days
    text source "own | bki | detected"
  }
  ext_snapshots {
    uuid client_id PK
    text source PK
    timestamptz fetched_at
    jsonb payload
  }
  ext_daily {
    uuid client_id PK
    text source PK "ofd | marketplace"
    date date PK
    bigint amount_kop
    int tx_count
    jsonb extra
  }
```

Дополнительно:
- `bank.counterparty_registry (inn PK, name, okved, kind, status, registered_at, flags jsonb)` — справочник контрагентов: маркетплейсы, банки, МФО, лизинговые компании, госзаказчики, поставщики оборудования. Нужен для категоризации и проверки поставщика из счёта.
- `bank.meta (key PK, value)` — `demo_today`, `dataset_version`, `seed`, `source` (`synthetic` | `<adapter>`).

### Источники в `ext_snapshots.source`

| source | Tier | Нужно согласие | Ключевые поля `payload` |
|---|---|---|---|
| `egr` | 2 | нет | дата регистрации, ОКВЭД, адрес, массовый адрес, недостоверность, руководители |
| `rmsp` | 2 | нет | категория МСП, численность, получатель поддержки |
| `fns_open` | 2 | нет | режим налогообложения, уплачено налогов за год, недоимка |
| `fns_blocks` | 2 | нет | действующие решения о приостановлении |
| `fssp` | 2 | нет | список производств: сумма, дата, предмет |
| `arbitr` | 2 | нет | дела: роль, сумма, статус |
| `fedresurs` | 2 | нет | банкротство, сообщения о лизинге и залогах |
| `eis` | 2 | нет | контракты: заказчик, сумма, сроки, статус; РНП |
| `zsk` | 2 | нет | уровень риска: `low` / `medium` / `high` |
| `bki` | 3 | **да** | кредиты и займы, просрочки, запросы за 30/90 дней, скор бюро |
| `fns_tax_secret` | 3 | **да** | доходы по декларациям, сальдо ЕНС |
| `ofd` | 3 | **да** | агрегаты по кассам (ряды в `ext_daily`), средний чек, число точек |
| `marketplace` | 3 | **да** | площадки, выручка (ряды в `ext_daily`), рейтинг, остатки |
| `openapi_bank` | 3 | **да** | счета в других банках (сами транзакции лежат в `transactions` с `is_own_bank=false`) |

Для каждого `source` есть Go-структура в `adapters/bankdata/ext/` и соответствующее proto-сообщение в `ClientDataBundle`.

## Схема `core`

```mermaid
erDiagram
  users ||--o{ consents : grants
  users ||--o{ applications : creates
  applications ||--o{ application_events : has
  applications ||--o{ documents : has
  applications ||--o{ score_log : scored

  users {
    uuid user_id PK
    text phone UK
    uuid client_id "→ bank.clients"
    timestamptz last_login_at
  }
  otp_codes {
    uuid id PK
    text phone
    text purpose "login | sign"
    text ref_id "id заявки для sign"
    text code_hash
    timestamptz expires_at
    int attempts
    timestamptz consumed_at
  }
  consents {
    uuid client_id PK
    text source PK
    text status "granted | revoked"
    jsonb scope
    timestamptz granted_at
    timestamptz revoked_at
    timestamptz expires_at
  }
  applications {
    uuid application_id PK
    bigint number UK "№ 48217"
    uuid client_id
    text product_id
    text status
    text purpose
    bigint amount_kop
    int term_months
    text schedule_type "annuity | differentiated | seasonal"
    int rate_bp
    bigint monthly_payment_kop
    jsonb form "поля + флаги autofill"
    jsonb offer_snapshot
    jsonb decision
    text origin "chat | credits | dashboard"
    timestamptz submitted_at
    timestamptz decided_at
    timestamptz signed_at
    timestamptz disbursed_at
  }
  application_events {
    bigint id PK
    uuid application_id FK
    text type
    text status_from
    text status_to
    jsonb payload
    timestamptz created_at
  }
  documents {
    uuid document_id PK
    uuid client_id
    uuid application_id FK
    text kind "invoice | spec | contract | other"
    text filename
    text mime
    bigint size
    text storage_key
    text extract_status
    jsonb extracted
  }
  score_log {
    bigint id PK
    uuid client_id
    uuid application_id FK
    jsonb request
    jsonb response
    text model_version
    timestamptz created_at
  }
```

Также: `sms_outbox` (mock SMS), `notifications`, `audit_log (id, client_id, actor: user|assistant|system, action, details jsonb, created_at)` и таблицы River (создаются миграцией River).

### Статусы заявки

```mermaid
stateDiagram-v2
  [*] --> DRAFT
  DRAFT --> SUBMITTED: клиент нажал «Отправить»
  SUBMITTED --> DATA_CHECK
  DATA_CHECK --> DECLINED: стоп-фактор
  DATA_CHECK --> SCORING
  SCORING --> DECISION
  DECISION --> APPROVED: шанс ≥ порога и сумма ≤ лимита
  DECISION --> COUNTER_OFFER: сумма > лимита
  DECISION --> MANUAL_REVIEW: сумма > авто-лимита
  DECISION --> DECLINED
  COUNTER_OFFER --> APPROVED: клиент принял
  COUNTER_OFFER --> CANCELLED
  MANUAL_REVIEW --> APPROVED: «андеррайтер» (в демо — таймер)
  MANUAL_REVIEW --> DECLINED
  APPROVED --> SIGNED: OTP-подпись
  SIGNED --> DISBURSED: выдача
  DISBURSED --> [*]
  DRAFT --> CANCELLED
  DECLINED --> [*]
  CANCELLED --> [*]
```

Каждый переход пишет строку в `application_events`. Таймлайн на экране — это проекция событий. Переходы выполняет **только** `application.StateMachine`, прямых `UPDATE status` нет.

## Схема `assistant`

| Таблица | Поля | Назначение |
|---|---|---|
| `sessions` | `session_id`, `client_id`, `title`, `slots jsonb`, `created_at`, `updated_at` | Сессия и состояние диалога (что понял ассистент) |
| `messages` | `message_id`, `session_id`, `role` (user / assistant / tool), `content`, `widgets jsonb`, `tool_calls jsonb`, `trace jsonb` (модель, токены, латентность, вызовы), `created_at` | История. Из `trace` строится режим прозрачности |
| `kb_chunks` | `chunk_id`, `doc_path`, `title`, `content`, `embedding vector(1024)`, `tsv tsvector`, `doc_hash` | База знаний для RAG: HNSW-индекс по `embedding` + GIN по `tsv` (гибридный поиск) |

## Индексы, которые нужны с первого дня

```sql
CREATE INDEX ON bank.transactions (client_id, booked_date);
CREATE INDEX ON bank.transactions (client_id, category, booked_date);
CREATE INDEX ON core.applications (client_id, created_at DESC);
CREATE INDEX ON core.application_events (application_id, id);
CREATE INDEX ON assistant.messages (session_id, created_at);
CREATE INDEX ON assistant.kb_chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX ON assistant.kb_chunks USING gin (tsv);
```

## Объёмы

В Postgres загружаются 8 персон и ~200 фоновых клиентов: 24 мес × ~5 операций в день ≈ 0,7–1 млн строк `transactions`. Для этого объёма Postgres справляется без партиционирования. Полная обучающая выборка (тысячи клиентов) живёт в parquet и в БД не грузится.
