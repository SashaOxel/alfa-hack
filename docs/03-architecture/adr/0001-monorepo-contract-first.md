# ADR-0001: Монорепозиторий и contract-first на protoc (без buf)

- **Статус:** принято
- **Дата:** 2026-10-06
- **Изменение:** изначально планировали buf. **buf исключён решением капитана**: из РФ ненадёжен доступ к buf.build (remote-плагины, BSR), работать с ним неудобно.

## Контекст
Три языка (Go, Python, TypeScript), пять человек, две недели. Самый частый источник поломок на хакатонах — рассинхрон контрактов между фронтом, бэком и ML. Тулчейн должен работать из РФ без внешних SaaS-зависимостей.

## Решение
- Один репозиторий: `frontend/`, `backend/`, `ml/`, `proto/`, `config/`, `prompts/`, `kb/`, `deploy/`, `scripts/`, `docs/`.
- Все API (публичные и внутренние) описаны в `proto/`.
- Генерация — **обычный `protoc` + локальные плагины**, версии закреплены в `scripts/proto-tools.env`, установка — `make proto-tools`:

| Что | Инструмент | Откуда ставится |
|---|---|---|
| Компилятор | `protoc` | GitHub Releases protobuf / `brew install protobuf` / `apt install protobuf-compiler` |
| Go-сообщения | `protoc-gen-go` | `go install google.golang.org/protobuf/cmd/protoc-gen-go@<ver>` |
| Go gRPC | `protoc-gen-go-grpc` | `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@<ver>` |
| REST-шлюз | `protoc-gen-grpc-gateway` | `go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@<ver>` |
| OpenAPI v3 для фронта | `protoc-gen-openapi` (gnostic) | `go install github.com/google/gnostic/cmd/protoc-gen-openapi@<ver>` |
| Python | `grpcio-tools` (свой встроенный protoc) | `uv add --dev grpcio-tools` |
| TypeScript-типы | `openapi-typescript` | npm devDependency |

- Сторонние `.proto` (`google/api/annotations.proto`, `google/api/http.proto`, `google/rpc/status.proto`) **вендорим** в `proto/third_party/`, скопировав из репозитория googleapis на GitHub. Сеть при генерации не нужна.
- `make proto` (скрипт `scripts/gen-proto.sh`) за один запуск:
  1. Go + gRPC + gateway для `alfa/core/v1` и `alfa/ml/v1` → `backend/gen`;
  2. Python для `alfa/ml/v1` → `ml/gen` (Python-сервису нужен только ML-контракт);
  3. OpenAPI v3 для `alfa/core/v1` → `frontend/src/api/openapi.yaml` → `schema.d.ts`.
- Сгенерированный код **коммитим**: джуну и ML не нужно ставить тулчейн, чтобы собрать проект. Генерирует тот, кто меняет proto.
- **Валидация запросов — в Go**, в сервисном слое (явные проверки + маленький helper `validate`). Proto-аннотации валидации не используем: они тянут лишние зависимости.
- **Линт и обратная совместимость — дисциплиной**: стиль Google AIP, enum с префиксом и `*_UNSPECIFIED = 0`, номера полей не переиспользуем, удалённые поля — в `reserved`. Изменение proto — только через PR с ревью потребителя. При желании: `protolint` (`go install github.com/yoheimuta/protolint/cmd/protolint@<ver>`).

## Альтернативы
| Вариант | Плюсы | Минусы |
|---|---|---|
| buf | Lint, breaking, удобный конфиг | **Исключён**: зависимость от buf.build, проблемы доступа из РФ |
| Генерация в Docker-образе | Одинаковое окружение у всех | Зависимость от Docker Hub (тоже бывает недоступен), дольше итерация. Оставляем как опцию |
| REST + OpenAPI вручную | Привычнее фронту | Стек требует grpc-gateway, двойная работа |

## Последствия
- Нет автоматического breaking-check: сломать контракт можно незаметно. Компенсируем ревью и интеграционными тестами персон.
- Версии плагинов у всех одинаковые за счёт `proto-tools.env`. Иначе сгенерированный код будет «дёргаться» в диффах.
