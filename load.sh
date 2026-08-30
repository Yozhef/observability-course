#!/usr/bin/env bash
# Генератор навантаження.
# Використання: ./load.sh [тривалість_сек] [цільовий_rps] [--profile steady|spike|degradation] [--endpoint mixed|hello|checkout|orders|aggregate]
#   ./load.sh                          # 120s, ~24 RPS, steady, mixed
#   ./load.sh 300 40                   # 5 хв, ~40 RPS
#   ./load.sh 300 30 --profile degradation   # Day 2: помилки й латентність ростуть поступово
#   ./load.sh 120 20 --endpoint orders       # тільки /api/orders (pool demo)
set -u

BASE_URL="${BASE_URL:-http://localhost:8080}"
DURATION=120
RPS=24
PROFILE=steady
ENDPOINT=mixed

pos=0
for arg in "$@"; do
  case "$arg" in
    --profile) expect=profile ;;
    --profile=*) PROFILE="${arg#*=}" ;;
    --endpoint) expect=endpoint ;;
    --endpoint=*) ENDPOINT="${arg#*=}" ;;
    *)
      if [ "${expect:-}" = "profile" ]; then PROFILE="$arg"; expect=
      elif [ "${expect:-}" = "endpoint" ]; then ENDPOINT="$arg"; expect=
      elif [ $pos -eq 0 ]; then DURATION="$arg"; pos=1
      elif [ $pos -eq 1 ]; then RPS="$arg"; pos=2
      fi ;;
  esac
done

case "$ENDPOINT" in
  hello)     ENDPOINTS=(/api/hello) ;;
  checkout)  ENDPOINTS=(/api/checkout) ;;
  orders)    ENDPOINTS=(/api/orders) ;;
  aggregate) ENDPOINTS=(/api/aggregate) ;;
  *)         ENDPOINTS=(/api/hello /api/checkout /api/orders) ;;
esac

if ! curl -sf "$BASE_URL/healthz" > /dev/null 2>&1; then
  echo "❌ Сервіс не відповідає на $BASE_URL — спершу запусти стек: make up"
  exit 1
fi

echo "▶ Навантаження: ~${RPS} RPS, ${DURATION}s, profile=${PROFILE}, endpoint=${ENDPOINT} → $BASE_URL"
echo "  (Ctrl+C щоб зупинити раніше)"

TICK=0.5
START=$(date +%s)
END=$(( START + DURATION ))
total=0
deg_stage=0

while [ "$(date +%s)" -lt "$END" ]; do
  now=$(date +%s)
  elapsed=$(( now - START ))
  cur_rps=$RPS

  case "$PROFILE" in
    spike)
      third=$(( DURATION / 3 ))
      if [ $elapsed -ge $third ] && [ $elapsed -lt $(( 2 * third )) ]; then
        cur_rps=$(( RPS * 5 / 2 ))   # сплеск x2.5 у середині
      fi ;;
    degradation)
      third=$(( DURATION / 3 ))
      if [ $elapsed -ge $third ] && [ $deg_stage -lt 1 ]; then
        curl -s "$BASE_URL/admin/chaos?error_rate=0.05&delay_ms=200" > /dev/null
        echo "  ⚠ degradation stage 1: error_rate=5%, delay=200ms"
        deg_stage=1
      fi
      if [ $elapsed -ge $(( 2 * third )) ] && [ $deg_stage -lt 2 ]; then
        curl -s "$BASE_URL/admin/chaos?error_rate=0.12&delay_ms=600" > /dev/null
        echo "  ⚠ degradation stage 2: error_rate=12%, delay=600ms"
        deg_stage=2
      fi ;;
  esac

  batch=$(( cur_rps / 2 ))
  [ $batch -lt 1 ] && batch=1
  for i in $(seq 1 "$batch"); do
    ep=${ENDPOINTS[$(( (total + i) % ${#ENDPOINTS[@]} ))]}
    curl -s -o /dev/null --max-time 10 "$BASE_URL$ep" &
  done
  total=$(( total + batch ))
  sleep "$TICK"
done
wait

elapsed=$(( $(date +%s) - START ))
[ "$elapsed" -eq 0 ] && elapsed=1
echo "✅ ~${total} запитів за ${elapsed}s → середній RPS ≈ $(( total / elapsed ))"
if [ "$PROFILE" = "degradation" ]; then
  echo "ℹ Хаос лишився увімкненим — поверни baseline: make reset"
fi
