# ADR-0002: REST через grpc-gateway, стриминг через SSE-хендлеры

- **Статус:** принято
- **Дата:** 2026-10-06

## Контекст
Фронту нужны: обычный REST, стрим ответа ассистента (POST с телом), поток событий о статусах заявок, загрузка файлов. grpc-gateway умеет server-streaming, но отдаёт его как поток JSON-объектов, разделённых переводом строки, без семантики SSE (типы событий, `Last-Event-ID`, heartbeat). Multipart в grpc-gateway неудобен.

## Решение
- Unary-методы: gRPC-сервисы + grpc-gateway (`/api/v1/...`).
- На том же HTTP-сервере (`gateway.Mux.HandlePath` или внешний `http.ServeMux`) регистрируются **ручные хендлеры**:
  - `POST /api/v1/chat/sessions/{id}/messages` → `text/event-stream` (ответ ассистента);
  - `GET /api/v1/events` → `text/event-stream` (статусы заявок, уведомления), heartbeat каждые 15 с, поддержка `Last-Event-ID`;
  - `POST /api/v1/documents` → `multipart/form-data`.
- Ручные хендлеры используют те же middleware (auth, request-id, логирование) и те же Go-сервисы модулей.
- На фронте SSE читается через `@microsoft/fetch-event-source` (поддерживает POST и автопереподключение).
- Payload событий описан proto-сообщениями (`AssistantEvent`, `ClientEvent`) и сериализуется protojson. Чтобы эти типы попали в OpenAPI (а значит, в TypeScript), объявляем технический RPC `EventSchemaService.Describe` с `GET /api/v1/_schema/events`, который возвращает обёртку с обоими сообщениями. Фронт его не вызывает, он нужен только для генерации типов.

## Альтернативы
| Вариант | Плюсы | Минусы |
|---|---|---|
| Server-streaming через grpc-gateway | Ноль ручного кода | Нет типов событий и reconnect, неудобно фронту |
| WebSocket | Двунаправленный | Нам не нужна двунаправленность, сложнее прокси и auth |
| Long polling | Просто | Плохой UX для стрима токенов |

## Последствия
- nginx: для `/api/v1/chat` и `/api/v1/events` выключить буферизацию (`proxy_buffering off`, `X-Accel-Buffering: no`) и увеличить `proxy_read_timeout`.
- Ручных хендлеров ровно три, все остальные методы через gateway.
