#!/usr/bin/env bash
set -euo pipefail

# A real five-second DC lease expires while the first index process is held.
export SEA_BGE_WORKER_EXPIRY=1
exec bash "$(dirname "$0")/bge_worker_process_acceptance.sh"
