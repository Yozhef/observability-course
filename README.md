# Observability Course — Demo Stack

Локальний стек для демонстрації обзервабіліті: **Go-сервіс → Prometheus (метрики) → Grafana (дашборд) + OpenTelemetry → Tempo (трейси)**.

## Архітектура

```
                 ┌──────────────┐   scrape /metrics   ┌────────────┐
  curl ──────▶   │ demo-service │ ◀────────────────── │ Prometheus │
                 │  (Go, :8080) │                     │   :9090    │
                 └──────┬───────┘                     └─────┬──────┘
                        │ OTLP HTTP (трейси)                │ PromQL
                        ▼                                   ▼
                 ┌──────────────┐                     ┌────────────┐
                 │    Tempo     │ ◀────── TraceQL ─── │  Grafana   │
                 │ :4318 / :3200│                     │   :3000    │
                 └──────────────┘                     └────────────┘
```

## Структура проєкту

```
observability-course/
├── docker-compose.yml
├── app/                    # Go-сервіс: 3 ендпоінти + /metrics + OTel
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
├── prometheus/prometheus.yml
├── tempo/tempo.yaml
└── grafana/
    ├── provisioning/
    │   ├── datasources/datasources.yml   # Prometheus + Tempo
    │   └── dashboards/dashboards.yml
    └── dashboards/demo-service.json      # готовий дашборд
```

## Швидкий старт

> **Передумова:** Docker-демон має бути запущений. Якщо бачиш помилку
> `failed to connect to the docker API at unix:///.../docker.sock` — виконай `open -a Docker`
> (Docker Desktop), дочекайся статусу "running" і повтори команду.

```bash
make demo
```

Ця команда сама: підніме стек → дочекається сервісу → нажене ~300 запитів → відкриє Grafana одразу на дашборді → крутитиме навантаження ~24 RPS протягом 2 хвилин.

Або по кроках:

```bash
make up      # docker compose up -d --build
make load    # 2 хв навантаження, середній RPS ~24 (>15)
make open    # відкрити дашборд у браузері
```

Інші команди: `make load-forever` (нескінченне навантаження), `make logs`, `make down`, `make clean` (з видаленням volume та образів).

## Доступи та URL

| Сервіс | URL | Логін |
|---|---|---|
| **Grafana — RED дашборд** | http://localhost:3000/d/demo-service | не потрібен (anonymous Admin) |
| **Grafana — USE дашборд (ресурси)** | http://localhost:3000/d/use-resources | не потрібен |
| Grafana — головна | http://localhost:3000 | не потрібен |
| Grafana — трейси (Tempo) | http://localhost:3000/explore | не потрібен |
| Prometheus | http://localhost:9090 | — |
| Demo-сервіс | http://localhost:8080 | — |
| Tempo API | http://localhost:3200 | — |
| cAdvisor (метрики контейнерів) | http://localhost:8081 | — |
| node-exporter (метрики хоста) | http://localhost:9100/metrics | — |

## Як зайти на дашборд Grafana

1. Відкрий **http://localhost:3000/d/demo-service** — це прямо дашборд **"Demo Service — RED Metrics"** (логін/пароль не потрібні, форму входу вимкнено).
2. Якщо відкрив головну (`localhost:3000`): зліва **Dashboards → Demo Service — RED Metrics**.
3. Автооновлення вже ввімкнене (5s), діапазон — останні 15 хвилин.
4. Трейси: зліва **Explore** → у селекторі datasource вибери **Tempo** → **Search** → service `demo-service` → Run query. Відкрий будь-який трейс `/api/orders` — побачиш вкладені `db.query` спани.

## Ендпоінти

- `GET /api/hello` — швидкий, завжди 200
- `GET /api/users` — cache lookup + 1 "запит у БД", ~10% помилок 500
- `GET /api/orders` — 2 "запити в БД" (повільніший), помилки теж бувають
- `GET /metrics` — Prometheus-метрики
- `GET /healthz` — healthcheck

## Скрипт навантаження (щоб побачити метрики)

Скрипт `load.sh` розкидає запити по трьох ендпоінтах паралельно і тримає **середній RPS вище 15** (за замовчуванням ~24 RPS протягом 2 хвилин):

```bash
./load.sh            # 120 секунд, ~24 RPS
./load.sh 300 40     # 5 хвилин, ~40 RPS
make load            # те саме, що ./load.sh 120 24
make load-forever    # нескінченно, поки не зупиниш Ctrl+C
```

У кінці скрипт друкує фактичний середній RPS. Поки він працює — відкрий дашборд і дивись, як наповнюються графіки (панель "Requests per Second by Endpoint" покаже ~8 RPS на кожен з 3 ендпоінтів = ~24 сумарно).

Разові виклики руками:

```bash
curl localhost:8080/api/hello
curl localhost:8080/api/users
curl localhost:8080/api/orders
```

## Що дивитись

1. **Grafana → Dashboards → "Demo Service — RED Metrics"** — RPS, error rate, p50/p95/p99, статус-коди.
2. **Prometheus → Graph** — спробуй PromQL вручну:
   `rate(http_requests_total[1m])`, `histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))`.
3. **Grafana → Explore → Tempo** — вибери Search, service `demo-service`. Відкрий трейс `/api/orders`: побачиш server span + два вкладені `db.query` спани; у трейсах з 500 — записана помилка.

## Ключові поняття для курсу

- **RED-метрики** (про сервіс, дашборд `demo-service`): Rate (`http_requests_total`), Errors (`status=~"5.."`), Duration (histogram + `histogram_quantile`).
- **USE-метрики** (про ресурси, дашборд `use-resources`): **U**tilization — CPU/пам'ять контейнерів (cAdvisor: `container_cpu_usage_seconds_total`, `container_memory_usage_bytes`) і хоста (node-exporter: `node_cpu_seconds_total`, `node_memory_*`); **S**aturation — `node_load1`; **E**rrors — мережеві/дискові помилки. Плюс внутрішні метрики Go-процесу: `go_goroutines`, `process_cpu_seconds_total`, `process_resident_memory_bytes` (client_golang експонує їх автоматично). Кількість запущених контейнерів: `count(container_last_seen{name=~".+"})`.
- **Pull-модель Prometheus**: сервіс лише експонує `/metrics`, Prometheus сам скрейпить кожні 5s.
- **OTel SDK**: `TracerProvider` + OTLP exporter → Tempo; `otelhttp.NewHandler` створює server span на кожен запит, ручні спани (`tracer.Start`) — для внутрішньої роботи.
- **Context propagation**: W3C `traceparent` заголовок — якщо додати другий сервіс, трейс "продовжиться" крізь нього.

## Можливі наступні кроки курсу

- Додати другий сервіс і показати розподілений трейс.
- Exemplars: зв'язати histogram-метрики з трейсами (кнопка "trace" прямо з графіка латентності).
- OTel Collector між сервісом і Tempo (batching, sampling, розгалуження на кілька бекендів).
- Loki для логів → повна тріада metrics/logs/traces в одній Grafana.
- Alerting: правило в Prometheus на error rate > 5%.
