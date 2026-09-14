#!/usr/bin/env bash
set -euo pipefail

favorite_root="$(cd "$(dirname "$0")/../../.." && pwd)"
favorite_btw_root="${SEA_BTW_WORKER_ROOT:?set SEA_BTW_WORKER_ROOT to the isolated BTW worker checkout}"
: "${SEA_DC_PLATFORM_ROOT:?set SEA_DC_PLATFORM_ROOT to the isolated DC platform checkout}"
test -f "$favorite_btw_root/cmd/worker/favorite_acceptance.sh"
test -f "$favorite_root/service/favorite/rpc/internal/server/favorite_article_worker_shared_fixture_test.go"

# The existing BTW script creates both the shared ready and release files.
# It inherits this mask so neither can expose the local service tokens.
umask 077
export FAVORITE_SHARED_FULL_CHAIN=1
export SEA_RTW_FAVORITE_ROOT="$favorite_root"
export SEA_EXPECT_FAVORITE_REVISION=article-shared-authority:r1
exec bash "$favorite_btw_root/cmd/worker/favorite_acceptance.sh"
