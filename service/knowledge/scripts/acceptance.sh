#!/usr/bin/env bash
set -euo pipefail
# Starts only a disposable local PostgreSQL cluster; never reuses a configured database.
knowledge_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
knowledge_pg_bin="${KNOWLEDGE_PG_BIN:-}"
if [[ -z "$knowledge_pg_bin" ]]; then
  knowledge_pg_bin="$(dirname "$(command -v initdb)")"
fi
knowledge_tmp="$(mktemp -d "${TMPDIR:-/tmp}/sea-knowledge-acceptance.XXXXXX")"
knowledge_started=false
cleanup() {
  if [[ "$knowledge_started" == true ]]; then
    "$knowledge_pg_bin/pg_ctl" -D "$knowledge_tmp/pg" -m fast stop > "$knowledge_tmp/stop.log" 2>&1 || true
  fi
  if [[ "${KNOWLEDGE_KEEP_EVIDENCE:-0}" == 1 ]]; then
    printf 'Evidence directory: %s\n' "$knowledge_tmp"
  else
    rm -rf "$knowledge_tmp"
  fi
}
trap cleanup EXIT
knowledge_port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
)"
"$knowledge_pg_bin/initdb" -D "$knowledge_tmp/pg" -A trust --no-locale -U sea_knowledge_test > "$knowledge_tmp/initdb.log"
"$knowledge_pg_bin/pg_ctl" -D "$knowledge_tmp/pg" -l "$knowledge_tmp/postgres.log" -o "-h 127.0.0.1 -p $knowledge_port -k $knowledge_tmp" start
knowledge_started=true
export KNOWLEDGE_TEST_DSN="postgres://sea_knowledge_test@127.0.0.1:$knowledge_port/postgres?sslmode=disable"
export KNOWLEDGE_TEST_VERSION="$(git -C "$knowledge_repo" rev-parse HEAD)"
if [[ "${KNOWLEDGE_KEEP_EVIDENCE:-0}" == 1 ]]; then
  export KNOWLEDGE_OBS_EVIDENCE_DIR="$knowledge_tmp/observability"
fi
cd "$knowledge_repo"
if [[ "${KNOWLEDGE_REAL_USER_GATE:-0}" == 1 ]]; then
  go build -race -o "$knowledge_tmp/user-rpc" ./service/user/user/rpc
  go build -race -o "$knowledge_tmp/usercenter" ./service/user/user/api
  export KNOWLEDGE_REAL_USER_RPC_BINARY="$knowledge_tmp/user-rpc"
  export KNOWLEDGE_REAL_USER_API_BINARY="$knowledge_tmp/usercenter"
fi
go test -race ./service/knowledge/... -count=1 -v | tee "$knowledge_tmp/test.log"
go vet ./service/knowledge/...
if [[ "${KNOWLEDGE_REAL_USER_GATE:-0}" == 1 ]]; then
  go test -race ./service/user/user/... -count=1 -v | tee "$knowledge_tmp/user-test.log"
  go vet ./service/user/user/...
fi
git diff --check
