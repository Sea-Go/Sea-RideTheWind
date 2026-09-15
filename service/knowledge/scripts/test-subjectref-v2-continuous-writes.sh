#!/usr/bin/env bash
set -euo pipefail

# Fresh local PG16, a marker provisioned by this independent test owner, and
# isolated per-test schemas. No configured/shared Knowledge database is used.
knowledge_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
knowledge_pg_bin="${KNOWLEDGE_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
knowledge_evidence="$(mktemp -d "${TMPDIR:-/tmp}/sea-rtw-v2-continuous-writes.XXXXXX")"
knowledge_started=false
finish() {
  local original_status=$? pg_status
  if "$knowledge_started"; then
    if ! "$knowledge_pg_bin/pg_ctl" -D "$knowledge_evidence/pg" -m fast -w stop \
      > "$knowledge_evidence/stop.log" 2>&1; then
      original_status=1
    fi
    set +e
    "$knowledge_pg_bin/pg_ctl" -D "$knowledge_evidence/pg" status \
      > "$knowledge_evidence/status.log" 2>&1
    pg_status=$?
    set -e
    if [[ "$pg_status" != 3 ]]; then original_status=1; fi
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
export KNOWLEDGE_TEST_DSN="postgres://sea_knowledge_test@127.0.0.1:$knowledge_port/postgres?sslmode=disable"
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
  > "$knowledge_evidence/marker.log" 2>&1
cd "$knowledge_repo"
knowledge_filter="${KNOWLEDGE_V2_TEST_FILTER:-^TestContinuousSubjectRefV2}"
GOMAXPROCS=2 go test -mod=readonly -race -p=1 -count=1 -v \
  -run "$knowledge_filter" ./service/knowledge/api/internal/model \
  ./service/knowledge/api/internal/svc ./service/knowledge/cmd/subjectref-preflight \
  > "$knowledge_evidence/affected-race.log" 2>&1
go vet -mod=readonly ./service/knowledge/api/internal/model \
  ./service/knowledge/api/internal/svc ./service/knowledge/cmd/subjectref-preflight \
  > "$knowledge_evidence/affected-vet.log" 2>&1
git diff --check > "$knowledge_evidence/diff-check.log" 2>&1
printf 'Continuous v2 Knowledge write tests passed against a marked fresh PostgreSQL 16 instance\n'
