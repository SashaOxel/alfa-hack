# Структура репозитория

```
alfa-hack/
├── proto/                        # контракты API — источник правды
│   ├── third_party/              #   вендоренные googleapis (annotations, http, status)
│   └── alfa/
│       ├── core/v1/              #   публичный API (grpc-gateway → REST)
│       └── ml/v1/                #   core → ml: ClientDataBundle, Score, Forecast, Categorize
│
├── backend/                      # Go: сервис core (модульный монолит)
│   ├── cmd/core/
│   ├── internal/{app,config,platform,modules,adapters,db}/
│   ├── queries/                  #   SQL для sqlc
│   ├── migrations/               #   goose
│   ├── gen/                      #   сгенерировано protoc (коммитим)
│   ├── Dockerfile
│   └── sqlc.yaml
│
├── frontend/                     # Vue 3 + Vite
│   ├── src/{api,components,widgets,views,stores,composables,router}/
│   ├── Dockerfile                #   сборка → nginx
│   └── nginx.conf                #   статика + proxy /api (буферизация SSE выключена)
│
├── ml/                           # Python (uv workspace): только работа с моделями
│   ├── packages/
│   │   ├── mlcore/               #   bundle → признаки (общий код train/serve)
│   │   ├── ml_service/           #   gRPC-сервер инференса
│   │   ├── datagen/              #   синтетика, персоны, адаптеры датасетов
│   │   └── training/             #   обучение, калибровка, model cards
│   ├── eval/                     #   eval LLM-агента и моделей
│   ├── notebooks/                #   исследования (не используются в рантайме)
│   ├── gen/                      #   сгенерировано grpcio-tools (коммитим)
│   ├── artifacts/                #   модели (gitignored)
│   ├── data/                     #   parquet (gitignored)
│   └── Dockerfile
│
├── config/                       # бизнес-конфиги (читает Go)
│   ├── products.yaml
│   ├── pricing.yaml
│   ├── gov_programs.yaml
│   └── feature_texts.yaml
├── prompts/assistant/            # промпты агента (читает Go, пишет ML-LLM)
├── kb/                           # база знаний для RAG (markdown)
│
├── deploy/
│   ├── docker-compose.yml
│   └── .env.example
├── scripts/
│   ├── gen-proto.sh
│   ├── proto-tools.env           #   версии protoc и плагинов
│   └── demo-reset.sh
├── docs/
├── Makefile
└── README.md
```

## Кто чем владеет (CODEOWNERS)

```
/proto/alfa/core/          @fullstack
/proto/alfa/ml/            @fullstack @ml-models            # контракт данных — обе стороны
/backend/                  @fullstack
/backend/internal/modules/{auth,consent,notify,audit}/   @junior @fullstack
/backend/internal/adapters/bankdata/                     @junior @fullstack @ml-data
/frontend/                 @fullstack
/ml/packages/datagen/      @ml-data
/ml/packages/{mlcore,training,ml_service}/  @ml-models
/ml/eval/                  @ml-llm
/prompts/  /kb/            @ml-llm
/config/                   @fullstack @ml-models
/docs/                     все
```

## Что генерируется и коммитится

| Каталог | Чем | Команда |
|---|---|---|
| `backend/gen/` | protoc + go, go-grpc, grpc-gateway | `make proto` |
| `ml/gen/` | grpcio-tools | `make proto` |
| `frontend/src/api/openapi.yaml`, `schema.d.ts` | protoc-gen-openapi + openapi-typescript | `make proto` |
| `backend/internal/db/` | sqlc | `make sqlc` |

Генерированный код руками не правим. Изменили proto или SQL — перегенерировали и закоммитили в том же PR.
