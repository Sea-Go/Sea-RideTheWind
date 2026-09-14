#!/usr/bin/env bash
set -euo pipefail

# Explicit opt-in, same-run RTW/BTW/DC jobs and locked BGE-M3 gateway acceptance.
rtw_root="$(cd "$(dirname "$0")/../../.." && pwd)"
: "${SEA_BTW_INDEX_CONSUMER_ROOT:?set independent BTW source worktree}"
: "${SEA_DC_JOB_PLATFORM_ROOT:?set independent DC source worktree}"
: "${BGE_SERVING_ROOT:?set locked BTW training/serving/bge_m3 directory}"
: "${BGE_MODEL_DIRECTORY:?set verified cached BGE-M3 snapshot}"
evidence="$(mktemp -d "${TMPDIR:-/tmp}/sea-h06-bge-job.XXXXXX")"
dc_log="$evidence/dc-bge-test.log"
dc_pid=""
dc_runtime=""
cleanup() {
  if [[ -n "$dc_runtime" ]]; then
    local dc_dir
    dc_dir="$(dirname "$dc_runtime")"
    touch "$dc_dir/release"
  fi
  if [[ -n "$dc_pid" ]]; then
    wait "$dc_pid" || true
  fi
  printf 'Evidence directory: %s\n' "$evidence"
}
trap cleanup EXIT

(cd "$SEA_DC_JOB_PLATFORM_ROOT" && BGE_HOLD_FOR_CONSUMER=1 \
  bash scripts/test-bge-representations.sh) >"$dc_log" 2>&1 &
dc_pid="$!"
for ((attempt=0; attempt<2400; attempt++)); do
  dc_dir="$(sed -n 's/^Live evidence directory: //p' "$dc_log" | head -1)"
  if [[ -n "$dc_dir" && -s "$dc_dir/runtime.json" ]]; then
    dc_runtime="$dc_dir/runtime.json"
    break
  fi
  if ! kill -0 "$dc_pid" 2>/dev/null; then
    printf 'DC BGE acceptance exited before runtime was ready; inspect %s\n' "$dc_log" >&2
    exit 1
  fi
  sleep 0.25
done
if [[ -z "$dc_runtime" ]]; then
  printf 'DC BGE runtime did not become ready; inspect %s\n' "$dc_log" >&2
  exit 1
fi

export SEA_BGE_RUNTIME_FILE="$dc_runtime"
export SEA_BTW_INDEX_CONSUMER_ROOT SEA_DC_JOB_PLATFORM_ROOT
export KNOWLEDGE_KEEP_EVIDENCE=1
knowledge_test_pattern='^TestRealHTTPKnowledgeWorkflow'
if [[ "${SEA_BGE_WORKER_PUBLISH_SEARCH:-0}" == 1 ]]; then
  export KNOWLEDGE_REAL_USER_GATE=0
  knowledge_test_pattern='^TestRealHTTPKnowledgeWorkflow$'
else
  export KNOWLEDGE_REAL_USER_GATE=1
fi
cd "$rtw_root"
GOFLAGS="-run=$knowledge_test_pattern" bash service/knowledge/scripts/acceptance.sh \
  >"$evidence/rtw-btw-jobs-test.log" 2>&1
touch "$(dirname "$dc_runtime")/release"
wait "$dc_pid"
dc_pid=""
printf 'Same-run RTW READY, DC job receipt and live BGE-M3 index acceptance passed.\n'
