# Ключевые сценарии (sequence-диаграммы)

Обозначения: `W` — web (Vue), `C` — core (Go), `ML` — ml (Python), `L` — LLM, `DB` — Postgres.

## 1. Вход по телефону

```mermaid
sequenceDiagram
  participant W as web
  participant C as core.auth
  participant DB as Postgres
  participant S as sms (mock)
  W->>C: POST /api/v1/auth/otp {phone}
  C->>DB: users по phone (есть ли клиент)
  C->>DB: INSERT otp_codes (hash, ttl 5 мин)
  C->>S: Send(phone, "Код: 4821")
  S->>DB: INSERT sms_outbox
  C-->>W: 200 {otpId, demoCode?}
  Note over W: в DEMO_MODE фронт показывает тост с кодом
  W->>C: POST /api/v1/auth/verify {otpId, code}
  C->>DB: проверка hash, attempts++, consumed_at
  C-->>W: 200 + Set-Cookie session=JWT (httpOnly, SameSite=Strict)
```

## 2. Главная (дашборд одним запросом)

```mermaid
sequenceDiagram
  participant W as web
  participant C as core.client
  participant O as core.offers
  participant F as core.cashflow
  participant B as bankdata
  participant ML as ml
  W->>C: GET /api/v1/dashboard
  par метрики
    C->>B: агрегаты за 12 мес, остаток, нагрузка
  and офферы
    C->>O: TopOffers(client, consents)
    O->>B: BuildBundle(client, consentMask)
    O->>ML: Score(bundle, сценарии по продуктам)
    ML-->>O: pd, grade, approval_prob, shap
  and прогноз
    C->>F: Cashflow(client, 60 дней)
    F->>ML: Forecast(дневные ряды)
    ML-->>F: остаток p10/p50/p90, регулярные платежи
    F->>F: детекция разрыва и причин
  end
  C-->>W: {metrics, health, offers[3], cashGapAlert, applications, dataBoost}
```

Результат кэшируется in-memory на 60 с по ключу `(client_id, consent_hash)` и сбрасывается при смене согласий или статуса заявки.

## 3. Сообщение ассистенту (tool calling + виджеты)

```mermaid
sequenceDiagram
  participant W as web
  participant A as core.assistant
  participant L as LLM
  participant O as core.offers
  participant DB as Postgres
  W->>A: POST /api/v1/chat/sessions/{id}/messages {text} (Accept text/event-stream)
  A->>DB: сохранить user-сообщение, загрузить историю и slots
  A->>A: контекст = system prompt + бриф клиента + slots + RAG + история
  A->>L: chat.completions(stream, tools)
  L-->>A: tool_call find_offers(purpose=equipment, amount=2000000)
  A-->>W: event tool_status "Подбираю варианты…"
  A->>O: ListOffers(client, goal)
  O-->>A: офферы (цифры из движка)
  A-->>W: event widget offer_cards
  A-->>W: event context_update {goal, amount, term}
  A->>L: результат инструмента (компактный JSON)
  L-->>A: текстовые дельты
  A-->>W: event text_delta …
  A->>A: guardrail - числа в тексте есть в результатах инструментов?
  A->>DB: сохранить ответ + widgets + trace
  A-->>W: event message_end {messageId, trace}
```

Если guardrail нашёл число, которого нет в результатах инструментов, ответ перегенерируется один раз со строгой инструкцией. Если и это не помогло, отправляется безопасный шаблон ([ai-assistant.md](ai-assistant.md#guardrails)).

## 4. Калькулятор (слайдеры)

```mermaid
sequenceDiagram
  participant W as web
  participant O as core.offers
  participant ML as ml
  W->>O: POST /api/v1/offers/simulate {productId, amount, term, schedule}
  O->>O: стоп-факторы, ставка, график (Go-калькулятор)
  O->>ML: Score(bundle, [сценарий])
  ML-->>O: approval_prob, shap
  O->>O: what-if сценарии «как повысить шанс» (батч в тот же Score)
  O-->>W: {payment, overpayment, paymentToRevenue, approvalProb, schedule[], tips[]}
```

Bundle клиента кэшируется в core на время сессии: при движении слайдера в ML уходит только набор сценариев.

## 5. Отправка заявки и решение

```mermaid
sequenceDiagram
  participant W as web
  participant AP as core.application
  participant R as River (Postgres)
  participant O as core.offers
  participant ML as ml
  participant N as core.notify
  W->>AP: POST /api/v1/applications/{id}/submit (Idempotency-Key)
  AP->>AP: DRAFT → SUBMITTED, event
  AP->>R: job check_data
  AP-->>W: 202 {status: SUBMITTED}
  R->>AP: check_data
  AP->>O: стоп-факторы (ЗСК, блокировки, банкротство, возраст)
  AP->>N: status DATA_CHECK → SCORING
  N-->>W: SSE application.status
  R->>AP: score
  AP->>ML: Score(bundle, сценарий заявки)
  ML-->>AP: pd, approval_prob, shap
  AP->>AP: INSERT score_log
  R->>AP: decide
  AP->>O: политика решения (порог, лимит, авто-лимит)
  AP->>AP: → APPROVED / COUNTER_OFFER / MANUAL_REVIEW / DECLINED
  AP->>N: событие + уведомление
  N-->>W: SSE application.status {APPROVED, decision}
```

## 6. Подписание и выдача

```mermaid
sequenceDiagram
  participant W as web
  participant AP as core.application
  participant AU as core.auth
  participant B as bankdata
  participant N as core.notify
  W->>AP: POST /applications/{id}/sign/otp
  AP->>AU: IssueOtp(purpose=sign, ref=id)
  AP-->>W: {otpId, demoCode?}
  W->>AP: POST /applications/{id}/sign/confirm {otpId, code}
  AP->>AU: VerifyOtp
  AP->>AP: APPROVED → SIGNED
  AP->>B: эмуляция АБС - транзакция выдачи + запись в loans
  AP->>AP: SIGNED → DISBURSED
  AP->>N: «Деньги на счёте»
  N-->>W: SSE → главная перезапрашивает дашборд (остаток +2 млн)
```

## 7. Подключение источника данных (согласие)

```mermaid
sequenceDiagram
  participant W as web
  participant CS as core.consent
  participant O as core.offers
  participant N as core.notify
  W->>CS: GET /consents (источники + ожидаемый эффект «до +X ₽»)
  W->>CS: POST /consents/ofd {scope, term}
  CS->>CS: INSERT consents, audit
  CS->>O: инвалидировать кэш офферов клиента
  O->>O: пересчёт с новой маской (bundle теперь включает ОФД)
  CS-->>W: {before, after} лимит и ставка
  N-->>W: SSE offers.updated
```

«Ожидаемый эффект» до согласия считается **без доступа к данным источника**: ML подставляет априорные значения признаков для такого клиента (сценарий с `assume_sources`). Реальные данные источника используются только после согласия ([security.md](security.md#согласия)).

## 8. Загрузка счёта и автозаполнение (P1)

```mermaid
sequenceDiagram
  participant W as web
  participant D as core.document
  participant L as LLM
  participant B as bankdata
  W->>D: POST /api/v1/documents (multipart, applicationId)
  D->>D: проверка mime и размера, сохранение в volume
  D-->>W: 201 {documentId, extractStatus: pending}
  D->>D: job extract - текст из PDF (pdftotext)
  D->>L: structured output по JSON-схеме счёта
  L-->>D: {supplierInn, supplierName, total, items[]}
  D->>D: проверка контрольной суммы ИНН
  D->>B: counterparty_registry по ИНН (mock ЕГРЮЛ)
  D-->>W: SSE document.extracted {fields, supplierCheck}
  Note over W: поля заявки заполняются, бейдж «Поставщик проверен»
```
