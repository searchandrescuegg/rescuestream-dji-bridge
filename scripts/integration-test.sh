#!/usr/bin/env bash
# End-to-end test: bring up EMQX + Postgres + the bridge + the mock RC Plus,
# then confirm telemetry is ingested live AND archived to Postgres.
set -euo pipefail
cd "$(dirname "$0")/.."

# Self-contained scenario: override every value docker compose would otherwise
# pick up from a developer's .env (real serial allowlist, Neon URL, LAN host).
# docker compose reads .env automatically, so these shell exports take
# precedence over it.
export API_KEY="dev-integration-key"
export SN_ALLOWLIST="RC-MOCK-0001"
export MQTT_PUBLIC_HOST="tcp://localhost:1883"
export DATABASE_URL="postgres://rescuestream:rescuestream@postgres:5432/rescuestream?sslmode=disable"

API="http://localhost:8080"
KEY="$API_KEY"
AIRCRAFT="M30T-MOCK-0001"

cleanup() {
  echo "==> tearing down"
  docker compose --profile mock down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> building and starting emqx + postgres + bridge + mock"
docker compose --profile mock up -d --build

echo "==> waiting for M30T telemetry on $API/api/devices (up to 90s)"
ingested=""
for _ in $(seq 1 45); do
  devices=$(curl -s -H "X-API-Key: $KEY" "$API/api/devices" 2>/dev/null || true)
  if printf '%s' "$devices" | grep -q "$AIRCRAFT"; then
    ingested="$devices"
    break
  fi
  sleep 2
done
if [ -z "$ingested" ]; then
  echo "==> FAIL — M30T telemetry did not appear on the API"
  docker compose logs --tail=40 main
  exit 1
fi
echo "==> live telemetry ingested"

echo "==> waiting for telemetry to be archived to Postgres (up to 30s)"
for _ in $(seq 1 15); do
  rows=$(docker compose exec -T postgres psql -U rescuestream -d rescuestream -tAc \
    "SELECT count(*) FROM aircraft_osd" 2>/dev/null | tr -d '[:space:]' || true)
  if [ -n "$rows" ] && [ "$rows" -gt 0 ] 2>/dev/null; then
    echo "==> PASS — $rows aircraft_osd rows archived; live telemetry:"
    printf '%s\n' "$ingested"
    exit 0
  fi
  sleep 2
done

echo "==> FAIL — telemetry ingested but not archived to Postgres"
docker compose logs --tail=40 main
exit 1
