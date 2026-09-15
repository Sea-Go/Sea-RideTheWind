#!/usr/bin/env bash
set -euo pipefail

# Test-only: creates a NEW disposable PG16 cluster and the random marker that
# the operator candidate deliberately cannot provision for an arbitrary DSN.
knowledge_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
knowledge_pg_bin="${KNOWLEDGE_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
knowledge_evidence="$(mktemp -d "${TMPDIR:-/tmp}/sea-rtw-v2-storage-gate-test.XXXXXX")"
knowledge_started=false
finish() {
  local original_status=$?
  if "$knowledge_started"; then
    if ! "$knowledge_pg_bin/pg_ctl" -D "$knowledge_evidence/pg" -m fast -w stop \
      > "$knowledge_evidence/stop.log" 2>&1; then
      original_status=1
    fi
    local pg_status
    set +e
    "$knowledge_pg_bin/pg_ctl" -D "$knowledge_evidence/pg" status \
      > "$knowledge_evidence/status.log" 2>&1
    pg_status=$?
    set -e
    if [[ "$pg_status" != 3 ]]; then
      original_status=1
    fi
  fi
  printf 'Evidence directory: %s\n' "$knowledge_evidence"
  return "$original_status"
}
trap finish EXIT
knowledge_port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
)"
"$knowledge_pg_bin/initdb" -D "$knowledge_evidence/pg" -A trust --no-locale \
  -U sea_knowledge_test > "$knowledge_evidence/initdb.log"
"$knowledge_pg_bin/pg_ctl" -D "$knowledge_evidence/pg" -l "$knowledge_evidence/postgres.log" \
  -o "-h 127.0.0.1 -p $knowledge_port -k $knowledge_evidence" -w start \
  > "$knowledge_evidence/start.log"
knowledge_started=true
knowledge_dsn="postgres://sea_knowledge_test@127.0.0.1:$knowledge_port/postgres?sslmode=disable"
knowledge_nonce="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
)"
knowledge_wrong_nonce="$(python3 - <<'PY'
print('0' * 64)
PY
)"
cd "$knowledge_repo"
"$knowledge_pg_bin/psql" -X "$knowledge_dsn" -v ON_ERROR_STOP=1 \
  -f service/knowledge/api/internal/model/schema.sql \
  > "$knowledge_evidence/schema.log" 2>&1

candidate="service/knowledge/scripts/apply-subjectref-v2-storage-candidate.sh"
expect_rejected() {
  local name="$1" dsn="$2" nonce="$3" status
  set +e
  KNOWLEDGE_SUBJECTREF_V2_DSN="$dsn" KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE="$nonce" \
    bash "$candidate" --apply > "$knowledge_evidence/$name.log" 2>&1
  status=$?
  set -e
  if [[ "$status" == 0 ]]; then
    printf 'unreviewed candidate target was accepted: %s\n' "$name" >&2
    exit 1
  fi
  if [[ "$("$knowledge_pg_bin/psql" -X "$knowledge_dsn" -At -c \
    "SELECT to_regclass('knowledge_answer_sessions_subject_v2') IS NULL")" != t ]]; then
    printf 'rejected candidate target caused DDL: %s\n' "$name" >&2
    exit 1
  fi
  printf '%s: rejected before DDL (exit=%s)\n' "$name" "$status"
}

expect_rejected missing-nonce "$knowledge_dsn" ''
expect_rejected missing-marker "$knowledge_dsn" "$knowledge_nonce"
"$knowledge_pg_bin/psql" -X "$knowledge_dsn" -v ON_ERROR_STOP=1 -c \
  "CREATE TABLE public.knowledge_subjectref_v2_local_test_gate (
      nonce text PRIMARY KEY CHECK(nonce ~ '^[0-9a-f]{64}$'),
      created_at timestamptz NOT NULL DEFAULT clock_timestamp());
   INSERT INTO public.knowledge_subjectref_v2_local_test_gate(nonce)
      VALUES('$knowledge_nonce')" > "$knowledge_evidence/marker.log" 2>&1
expect_rejected wrong-nonce "$knowledge_dsn" "$knowledge_wrong_nonce"
knowledge_socket_dsn="host=$knowledge_evidence dbname=postgres user=sea_knowledge_test"
expect_rejected non-tcp "$knowledge_socket_dsn" "$knowledge_nonce"

KNOWLEDGE_SUBJECTREF_V2_DSN="$knowledge_dsn" KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE="$knowledge_nonce" \
  bash "$candidate" --apply > "$knowledge_evidence/first.log" 2>&1
KNOWLEDGE_SUBJECTREF_V2_DSN="$knowledge_dsn" KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE="$knowledge_nonce" \
  bash "$candidate" --apply > "$knowledge_evidence/replay.log" 2>&1
"$knowledge_pg_bin/psql" -X "$knowledge_dsn" -v ON_ERROR_STOP=1 -c \
  "INSERT INTO knowledge_answer_sessions(authority_id,tenant_id,subject_id,session_id)
   VALUES('rtw.identity','archive','42','legacy')" \
  > "$knowledge_evidence/seed-blocker.log" 2>&1
set +e
KNOWLEDGE_SUBJECTREF_V2_DSN="$knowledge_dsn" KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE="$knowledge_nonce" \
  bash "$candidate" --apply > "$knowledge_evidence/preflight-blocked.log" 2>&1
knowledge_block_status=$?
set -e
if [[ "$knowledge_block_status" == 0 ||
      "$("$knowledge_pg_bin/psql" -X "$knowledge_dsn" -At -c \
      "SELECT count(*) FROM knowledge_answer_sessions_subject_v2")" != 0 ]]; then
  printf 'blocking preflight advanced a projection\n' >&2
  exit 1
fi
printf 'valid marked target: first/replay passed; later invalid v1 slot blocked (exit=%s)\n' \
  "$knowledge_block_status"
