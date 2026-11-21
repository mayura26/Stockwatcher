#!/usr/bin/env bash
# Launch Stockwatcher. Optional .env in this directory supplies keys.
#   ./run.sh          serve on :8080
#   ./run.sh tick     refresh quotes and fire alerts once
#   ./run.sh vapid    print a new VAPID key pair
set -euo pipefail
cd "$(dirname "$0")"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source <(sed 's/\r$//' .env)
  set +a
fi

cmd="${1:-serve}"
export ACCESS_TOKEN="${ACCESS_TOKEN:-devtoken}"
export DB_PATH="${DB_PATH:-data/stockwatcher.db}"
export ADDR="${ADDR:-:8080}"
mkdir -p "$(dirname "$DB_PATH")"

if [[ "$cmd" == serve ]]; then
  if [[ "$ADDR" == :* ]]; then
    echo "Stockwatcher  http://127.0.0.1${ADDR}  token ${ACCESS_TOKEN}"
  else
    echo "Stockwatcher  http://${ADDR}  token ${ACCESS_TOKEN}"
  fi
fi

exec go run ./cmd/stockwatcher "$cmd"
