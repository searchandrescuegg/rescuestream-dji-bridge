#!/usr/bin/env bash
# End-to-end test: bring up EMQX + the bridge + the mock RC Plus, then confirm
# simulated M30T telemetry is ingested and exposed on /api/devices.
set -euo pipefail
cd "$(dirname "$0")/.."

# Pick up overrides from .env (the same file docker compose reads).
if [ -f .env ]; then
  set -a; . ./.env; set +a
fi

API="http://localhost:8080"
KEY="${API_KEY:-dev-integration-key}"
AIRCRAFT="M30T-MOCK-0001"

cleanup() {
  echo "==> tearing down"
  docker compose --profile mock down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> building and starting emqx + bridge + mock"
docker compose --profile mock up -d --build

echo "==> waiting for M30T telemetry on $API/api/devices (up to 90s)"
for _ in $(seq 1 45); do
  devices=$(curl -s -H "X-API-Key: $KEY" "$API/api/devices" 2>/dev/null || true)
  if printf '%s' "$devices" | grep -q "$AIRCRAFT"; then
    echo "==> PASS — M30T telemetry ingested:"
    printf '%s\n' "$devices"
    exit 0
  fi
  sleep 2
done

echo "==> FAIL — M30T telemetry did not appear"
echo "--- bridge logs ---"
docker compose logs --tail=40 main
exit 1
