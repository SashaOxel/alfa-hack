# Документация: «Умный кредитный кабинет ММБ»

Трек хакатона: личный кабинет для малого и микробизнеса с ИИ‑ассистентом, который понимает потребности клиента, подбирает кредитный продукт и проводит весь путь от заявки до решения.

## Структура

```
docs/
├── README.md                     ← вы здесь: навигация
├── 00-overview/                  ← зачем мы это делаем и что уже решили
│   ├── vision.md                 видение, wow-фичи, позиционирование
│   ├── decisions.md              ответы на вопросы, допущения, открытые вопросы
│   └── glossary.md               словарь терминов (банк + техника)
├── 01-research/                  ← ресёрч предметной области
│   ├── data-sources.md           какие данные банк может получить о клиенте и что из них вытянуть
│   ├── products.md               каталог кредитных продуктов + матрица «сигнал → продукт»
│   └── market.md                 конкуренты и контекст рынка (осень 2026)
├── 02-product/                   ← продукт и демо
│   ├── personas.md               демо-персоны (кофейня, селлер, СТО…)
│   ├── user-flows.md             экраны, пользовательские сценарии, правки к дизайну
│   └── demo-script.md            сценарий защиты поминутно
├── 03-architecture/              ← как устроена система
│   ├── overview.md               C4-диаграммы, принципы, стек
│   ├── services.md               сервисы и модули, зоны ответственности
│   ├── data-model.md             схемы БД и владельцы данных
│   ├── flows.md                  sequence-диаграммы ключевых сценариев
│   ├── ai-assistant.md           устройство ИИ-агента, LLM, RAG, guardrails
│   ├── security.md               безопасность, 152-ФЗ, согласия, ответственный ИИ
│   └── adr/                      архитектурные решения (ADR)
├── 04-api/                       ← контракты
│   ├── api.md                    REST/SSE для фронта, конвенции, примеры
│   └── proto-draft.md            черновики .proto (источник правды — /proto)
├── 05-ml/                        ← данные и модели
│   ├── data-generation.md        синтетика + адаптеры под датасеты организаторов
│   ├── features.md               каталог признаков
│   ├── models.md                 скоринг, SHAP, прогноз кассового разрыва, категоризация
│   └── offer-engine.md           подбор оффера: стоп-факторы, лимит, ставка, ранжирование
├── 06-dev/                       ← как разрабатывать
│   ├── repo-structure.md         структура монорепозитория
│   ├── local-setup.md            запуск через docker compose
│   ├── conventions.md            git, код-стайл, Definition of Done
│   └── junior-onboarding.md      гайд для новичка на бэкенде
└── 07-plan/                      ← план работ
    ├── roadmap.md                вехи на 2 недели, приоритеты P0/P1/P2, риски
    └── backlog.md                задачи по людям
```

## Что читать в зависимости от роли

| Роль | Порядок чтения |
|---|---|
| **Все (обязательно)** | [vision](00-overview/vision.md) → [decisions](00-overview/decisions.md) → [personas](02-product/personas.md) → [architecture overview](03-architecture/overview.md) → [roadmap](07-plan/roadmap.md) |
| **Fullstack (Go + Vue)** | [services](03-architecture/services.md) → [api](04-api/api.md) → [proto-draft](04-api/proto-draft.md) → [flows](03-architecture/flows.md) → [user-flows](02-product/user-flows.md) |
| **Junior backend** | [junior-onboarding](06-dev/junior-onboarding.md) → [local-setup](06-dev/local-setup.md) → [api](04-api/api.md) → [data-model](03-architecture/data-model.md) |
| **ML: данные** | [data-sources](01-research/data-sources.md) → [data-generation](05-ml/data-generation.md) → [features](05-ml/features.md) |
| **ML: модели и офферы** | [products](01-research/products.md) → [models](05-ml/models.md) → [offer-engine](05-ml/offer-engine.md) |
| **ML: LLM / агент** | [ai-assistant](03-architecture/ai-assistant.md) → [products](01-research/products.md) → [security](03-architecture/security.md) |
| **Питч / защита** | [vision](00-overview/vision.md) → [market](01-research/market.md) → [demo-script](02-product/demo-script.md) |

## Правила ведения документации

- Принимаете решение, которое трудно откатить (меняет контракт, БД или стек) — пишете ADR в [`03-architecture/adr/`](03-architecture/adr/) по [шаблону](03-architecture/adr/template.md).
- Получили ответ на открытый вопрос — обновите [decisions.md](00-overview/decisions.md).
- Источник правды для API — `.proto` в `/proto`. `04-api/proto-draft.md` — стартовый черновик, после появления `/proto` он не обновляется.
- Диаграммы пишем в Mermaid прямо в markdown: GitHub рендерит их сам.
