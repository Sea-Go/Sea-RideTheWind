#!/usr/bin/env bash
set -euo pipefail
favorite_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
favorite_pg_bin="${FAVORITE_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
favorite_tmp="$(mktemp -d "${TMPDIR:-/tmp}/sea-favorite-fact.XXXXXX")"
favorite_started=false
finish() {
  if "$favorite_started"; then
    "$favorite_pg_bin/pg_ctl" -D "$favorite_tmp/pg" -m fast -w stop >"$favorite_tmp/stop.log" 2>&1 || true
  fi
  printf 'Evidence directory: %s\n' "$favorite_tmp"
}
trap finish EXIT
favorite_port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1',0))
    print(sock.getsockname()[1])
PY
)"
"$favorite_pg_bin/initdb" -D "$favorite_tmp/pg" -A trust --no-locale -U sea_favorite_test >"$favorite_tmp/initdb.log"
"$favorite_pg_bin/pg_ctl" -D "$favorite_tmp/pg" -l "$favorite_tmp/postgres.log" \
  -o "-h 127.0.0.1 -p $favorite_port -k $favorite_tmp" -w start
favorite_started=true
export FAVORITE_TEST_DSN="postgres://sea_favorite_test@127.0.0.1:$favorite_port/postgres?sslmode=disable"
cd "$favorite_repo"
go test -mod=readonly -race -count=1 -v ./service/favorite/rpc/... | tee "$favorite_tmp/go-test.log"
go vet ./service/favorite/rpc/...
git diff --check
