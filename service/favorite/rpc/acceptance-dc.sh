#!/usr/bin/env bash
set -euo pipefail

favorite_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
favorite_dc_root="${SEA_DC_PLATFORM_ROOT:?set SEA_DC_PLATFORM_ROOT to an isolated DataCenter checkout}"
favorite_pg_bin="${FAVORITE_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
favorite_tmp="$(mktemp -d "${TMPDIR:-/tmp}/sea-favorite-dc.XXXXXX")"
favorite_pg_started=false
favorite_dc_pid=""
finish() {
  if [[ -n "$favorite_dc_pid" ]]; then
    kill "$favorite_dc_pid" 2>/dev/null || true
    wait "$favorite_dc_pid" 2>/dev/null || true
  fi
  if "$favorite_pg_started"; then
    "$favorite_pg_bin/pg_ctl" -D "$favorite_tmp/pg" -m fast -w stop >"$favorite_tmp/stop.log" 2>&1 || true
  fi
  printf 'Evidence directory: %s\n' "$favorite_tmp"
}
trap finish EXIT

if [[ ! -f "$favorite_dc_root/cmd/platform/main.go" ]]; then
  printf 'DataCenter cmd/platform not found\n' >&2
  exit 1
fi
read -r favorite_pg_port favorite_dc_port < <(python3 - <<'PY'
import socket
ports = []
for _ in range(2):
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        ports.append(str(sock.getsockname()[1]))
print(*ports)
PY
)
"$favorite_pg_bin/initdb" -D "$favorite_tmp/pg" -A trust --no-locale -U sea_favorite_test >"$favorite_tmp/initdb.log"
"$favorite_pg_bin/pg_ctl" -D "$favorite_tmp/pg" -l "$favorite_tmp/postgres.log" \
  -o "-h 127.0.0.1 -p $favorite_pg_port -k $favorite_tmp" -w start
favorite_pg_started=true
export FAVORITE_TEST_DSN="postgres://sea_favorite_test@127.0.0.1:$favorite_pg_port/postgres?sslmode=disable"
export DATABASE_URL="$FAVORITE_TEST_DSN"
export PLATFORM_SERVICE_TOKEN="$(python3 - <<'PY'
import secrets
print(secrets.token_urlsafe(32))
PY
)"
export FAVORITE_DC_URL="http://127.0.0.1:$favorite_dc_port"
export FAVORITE_DC_TOKEN="$PLATFORM_SERVICE_TOKEN"
(cd "$favorite_dc_root" && go build -mod=readonly -o "$favorite_tmp/dc-platform" ./cmd/platform)
(cd "$favorite_repo" && go build -mod=readonly -o "$favorite_tmp/favorite-fact-dispatch" ./service/favorite/rpc/cmd/fact-dispatch)
(cd "$favorite_repo" && go build -mod=readonly -o "$favorite_tmp/favorite-fact-authority" ./service/favorite/rpc/cmd/fact-authority)
export FAVORITE_DISPATCH_BIN="$favorite_tmp/favorite-fact-dispatch"
export FAVORITE_AUTHORITY_BIN="$favorite_tmp/favorite-fact-authority"
"$favorite_tmp/dc-platform" -listen "127.0.0.1:$favorite_dc_port" -migrate >"$favorite_tmp/dc-platform.log" 2>&1 &
favorite_dc_pid=$!
favorite_ready=false
for _ in $(seq 1 80); do
  if [[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $PLATFORM_SERVICE_TOKEN" \
      "$FAVORITE_DC_URL/v1/events/rtw.community.favorite/readiness" 2>/dev/null || true)" == "404" ]]; then
    favorite_ready=true
    break
  fi
  sleep 0.25
done
if ! "$favorite_ready"; then
  printf 'DataCenter platform did not become ready; inspect %s\n' "$favorite_tmp/dc-platform.log" >&2
  exit 1
fi
(cd "$favorite_repo" && GOFLAGS='-p=2' GOMAXPROCS=2 go test -mod=readonly -race -count=1 -v ./service/favorite/rpc/... ) | tee "$favorite_tmp/go-test.log"
(cd "$favorite_repo" && GOFLAGS='-p=2' GOMAXPROCS=2 go vet ./service/favorite/rpc/...)
(cd "$favorite_repo" && git diff --check)
