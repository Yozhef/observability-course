.PHONY: demo up wait seed load load-forever open down clean logs check-docker

# Одна команда: підняти все, нагнати метрики, відкрити Grafana, 2 хв навантаження >15 RPS
demo: up wait seed open
	@echo ""
	@echo "✅ Grafana: http://localhost:3000/d/demo-service (логін не потрібен)"
	@./load.sh 120 24

check-docker:
	@docker info > /dev/null 2>&1 || \
		( echo "❌ Docker-демон не запущений."; \
		  echo "   Запусти Docker Desktop:  open -a Docker"; \
		  echo "   (або colima start / OrbStack, якщо користуєшся ними)"; \
		  echo "   Дочекайся статусу 'running' і повтори команду."; \
		  exit 1 )

up: check-docker
	docker compose up -d --build

wait:
	@echo "⏳ Чекаю на сервіс..."
	@until curl -sf localhost:8080/healthz > /dev/null 2>&1; do sleep 1; done
	@echo "✅ demo-service готовий"

# швидкий буст: ~300 запитів, щоб графіки одразу наповнились
seed:
	@echo "📈 Генерую початкові метрики..."
	@for i in $$(seq 1 100); do \
		curl -s localhost:8080/api/hello  > /dev/null; \
		curl -s localhost:8080/api/users  > /dev/null; \
		curl -s localhost:8080/api/orders > /dev/null; \
	done
	@echo "✅ ~300 запитів відправлено"

# 2 хвилини навантаження, середній RPS ~24 (гарантовано >15)
load:
	@./load.sh 120 24

# нескінченне навантаження для довгого демо
load-forever:
	@while true; do ./load.sh 60 24; done

open:
	@open http://localhost:3000/d/demo-service 2>/dev/null || true

logs:
	docker compose logs -f app

down:
	docker compose down

clean:
	docker compose down -v --rmi local
