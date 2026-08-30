#!/usr/bin/env bash
# Ідемпотентна ініціалізація GlitchTip: user + org + team + project → DSN у .env.
# Якщо щось піде не так — скрипт друкує ручну інструкцію і НЕ валить make demo.
set -u

BASE="${GLITCHTIP_URL:-http://localhost:8000}"
EMAIL="admin@course.local"
PASS="course-demo-123"
ORG="course"
TEAM="demo"
PROJECT="demo-app"

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

echo "⏳ Чекаю на GlitchTip..."
ok=""
for i in $(seq 1 60); do
  if curl -sf "$BASE/_health/" > /dev/null 2>&1 || curl -sf "$BASE/api/0/" -o /dev/null 2>&1; then
    ok=1; break
  fi
  sleep 2
done
[ -z "$ok" ] && manual "web не піднявся"

# реєстрація (409/400 якщо існує — це ок)
curl -s -X POST "$BASE/rest-auth/registration/" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password1\":\"$PASS\",\"password2\":\"$PASS\"}" > /dev/null 2>&1

LOGIN=$(curl -s -X POST "$BASE/rest-auth/login/" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}")
KEY=$(echo "$LOGIN" | python3 -c "import sys,json;print(json.load(sys.stdin).get('key',''))" 2>/dev/null)
[ -z "$KEY" ] && manual "login не повернув token"

auth_hdr=""
for hdr in "Authorization: Token $KEY" "Authorization: Bearer $KEY"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "$hdr" "$BASE/api/0/organizations/")
  if [ "$code" = "200" ]; then auth_hdr="$hdr"; break; fi
done
[ -z "$auth_hdr" ] && manual "API auth не прийняв token"

# org / team / project (ідемпотентно: помилки existing ігноруємо)
curl -s -X POST -H "$auth_hdr" -H 'Content-Type: application/json' \
  "$BASE/api/0/organizations/" -d "{\"name\":\"$ORG\"}" > /dev/null 2>&1
curl -s -X POST -H "$auth_hdr" -H 'Content-Type: application/json' \
  "$BASE/api/0/organizations/$ORG/teams/" -d "{\"slug\":\"$TEAM\"}" > /dev/null 2>&1
curl -s -X POST -H "$auth_hdr" -H 'Content-Type: application/json' \
  "$BASE/api/0/teams/$ORG/$TEAM/projects/" -d "{\"name\":\"$PROJECT\",\"platform\":\"go\"}" > /dev/null 2>&1

DSN=$(curl -s -H "$auth_hdr" "$BASE/api/0/projects/$ORG/$PROJECT/keys/" | python3 -c "
import sys, json
try:
    keys = json.load(sys.stdin)
    print(keys[0]['dsn']['public'])
except Exception:
    print('')
" 2>/dev/null)
[ -z "$DSN" ] && manual "не отримав DSN"

DSN_INTERNAL="${DSN/localhost:8000/glitchtip:8000}"
DSN_INTERNAL="${DSN_INTERNAL/127.0.0.1:8000/glitchtip:8000}"

grep -v '^GLITCHTIP_DSN=' .env 2>/dev/null > .env.tmp || true
echo "GLITCHTIP_DSN=$DSN_INTERNAL" >> .env.tmp
mv .env.tmp .env

echo "✅ GlitchTip готовий: $BASE (login: $EMAIL / $PASS)"
echo "   DSN записано в .env → перезапускаю застосунки з error tracking..."
docker compose up -d checkout payment worker > /dev/null 2>&1
echo "✅ Error tracking увімкнено"
