#!/usr/bin/env bash
set -euo pipefail

# Explicit operator gate. The ordinary Knowledge Store/schema constructor does
# not apply this candidate. Use only a reviewed, disposable/local target here.
knowledge_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
if [[ $# != 1 || "$1" != --apply || -z "${KNOWLEDGE_SUBJECTREF_V2_DSN:-}" ]]; then
  printf 'usage: KNOWLEDGE_SUBJECTREF_V2_DSN=<reviewed-local-dsn> %s --apply\n' "$0" >&2
  exit 2
fi
knowledge_evidence="$(mktemp -d "${TMPDIR:-/tmp}/sea-rtw-v2-storage-gate.XXXXXX")"
printf 'Evidence directory: %s\n' "$knowledge_evidence"
cd "$knowledge_repo"
KNOWLEDGE_PREFLIGHT_DSN="$KNOWLEDGE_SUBJECTREF_V2_DSN" \
  go run -mod=readonly ./service/knowledge/cmd/subjectref-preflight \
    -report "$knowledge_evidence/preflight.json" \
    > "$knowledge_evidence/preflight.stdout" 2> "$knowledge_evidence/preflight.stderr"
psql "$KNOWLEDGE_SUBJECTREF_V2_DSN" -v ON_ERROR_STOP=1 \
  -f service/knowledge/scripts/migrate-subjectref-v2-storage.sql \
  > "$knowledge_evidence/migration.log" 2>&1
psql "$KNOWLEDGE_SUBJECTREF_V2_DSN" -v ON_ERROR_STOP=1 -At -c \
  "SELECT (SELECT count(*) FROM knowledge_answer_sessions_subject_v2),
          (SELECT count(*) FROM knowledge_accepted_answers_subject_v2),
          (SELECT count(*) FROM knowledge_product_search_operations_subject_v2),
          (SELECT count(*) FROM knowledge_tool_parents_subject_v2)" \
  > "$knowledge_evidence/projected-counts.txt"
printf 'Projection row counts (session|accepted|product|tool): '
cat "$knowledge_evidence/projected-counts.txt"
