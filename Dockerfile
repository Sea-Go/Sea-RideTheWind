# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS builder

WORKDIR /src

ENV PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG no_proxy

ARG API_BUILD_PATH=""
ARG RPC_BUILD_PATH=""
ARG DEBUG_BUILD="0"

ENV GOPROXY=https://proxy.golang.org,direct
ENV GOCACHE=/root/.cache/go-build

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    echo "[builder] proxy env:"; \
    env | grep -i proxy || true; \
    echo "[builder] go env:"; \
    go env; \
    echo "[builder] go.mod preview:"; \
    sed -n '1,160p' go.mod; \
    echo "[builder] downloading modules..."; \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    mkdir -p /out/bin; \
    dbg_args=""; \
    if [ "${DEBUG_BUILD}" = "1" ]; then \
      dbg_args="-x -v -work"; \
      echo "[builder] DEBUG_BUILD=1"; \
    else \
      echo "[builder] DEBUG_BUILD=0"; \
    fi; \
    echo "[builder] API_BUILD_PATH=${API_BUILD_PATH}"; \
    echo "[builder] RPC_BUILD_PATH=${RPC_BUILD_PATH}"; \
    if [ "${DEBUG_BUILD}" = "1" ] && [ -n "${API_BUILD_PATH}" ]; then \
      echo "[builder] ===== API dependency list begin ====="; \
      go list -deps "${API_BUILD_PATH}" 2>&1 | tee /tmp/api-deps.log; \
      echo "[builder] ===== API dependency list end ====="; \
      echo "[builder] api deps count=$(wc -l < /tmp/api-deps.log)"; \
      echo "[builder] first 80 api deps:"; \
      sed -n '1,80p' /tmp/api-deps.log || true; \
    fi; \
    if [ "${DEBUG_BUILD}" = "1" ] && [ -n "${RPC_BUILD_PATH}" ]; then \
      echo "[builder] ===== RPC dependency list begin ====="; \
      go list -deps "${RPC_BUILD_PATH}" 2>&1 | tee /tmp/rpc-deps.log; \
      echo "[builder] ===== RPC dependency list end ====="; \
      echo "[builder] rpc deps count=$(wc -l < /tmp/rpc-deps.log)"; \
      echo "[builder] first 80 rpc deps:"; \
      sed -n '1,80p' /tmp/rpc-deps.log || true; \
    fi; \
    if [ -n "${API_BUILD_PATH}" ]; then \
      start_ts=$(date +%s); \
      echo "[builder] ===== API build begin ====="; \
      echo "[builder] start_time=$(date '+%Y-%m-%d %H:%M:%S')"; \
      echo "[builder] path=${API_BUILD_PATH}"; \
      sh -c "go build ${dbg_args} -trimpath -ldflags='-s -w' -o /out/bin/api '${API_BUILD_PATH}'"; \
      end_ts=$(date +%s); \
      echo "[builder] ===== API build finished ====="; \
      echo "[builder] end_time=$(date '+%Y-%m-%d %H:%M:%S')"; \
      echo "[builder] elapsed=$((end_ts-start_ts))s"; \
      ls -lah /out/bin/api; \
    else \
      echo "[builder] API build skipped"; \
    fi; \
    if [ -n "${RPC_BUILD_PATH}" ]; then \
      start_ts=$(date +%s); \
      echo "[builder] ===== RPC build begin ====="; \
      echo "[builder] start_time=$(date '+%Y-%m-%d %H:%M:%S')"; \
      echo "[builder] path=${RPC_BUILD_PATH}"; \
      sh -c "go build ${dbg_args} -trimpath -ldflags='-s -w' -o /out/bin/rpc '${RPC_BUILD_PATH}'"; \
      end_ts=$(date +%s); \
      echo "[builder] ===== RPC build finished ====="; \
      echo "[builder] end_time=$(date '+%Y-%m-%d %H:%M:%S')"; \
      echo "[builder] elapsed=$((end_ts-start_ts))s"; \
      ls -lah /out/bin/rpc; \
    else \
      echo "[builder] RPC build skipped"; \
    fi; \
    echo "[builder] final output:"; \
    ls -lah /out/bin

FROM debian:bookworm-slim

WORKDIR /app

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG no_proxy

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Asia/Shanghai

RUN set -eux; \
    echo "[runtime] proxy env:"; \
    env | grep -i proxy || true; \
    echo "[runtime] apt sources:"; \
    cat /etc/apt/sources.list.d/debian.sources || true; \
    printf 'Acquire::Retries "5";\nAcquire::http::Pipeline-Depth "0";\nAcquire::https::Pipeline-Depth "0";\nAcquire::ForceIPv4 "true";\n' > /etc/apt/apt.conf.d/99fix-network; \
    apt-get update > /tmp/apt-update.log 2>&1 || { \
      rc=$?; \
      echo "[runtime] apt-get update failed, rc=${rc}"; \
      tail -n 200 /tmp/apt-update.log || true; \
      exit "${rc}"; \
    }; \
    apt-get install -y --no-install-recommends ca-certificates tzdata bash > /tmp/apt-install.log 2>&1 || { \
      rc=$?; \
      echo "[runtime] apt-get install failed, rc=${rc}"; \
      tail -n 200 /tmp/apt-install.log || true; \
      exit "${rc}"; \
    }; \
    rm -rf /var/lib/apt/lists/*; \
    echo "[runtime] apt update tail:"; \
    tail -n 80 /tmp/apt-update.log || true; \
    echo "[runtime] apt install tail:"; \
    tail -n 80 /tmp/apt-install.log || true

COPY service /app/service
COPY --from=builder /out/bin /app/bin
COPY docker-entrypoint.sh /app/docker-entrypoint.sh

RUN set -eux; \
    chmod +x /app/docker-entrypoint.sh; \
    if [ -f /app/bin/api ]; then chmod +x /app/bin/api; fi; \
    if [ -f /app/bin/rpc ]; then chmod +x /app/bin/rpc; fi; \
    mkdir -p /app/log /tmp/service-configs /app/service/like/rpc/data; \
    ls -lah /app/bin || true

ENTRYPOINT ["/app/docker-entrypoint.sh"]
