#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/rtw-community-facts.XXXXXX")"
pg_data="$tmp_dir/data"
pg_bin="${RTW_TEST_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
pg_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"

stop_pg() {
  "$pg_bin/pg_ctl" -D "$pg_data" -m immediate -w stop >/dev/null 2>&1 || true
}
trap stop_pg EXIT

"$pg_bin/initdb" -A trust --no-instructions -D "$pg_data" >"$tmp_dir/init.log"
"$pg_bin/pg_ctl" -D "$pg_data" -l "$tmp_dir/server.log" \
  -o "-c listen_addresses='' -c unix_socket_directories='$tmp_dir' -p $pg_port" -w start >/dev/null

"$pg_bin/createdb" -h "$tmp_dir" -p "$pg_port" rtw_comment_fact_test
"$pg_bin/createdb" -h "$tmp_dir" -p "$pg_port" rtw_like_fact_test
"$pg_bin/createdb" -h "$tmp_dir" -p "$pg_port" rtw_like_consumer_fact_test
export RTW_COMMENT_FACT_TEST_DSN="host=$tmp_dir port=$pg_port dbname=rtw_comment_fact_test sslmode=disable"
export RTW_LIKE_FACT_TEST_DSN="host=$tmp_dir port=$pg_port dbname=rtw_like_fact_test sslmode=disable"
export RTW_LIKE_CONSUMER_FACT_TEST_DSN="host=$tmp_dir port=$pg_port dbname=rtw_like_consumer_fact_test sslmode=disable"

cd "$repo_root"
go test -mod=readonly -race -count=1 \
  ./service/comment/rpc/internal/model \
  ./service/like/rpc/internal/model \
  ./service/like/rpc/internal/mq/internal/mqs
for i in 1 2; do
  "$pg_bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$tmp_dir" -p "$pg_port" -d rtw_comment_fact_test \
    -f service/comment/rpc/internal/model/migrations/001_comment_domain_fact_outbox.sql >/dev/null
  "$pg_bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$tmp_dir" -p "$pg_port" -d rtw_like_fact_test \
    -f service/like/rpc/internal/model/migrations/001_like_domain_fact_outbox.sql >/dev/null
done
echo "isolated PostgreSQL 16 community fact tests passed (data: $tmp_dir)"
