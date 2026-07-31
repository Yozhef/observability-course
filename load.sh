#!/usr/bin/env bash
# Генератор навантаження: тримає середній RPS вище цільового протягом заданого часу.
# Використання: ./load.sh [тривалість_сек] [цільовий_rps]
#   ./load.sh            # 120 секунд, ~24 RPS (середній > 15 гарантовано)
#   ./load.sh 300 40     # 5 хвилин, ~40 RPS
set -u

BASE_URL="${BASE_URL:-http://localhost:8080}"
DURATION="${1:-120}"
RPS="${2:-24}"

TICK=0.5
BATCH=$(( RPS / 2 ))                      # запитів за один тік (тік = 0.5s)
END=$(( $(date +%s) + DURATION ))
ENDPOINTS=(/api/hello /api/users /api/orders)

# перевірка, що сервіс живий
if ! curl -sf "$BASE_URL/healthz" > /dev/null 2>&1; then
  echo "❌ Сервіс не відповідає на $BASE_URL — спершу запусти стек: make up"
  exit 1
fi

echo "▶ Навантаження: ~${RPS} RPS протягом ${DURATION}s → $BASE_URL"
echo "  (Ctrl+C щоб зупинити раніше)"

total=0
start=$(date +%s)
while [ "$(date +%s)" -lt "$END" ]; do
  for i in $(seq 1 "$BATCH"); do
    ep=${ENDPOINTS[$(( (total + i) % 3 ))]}
    curl -s -o /dev/null --max-time 5 "$BASE_URL$ep" &
  done
  total=$(( total + BATCH ))
  sleep "$TICK"
done
wait

elapsed=$(( $(date +%s) - start ))
[ "$elapsed" -eq 0 ] && elapsed=1
echo "✅ ~${total} запитів за ${elapsed}s → середній RPS ≈ $(( total / elapsed ))"
