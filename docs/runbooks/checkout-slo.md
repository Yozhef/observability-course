# Runbook: Checkout SLO burn

Алерт означає: частка невдалих `/api/checkout` витрачає error budget швидше за поріг.

1. Відкрий RED-дашборд: http://localhost:3000/d/demo-service — подивись Errors і p95, чи є deployment annotation поруч зі зламом.
2. Якщо annotation є — підозра на реліз: `make deploy-fix` (rollback) і спостерігай 5 хв.
3. Якщо релізу не було — Explore → Tempo: знайди повільні/failed traces `/api/checkout`, подивись який span (payment? db?) винен.
4. З trace перейди в Logs (кнопка Logs for this span) — подивись причину: provider 503? pool wait? retry storm?
5. USE-дашборд: http://localhost:3000/d/use-resources — чи є saturation (pool, queue, CPU).
6. Після фіксу переконайся, що burn rate впав: Prometheus → `checkout:burnrate5m` < 1.
