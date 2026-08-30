# AI-Native Observability — план розширення v2

Доповнення до master brief (177 слайдів). Мета: кожен теоретичний блок закінчується **живим демо на demo-проєкті** (`observability-course` repo). Після розширення: **День 1 — 65, День 2 — 80, День 3 — 75, разом ~220 слайдів.**

Рішення зафіксовані: error tracking — **GlitchTip** (self-hosted, Sentry-сумісний DSN), broker — **RabbitMQ**, метрики — Prometheus (без Mimir), трейси — Tempo + OTel Collector.

## Правила для нових слайдів

- Нові слайди успадковують усі правила master brief (одна думка, заголовок-висновок, мінімум тексту).
- Нумерація: вставки позначені `D1-A … D3-N` із зазначенням "після слайда N" — щоб не перенумеровувати brief.
- Кожен **ДЕМО-слайд** обов'язково містить: (1) команду запуску, (2) що саме показати на екрані, (3) 1 речення-висновок, (4) **fallback-скріншот** у speaker notes на випадок, якщо демо впаде на сесії.
- Типи нових слайдів: `ДЕМО` (живий показ), `КОД` (фрагмент з repo, темна область), `АНТИДЕМО` (навмисна помилка — найцінніший формат).

---

# День 1 — Logs, Errors та AI (50 → 65)

## Нові слайди

### D1-A · ДЕМО · після слайда 11 («Поганий log повідомляє факт»)
**Заголовок:** Спробуймо знайти помилку так, як це роблять без Observability
**Демо:** `LOG_FORMAT=text make up && make incident-day1` → `docker compose logs app | grep -i error`. Засікти час на секундомір: знайти, який order зачеплено і чому. Не вийде — про це й слайд.
**Висновок:** grep відповідає «щось сталося», але не «що, з ким і чому».
**Перехід:** «А тепер той самий запис — як дані».

### D1-B · КОД · після слайда 12 («Structured Log перетворює текст на дані»)
**Заголовок:** Structured logging у Go — це middleware + context, а не Sprintf
**На слайді:** фрагмент logging middleware з repo: `slog.With("request_id", …, "trace_id", …, "correlation_id", …, "release", …)`; хендлер бере logger з context.
**Проговорити:** поля додаються один раз у middleware — далі кожен лог у request автоматично збагачений. Показати, що `release` приходить з env при деплої.

### D1-C · ДЕМО · після слайда 14 («Логи мають потрапити у централізоване сховище»)
**Заголовок:** Той самий інцидент через Grafana Explore — 30 секунд замість 10 хвилин
**Демо:** Grafana → Explore → Loki: `{service="checkout"} | json | level="error"`. Той самий інцидент, що в D1-A. Знову секундомір — порівняти час із D1-A вголос.
**Висновок:** різниця в часі — це і є цінність структури + централізації.

### D1-D · КОД · новий міні-розбір LogQL (1/3), після D1-C
**Заголовок:** LogQL починається зі stream selector — і тому labels мають бути дешевими
**На слайді:** `{service="checkout", environment="production"}` + список labels у нашому Alloy config.
**Проговорити:** зв'язати зі слайдом 24 (user_id як label — помилка): у нас labels лише service/env/level.

### D1-E · КОД · LogQL (2/3)
**Заголовок:** Parser і filter працюють уже після вибору stream
**На слайді:** `| json | order_id="o-456" | line_format "{{.message}} [{{.provider}}]"`.
**Проговорити:** порядок виконання: спочатку дешеві labels, потім дорогий парсинг. Показати наживо, як міняється вивід від додавання кожного рядка.

### D1-F · ДЕМО · LogQL (3/3)
**Заголовок:** Логи теж вміють ставати графіком
**Демо:** `sum(count_over_time({service="checkout"} | json | level="error" [1m]))` — error-графік із логів.
**Висновок + перехід:** це вже майже метрика — але дорога. День 2 пояснить, чому лічильник дешевший за підрахунок логів.

### D1-G · ДЕМО · після слайда 17 («Одна операція — послідовність подій»)
**Заголовок:** Відновлюємо всю історію checkout-456 одним запитом
**Демо:** `{service=~".+"} | json | correlation_id="checkout-456"` — повний lifecycle: OrderCreated → PaymentFailed → Compensation, з різних сервісів, відсортований за часом.
**Висновок:** correlation_id перетворює купу записів на біографію операції.

### D1-H · ДЕМО · після слайда 30 («Sentry додає контекст»)
**Заголовок:** Перший exception прилітає сам — його ніхто не шукав
**Демо:** `make incident-day1` → відкрити GlitchTip: Issue з'явився без жодного пошуку. Показати: message, stack trace, tags (release, environment), counter events.
**Проговорити:** чесно сказати: у курсі GlitchTip — легкий self-hosted аналог Sentry з тим самим SDK і DSN; у production-Sentry той самий workflow + більше AI-фіч.

### D1-I · ДЕМО · після слайда 31 («Як читати Sentry Issue»)
**Заголовок:** Читаємо Issue за чеклистом — разом
**Демо:** пройти по живому Issue всі 6 кроків таблиці слайда 31 (exception → stack trace → breadcrumbs → tags → release → frequency). Аудиторія називає наступний крок — спікер клікає.

### D1-J · ДЕМО · після слайда 34 («Release tracking показує Regression»)
**Заголовок:** Деплоїмо поганий реліз і дивимось, як він себе видає
**Демо:** `make deploy-bad` (бампає версію до v2.1.0, вмикає регресію) → у GlitchTip новий Issue з release=v2.1.0; `make deploy-fix` → resolve; повторний `make deploy-bad` → **Regression** badge.
**Висновок:** release — це вісь, на якій grouping перетворюється на історію.

### D1-K · ДЕМО · після слайда 41 («Timeline відокремлює події від припущень»)
**Заголовок:** Збираємо timeline інциденту з трьох джерел за 5 хвилин
**Демо:** реконструюємо таблицю слайда 41 наживо: час деплою — з виводу `make deploy-bad`, перший timeout — з GlitchTip, скарга — «репліка користувача» (спікер), rollback — `make deploy-fix`, повернення до baseline — Loki-графік з D1-F.
**Висновок:** кожен рядок timeline має джерело — це і відрізняє факт від припущення.

### D1-L · ДЕМО · після слайда 45 («AI прискорює механічну частину»)
**Заголовок:** Віддаємо агенту stack trace і 20 логів — дивимось, що він зробить
**Демо:** скопіювати stack trace із GlitchTip + вивід LogQL за trace_id → у Claude з prompt зі слайда 47. Розібрати відповідь: що агент зробив добре (timeline, гіпотези), де він вгадує.

### D1-M · ДЕМО · після слайда 47 («Хороший prompt просить докази»)
**Заголовок:** Поганий і хороший prompt на тих самих даних дають різну якість
**Демо:** порівняти: «чому впав checkout?» vs структурований prompt зі слайда 47 — на однакових вхідних даних. Показати різницю у відповідях поруч.
**Висновок:** якість відповіді агента — функція від структури запиту й повноти сигналів.

### D1-N · АНТИДЕМО · перед слайдом 48 (практика)
**Заголовок:** AI впевнено помиляється, коли в даних діра
**Демо:** прибрати з вхідних даних correlation_id і release (заготовлений «обрізаний» лог-файл) → агент будує зв'язну, переконливу і **хибну** історію. Розібрати, чому: він не бачить того, чого ми не записали (слайд 46 — наживо).
**Висновок:** найважливіший слайд блоку. Верифікація людиною — не опція.

### D1-O · ПРАКТИКА (заміна формату слайда 48)
**Слайд 48 лишається, але сценарій стає живим:** учасники самі відкривають Grafana Explore + GlitchTip на своєму `make demo`-стеку після `make incident-day1` і виконують 4 завдання слайда на реальних даних. Debrief той самий.

## Рекомпозиція Дня 1
Разом: 50 + 15 = **65 слайдів**. Практик стало 2 «паперові» + 1 жива. Всі демо працюють на стеку Дня 1 (app + Loki + Alloy + GlitchTip + Grafana).

---

# День 2 — Metrics, Alerting та AI (66 → 80)

## Нові слайди

### D2-A · КОД · після слайда 7 («Metric починається з семантики»)
**Заголовок:** Три типи метрик — це три рядки Go-коду
**На слайді:** фрагменти з repo: `promauto.NewCounterVec` (http_requests_total), `NewGauge` (in_flight), `NewHistogramVec` (duration + buckets).
**Проговорити:** buckets визначаються ДО продакшена — показати, як обрані наші (10ms…2.5s) і чому під наш профіль латентності.

### D2-B · АНТИДЕМО · після слайда 9 («Cardinality визначає керованість»)
**Заголовок:** Вибух cardinality наживо — і чому його помічають запізно
**Демо:** `make cardinality-bomb` (ендпоінт починає писати user_id у label) → `curl :8080/metrics | wc -l` росте на очах; Prometheus → Status → TSDB stats → head series стрибає.
**Висновок:** система не падає одразу — вона дорожчає і сповільнюється тихо. Тому це code-review правило, а не алерт.

### D2-C · ДЕМО · після слайда 11 («Grafana не створює Metrics»)
**Заголовок:** Шлях однієї метрики: код → /metrics → scrape → query
**Демо:** показати всі 4 кроки за 2 хвилини: рядок коду → `curl :8080/metrics | grep checkout` → Prometheus Targets (UP, last scrape) → перший query у Graph.

### Новий міні-блок «PromQL руками» · після слайда 12 (4 слайди)

**D2-D · КОД+ДЕМО:** «rate() перетворює нескінченно зростаючий counter на швидкість» — `http_requests_total` → `rate(...[1m])`, показати обидва графіки поруч: піла vs горизонталь.

**D2-E · КОД+ДЕМО:** «sum by() відповідає на питання "в розрізі чого?"» — `sum(rate(...)) by (path)` vs `by (status)`. Типова помилка: `rate(sum(...))` не існує — пояснити чому (rate потребує сирих series).

**D2-F · КОД+ДЕМО:** «histogram_quantile збирає p95 з buckets» — розібрати повний вираз з нашого дашборда, показати `_bucket`-серії в сирому вигляді, щоб зникла магія.

**D2-G · СПИСОК:** «Типові помилки PromQL» — rate від gauge; quantile без `by (le)`; занадто коротке вікно rate (< 2×scrape interval); порівняння avg vs quantile; irate там, де треба rate.

### D2-H · ДЕМО · після слайда 25 («RED Dashboard показує одну історію»)
**Заголовок:** RED-дашборд під живим навантаженням
**Демо:** `./load.sh 300 40 --profile degradation` → на дашборді на очах: Rate стабільний, Errors повзе, p95 росте — точна картинка зі слайда 25, але жива. Прочитати за таблицею слайда 26: яка комбінація — яка гіпотеза.

### D2-I · ДЕМО · після слайда 30 («Deployment Annotations»)
**Заголовок:** Annotation перетворює «щось зламалося» на «зламалося після оцього»
**Демо:** `make deploy-bad` → вертикальна лінія на всіх панелях (Makefile постить annotation у Grafana API) → p95 злітає одразу після лінії → `make deploy-fix` → друга лінія, метрики повертаються.

### D2-J · ДЕМО · після слайда 34 («Saturation проявляється як очікування»)
**Заголовок:** USE наживо: utilization ще зелена, а черга вже росте
**Демо:** `make chaos-cpu` (сервіс починає палити CPU) → use-resources дашборд: container CPU росте, host load росте, in-flight requests (черга) росте раніше, ніж p95 на RED. Показати порядок: saturation — випереджальний сигнал.
**Рекомендація по існуючих слайдах:** таблиці 35–39 (USE для CPU/Memory/PG/Queue/Network) винести в handout/repo docs, на екрані лишити 35 (CPU) і 38 (Queue — знадобиться в Дні 3).

### D2-K · КОД · після слайда 49 («Error Budget — вимірюваний компроміс»)
**Заголовок:** SLI стає recording rule — і рахується постійно, а не під час інциденту
**На слайді:** реальний YAML з repo: `checkout:availability:ratio_rate5m = sum(rate(...status!~"5.."[5m])) / sum(rate(...[5m]))`.
**Проговорити:** recording rule — це «збережена формула»: дешеві запити, спільна мова для дашборда й алерта.

### D2-L · КОД+ДЕМО · після слайда 50 («Burn Rate»)
**Заголовок:** Multiwindow burn-rate ловить і пожежу, і повільний витік
**На слайді:** правило з repo: fast burn (14.4× за 5m і 1h → page) + slow burn (6× за 30m і 6h → ticket). Схема: чому два вікна разом прибирають і false positives, і пропуски.
**Демо:** показати обидва правила у Grafana Alerting (state: Normal).

### D2-M · ДЕМО · після слайда 55 («Grafana Alerting відокремлює detection від routing»)
**Заголовок:** Дивимось повний шлях алерта: від умови до повідомлення
**Демо:** `make incident-day2` (вмикає error rate вище SLO) → Grafana Alerting: rule переходить Pending → Firing → повідомлення прилітає в alert-sink (локальний webhook, вивід на екран). Показати annotations/labels в повідомленні: dashboard link, runbook, severity.
**Висновок:** алерт зі слайда 52 — «контекст + наступний крок» — саме так виглядає в реальності.

### D2-N · ДЕМО · після слайда 62 («Grafana MCP дозволяє ставити питання»)
**Заголовок:** Exemplar: з p95-панелі — у конкретний trace одним кліком
**Демо:** увімкнені exemplars: на histogram-панелі з'являються ромбики → клік → Tempo trace. Це місток у День 3: «завтра ми розберемо, що всередині».
**Плюс:** попросити агента через MCP порівняти p95 до/після `make deploy-bad` — жива версія слайда 62.

## Рекомпозиція Дня 2
Разом: 66 + 14 = **80 слайдів**, з них 9 демо. Якщо таблиці 35–39 підуть у handout (−3 слайди на екрані), темп виходить комфортніший за поточний brief попри більшу кількість слайдів — бо демо «дихають» інакше, ніж текстові слайди.

---

# День 3 — Distributed Tracing та AI (61 → 75)

## Нові слайди

### D3-A · ДЕМО · після слайда 9 («Waterfall читається у двох напрямках»)
**Заголовок:** Перший waterfall: один клік — чотири сервіси
**Демо:** `curl :8080/api/checkout` → Tempo → trace: gateway → checkout → payment (HTTP) → RabbitMQ → entitlement-worker. Пройти обидва напрямки читання зі слайда 9 на живому trace.

### D3-B · ДЕМО · після слайда 11 («Sequential і Parallel»)
**Заголовок:** Паралелимо виклики — total time падає на очах
**Демо:** `PARALLEL=false` → trace: 300+500+700 ms послідовно; `PARALLEL=true` → ті самі spans поруч, total = 700 ms. Показати, що найдовший span (700) тепер і Є critical path — зв'язати зі слайдом 10.

### D3-C · КОД · після слайда 15 («Auto + Manual instrumentation»)
**Заголовок:** У Go це виглядає так: otelhttp дає скелет, tracer.Start дає сенс
**На слайді:** фрагменти з repo: `otelhttp.NewHandler(mux, …)` + `tracer.Start(ctx, "payment.authorize", attrs…)` з бізнес-атрибутами (order_id як attribute, не label!).
**Проговорити:** зв'язати зі слайдом 45 Дня 2 — куди кладемо high-cardinality: у span attributes.

### D3-D · КОД+ДЕМО · після слайда 18 («Collector pipeline»)
**Заголовок:** Collector — це кран між застосунком і сховищем
**На слайді:** наш otel-collector config: receivers (otlp) → processors (batch, probabilistic_sampler) → exporters (tempo).
**Демо:** змінити sampler з 100% на 20%, перезапустити collector — потік трейсів у Tempo падає, застосунок не чіпали.

### D3-E · ДЕМО · після слайда 22 («Кожен HTTP service продовжує Context»)
**Заголовок:** traceparent — ось він, у живому запиті
**Демо:** `curl -v :8080/api/checkout` → показати відповідь з trace_id; логи payment-сервісу: той самий trace_id прилетів у header. Розібрати формат `00-{trace_id}-{span_id}-01` на реальному значенні (слайд 21 — наживо).

### D3-F · АНТИДЕМО · після слайда 23 («Broken propagation»)
**Заголовок:** Ламаємо propagation — і одна історія розсипається на дві
**Демо:** `BREAK_PROPAGATION=true make restart payment` → той самий curl → у Tempo тепер ДВА trace: checkout обривається на HTTP call, payment живе окремо. Показати, як це діагностувати: спільний request_id у логах, різні trace_id.
**Висновок:** найчастіший спосіб «зламати» tracing у реальних командах — забутий header у самописному HTTP-клієнті.

### D3-G · ДЕМО · після слайда 26 («Producer і Consumer — причинний зв'язок»)
**Заголовок:** Context переживає RabbitMQ у message headers
**Демо:** RabbitMQ management UI → зазирнути в message: traceparent у headers; Tempo → trace: producer span (checkout) → consumer span (worker) з часовим розривом «broker wait».

### D3-H · ДЕМО · після слайда 27 («Event-Driven додає складності»)
**Заголовок:** Consumer lag наживо: publish о 12:00, process о 12:04
**Демо:** `make incident-3` (зупиняє workers, трафік іде) → RabbitMQ UI: queue depth росте; use-дашборд: oldest message age росте; запустити workers назад → trace: величезний broker-wait span.
**Висновок:** для async-флоу «latency» — це вік повідомлення, а не тривалість обробки.

### D3-I · ДЕМО · після слайда 33 («Database span розділяє проблеми»)
**Заголовок:** N+1 у waterfall неможливо не помітити
**Демо:** `curl ":8080/api/orders?n_plus_one=true"` → Tempo: 200 однакових коротких SQL-spans драбинкою. Поруч нормальний запит: 1 span з JOIN. Показати, що avg DB latency в обох випадках «зелена» (слайд 34 — наживо).

### D3-J · ДЕМО · після D3-I
**Заголовок:** Pool exhaustion: повільний не query, а очікування connection
**Демо:** `make incident-2` (pool=2 + навантаження) → trace: `db.acquire_connection` 3s, сам query 8ms; USE-дашборд PG: waiting connections. Це живий Кейс 2 зі слайда 47 — сказати про це прямо, на слайді 47 буде recap.

### D3-K · ДЕМО · після слайда 39 («Service Graph не замінює Trace»)
**Заголовок:** Service Graph малюється сам — із трейсів
**Демо:** Grafana → Tempo → Service Graph (metrics generator): вузли, ребра, error rate на ребрі payment→provider після `make incident-1`.

### D3-L · ДЕМО · після слайда 42 («Trace to Logs»)
**Заголовок:** Повний цикл без ручного пошуку: Metric → Exemplar → Trace → Logs
**Демо:** головне демо курсу, одним ланцюжком: p95-панель → exemplar → Tempo waterfall → повільний span → «Logs for this span» → Loki-рядки з тим самим trace_id. Жодного copy-paste.
**Висновок:** це і є «cross-signal investigation» — сигнали з'єднані ідентифікаторами, а не пам'яттю інженера.

### D3-M · ДЕМО · після слайда 52 («Просимо агента провести розслідування»)
**Заголовок:** Агент розслідує наш інцидент — дивимось на його кроки
**Демо:** `make incident-1` → віддати агенту prompt зі слайда 52 через Grafana MCP (read-only service account) → на екрані видно tool calls: search dashboards → query p95 → find traces → get logs. Розібрати фінальні гіпотези: де evidence links, де confidence.

### D3-N · АНТИДЕМО · після слайда 53 («Guardrails»)
**Заголовок:** Той самий агент без структури: швидка відповідь, нуль доказів
**Демо:** prompt «чому все повільно?» без вимог evidence → порівняти з D3-M поруч: перша відповідь звучить впевнено, але неперевірювана.
**Висновок:** guardrails — це не тільки безпека, це якість висновків.

## Рекомпозиція Дня 3
Разом: 61 + 14 = **75 слайдів**, з них 12 демо. Кейси слайдів 46–48 тепер мають живі відповідники (incident-1/2/3) — на самих слайдах-таблицях робимо recap того, що бачили.

---

# Зміни в demo-проєкті

## Етап 1 — під День 1
| Зміна | Для слайдів |
|---|---|
| slog JSON logging + middleware (request_id, correlation_id, trace_id, release), перемикач `LOG_FORMAT=text\|json` | D1-A, D1-B, D1-C, 12, 15 |
| Loki + Alloy у compose, Loki datasource | D1-C…G, 14, 26 |
| GlitchTip (web + worker + postgres + redis) + sentry-go SDK у app, DSN через env | D1-H…J, 28–36 |
| Fault injection через env/admin: `ERROR_RATE`, `DELAY_MS` | D1-A, D1-H, всі інциденти |
| Business events у логах: OrderCreated, PaymentFailed(expected), Compensation | D1-G, 17–19 |
| `make incident-day1` (регресія + трафік), `make deploy-bad` / `make deploy-fix` (версія + annotation + GlitchTip release) | D1-A, D1-H, D1-J, D1-K |
| «Обрізаний» лог-файл для антидемо AI (без correlation_id/release) | D1-N |

## Етап 2 — під День 2
| Зміна | Для слайдів |
|---|---|
| Exemplars: trace_id у histogram observations + `--enable-feature=exemplar-storage` | D2-N, D3-L |
| Recording rules (SLI availability/latency) + multiwindow burn-rate alerts, provisioned у Grafana Alerting | D2-K, D2-L, D2-M |
| alert-sink: мінімальний webhook-контейнер, друкує алерти (contact point) | D2-M |
| load.sh профілі: `normal\|spike\|degradation` | D2-H, 15, 25 |
| Product-метрики: checkout_started / payment_succeeded / entitlement_granted (funnel) | 42–44 |
| `make cardinality-bomb` (user_id у label — навмисно), `make chaos-cpu`, `make chaos-mem` | D2-B, D2-J |
| Deployment annotations через Grafana API у `make deploy-bad/fix` | D2-I |
| `make incident-day2` (error rate > SLO burn) | D2-M |
| docs/handouts/use-tables.md (таблиці USE зі слайдів 35–39) | рекомендація Дня 2 |

## Етап 3 — під День 3
| Зміна | Для слайдів |
|---|---|
| Розбити app на: **checkout** (:8080) → **payment** (:8081, HTTP) → **entitlement-worker** (consumer) | D3-A…N, весь день |
| **RabbitMQ** + management UI, OTel producer/consumer spans, context у headers | D3-G, D3-H, 25–27 |
| **PostgreSQL** справжній: orders/items схема, `?n_plus_one=true`, малий pool для incident-2 | D3-I, D3-J, 33–34 |
| **OTel Collector** (otlp → batch/sampler → tempo), app-и шлють у collector | D3-D, 14, 18 |
| Tempo metrics generator → service graph datasource config | D3-K |
| Trace→Logs derived fields (Tempo datasource → Loki за trace_id) | D3-L, 42 |
| Прапорці: `BREAK_PROPAGATION`, `PARALLEL`, `RETRY_STORM`; payment-provider stub з керованою латентністю/503 | D3-B, D3-F, 35, incident-1 |
| Grafana MCP: read-only service account, конфіг для Claude/Cursor, investigation prompt у repo | D3-M, D3-N, 49–53 |
| `make incident-1` (provider degradation), `make incident-2` (pool exhaustion), `make incident-3` (consumer lag) | D3-H…J, 46–48, D3-M |

## Цільові make-цілі (підсумок)
```
make demo                # підняти все + seed + load
make incident-day1       # День 1: помилки + регресія релізу
make incident-day2       # День 2: SLO burn → живий алерт
make incident-1|2|3      # День 3: provider / pool / consumer lag
make deploy-bad|fix      # реліз з регресією / відкат (+annotation, +GlitchTip release)
make cardinality-bomb    # антидемо cardinality
make chaos-cpu|mem       # USE-демо
make reset               # повернути все до здорового стану між демо
```

`make reset` — обов'язковий: між блоками стек має повертатися в baseline, інакше демо накладаються.

## Порядок роботи
1. План слайдів (цей документ) → затвердити.
2. Етап 1 repo (День 1) → прогнати всі демо Дня 1 end-to-end.
3. Етап 2 → прогнати демо Дня 2.
4. Етап 3 (найбільший: 3 сервіси + RabbitMQ + PG + Collector) → прогнати демо Дня 3.
5. Згенерувати розширені презентації (brief + цей план як вхід для генератора).
6. Для кожного демо — записати fallback-скріншоти у `docs/screenshots/`.
