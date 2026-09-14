#!/usr/bin/env bash
set -euo pipefail

# Test-only RTW Knowledge + real User Center rendezvous for a separate desktop
# client. The caller supplies four new absolute ready/release paths and must
# release both stages; no credential is printed or copied into Git.
knowledge_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
for key in KNOWLEDGE_SHARED_HISTORY_READY KNOWLEDGE_SHARED_HISTORY_RELEASE \
  KNOWLEDGE_SHARED_HISTORY_WITHDRAWN_READY KNOWLEDGE_SHARED_HISTORY_WITHDRAWN_RELEASE; do
  if [[ -z "${!key:-}" || "${!key}" != /* ]]; then
    printf 'set %s to a new absolute temporary path\n' "$key" >&2
    exit 2
  fi
  if [[ -e "${!key}" ]]; then
    printf '%s must not exist before the shared test\n' "$key" >&2
    exit 2
  fi
done
knowledge_pg_bin="${KNOWLEDGE_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
knowledge_tmp="$(mktemp -d "${TMPDIR:-/tmp}/sea-knowledge-shared-history.XXXXXX")"
knowledge_started=false
finish() {
  if "$knowledge_started"; then
    "$knowledge_pg_bin/pg_ctl" -D "$knowledge_tmp/pg" -m fast -w stop >"$knowledge_tmp/stop.log" 2>&1 || true
  fi
  printf 'Evidence directory: %s\n' "$knowledge_tmp"
}
trap finish EXIT
knowledge_port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
)"
"$knowledge_pg_bin/initdb" -D "$knowledge_tmp/pg" -A trust --no-locale -U sea_knowledge_test >"$knowledge_tmp/initdb.log"
"$knowledge_pg_bin/pg_ctl" -D "$knowledge_tmp/pg" -l "$knowledge_tmp/postgres.log" \
  -o "-h 127.0.0.1 -p $knowledge_port -k $knowledge_tmp" -w start >"$knowledge_tmp/pg-start.log"
knowledge_started=true
export KNOWLEDGE_TEST_DSN="postgres://sea_knowledge_test@127.0.0.1:$knowledge_port/postgres?sslmode=disable"
export KNOWLEDGE_TEST_VERSION="$(git -C "$knowledge_repo" rev-parse HEAD)"
cd "$knowledge_repo"
GOFLAGS='-p=2' GOMAXPROCS=2 go build -mod=readonly -race -o "$knowledge_tmp/user-rpc" ./service/user/user/rpc
GOFLAGS='-p=2' GOMAXPROCS=2 go build -mod=readonly -race -o "$knowledge_tmp/usercenter" ./service/user/user/api
export KNOWLEDGE_REAL_USER_RPC_BINARY="$knowledge_tmp/user-rpc"
export KNOWLEDGE_REAL_USER_API_BINARY="$knowledge_tmp/usercenter"
GOFLAGS='-p=2' GOMAXPROCS=2 go test -mod=readonly -race -count=1 -v \
  -run '^TestRealHTTPKnowledgeWorkflowWithUserCenter$' ./service/knowledge/api >"$knowledge_tmp/go-test.log" 2>&1
GOFLAGS='-p=2' GOMAXPROCS=2 go vet -mod=readonly ./service/knowledge/api
go mod verify
git diff --check
rg '^--- PASS:|^PASS$|^ok[[:space:]]' "$knowledge_tmp/go-test.log" || true
