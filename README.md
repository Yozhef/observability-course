# AI-Native Observability — Demo Stack

Повний локальний стек для 3-денного курсу: **3 Go-сервіси → OTel Collector → Tempo (трейси), Prometheus (метрики + exemplars), Loki+Alloy (логи), GlitchTip (errors), RabbitMQ, PostgreSQL, Grafana (дашборди + алерти)**. Кожен урок курсу має свою `make`-команду, яка відтворює інцидент наживо.

## Одна команда

```bash
make demo
```

Вона: перевірить Docker → збілдить і підніме всі ~18 контейнерів → дочекається готовності → автоматично налаштує GlitchTip (user/org/project/DSN) → нажене стартові метрики і трейси → відкриє Grafana на дашборді → крутитиме навантаження 5 хвилин.

> **Вимоги:** Docker Desktop запущений, 6-8 GB RAM для Docker, вільні порти 3000, 3100, 3200, 5433, 5672, 8000, 8080-8083, 8090, 9090, 9100, 15672.

## Архітектура

```
 curl ──▶ checkout :8080 ──HTTP(traceparent)──▶ payment :8081 ──▶ [provider stub]
             │  │  │                                │
             │  │  └─▶ RabbitMQ ──▶ worker ──▶ PostgreSQL (entitlements)
             │  └────▶ PostgreSQL (orders)          │
             │                                      │
             └── traces ─▶ OTel Collector ─▶ Tempo ─┴─ service graph ─▶ Prometheus
                 logs   ─▶ Alloy ─▶ Loki                      ▲
                 errors ─▶ GlitchTip                          │ scrape /metrics
                                                              │ (+ exemplars)
                       Grafana ◀── дашборди, алерти, Explore ─┘
```

## Адреси

| Що | URL | Доступ |
|---|---|---|
| **Grafana — RED дашборд** | http://localhost:3000/d/demo-service | без логіна |
| **Grafana — USE дашборд** | http://localhost:3000/d/use-resources | без логіна |
| Grafana — Alerting | http://localhost:3000/alerting/list | без логіна |
| Grafana — Explore (Loki/Tempo) | http://localhost:3000/explore | без логіна |
| **GlitchTip (errors)** | http://localhost:8000 | admin@course.local / course-demo-123 |
| RabbitMQ management | http://localhost:15672 | course / course |
| Prometheus | http://localhost:9090 | — |
| checkout / payment / worker | :8080 / :8081 / :8083 | — |
| cAdvisor / node-exporter | :8082 / :9100 | — |
| alert-sink (webhook echo) | :8090 | `make alerts-log` |

## Уроки по днях

Між будь-якими демо повертай стек у baseline: **`make reset`**.

### День 1 — Logs, Errors, RCA

| Демо | Команда | Що показувати |
|---|---|---|
| Біль grep-а | `LOG_FORMAT=text make up` → `make incident-day1` → `docker compose logs checkout \| grep -i error` | час на секундомір |
| Той самий інцидент у LogQL | Grafana → Explore → Loki: `{service="checkout"} \| json \| level="error"` | 30 секунд проти хвилин |
| Історія операції | `{service=~".+"} \| json \| correlation_id="checkout-<id>"` | lifecycle з усіх сервісів |
| Логи як графік | `sum(count_over_time({service="checkout"} \| json \| level="error" [1m]))` | місток до метрик |
| Перший Issue | `make incident-day1` → GlitchTip | push-модель error tracking |
| Release + Regression | `make deploy-bad` → `make deploy-fix` → знову `make deploy-bad` | Regression badge, annotation |
| Timeline із джерел | вивід deploy-bad + GlitchTip timestamps + Loki | кожен рядок має джерело |

### День 2 — Metrics, SLO, Alerting

| Демо | Команда | Що показувати |
|---|---|---|
| Шлях метрики | `curl -s localhost:8080/metrics \| grep http_requests` → Prometheus Targets | pull-модель |
| Cardinality-вибух | `make cardinality-bomb` | TSDB head series росте |
| RED під навантаженням | `./load.sh 300 30 --profile degradation` | Rate стабільний, Errors/p95 ростуть |
| Deployment annotation | `make deploy-bad` / `make deploy-fix` | лінія на графіку = «що змінилось» |
| USE наживо | `make chaos-cpu` (потім `make chaos-cpu-off`) | черга росте раніше за p95 |
| SLO burn → алерт | `make incident-day2` → Alerting → `make alerts-log` | Normal→Pending→Firing→webhook |
| Exemplars | RED дашборд → ромбики на p95 → клік → Tempo | з агрегату в конкретний trace |

Recording rules і burn-rate правила: `prometheus/rules.yml`, `grafana/provisioning/alerting/alerting.yml`. Product-метрики: `checkout_started_total`, `payment_succeeded_total`, `payment_failed_total{kind}`, `entitlement_granted_total`.

### День 3 — Distributed Tracing, MCP

| Демо | Команда | Що показувати |
|---|---|---|
| Перший waterfall | `curl localhost:8080/api/checkout` → Tempo | 4 сервіси в одному trace |
| Sequential vs parallel | `make parallel-off` / `make parallel-on` | 1.5s → 700ms, critical path |
| traceparent наживо | `curl -v localhost:8080/api/checkout` | header + той самий trace_id у логах |
| Зламаний propagation | `make break-propagation` / `make fix-propagation` | два trace замість одного |
| Context через RabbitMQ | RabbitMQ UI → message headers | traceparent у headers |
| Consumer lag | `make incident-3` → `make workers-back` | queue depth, broker-wait span |
| N+1 | `make nplus1` → Tempo, обидва traces | драбинка vs один JOIN |
| Pool exhaustion | `make incident-2` | db.acquire_connection 3s, query 8ms |
| Provider degradation | `make incident-1` | Кейс 1: RED→Trace→Logs→Service Graph |
| Service Graph | Grafana → Explore → Tempo → Service Graph | topology з трейсів |
| Повний цикл | p95 → exemplar → trace → Logs for this span | без copy-paste |
| Sampling у Collector | `make collector-sample-20` / `collector-sample-100` | політика без зміни коду |
| AI-розслідування | `make mcp-token` → підключи агента → `make incident-1` | evidence-based investigation |

## Усі команди

```
make demo                 # ОДНА КОМАНДА: все підняти + налаштувати + нагнати дані
make up / down / clean    # старт / стоп / повне прибирання (з volume)
make reset                # baseline між демо (вимкнути chaos, повернути worker)
make load                 # 2 хв ~24 RPS   |  make load-forever
./load.sh 300 40 --profile spike|degradation --endpoint orders|checkout|...
make incident-day1        # День 1: помилки+затримки для logs/errors демо
make deploy-bad|deploy-fix# реліз з регресією / відкат (+annotation, +release)
make incident-day2        # День 2: SLO burn → живий алерт у alert-sink
make cardinality-bomb     # День 2: антидемо high-cardinality
make chaos-cpu|chaos-mem  # День 2: USE-демо
make incident-1|2|3       # День 3: provider / pool / consumer lag
make workers-back         # повернути worker після incident-3
make break-propagation    # День 3: антидемо (fix-propagation — назад)
make parallel-on|off      # День 3: critical path демо
make nplus1               # День 3: N+1 trace
make collector-sample-20  # День 3: sampling без зміни коду
make mcp-token            # День 3: read-only токен для Grafana MCP
make alerts-log / logs    # повідомлення алертів / логи застосунків
```

## Fault injection (для власних сценаріїв)

Кожен сервіс має runtime-ручки без рестартів:

```bash
curl "localhost:8080/admin/chaos?error_rate=0.3&delay_ms=500"     # checkout
curl "localhost:8081/admin/chaos?provider_error_rate=0.5"          # payment
curl "localhost:8080/admin/chaos?pool_max=2&query_delay_ms=200"    # pool демо
curl "localhost:8080/admin/reset"                                  # скинути
```

Параметри: `error_rate, delay_ms, query_delay_ms, pool_max, provider_delay_ms, provider_error_rate, retry_storm, parallel, break_propagation, cardinality, cpu_burn, mem_mb` (bool: `on/off`).

## Troubleshooting

- **`failed to connect to the docker API`** — Docker Desktop не запущений: `open -a Docker`, дочекайся "running".
- **GlitchTip не налаштувався автоматично** — скрипт надрукує ручну інструкцію (2 хв): зареєструйся на http://localhost:8000, створи project (Go), поклади DSN у `.env` як `GLITCHTIP_DSN=http://<key>@glitchtip:8000/<id>`, потім `docker compose up -d checkout payment worker`. Все інше працює і без GlitchTip.
- **Мало RAM** — GlitchTip можна вимкнути: `docker compose stop glitchtip glitchtip-worker glitchtip-migrate glitchtip-postgres glitchtip-redis`.
- **Порти зайняті** — подивись таблицю адрес і звільни порт або зміни мапінг у compose.
- **Демо «залипло»** — `make reset` повертає baseline; якщо не допомогло — `make down && make up`.

## Структура

```
app/                    # Go: cmd/{checkout,payment,worker} + internal/{obs,httpx,events,db}
docker-compose.yml      # весь стек
prometheus/             # scrape + recording rules (SLI/burn rate)
tempo/                  # + metrics_generator (service graph)
otel-collector/         # otlp → sampler → tempo
loki/ alloy/            # логи контейнерів
grafana/provisioning/   # datasources (cross-links), alerting (burn-rate), dashboards
db/init.sql             # orders / order_items / entitlements
scripts/                # glitchtip-init, mcp-token
docs/                   # expansion-plan, gamma-брифи, runbooks
```
