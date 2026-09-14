#!/usr/bin/env bash
set -euo pipefail
article_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
article_pg_bin="${ARTICLE_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
article_tmp="$(mktemp -d "${TMPDIR:-/tmp}/sea-article-revision.XXXXXX")"
article_started=false
finish() {
  if "$article_started"; then
    "$article_pg_bin/pg_ctl" -D "$article_tmp/pg" -m fast -w stop >"$article_tmp/stop.log" 2>&1 || true
  fi
  printf 'Evidence directory: %s\n' "$article_tmp"
}
trap finish EXIT
article_port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1',0))
    print(sock.getsockname()[1])
PY
)"
"$article_pg_bin/initdb" -D "$article_tmp/pg" -A trust --no-locale -U sea_article_test >"$article_tmp/initdb.log"
"$article_pg_bin/pg_ctl" -D "$article_tmp/pg" -l "$article_tmp/postgres.log" \
  -o "-h 127.0.0.1 -p $article_port -k $article_tmp" -w start
article_started=true
export ARTICLE_TEST_DSN="postgres://sea_article_test@127.0.0.1:$article_port/postgres?sslmode=disable"
cd "$article_repo"
go test -mod=readonly -race -count=1 -v ./service/article/rpc/... | tee "$article_tmp/go-test.log"
go vet ./service/article/rpc/...
git diff --check
