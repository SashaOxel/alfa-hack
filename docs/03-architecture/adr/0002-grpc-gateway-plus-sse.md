# ADR-0002: REST через grpc-gateway, стриминг через SSE-хендлеры

- **Статус:** принято
- **Дата:** 2026-10-06
- **Изменение (F2, 07.10.2026):** вместо `EventSchemaService.Describe` — streaming RPC без http-аннотации; хендлеры ходят в gRPC через loopback, а не in-process.

## Контекст
Фронту нужны: обычный REST, стрим ответа ассистента (POST с телом), поток событий о статусах заявок, загрузка файлов. grpc-gateway умеет server-streaming, но отдаёт его как поток JSON-объектов, разделённых переводом строки, без семантики SSE (типы событий, `Last-Event-ID`, heartbeat). Multipart в grpc-gateway неудобен.

## Решение
- Unary-методы: gRPC-сервисы + grpc-gateway (`/api/v1/...`).
- На том же HTTP-сервере (`gateway.Mux.HandlePath` или внешний `http.ServeMux`) регистрируются **ручные хендлеры**:
  - `POST /api/v1/chat/sessions/{id}/messages` → `text/event-stream` (ответ ассистента);
  - `GET /api/v1/events` → `text/event-stream` (статусы заявок, уведомления), heartbeat каждые 15 с, поддержка `Last-Event-ID`;
  - `POST /api/v1/documents` → `multipart/form-data`.
- Ручные хендлеры **не вызывают сервисы напрямую**: они зовут gRPC-сервер core через loopback-клиент, как и gateway. Поэтому auth, request-id и логирование живут в одной цепочке interceptors для всех путей.
- Стримы описаны как **server-streaming RPC без `google.api.http`** (`ChatService.SendMessage`, `EventsService.Stream`): grpc-gateway их не публикует, а сервисы получают типизированный интерфейс. Имя SSE-события — имя заполненного варианта `oneof` (для `ClientEvent` первое `_` заменяется на `.`: `application_status` → `application.status`).
- Отказ в доступе к SSE — обычный HTTP 401: interceptor после проверки сессии сразу отправляет заголовки стрима, а хендлер ждёт их перед тем, как ответить `200 text/event-stream`. Ошибка после старта стрима приходит событием `error` (чат) или закрывает поток.
- На фронте SSE читается через `@microsoft/fetch-event-source` (поддерживает POST и автопереподключение).
- Payload событий — proto-сообщения `AssistantEvent` и `ClientEvent`, protojson. В OpenAPI (и TypeScript) они попадают как схемы, потому что на них ссылаются унарные методы (`GetMessages` возвращает `Widget`, `Trace`). Отдельный технический RPC `EventSchemaService`, как планировалось, не нужен.

## Альтернативы
| Вариант | Плюсы | Минусы |
|---|---|---|
| Server-streaming через grpc-gateway | Ноль ручного кода | Нет типов событий и reconnect, неудобно фронту |
| WebSocket | Двунаправленный | Нам не нужна двунаправленность, сложнее прокси и auth |
| Long polling | Просто | Плохой UX для стрима токенов |

## Последствия
- nginx: для `/api/v1/chat` и `/api/v1/events` выключить буферизацию (`proxy_buffering off`, `X-Accel-Buffering: no`) и увеличить `proxy_read_timeout`.
- Ручных хендлеров ровно три, все остальные методы через gateway.
