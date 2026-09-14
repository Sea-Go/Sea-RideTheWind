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
h01_dc_pid=""
cleanup() {
  if [[ -n "$h01_dc_pid" ]]; then
    touch "$knowledge_tmp/dc-fixture.stop"
    wait "$h01_dc_pid" || true
  fi
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
unset H01_DC_AUTH_URL H01_DC_OWNER_BEARER H01_DC_OTHER_BEARER
if [[ "${H01_DC_REAL_GATE:-0}" == 1 ]]; then
  if [[ "${KNOWLEDGE_REAL_USER_GATE:-0}" != 1 || -z "${H01_DC_CHECKOUT:-}" ]]; then
    echo "H01 real DC gate requires KNOWLEDGE_REAL_USER_GATE=1 and H01_DC_CHECKOUT" >&2
    exit 1
  fi
  h01_dc_checkout="$(cd "$H01_DC_CHECKOUT" && pwd)"
  h01_dc_test="$knowledge_repo/service/user/user/identity/linking/testdata/dc_auth_fixture_test.go"
  test -f "$h01_dc_test"
  python3 - "$h01_dc_checkout/cmd/server/h01_overlay_test.go" "$h01_dc_test" "$knowledge_tmp/dc-overlay.json" <<'PY'
import json
import sys
with open(sys.argv[3], 'w', encoding='utf-8') as output:
    json.dump({'Replace': {sys.argv[1]: sys.argv[2]}}, output)
PY
  export H01_DC_FIXTURE_INFO="$knowledge_tmp/dc-fixture.json"
  export H01_DC_FIXTURE_STOP="$knowledge_tmp/dc-fixture.stop"
  (
    cd "$h01_dc_checkout"
    go test -overlay "$knowledge_tmp/dc-overlay.json" -run '^TestH01RealNativeAuthFixture$' \
      -count=1 -timeout=4m ./cmd/server
  ) > "$knowledge_tmp/dc-auth.log" 2>&1 &
  h01_dc_pid="$!"
  for _ in {1..900}; do
    if [[ -s "$H01_DC_FIXTURE_INFO" ]]; then
      break
    fi
    if ! kill -0 "$h01_dc_pid" 2>/dev/null; then
      echo "real DC auth fixture exited before issuing sessions; see dc-auth.log" >&2
      exit 1
    fi
    sleep 0.1
  done
  if [[ ! -s "$H01_DC_FIXTURE_INFO" ]]; then
    echo "real DC auth fixture did not become ready; see dc-auth.log" >&2
    exit 1
  fi
  export H01_DC_AUTH_URL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["url"])' "$H01_DC_FIXTURE_INFO")"
  export H01_DC_OWNER_BEARER="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["owner_bearer"])' "$H01_DC_FIXTURE_INFO")"
  export H01_DC_OTHER_BEARER="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["other_bearer"])' "$H01_DC_FIXTURE_INFO")"
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
go test -race ./service/knowledge/... -count=1 -v | tee "$knowledge_tmp/test.log"
go vet ./service/knowledge/...
if [[ "${KNOWLEDGE_REAL_USER_GATE:-0}" == 1 ]]; then
  go test -race ./service/user/user/... -count=1 -v | tee "$knowledge_tmp/user-test.log"
  go vet ./service/user/user/...
fi
if [[ -n "$h01_dc_pid" ]]; then
  touch "$knowledge_tmp/dc-fixture.stop"
  wait "$h01_dc_pid"
  h01_dc_pid=""
fi
git diff --check
