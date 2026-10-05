# third_party

Вендоренные `.proto` из [googleapis](https://github.com/googleapis/googleapis). Сеть при генерации не нужна ([ADR-0001](../../docs/03-architecture/adr/0001-monorepo-contract-first.md)).

| Файл | Зачем |
|---|---|
| `google/api/annotations.proto`, `google/api/http.proto` | HTTP-аннотации `google.api.http` для grpc-gateway и OpenAPI |

Руками не правим. Go-код для этих файлов не генерируем: он есть в модуле `google.golang.org/genproto/googleapis/api`.
Нужен ещё один файл googleapis (например, `google/rpc/status.proto`) — кладём сюда с тем же путём и дописываем в таблицу.
