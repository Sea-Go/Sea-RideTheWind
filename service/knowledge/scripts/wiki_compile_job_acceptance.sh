#!/usr/bin/env bash
set -euo pipefail

: "${SEA_DC_JOB_PLATFORM_ROOT:?set an independent DataCenter development checkout}"
knowledge_root="$(cd "$(dirname "$0")/../../.." && pwd)"
export KNOWLEDGE_KEEP_EVIDENCE=1
export KNOWLEDGE_TEST_FILTER='^TestWikiCompile'
cd "$knowledge_root"
exec bash service/knowledge/scripts/acceptance.sh
