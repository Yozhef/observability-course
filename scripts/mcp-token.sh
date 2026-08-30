#!/usr/bin/env bash
# Створює read-only service account у Grafana і друкує конфіг для Grafana MCP (Day 3).
set -euo pipefail

G="http://admin:admin@localhost:3000"

SA_ID=$(curl -s -X POST "$G/api/serviceaccounts" -H 'Content-Type: application/json' \
  -d '{"name":"mcp-agent","role":"Viewer"}' | python3 -c "import sys,json;print(json.load(sys.stdin).get('id',''))")

if [ -z "$SA_ID" ]; then
  # можливо вже існує — знайдемо
  SA_ID=$(curl -s "$G/api/serviceaccounts/search?query=mcp-agent" | python3 -c "
import sys, json
d = json.load(sys.stdin)
sas = d.get('serviceAccounts', [])
print(sas[0]['id'] if sas else '')")
fi
[ -z "$SA_ID" ] && { echo "❌ Не вдалося створити service account"; exit 1; }

TOKEN=$(curl -s -X POST "$G/api/serviceaccounts/$SA_ID/tokens" -H 'Content-Type: application/json' \
  -d "{\"name\":\"mcp-$(date +%s)\"}" | python3 -c "import sys,json;print(json.load(sys.stdin).get('key',''))")
[ -z "$TOKEN" ] && { echo "❌ Не вдалося створити token"; exit 1; }

cat <<EOF
✅ Read-only service account готовий.

Конфіг для Claude Desktop / Cursor (mcp.json):
{
  "mcpServers": {
    "grafana": {
      "command": "docker",
      "args": ["run", "--rm", "-i",
        "-e", "GRAFANA_URL=http://host.docker.internal:3000",
        "-e", "GRAFANA_API_KEY=$TOKEN",
        "mcp/grafana"]
    }
  }
}

Investigation prompt (Day 3, слайд «Просимо агента провести повне розслідування»):
  Досліди checkout p95 spike за останні 30 хвилин.
  1. Знайди relevant dashboard і deployment annotation.
  2. Порівняй RED до/після зміни.
  3. Перевір USE для Checkout і PostgreSQL.
  4. Знайди 3 повільні traces та critical path.
  5. Відкрий related logs.
  6. Дай гіпотези з evidence links і confidence.
  7. Не виконуй жодних змін.
EOF
