#!/usr/bin/env bash
# Ідемпотентна ініціалізація GlitchTip: user + org + team + project → DSN у .env.
# Працює через Django shell усередині контейнера (не залежить від версій HTTP API).
set -u

BASE="${GLITCHTIP_URL:-http://localhost:8000}"
EMAIL="admin@course.local"
PASS="course-demo-123"

manual() {
  cat <<EOF
⚠ Не вдалося автоматично налаштувати GlitchTip ($1).
  Ручний шлях (2 хвилини):
    1. Відкрий $BASE і зареєструйся (будь-який email/пароль)
    2. Створи organization + project (platform: Go)
    3. Скопіюй DSN, заміни host на glitchtip:8000 і додай у .env:
       GLITCHTIP_DSN=http://<key>@glitchtip:8000/<project_id>
    4. docker compose up -d checkout payment worker
EOF
  exit 1
}

# вже налаштовано?
if [ -f .env ] && grep -q '^GLITCHTIP_DSN=.\+' .env; then
  echo "✅ GlitchTip DSN вже у .env"
  exit 0
fi

echo "⏳ Чекаю на GlitchTip (перший запуск: міграції можуть тривати кілька хвилин)..."
ok=""
for i in $(seq 1 150); do
  if curl -sf "$BASE/_health/" > /dev/null 2>&1; then ok=1; break; fi
  sleep 2
done
[ -z "$ok" ] && manual "web не піднявся"

echo "🔧 Створюю користувача і проєкт через Django shell..."
OUT=$(docker compose exec -T glitchtip ./manage.py shell < scripts/glitchtip_bootstrap.py 2>&1)
DSN=$(echo "$OUT" | grep -o 'DSN=.*' | head -1 | cut -d= -f2-)

if [ -z "$DSN" ]; then
  echo "$OUT" | tail -10
  manual "bootstrap не повернув DSN"
fi

grep -v '^GLITCHTIP_DSN=' .env 2>/dev/null > .env.tmp || true
echo "GLITCHTIP_DSN=$DSN" >> .env.tmp
mv .env.tmp .env

echo "✅ GlitchTip готовий: $BASE (login: $EMAIL / $PASS)"
echo "   DSN записано в .env → перезапускаю застосунки з error tracking..."
docker compose up -d checkout payment worker > /dev/null 2>&1
echo "✅ Error tracking увімкнено"
