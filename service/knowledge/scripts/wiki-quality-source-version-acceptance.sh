#!/usr/bin/env bash
set -euo pipefail

# Runs only against a new loopback PostgreSQL 16 cluster and retains every
# report/log for source-version review. No shared or production DSN is used.
source_root="$(cd "$(dirname "$0")/../../.." && pwd)"
pg_bin="${KNOWLEDGE_PG_BIN:-$(dirname "$(command -v initdb)")}"
if [[ -n "${SEA_BTW_WIKI_QUALITY_CONSUMER_ROOT:-}" ||
      -n "${SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT:-}" ||
      -n "${SEA_DC_EVENT_PLATFORM_ROOT:-}" ]]; then
  printf 'source-version package acceptance does not run cross-project holders\n' >&2
  exit 2
fi
evidence_dir="$(mktemp -d "${TMPDIR:-/tmp}/sea-wiki-source-version.XXXXXX")"
printf 'Wiki source-version evidence: %s\n' "$evidence_dir"
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
  printf 'Wiki source-version evidence: %s\n' "$evidence_dir"
  printf 'PostgreSQL stopped status: %s\n' "${stopped_status:-not-started}"
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
"$pg_bin/initdb" -D "$evidence_dir/pg" -A trust --no-locale -U sea_wiki_source_test \
  > "$evidence_dir/initdb.log" 2>&1
pg_attempted=true
"$pg_bin/pg_ctl" -D "$evidence_dir/pg" -l "$evidence_dir/postgres.log" \
  -o "-h 127.0.0.1 -p $port -k $evidence_dir" start \
  > "$evidence_dir/start.log" 2>&1
pg_started=true
export KNOWLEDGE_TEST_DSN="postgres://sea_wiki_source_test@127.0.0.1:$port/postgres?sslmode=disable"
export KNOWLEDGE_TEST_VERSION="$(git -C "$source_root" rev-parse HEAD)"
cd "$source_root"
GOMAXPROCS=2 go test -mod=readonly -race -p=1 -count=1 -v \
  -run '^(TestSourceVersionWithdrawalEventKeepsTypedTargetAndWholeJCS|TestWikiQualitySourceVersion.*|TestWikiQualitySourceSnapshot.*|TestWikiFactSetFullApprovedScopeAIManualCASAndOriginalEvent|TestWikiQualityFactAIManualRejudgeOriginalEventAndWithdrawal|TestWikiQualityOutboxFailureRollsBackJudgmentHeadAndReplay|TestWikiFactSetOutboxFailureRollsBackRevisionHeadAndReplay|TestExpiredBuildAndOutboxVersions)$' \
  ./service/knowledge/api/internal/model \
  > "$evidence_dir/test.log" 2>&1
KNOWLEDGE_FACT_SET_REAL_HTTP=1 GOMAXPROCS=2 go test -mod=readonly -race -p=1 \
  -count=1 -v -run '^TestRealHTTPKnowledgeWorkflow$' ./service/knowledge/api \
  > "$evidence_dir/http-test.log" 2>&1
GOMAXPROCS=2 go vet -mod=readonly ./service/knowledge/api/internal/model \
  ./service/knowledge/api/internal/logic/worker ./service/knowledge/api \
  > "$evidence_dir/vet.log" 2>&1
git diff --check > "$evidence_dir/diff-check.log" 2>&1
