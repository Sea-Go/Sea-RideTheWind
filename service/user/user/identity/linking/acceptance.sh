#!/usr/bin/env bash
set -euo pipefail
link_repo="$(cd "$(dirname "$0")/../../../../.." && pwd)"
link_pg_bin="${KNOWLEDGE_PG_BIN:-$(dirname "$(command -v initdb)")}"
link_evidence="$(mktemp -d "${TMPDIR:-/tmp}/sea-h01-account-link.XXXXXX")"
link_started=false
finish() {
  if "$link_started"; then
    "$link_pg_bin/pg_ctl" -D "$link_evidence/pg" -m fast stop > "$link_evidence/stop.log" 2>&1 || true
  fi
  printf 'Evidence directory: %s\n' "$link_evidence"
}
trap finish EXIT
link_port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
)"
"$link_pg_bin/initdb" -D "$link_evidence/pg" -A trust --no-locale -U sea_h01_test > "$link_evidence/initdb.log"
"$link_pg_bin/pg_ctl" -D "$link_evidence/pg" -l "$link_evidence/postgres.log" \
  -o "-h 127.0.0.1 -p $link_port -k $link_evidence" start
link_started=true
export KNOWLEDGE_TEST_DSN="postgres://sea_h01_test@127.0.0.1:$link_port/postgres?sslmode=disable"
cd "$link_repo"
go test -mod=readonly -race -count=1 -v ./service/user/user/api/internal/config \
  ./service/user/user/api/internal/handler \
  ./service/user/user/identity/... | tee "$link_evidence/go-test.log"
go vet ./service/user/user/api/internal/config ./service/user/user/api/internal/handler ./service/user/user/identity/...
git diff --check
