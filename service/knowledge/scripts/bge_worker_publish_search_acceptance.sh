#!/usr/bin/env bash
set -euo pipefail

# Test-only administrator publication and rollback over an actual BGE index.
export SEA_BGE_WORKER_PUBLISH_SEARCH=1
exec bash "$(dirname "$0")/bge_worker_cancel_new_release_acceptance.sh"
