#!/usr/bin/env bash
set -euo pipefail

# Opt into two actual BTW command processes and their same-run restart gate.
export SEA_BGE_WORKER_PROCESSES=1
exec bash "$(dirname "$0")/bge_job_acceptance.sh"
