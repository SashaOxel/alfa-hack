# Локальный запуск

## Что нужно установить

| Инструмент | Зачем | Кому |
|---|---|---|
| Docker + Compose v2 | Запуск всего стека | Всем |
| Go 1.26+ | Разработка core | Fullstack, Junior |
| Node.js 22 LTS | Фронтенд | Fullstack |
| Python 3.12 + [uv](https://docs.astral.sh/uv/) | ML | ML |
| protoc + плагины (`make proto-tools`) | Только тем, кто меняет `.proto` | Fullstack (+ ML для `alfa/ml`) |
| Ollama (на Mac — **нативно**, не в Docker) | Локальная LLM | Всем, кто работает с чатом |
| sqlc, goose | Только при изменении SQL и миграций | Junior, Fullstack |

## Быстрый старт

```bash
cp deploy/.env.example deploy/.env
```

```bash
ollama pull qwen3:8b && ollama pull bge-m3
```

```bash
make up
```

```bash
make data
```

```bash
make train
```

Откройте http://localhost:3000 и войдите по демо-номеру `+7 900 000-00-01` (кофейня). Код придёт в тосте.

> `make train` нужен один раз (или берёте готовый архив `ml/artifacts`). Пока моделей нет, core можно запустить с `ML_CLIENT=fake`: интерфейс работает на фейковых скорах.

## docker-compose (каркас)

```yaml
# deploy/docker-compose.yml
name: alfa-hack
services:
  postgres:
    image: pgvector/pgvector:pg16
    environment: { POSTGRES_DB: alfa, POSTGRES_USER: alfa, POSTGRES_PASSWORD: alfa }
    ports: ["5432:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck: { test: ["CMD-SHELL", "pg_isready -U alfa"], interval: 5s, retries: 20 }

  ml:
    build: ../ml
    environment: { ARTIFACTS_DIR: /app/artifacts }
    volumes: ["../ml/artifacts:/app/artifacts:ro"]
    ports: ["50051:50051"]

  core:
    build: ../backend
    env_file: .env
    depends_on:
      postgres: { condition: service_healthy }
      ml: { condition: service_started }
    volumes:
      - ../config:/app/config:ro
      - ../prompts:/app/prompts:ro
      - ../kb:/app/kb:ro
      - uploads:/app/uploads
    extra_hosts: ["host.docker.internal:host-gateway"]   # доступ к Ollama на хосте
    ports: ["8080:8080"]

  web:
    build: ../frontend
    depends_on: [core]
    ports: ["3000:80"]

  datagen:                       # make data → docker compose --profile seed run --rm datagen
    profiles: [seed]
    build: { context: ../ml, dockerfile: datagen.Dockerfile }
    env_file: .env
    depends_on: { postgres: { condition: service_healthy } }
    volumes: ["../ml/data:/app/data", "../ml/artifacts:/app/artifacts:ro"]

  ollama:                        # только Linux; на Mac запускайте Ollama нативно
    profiles: [llm-docker]
    image: ollama/ollama
    volumes: [ollama:/root/.ollama]
    ports: ["11434:11434"]
    # для NVIDIA GPU: deploy.resources.reservations.devices: [{ capabilities: [gpu] }]

volumes: { pgdata: {}, uploads: {}, ollama: {} }
```

Core применяет миграции при старте (goose, встроенные через `embed`).

## Переменные окружения

| Переменная | По умолчанию | Описание |
|---|---|---|
| `DATABASE_URL` | `postgres://alfa:alfa@postgres:5432/alfa` | |
| `ML_ADDR` | `ml:50051` | |
| `ML_CLIENT` | `grpc` | `fake` — фейковые скоры без ml-сервиса |
| `LLM_PROVIDER` | `openai_compat` | `openai_compat` \| `gigachat` |
| `LLM_BASE_URL` | `http://host.docker.internal:11434/v1` | Ollama на хосте / vLLM / API организаторов |
| `LLM_API_KEY` | `ollama` | Для Ollama любой непустой |
| `LLM_MODEL` | `qwen3:8b` | |
| `EMBED_BASE_URL`, `EMBED_MODEL` | как LLM / `bge-m3` | |
| `AGENT_MODE` | `tools` | `tools` \| `router` |
| `PII_MASKING` | `auto` | `auto` (вкл. для внешних провайдеров) \| `on` \| `off` |
| `DEMO_MODE` | `true` | Код OTP в ответе, `/demo/reset`, чипы персон |
| `DEMO_OTP_CODE` | пусто | Фиксированный код (удобно на защите) |
| `DEMO_TODAY` | `2026-10-06` | «Сегодня» демо-мира |
| `PIPELINE_STEP_DELAY` | `4s` | Задержка шагов пайплайна заявки (для красоты таймлайна) |
| `AUTO_DECISION_LIMIT_KOP` | `1000000000` | 10 млн ₽ |
| `JWT_SECRET` | генерируется в DEMO_MODE | |
| `DATA_SOURCE` | `synthetic` | Для datagen: `synthetic` \| имя адаптера |

## Порты

| Сервис | Порт |
|---|---|
| web (nginx) | 3000 |
| core HTTP (REST + SSE) | 8080 |
| core gRPC (внутренний, для отладки) | 9090 |
| ml gRPC | 50051 |
| Postgres | 5432 |
| Ollama | 11434 |

## Команды Makefile

| Команда | Что делает |
|---|---|
| `make up` / `make down` / `make logs` | Поднять / остановить / логи стека |
| `make dev-core` | core локально (`go run`), БД и ml в Docker |
| `make dev-web` | Vite dev-server, прокси `/api` → `localhost:8080` |
| `make dev-ml` | ml-сервис локально (`uv run`) |
| `make proto-tools` | Установить protoc-плагины закреплённых версий |
| `make proto` | Сгенерировать Go, Python, OpenAPI и TS из `.proto` |
| `make sqlc` | Сгенерировать Go-код из `backend/queries` |
| `make migrate-new NAME=…` | Новая миграция goose |
| `make data` | Синтетика → parquet → Postgres |
| `make import DATASET=… ADAPTER=…` | Импорт датасета организаторов |
| `make train` | Обучить все модели → `ml/artifacts` |
| `make eval-llm` | Прогнать golden-диалоги через API |
| `make demo-reset` | Сбросить демо-мир (заявки, чаты, согласия, балансы) |
| `make test` | `go test ./...` + `pytest` + `vitest` |
| `make lint` | golangci-lint + ruff + eslint |

## Варианты запуска LLM

| Где | Как |
|---|---|
| **Mac (M-серия)** | `brew install ollama` или приложение Ollama → `ollama serve`. Core в Docker ходит на `host.docker.internal:11434`. Ollama **не** запускать в Docker: там нет доступа к GPU Apple |
| **Linux + NVIDIA** | Профиль `llm-docker` (Ollama) или vLLM: `vllm serve Qwen/Qwen3-8B --enable-auto-tool-choice --tool-call-parser hermes` и `LLM_BASE_URL=http://<host>:8000/v1` |
| **API организаторов** | `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` из их инструкции. Если нет tool calling → `AGENT_MODE=router` |

## Если что-то не скачивается из РФ

Внешние реестры бывают недоступны или медленны. Наши зависимости и обходные пути:

| Что | Признак проблемы | Что делать |
|---|---|---|
| **Docker Hub** (`postgres`, `ollama`, базовые образы) | `pull` висит или отдаёт 403 | Прописать зеркало в `daemon.json` → `"registry-mirrors": ["https://mirror.gcr.io"]` или зеркало вашего облачного провайдера; перезапустить Docker |
| **Go-модули** | `go mod download` таймаутит | `go env -w GOPROXY=https://proxy.golang.org,direct`, при недоступности — альтернативный публичный прокси (например `https://goproxy.io`) |
| **PyPI** (uv) | Медленно | `UV_INDEX_URL=<зеркало PyPI>` |
| **npm** | Медленно | `npm config set registry <зеркало npm>` |
| **Hugging Face** (модели для vLLM, эмбеддинги) | Не открывается | `HF_ENDPOINT=<зеркало HF>`, либо скачать веса один раз и положить в общий volume |
| **Ollama registry** | `ollama pull` падает | Скачать GGUF с доступного зеркала и собрать модель локально: `ollama create qwen3-8b -f Modelfile` (`FROM ./model.gguf`) |
| **protoc / плагины** | — | GitHub Releases и Go-прокси. buf.build не используется совсем ([ADR-0001](../03-architecture/adr/0001-monorepo-contract-first.md)) |

> Совет: за 2–3 дня до защиты соберите все образы и скачайте модели на демо-машину, затем один раз проверьте запуск **без интернета**.

## Частые проблемы

| Симптом | Причина | Решение |
|---|---|---|
| Чат «висит», потом ответ целиком | nginx буферизует SSE | `proxy_buffering off;` и `X-Accel-Buffering: no` для `/api/v1/chat` и `/api/v1/events` |
| `connection refused` к LLM из core | Ollama слушает только localhost | `OLLAMA_HOST=0.0.0.0 ollama serve` |
| Первый ответ ассистента 20+ с | Модель не прогрета | Запрос-прогрев при старте core (`LLM_WARMUP=true`) |
| `ml` падает при старте | Нет артефактов или несовместимая версия бандла | `make train`; проверить `feature_spec.json` |
| Пустой дашборд | Не загружены данные | `make data` |
