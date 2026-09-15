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
if [[ "${KNOWLEDGE_V2_PRODUCER_SCOPE:-0}" == 1 ]]; then
  if [[ "${KNOWLEDGE_REAL_USER_GATE:-0}" != 1 ]]; then
    printf 'v2 producer candidate requires the real UserCenter gate\n' >&2
    exit 2
  fi
  export KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
)"
  "$knowledge_pg_bin/psql" -X "$KNOWLEDGE_TEST_DSN" -v ON_ERROR_STOP=1 \
    -c "CREATE TABLE public.knowledge_subjectref_v2_local_test_gate (
      nonce text PRIMARY KEY CHECK(nonce ~ '^[0-9a-f]{64}$'),
      created_at timestamptz NOT NULL DEFAULT clock_timestamp());
      INSERT INTO public.knowledge_subjectref_v2_local_test_gate(nonce)
      VALUES('$KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE')" \
    > "$knowledge_tmp/v2-marker.log" 2>&1
fi
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
GOMAXPROCS=2 go test -mod=readonly -race -p=1 ./service/knowledge/... -count=1 -v \
  -run "${KNOWLEDGE_TEST_FILTER:-.}" | tee "$knowledge_tmp/test.log"
go vet ./service/knowledge/...
if [[ "${KNOWLEDGE_REAL_USER_GATE:-0}" == 1 ]]; then
  GOMAXPROCS=2 go test -mod=readonly -race -p=1 ./service/user/user/... -count=1 -v \
    -run "${KNOWLEDGE_TEST_FILTER:-.}" | tee "$knowledge_tmp/user-test.log"
  go vet ./service/user/user/...
fi
git diff --check
