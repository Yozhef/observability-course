.PHONY: demo up wait seed open load load-forever reset check-docker glitchtip-init \
        incident-day1 incident-day2 incident-1 incident-2 incident-3 workers-back \
        deploy-bad deploy-fix annotate cardinality-bomb chaos-cpu chaos-cpu-off chaos-mem \
        break-propagation fix-propagation parallel-on parallel-off nplus1 \
        collector-sample-20 collector-sample-100 mcp-token alerts-log logs down clean

GRAFANA = http://admin:admin@localhost:3000
CHECKOUT = http://localhost:8080
PAYMENT  = http://localhost:8081

# ══════════════════════════════ ONE COMMAND ══════════════════════════════
# Піднімає ВЕСЬ стек (3 сервіси, PG, RabbitMQ, Collector, Tempo, Prometheus,
# Loki+Alloy, GlitchTip, Grafana, cAdvisor, node-exporter, alert-sink),
# налаштовує error tracking, наганяє метрики і відкриває дашборд.
demo: up wait glitchtip-init seed open
	@echo ""
	@echo "✅ Стек готовий. Головні адреси:"
	@echo "   Grafana (RED):   http://localhost:3000/d/demo-service"
	@echo "   Grafana (USE):   http://localhost:3000/d/use-resources"
	@echo "   GlitchTip:       http://localhost:8000  (admin@course.local / course-demo-123)"
	@echo "   RabbitMQ UI:     http://localhost:15672 (course / course)"
	@echo "   Prometheus:      http://localhost:9090"
	@echo ""
	@echo "▶ Запускаю постійне навантаження (Ctrl+C зупиняє тільки трафік)..."
	@./load.sh 300 24

check-docker:
	@docker info > /dev/null 2>&1 || \
		( echo "❌ Docker-демон не запущений."; \
		  echo "   Запусти Docker Desktop:  open -a Docker"; \
		  echo "   Дочекайся статусу 'running' і повтори команду."; \
		  exit 1 )

up: check-docker
	docker compose up -d --build

wait:
	@echo "⏳ Чекаю на сервіси..."
	@until curl -sf $(CHECKOUT)/healthz > /dev/null 2>&1; do sleep 2; done
	@until curl -sf $(PAYMENT)/healthz  > /dev/null 2>&1; do sleep 2; done
	@until curl -sf http://localhost:8083/healthz > /dev/null 2>&1; do sleep 2; done
	@until curl -sf http://localhost:3000/api/health > /dev/null 2>&1; do sleep 2; done
	@echo "✅ checkout, payment, worker і Grafana готові"

glitchtip-init:
	@bash scripts/glitchtip-init.sh || echo "⚠ Продовжую без error tracking (див. інструкцію вище)"

seed:
	@echo "📈 Генерую початкові метрики й трейси..."
	@for i in $$(seq 1 60); do \
		curl -s $(CHECKOUT)/api/hello    > /dev/null; \
		curl -s $(CHECKOUT)/api/checkout > /dev/null; \
		curl -s $(CHECKOUT)/api/orders   > /dev/null; \
	done
	@curl -s $(CHECKOUT)/api/aggregate > /dev/null
	@echo "✅ ~180 запитів відправлено"

open:
	@open http://localhost:3000/d/demo-service 2>/dev/null || true

load:
	@./load.sh 120 24

load-forever:
	@while true; do ./load.sh 60 24; done

# Повертає стек у baseline між демо (обов'язково між уроками!)
reset:
	@curl -s "$(CHECKOUT)/admin/reset" > /dev/null || true
	@curl -s "$(PAYMENT)/admin/reset" > /dev/null || true
	@docker compose start worker > /dev/null 2>&1 || true
	@echo "✅ Chaos вимкнено, worker працює — baseline відновлено"

# ══════════════════════════════ ДЕНЬ 1: Logs & Errors ══════════════════════════════

# Інцидент для grep/LogQL/GlitchTip демо: 25% помилок + затримки
incident-day1:
	@curl -s "$(CHECKOUT)/admin/chaos?error_rate=0.25&delay_ms=400" > /dev/null
	@echo "🔥 Інцидент активний: error_rate=25%, delay=400ms. Генерую трафік..."
	@./load.sh 90 24
	@echo "👀 Дивись: docker compose logs checkout | grep -i error  (боляче)"
	@echo "         Grafana Explore → Loki: {service=\"checkout\"} | json | level=\"error\""
	@echo "         GlitchTip: http://localhost:8000"
	@echo "🔧 Повернути baseline: make reset"

# Реліз із регресією: version bump + annotation у Grafana + release у GlitchTip
deploy-bad:
	VERSION=v2.1.0 REGRESSION=on docker compose up -d checkout
	@$(MAKE) -s annotate TEXT="deploy v2.1.0 (regression)"
	@echo "🚀 v2.1.0 задеплоєно. /api/orders тепер повільний і падає. Подивись annotation на дашборді."

deploy-fix:
	VERSION=v1.0.1 REGRESSION=off docker compose up -d checkout
	@$(MAKE) -s annotate TEXT="rollback v1.0.1"
	@echo "✅ Відкат на v1.0.1. Метрики мають повернутись до baseline."

annotate:
	@curl -s -X POST "$(GRAFANA)/api/annotations" -H 'Content-Type: application/json' \
		-d '{"text":"$(TEXT)","tags":["deployment"]}' > /dev/null || true

# ══════════════════════════════ ДЕНЬ 2: Metrics & Alerting ══════════════════════════════

# SLO burn: помилки провайдера > бюджету → Grafana Alert → alert-sink
incident-day2:
	@curl -s "$(PAYMENT)/admin/chaos?provider_error_rate=0.2" > /dev/null
	@echo "🔥 Provider error rate = 20%. Запускаю трафік на 6 хвилин у фоні..."
	@( ./load.sh 360 24 --endpoint checkout > /dev/null 2>&1 & )
	@echo "👀 Дивись: Grafana → Alerting → 'Checkout SLO fast burn' (Normal → Pending → Firing)"
	@echo "         Повідомлення: make alerts-log"
	@echo "🔧 Повернути baseline: make reset"

# Антидемо: user_id стає label → вибух series
cardinality-bomb:
	@curl -s "$(CHECKOUT)/admin/chaos?cardinality=on" > /dev/null
	@( ./load.sh 60 30 > /dev/null 2>&1 & )
	@echo "💣 Кожен запит тепер створює нову series (user_id label)."
	@echo "👀 Дивись: curl -s localhost:8080/metrics | wc -l   (росте)"
	@echo "         Prometheus → Status → TSDB Status → head series"
	@echo "🔧 make reset"

chaos-cpu:
	@curl -s "$(CHECKOUT)/admin/chaos?cpu_burn=on" > /dev/null
	@echo "🔥 CPU burn увімкнено. Дивись USE-дашборд: черга росте раніше за p95."

chaos-cpu-off:
	@curl -s "$(CHECKOUT)/admin/chaos?cpu_burn=off" > /dev/null

chaos-mem:
	@curl -s "$(CHECKOUT)/admin/chaos?mem_mb=300" > /dev/null
	@echo "🔥 +300MB пам'яті. USE-дашборд: Memory per Container."

alerts-log:
	docker compose logs -f alert-sink

# ══════════════════════════════ ДЕНЬ 3: Tracing ══════════════════════════════

# Кейс 1: Payment provider degradation (повільний + 503)
incident-1:
	@curl -s "$(PAYMENT)/admin/chaos?provider_delay_ms=4000&provider_error_rate=0.5" > /dev/null
	@( ./load.sh 120 20 --endpoint checkout > /dev/null 2>&1 & )
	@echo "🔥 Provider: 4s latency, 50% 503. Дивись: Tempo traces (повільний provider.charge span),"
	@echo "   Service Graph (червоне ребро), logs retry, RED errors."
	@echo "🔧 make reset"

# Кейс 2: PostgreSQL pool exhaustion (повільне очікування, швидкий query)
incident-2:
	@curl -s "$(CHECKOUT)/admin/chaos?pool_max=2&query_delay_ms=150" > /dev/null
	@( ./load.sh 120 24 --endpoint orders > /dev/null 2>&1 & )
	@echo "🔥 Pool=2, query=150ms. Дивись: Tempo → trace /api/orders →"
	@echo "   span db.acquire_connection росте, сам query швидкий."
	@echo "🔧 make reset"

# Кейс 3: Consumer lag (worker зупинений, черга росте)
incident-3:
	@docker compose stop worker > /dev/null
	@( ./load.sh 120 20 --endpoint checkout > /dev/null 2>&1 & )
	@echo "🔥 Worker зупинений, checkout публікує події далі."
	@echo "👀 Дивись: RabbitMQ UI → queue orders (depth росте), Prometheus: rabbitmq_queue_messages"
	@echo "   Потім: make workers-back → відкрий trace → величезний розрив publish→process"
	@echo "🔧 make workers-back && make reset"

workers-back:
	@docker compose start worker > /dev/null
	@echo "✅ Worker знову споживає — подивись event_age_seconds і trace з broker wait"

# Антидемо: зламаний context propagation
break-propagation:
	@curl -s "$(CHECKOUT)/admin/chaos?break_propagation=on" > /dev/null
	@curl -s $(CHECKOUT)/api/checkout > /dev/null
	@echo "🔗💥 traceparent більше не передається: у Tempo тепер ДВА trace замість одного."

fix-propagation:
	@curl -s "$(CHECKOUT)/admin/chaos?break_propagation=off" > /dev/null
	@echo "✅ Propagation відновлено"

parallel-on:
	@curl -s "$(CHECKOUT)/admin/chaos?parallel=on" > /dev/null
	@curl -s $(CHECKOUT)/api/aggregate > /dev/null
	@echo "⚡ /api/aggregate тепер паралельний (~700ms). Порівняй trace із послідовним (~1.5s)."

parallel-off:
	@curl -s "$(CHECKOUT)/admin/chaos?parallel=off" > /dev/null
	@curl -s $(CHECKOUT)/api/aggregate > /dev/null
	@echo "🐌 /api/aggregate послідовний (~1.5s)"

nplus1:
	@curl -s "$(CHECKOUT)/api/orders?n_plus_one=true" > /dev/null
	@curl -s "$(CHECKOUT)/api/orders" > /dev/null
	@echo "🪜 Два запити зроблено: відкрий обидва traces /api/orders у Tempo — драбинка vs один JOIN."

# Керування sampling у Collector без рестарту застосунків
collector-sample-20:
	SAMPLE_PERCENT=20 docker compose up -d otel-collector
	@echo "🎯 Sampling 20% — потік у Tempo впав, застосунки не чіпали"

collector-sample-100:
	SAMPLE_PERCENT=100 docker compose up -d otel-collector
	@echo "🎯 Sampling 100%"

# Read-only доступ для AI-агента (Grafana MCP)
mcp-token:
	@bash scripts/mcp-token.sh

# ══════════════════════════════ Службові ══════════════════════════════

logs:
	docker compose logs -f checkout payment worker

down:
	docker compose down

clean:
	docker compose down -v --rmi local
	rm -f .env
