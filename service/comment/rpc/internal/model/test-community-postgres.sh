#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/rtw-community-facts.XXXXXX")"
pg_data="$tmp_dir/data"
pg_bin="${RTW_TEST_PG_BIN:-/opt/homebrew/opt/postgresql@16/bin}"
pg_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"

stop_pg() {
  if [[ -n "${dc_pid:-}" ]]; then
    kill "$dc_pid" 2>/dev/null || true
    wait "$dc_pid" 2>/dev/null || true
  fi
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
  "$pg_bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$tmp_dir" -p "$pg_port" -d rtw_comment_fact_test \
    -f service/comment/rpc/internal/model/migrations/002_comment_dc_wire.sql >/dev/null
  "$pg_bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$tmp_dir" -p "$pg_port" -d rtw_like_fact_test \
    -f service/like/rpc/internal/model/migrations/001_like_domain_fact_outbox.sql >/dev/null
  "$pg_bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$tmp_dir" -p "$pg_port" -d rtw_like_fact_test \
    -f service/like/rpc/internal/model/migrations/002_like_dc_wire.sql >/dev/null
done

# The 002 NOT VALID checks retain old rows but must reject any new writer that
# still emits a legacy envelope after the cutover migration.
if "$pg_bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$tmp_dir" -p "$pg_port" -d rtw_comment_fact_test \
    -c "INSERT INTO comment_domain_fact_outbox(event_id,event_type,payload) VALUES('post-migration-legacy-comment','legacy','{}'::jsonb)" \
    >"$tmp_dir/comment-legacy-reject.log" 2>&1; then
  echo "comment 002 migration allowed an unversioned new fact" >&2
  exit 1
fi
rg -q 'comment_new_fact_has_dc_wire' "$tmp_dir/comment-legacy-reject.log"
if "$pg_bin/psql" -X -q -v ON_ERROR_STOP=1 -h "$tmp_dir" -p "$pg_port" -d rtw_like_fact_test \
    -c "INSERT INTO like_domain_fact_outbox(event_id,event_type,payload) VALUES('post-migration-legacy-like','legacy','{}'::jsonb)" \
    >"$tmp_dir/like-legacy-reject.log" 2>&1; then
  echo "like 002 migration allowed an unversioned new fact" >&2
  exit 1
fi
rg -q 'like_new_fact_has_dc_wire' "$tmp_dir/like-legacy-reject.log"

if [[ -n "${SEA_DC_PLATFORM_ROOT:-}" ]]; then
  if [[ ! -f "$SEA_DC_PLATFORM_ROOT/cmd/platform/main.go" ]]; then
    echo "SEA_DC_PLATFORM_ROOT is not a DataCenter cmd/platform checkout" >&2
    exit 1
  fi
  "$pg_bin/createdb" -h "$tmp_dir" -p "$pg_port" rtw_dc_wire_test
  dc_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
  dc_url="http://127.0.0.1:$dc_port"
  dc_token="fixture-community-dc-wire-token"
  (cd "$SEA_DC_PLATFORM_ROOT" && go build -mod=readonly -o "$tmp_dir/dc-platform" ./cmd/platform)
  DATABASE_URL="host=$tmp_dir port=$pg_port dbname=rtw_dc_wire_test sslmode=disable" \
    PLATFORM_SERVICE_TOKEN="$dc_token" \
    "$tmp_dir/dc-platform" -listen "127.0.0.1:$dc_port" -migrate >"$tmp_dir/dc-platform.log" 2>&1 &
  dc_pid=$!
  dc_ready=false
  for _ in $(seq 1 80); do
    if [[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $dc_token" \
      "$dc_url/v1/events/rtw.comment-rpc/readiness" 2>/dev/null || true)" == "404" ]]; then
      dc_ready=true
      break
    fi
    sleep 0.25
  done
  if ! "$dc_ready"; then
    echo "DataCenter cmd/platform did not become ready: $tmp_dir/dc-platform.log" >&2
    exit 1
  fi

  post_event() {
    local body="$1" expected="$2" response="$3" key="$4"
    local status
    status="$(curl -sS -o "$response" -w '%{http_code}' -H "Authorization: Bearer $dc_token" \
      -H 'Content-Type: application/json' -H "Idempotency-Key: $key" \
      --data-binary "$body" "$dc_url/v1/events")"
    if [[ "$status" != "$expected" ]]; then
      echo "DataCenter status $status, expected $expected: $(cat "$response")" >&2
      exit 1
    fi
  }
  "$pg_bin/psql" -X -At -h "$tmp_dir" -p "$pg_port" -d rtw_comment_fact_test \
    -c "SELECT delivery_envelope::text FROM comment_domain_fact_outbox WHERE aggregate_id='1001' ORDER BY fact_version" \
    >"$tmp_dir/comment-events.jsonl"
  "$pg_bin/psql" -X -At -h "$tmp_dir" -p "$pg_port" -d rtw_like_fact_test \
    -c "SELECT delivery_envelope::text FROM like_domain_fact_outbox WHERE aggregate_id='like-state/101/article/200' ORDER BY fact_version" \
    >"$tmp_dir/like-events.jsonl"
  [[ "$(wc -l <"$tmp_dir/comment-events.jsonl" | tr -d ' ')" == "4" ]]
  [[ "$(wc -l <"$tmp_dir/like-events.jsonl" | tr -d ' ')" == "6" ]]

  for domain in comment like; do
    expected_version=1
    while IFS= read -r body; do
      event_id="$(jq -r '.event_id' <<<"$body")"
      jq -e --argjson version "$expected_version" \
        '.schema_version == 1 and .aggregate_version == $version and .payload.schema_version == "rtw.community-fact.v1" and .payload.source_ref != "" and .payload.target_revision == null and .payload.aggregate_version == null' \
        <<<"$body" >/dev/null
      post_event "$body" 201 "$tmp_dir/${domain}-${expected_version}-accepted.json" "$event_id"
      jq -e --arg id "$event_id" --arg producer "rtw.$([[ "$domain" == "comment" ]] && echo comment-rpc || echo like-mq)" \
        --argjson offset "$expected_version" \
        '.event_id == $id and .producer == $producer and .technical_status == "accepted" and .offset == $offset and (.receipt_id | length) > 0 and (.input_hash | length) == 64' \
        "$tmp_dir/${domain}-${expected_version}-accepted.json" >/dev/null
      if [[ "$expected_version" == "1" ]]; then
        printf '%s\n' "$body" >"$tmp_dir/${domain}-first.json"
      fi
      expected_version=$((expected_version + 1))
    done <"$tmp_dir/${domain}-events.jsonl"
  done

  first_comment="$(cat "$tmp_dir/comment-first.json")"
  first_id="$(jq -r '.event_id' <<<"$first_comment")"
  post_event "$first_comment" 200 "$tmp_dir/comment-replayed.json" "$first_id"
  diff -u <(jq -S . "$tmp_dir/comment-1-accepted.json") <(jq -S . "$tmp_dir/comment-replayed.json")
  post_event "$(jq -c '.payload.operation="tampered"' <<<"$first_comment")" 409 "$tmp_dir/conflicting-body.json" "$first_id"
  post_event "$(jq -c '.schema_version="rtw.community-fact.v1"' <<<"$first_comment")" 400 "$tmp_dir/string-schema.json" "$first_id"
  post_event "$(jq -c '.aggregate_version=null' <<<"$first_comment")" 400 "$tmp_dir/null-version.json" "$first_id"
  post_event "$(jq -c '.event_id="other-comment-event" | .operation_id="other-comment-operation"' <<<"$first_comment")" \
    409 "$tmp_dir/duplicate-aggregate-version.json" "other-comment-event"
  comment_watermark="$(curl -fsS -H "Authorization: Bearer $dc_token" \
    "$dc_url/v1/event-sources/rtw.comment-rpc/aggregates/1001")"
  jq -e '.contiguous_version == 4 and .max_seen_version == 4 and .has_gap == false' \
    <<<"$comment_watermark" >/dev/null
  echo "actual DataCenter cmd/platform HTTP event contract passed (comment 4, like 6, replay/conflict/schema/watermark)"
fi
echo "isolated PostgreSQL 16 community fact tests passed (data: $tmp_dir)"
