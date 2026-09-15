#!/usr/bin/env bash
set -euo pipefail

# Explicit operator gate. The ordinary Knowledge Store/schema constructor does
# not apply this candidate. A disposable test harness must provision a fresh
# random nonce in public.knowledge_subjectref_v2_local_test_gate first. This
# script never creates the marker and refuses an unmarked or stale target.
knowledge_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
knowledge_test_nonce="${KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE:-}"
if [[ $# != 1 || "$1" != --apply || -z "${KNOWLEDGE_SUBJECTREF_V2_DSN:-}" ||
      ! "$knowledge_test_nonce" =~ ^[0-9a-f]{64}$ ]]; then
  printf 'usage: KNOWLEDGE_SUBJECTREF_V2_DSN=<disposable-loopback-dsn> KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE=<fresh-64-hex> %s --apply\n' "$0" >&2
  exit 2
fi
verify_disposable_target() {
  local verified
  verified="$(psql -X "$KNOWLEDGE_SUBJECTREF_V2_DSN" -v ON_ERROR_STOP=1 -At -c \
    "SELECT EXISTS (SELECT 1 FROM public.knowledge_subjectref_v2_local_test_gate
      WHERE nonce='$knowledge_test_nonce'
        AND created_at BETWEEN clock_timestamp()-interval '1 hour' AND clock_timestamp())
      AND current_database()='postgres' AND current_user='sea_knowledge_test'
      AND inet_server_addr()='127.0.0.1'::inet
      AND inet_client_addr()='127.0.0.1'::inet" 2>/dev/null)" || {
    printf 'SubjectRef v2 storage candidate requires a marked disposable PostgreSQL test target\n' >&2
    exit 2
  }
  if [[ "$verified" != t ]]; then
    printf 'SubjectRef v2 storage candidate rejected an unmarked or nonlocal PostgreSQL target\n' >&2
    exit 2
  fi
}
verify_disposable_target
knowledge_evidence="$(mktemp -d "${TMPDIR:-/tmp}/sea-rtw-v2-storage-gate.XXXXXX")"
printf 'Evidence directory: %s\n' "$knowledge_evidence"
cd "$knowledge_repo"
KNOWLEDGE_PREFLIGHT_DSN="$KNOWLEDGE_SUBJECTREF_V2_DSN" \
  go run -mod=readonly ./service/knowledge/cmd/subjectref-preflight \
    -report "$knowledge_evidence/preflight.json" \
    > "$knowledge_evidence/preflight.stdout" 2> "$knowledge_evidence/preflight.stderr"
verify_disposable_target
psql -X "$KNOWLEDGE_SUBJECTREF_V2_DSN" -v ON_ERROR_STOP=1 \
  -f service/knowledge/scripts/migrate-subjectref-v2-storage.sql \
  > "$knowledge_evidence/migration.log" 2>&1
psql -X "$KNOWLEDGE_SUBJECTREF_V2_DSN" -v ON_ERROR_STOP=1 -At -c \
  "SELECT (SELECT count(*) FROM knowledge_answer_sessions_subject_v2),
          (SELECT count(*) FROM knowledge_accepted_answers_subject_v2),
          (SELECT count(*) FROM knowledge_product_search_operations_subject_v2),
          (SELECT count(*) FROM knowledge_tool_parents_subject_v2)" \
  > "$knowledge_evidence/projected-counts.txt"
printf 'Projection row counts (session|accepted|product|tool): '
cat "$knowledge_evidence/projected-counts.txt"
