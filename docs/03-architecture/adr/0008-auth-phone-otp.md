# ADR-0008: Вход по телефону + mock OTP, JWT в httpOnly cookie

- **Статус:** принято
- **Дата:** 2026-10-06

## Контекст
Решение команды: вход по телефону с mock-SMS. На демо нужно быстро переключаться между персонами. SSE-эндпоинты должны авторизовываться так же, как REST.

## Решение
- `POST /auth/otp {phone}` → код (4 цифры) хэшируется в `otp_codes`, TTL 5 мин. Mock-SMS пишет в `sms_outbox` и лог.
- `DEMO_MODE=true`: ответ содержит `demoCode`, фронт показывает тост «SMS: 4821». Можно задать фиксированный `DEMO_OTP_CODE`.
- На экране входа чипы демо-персон подставляют номер.
- `POST /auth/verify` → JWT (HS256, `sub=user_id`, `cid=client_id`, TTL 12 ч) в cookie `session; HttpOnly; Secure; SameSite=Strict; Path=/api`.
- Middleware gateway и SSE-хендлеров читает cookie и кладёт `client_id` в контекст. gRPC-методы берут его из контекста.
- Тот же механизм OTP (`purpose=sign`) используется для подписания договора.

## Альтернативы
| Вариант | Плюсы | Минусы |
|---|---|---|
| Bearer в памяти + refresh-cookie | Классика SPA | Сложнее: нужен refresh-флоу, заголовки в SSE |
| Без авторизации | Быстро | Не выглядит как банк. Решение команды — телефон + SMS |

## Последствия
- Один origin (nginx проксирует `/api`), CORS не нужен.
- Защита от CSRF: `SameSite=Strict` + заголовок `X-Requested-With`.
