# Каталог признаков

Код признаков: `ml/packages/mlcore/features/`. **Один и тот же код** работает при обучении (бандлы из parquet) и в инференсе (бандл от Go). Вход — `ClientDataBundle`, выход — словарь `{feature: float | NaN}`.

## Контракт ClientDataBundle

Описание в [proto-draft.md → bundle.proto](../04-api/proto-draft.md#alfamlv1bundleproto--контракт-данных-для-моделей).

| Сторона | Что делает |
|---|---|
| Go (`bankdata.BuildBundle`) | SQL-агрегации по `bank.transactions` (по категориям), остатки, долги, снимки внешних источников **с учётом маски согласий** |
| Python офлайн (`mlcore.bundle.from_parquet`) | То же из parquet для обучающей выборки |
| Контрактный тест | Для каждой персоны бандл из Go (JSON через protojson) == бандл из Python (с допуском 0 коп.). Падает, если кто-то поменял логику агрегации на одной стороне |

> Логика «какая транзакция к какой категории относится» не дублируется: категория уже лежит в `bank.transactions.category` (её проставил категоризатор при загрузке). Обе стороны просто суммируют по ней.

## Категории транзакций

| Направление | Категории |
|---|---|
| Поступления | `revenue_acquiring`, `revenue_sbp`, `revenue_b2b`, `revenue_marketplace`, `revenue_gov`, `loan_received`, `own_transfer_in`, `refund_in`, `other_in` |
| Списания | `suppliers`, `payroll`, `taxes`, `rent`, `debt_service`, `leasing`, `mfo`, `owner_withdrawal`, `cash_out`, `own_transfer_out`, `bank_fees`, `marketing`, `utilities`, `other_out` |

## Признаки

Окна: `3m`, `6m`, `12m` — последние полные месяцы до `as_of`. Tier — уровень источника ([data-sources.md](../01-research/data-sources.md#уровни-данных)). Если источника нет (не подключён или не дан согласие), признак равен `NaN`. CatBoost обрабатывает это нативно.

### Профиль (Tier 0–2)

| Признак | Формула / смысл | Tier |
|---|---|---|
| `business_age_months` | от даты регистрации | 0 |
| `months_with_bank` | от открытия счёта | 1 |
| `okved_section` | буква раздела ОКВЭД (категориальный) | 0 |
| `okved_risk_class` | отраслевой класс риска из справочника (1–5) | 0 |
| `legal_form` | IP / OOO (категориальный) | 0 |
| `tax_regime` | категориальный | 2 |
| `employees` | из реестра МСП или по числу получателей ЗП | 1–2 |
| `region_code` | категориальный | 0 |

### Выручка и обороты (Tier 0–1)

| Признак | Формула | Tier |
|---|---|---|
| `revenue_avg_3m`, `_6m`, `_12m` | среднее `revenue_kop` | 0 |
| `inflow_avg_3m` | среднее оборотов | 0 |
| `revenue_to_inflow_6m` | `Σrevenue / Σinflow` (низкое значение — транзит или займы) | 0–1 |
| `revenue_growth_qoq` | `rev(3m) / rev(пред. 3m) − 1` | 1 |
| `revenue_growth_yoy` | `rev(3m) / rev(те же 3m год назад) − 1` | 1 |
| `revenue_cv_12m` | `std / mean` помесячной выручки | 1 |
| `revenue_trend_slope_12m` | наклон линейной регрессии log(revenue) | 1 |
| `revenue_drawdown_6m` | максимальное падение месяц к месяцу | 1 |
| `seasonality_strength` | `1 − var(остаток после сезонности) / var(ряд)` по 24 мес | 1 |
| `share_acquiring`, `share_sbp`, `share_b2b`, `share_marketplace`, `share_gov` | доли каналов выручки за 6m | 1 |

### Ликвидность (Tier 0–1)

| Признак | Формула | Tier |
|---|---|---|
| `balance_last` | остаток на `as_of` | 0 |
| `balance_avg_3m`, `balance_min_3m` | | 1 |
| `balance_to_revenue` | `balance_avg_3m / revenue_avg_3m` | 1 |
| `low_balance_days_3m` | дни с остатком < 10% месячных расходов | 1 |
| `negative_fcf_months_12m` | число месяцев с отрицательным FCF | 1 |

### Расходы и денежный поток (Tier 1)

| Признак | Формула |
|---|---|
| `fcf_avg_6m` | `revenue − suppliers − payroll − rent − taxes − marketing − utilities − bank_fees` (без долгов, переводов себе и изъятий) |
| `fcf_margin_6m` | `fcf_avg_6m / revenue_avg_6m` |
| `payroll_share`, `rent_share`, `suppliers_share` | доля в выручке за 6m |
| `owner_withdrawal_share` | изъятия / выручка |
| `cash_out_share` | (снятия + переводы ФЛ) / обороты |
| `tax_regularity_12m` | доля месяцев с ожидаемым налоговым платежом, где он был |

### Долговая нагрузка (Tier 0–1, уточняется в Tier 3)

| Признак | Формула | Tier |
|---|---|---|
| `credit_load` | `debt_service_avg_3m / revenue_avg_3m` (**входной параметр трека**) | 0 |
| `dscr_6m` | `fcf_avg_6m / debt_service_avg_6m` (если долгов нет — большое число) | 1 |
| `debt_hidden_detected` | 1, если найдены платежи по кредитам или лизингу в других банках | 1 |
| `mfo_payments_6m` | сумма платежей в МФО | 1 |
| `loans_received_6m` | новые займы за 6m («кредитный голод») | 1 |
| `bki_total_debt_to_revenue` | `bki.total_debt / revenue_12m` | 3 |
| `bki_max_overdue_24m` | | 3 |
| `bki_inquiries_30d` | | 3 |

### Контрагенты (Tier 1)

| Признак | Формула |
|---|---|
| `customer_hhi`, `top_customer_share` | концентрация покупателей |
| `supplier_hhi` | концентрация поставщиков |
| `active_customers_90d` | |
| `has_marketplace_payouts`, `has_gov_customers`, `has_equipment_supplier_prepay` | флаги-сигналы (также используются в Go для матрицы «сигнал → продукт») |

### Внешние риски (Tier 2)

| Признак | Источник |
|---|---|
| `tax_debt_to_revenue` | `fns_open.tax_debt / revenue_avg_3m` |
| `fns_blocked` | `fns_blocks` |
| `fssp_count`, `fssp_total_to_revenue` | `fssp` |
| `arbitr_defendant_12m`, `arbitr_claims_to_revenue` | `arbitr` |
| `egr_mass_address`, `egr_unreliable` | `egr` |
| `zsk_level` | `zsk` (категориальный) |
| `eis_contracts_12m`, `eis_active_to_revenue` | `eis` |

### Источники по согласию (Tier 3)

| Признак | Источник | Почему повышает лимит |
|---|---|---|
| `ofd_revenue_to_bank_revenue` | `ofd` | Видна наличная выручка: реальная выручка больше, чем по выписке |
| `ofd_avg_check`, `ofd_points` | `ofd` | Масштаб и устойчивость |
| `mp_revenue_to_payouts` | `marketplace` | Продажи vs выплаты: видно удержания и будущие поступления |
| `mp_rating`, `mp_buyout_rate` | `marketplace` | Качество бизнеса селлера |
| `tax_declared_to_bank_revenue` | `fns_tax_secret` | Подтверждение выручки |

## Признаки-утечки (запрещены)

- `category_true` и любые поля генератора, кроме наблюдаемых.
- Скрытые факторы (`quality`, `growth`…) и любые производные от метки.
- Информация после `as_of`.
- Пол, возраст, ФИО владельца ([security.md](../03-architecture/security.md#ответственный-ии-и-кредитование)).

## Тексты для объяснений

`config/feature_texts.yaml` (владелец — ML, читает Go при сборке `Factor`):

```yaml
revenue_growth_qoq:
  positive: "Выручка растёт: +{value:pct} за квартал"
  negative: "Выручка снизилась на {value:pct_abs} за квартал"
credit_load:
  positive: "Низкая кредитная нагрузка ({value:pct})"
  negative: "Высокая кредитная нагрузка ({value:pct})"
mfo_payments_6m:
  negative: "Есть платежи в микрофинансовые организации"
tax_debt_to_revenue:
  negative: "Есть задолженность по налогам"
business_age_months:
  positive: "Бизнес работает {value:months}"
  negative: "Бизнес работает меньше года"
```

Направление (`positive` / `negative`) берётся из знака SHAP относительно PD: уменьшает риск — positive.
