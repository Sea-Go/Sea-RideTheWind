#!/usr/bin/env bash
set -euo pipefail

# Runs the real UserCenter + Knowledge HTTP process only on a fresh marked
# local PostgreSQL. Optional BTW roots are independently built consumers.
knowledge_script_dir="$(cd "$(dirname "$0")" && pwd)"
export KNOWLEDGE_V2_PRODUCER_SCOPE=1
export KNOWLEDGE_REAL_USER_GATE=1
export KNOWLEDGE_KEEP_EVIDENCE=1
export KNOWLEDGE_TEST_FILTER='^TestRealHTTPKnowledgeWorkflowWithUserCenter$'
bash "$knowledge_script_dir/acceptance.sh"
