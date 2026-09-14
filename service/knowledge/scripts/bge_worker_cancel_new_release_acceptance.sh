#!/usr/bin/env bash
set -euo pipefail

# Opt into explicit RTW/DC cancellation and a new frozen Release/Build generation.
export SEA_BGE_WORKER_CANCEL_RELEASE=1
exec bash "$(dirname "$0")/bge_worker_process_acceptance.sh"
