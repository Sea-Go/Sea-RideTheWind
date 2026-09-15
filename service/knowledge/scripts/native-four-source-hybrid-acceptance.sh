#!/usr/bin/env bash
set -euo pipefail

# Owns only a new RTW loopback PostgreSQL 16. The explicit test starts one
# fixed BTW Hybrid Lite owner and stops it after its formal API closes.
source_root="$(cd "$(dirname "$0")/../../.." && pwd)"
pg_bin="${KNOWLEDGE_PG_BIN:-$(dirname "$(command -v initdb)")}"
if [[ "${SEA_RTW_NATIVE_FOUR_SOURCE:-}" != 1 ||
      -z "${SEA_DC_BGE_RUNTIME:-}" ||
      -z "${SEA_BTW_PRODUCT_SEARCH_ROOT:-}" ||
      "${SEA_BTW_PRODUCT_SEARCH_ROOT:-}" != "${SEA_BTW_SEARCH_API_SOCKET_ROOT:-}" ||
      -z "${SEA_BTW_NATIVE_SOURCE_SHA:-}" ||
      -z "${SEA_RTW_NATIVE_PYTHON:-}" ]]; then
  printf 'Native Hybrid requires one explicit RTW/DC/BTW/Python fixed test source\n' >&2
  exit 2
fi
if [[ "$($pg_bin/initdb --version)" != *'PostgreSQL) 16.'* ]]; then
  printf 'Native Hybrid acceptance requires PostgreSQL 16\n' >&2
  exit 2
fi
evidence_dir="$(mktemp -d "${TMPDIR:-/tmp}/sea-rtw-native-hybrid.XXXXXX")"
printf 'RTW Native Hybrid evidence: %s\n' "$evidence_dir"
pg_started=false
pg_attempted=false
finish() {
  original_status=$?
  trap - EXIT
  final_status="$original_status"
  if [[ "$pg_started" == true || "$pg_attempted" == true ]]; then
    set +e
    "$pg_bin/pg_ctl" -D "$evidence_dir/pg" status > "$evidence_dir/status-before-stop.log" 2>&1
    before_stop_status=$?
    set -e
    if [[ "$before_stop_status" == 0 ]]; then
      if ! "$pg_bin/pg_ctl" -D "$evidence_dir/pg" -m fast stop > "$evidence_dir/stop.log" 2>&1; then
        final_status=2
      fi
    elif [[ "$pg_started" == true || "$before_stop_status" != 3 ]]; then
      final_status=2
    fi
    set +e
    "$pg_bin/pg_ctl" -D "$evidence_dir/pg" status > "$evidence_dir/status.log" 2>&1
    stopped_status=$?
    set -e
    if [[ "$stopped_status" != 3 ]]; then
      final_status=2
    fi
  fi
  printf 'RTW Native Hybrid PG stopped status: %s\n' "${stopped_status:-not-started}"
  printf 'RTW Native Hybrid evidence: %s\n' "$evidence_dir"
  exit "$final_status"
}
trap finish EXIT

port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
)"
"$pg_bin/initdb" -D "$evidence_dir/pg" -A trust --no-locale -U sea_native_hybrid_test \
  > "$evidence_dir/initdb.log" 2>&1
pg_attempted=true
"$pg_bin/pg_ctl" -D "$evidence_dir/pg" -l "$evidence_dir/postgres.log" \
  -o "-h 127.0.0.1 -p $port -k $evidence_dir" start \
  > "$evidence_dir/start.log" 2>&1
pg_started=true
export KNOWLEDGE_TEST_DSN="postgres://sea_native_hybrid_test@127.0.0.1:$port/postgres?sslmode=disable"
export KNOWLEDGE_TEST_VERSION="$(git -C "$source_root" rev-parse HEAD)"
export KNOWLEDGE_OBS_EVIDENCE_DIR="$evidence_dir/observability"
mkdir -m 700 "$KNOWLEDGE_OBS_EVIDENCE_DIR"
cd "$source_root"
GOMAXPROCS=2 go test -mod=readonly -race -p=1 -count=1 -v \
  -run '^TestRealHTTPNativeFourSourceWorkflow$' ./service/knowledge/api \
  > "$evidence_dir/test.log" 2>&1
python3 - "$KNOWLEDGE_OBS_EVIDENCE_DIR/native-four-source" <<'PY'
import json
from pathlib import Path
import sys
root=Path(sys.argv[1])
report=json.loads((root / 'native-four-source-parent.json').read_bytes())
stop=json.loads((root / 'native-lite-stop.json').read_bytes())
if (report['schema_version'] != 'sea.rtw.native-four-source-parent.v1'
    or report['physical_qualified'] or report['qrel_evaluable']
    or report['production_verified'] or len(report['source_revision_ids']) != 4
    or not stop['stopped']
    or stop['engine_package_sha256'] != report['engine_package_sha256']):
    raise SystemExit('Native Hybrid parent/Lite stop receipts disagree')
PY
GOMAXPROCS=2 go vet -mod=readonly ./service/knowledge/api > "$evidence_dir/vet.log" 2>&1
git diff --check > "$evidence_dir/diff-check.log" 2>&1
