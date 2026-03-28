#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
DOCKERFILE="$ROOT_DIR/Dockerfile"
ENTRYPOINT_FILE="$ROOT_DIR/docker-entrypoint.sh"
DOCKERIGNORE_FILE="$ROOT_DIR/.dockerignore"
IMAGE_PREFIX="sea-trygo"
LOG_ROOT_DIR="$ROOT_DIR/.manage-logs"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="$LOG_ROOT_DIR/$RUN_ID"

# 机器角色：
# infra -> 基础设施机器，只负责 docker compose up infra
# app   -> Go 服务机器，只负责 docker run 各服务
NODE_ROLE="${NODE_ROLE:-app}"

# 远端基础设施主机 IP（必须手动填写）
# app 机器统一通过这个地址生成所有依赖地址
INFRA_HOST="请填写你的基础设施主机 IP，例如：INFRA_HOST=\"

# 如果你想手工覆盖下面这些地址，可以直接修改这里，或 export 后再执行脚本
ETCD_ADDR="${ETCD_ADDR:-${INFRA_HOST}:32379}"
REDIS_ADDR="${REDIS_ADDR:-${INFRA_HOST}:36379}"
POSTGRES_ADDR="${POSTGRES_ADDR:-${INFRA_HOST}}"
POSTGRES_PORT="${POSTGRES_PORT:-35432}"
KAFKA_ADDR="${KAFKA_ADDR:-${INFRA_HOST}:39092}"
MINIO_ADDR="${MINIO_ADDR:-${INFRA_HOST}:39000}"
BEANSTALKD1_ADDR="${BEANSTALKD1_ADDR:-${INFRA_HOST}:41300}"
BEANSTALKD2_ADDR="${BEANSTALKD2_ADDR:-${INFRA_HOST}:41301}"
OTEL_ADDR="${OTEL_ADDR:-${INFRA_HOST}:34317}"

# 只保留当前 compose 里真实存在的 infra 服务
INFRA_SERVICES=(etcd postgres redis kafka minio)

ALL_SERVICES=(article comment like follow favorite message task user admin hot points security)

BUILD_PROGRESS="${BUILD_PROGRESS:-plain}"
START_WAIT_SECONDS="${START_WAIT_SECONDS:-20}"
STATUS_TAIL_LINES="${STATUS_TAIL_LINES:-30}"
LOG_TAIL_LINES="${LOG_TAIL_LINES:-80}"
DEBUG_BUILD="${DEBUG_BUILD:-0}"
DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}"

timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

log() {
  local level="$1"
  shift
  echo "[$(timestamp)] [$level] $*"
}

info() {
  log INFO "$@"
}

warn() {
  log WARN "$@"
}

error() {
  log ERROR "$@" >&2
}

die() {
  error "$@"
  exit 1
}

section() {
  echo
  echo "================================================================"
  echo "[$(timestamp)] $*"
  echo "================================================================"
}

ensure_dir() {
  mkdir -p "$1"
}

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

run_logged() {
  local label="$1"
  local logfile="$2"
  shift 2

  local start_ts end_ts elapsed rc
  start_ts="$(date +%s)"

  section "$label"
  info "command: $*"
  info "logfile: $logfile"

  set +e
  "$@" 2>&1 | tee "$logfile"
  rc=${PIPESTATUS[0]}
  set -e

  end_ts="$(date +%s)"
  elapsed=$((end_ts - start_ts))

  if [ "$rc" -eq 0 ]; then
    info "$label finished successfully, elapsed=${elapsed}s"
  else
    error "$label failed, exit_code=${rc}, elapsed=${elapsed}s"
    error "see logfile: $logfile"
  fi

  return "$rc"
}

service_exists() {
  local target="$1"
  local s
  for s in "${ALL_SERVICES[@]}"; do
    if [ "$s" = "$target" ]; then
      return 0
    fi
  done
  return 1
}

resolve_infra_env_defaults() {
  case "$NODE_ROLE" in
    infra)
      # infra 机只启动 compose，不依赖这些变量
      ;;
    app)
      [ -n "$INFRA_HOST" ] || die "请先在脚本顶部手动填写 INFRA_HOST，例如：INFRA_HOST=\"10.0.0.12\""
      ;;
    *)
      die "unsupported NODE_ROLE=$NODE_ROLE, expected: infra | app"
      ;;
  esac
}

ensure_runtime_files() {
  ensure_dir "$LOG_ROOT_DIR"
  ensure_dir "$LOG_DIR"

  cat > "$ENTRYPOINT_FILE" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

log() {
  echo "[$(timestamp)] [entrypoint] $*"
}

SERVICE_NAME="${SERVICE_NAME:-unknown}"
API_ENABLED="${API_ENABLED:-0}"
RPC_ENABLED="${RPC_ENABLED:-0}"
API_WORKDIR="${API_WORKDIR:-}"
RPC_WORKDIR="${RPC_WORKDIR:-}"
API_BIN="${API_BIN:-/app/bin/api}"
RPC_BIN="${RPC_BIN:-/app/bin/rpc}"
API_CONFIG="${API_CONFIG:-}"
RPC_CONFIG="${RPC_CONFIG:-}"

ETCD_ADDR="${ETCD_ADDR:-}"
REDIS_ADDR="${REDIS_ADDR:-}"
POSTGRES_ADDR="${POSTGRES_ADDR:-}"
POSTGRES_PORT="${POSTGRES_PORT:-35432}"
KAFKA_ADDR="${KAFKA_ADDR:-}"
MINIO_ADDR="${MINIO_ADDR:-}"
BEANSTALKD1_ADDR="${BEANSTALKD1_ADDR:-}"
BEANSTALKD2_ADDR="${BEANSTALKD2_ADDR:-}"
OTEL_ADDR="${OTEL_ADDR:-}"

mkdir -p /app/log /tmp/service-configs /app/service/like/rpc/data

require_file() {
  local f="$1"
  if [ ! -f "$f" ]; then
    log "missing file: $f"
    exit 1
  fi
}

patch_config() {
  local src="$1"
  local dst="$2"

  require_file "$src"
  cp "$src" "$dst"

sed -i \
  -e "s#175\.24\.130\.226:32379#${ETCD_ADDR}#g" \
  -e "s#host\.docker\.internal:32379#${ETCD_ADDR}#g" \
  -e "s#175\.24\.130\.226:36379#${REDIS_ADDR}#g" \
  -e "s#175\.24\.130\.226:6379#${REDIS_ADDR}#g" \
  -e "s#host\.docker\.internal:36379#${REDIS_ADDR}#g" \
  -e "s#175\.24\.130\.226:39092#${KAFKA_ADDR}#g" \
  -e "s#host\.docker\.internal:39092#${KAFKA_ADDR}#g" \
  -e "s#175\.24\.130\.226:39000#${MINIO_ADDR}#g" \
  -e "s#host\.docker\.internal:39000#${MINIO_ADDR}#g" \
  -e "s#175\.24\.130\.226:41300#${BEANSTALKD1_ADDR}#g" \
  -e "s#host\.docker\.internal:41300#${BEANSTALKD1_ADDR}#g" \
  -e "s#175\.24\.130\.226:41301#${BEANSTALKD2_ADDR}#g" \
  -e "s#host\.docker\.internal:41301#${BEANSTALKD2_ADDR}#g" \
  -e "s#Host: 175\.24\.130\.226:36379#Host: ${REDIS_ADDR}#g" \
  -e "s#Addr: 175\.24\.130\.226:36379#Addr: ${REDIS_ADDR}#g" \
  -e "s#Host: \"175\.24\.130\.226:36379\"#Host: \"${REDIS_ADDR}\"#g" \
  -e "s#Host: \"host\.docker\.internal:36379\"#Host: \"${REDIS_ADDR}\"#g" \
  -e "s#Host: \"http://175\.24\.130\.226\"#Host: \"${POSTGRES_ADDR}\"#g" \
  -e "s#Host: \"https://175\.24\.130\.226\"#Host: \"${POSTGRES_ADDR}\"#g" \
  -e "s#Host: \"http://host\.docker\.internal\"#Host: \"${POSTGRES_ADDR}\"#g" \
  -e "s#Host: \"https://host\.docker\.internal\"#Host: \"${POSTGRES_ADDR}\"#g" \
  -e "s#Host: \"175\.24\.130\.226\"#Host: \"${POSTGRES_ADDR}\"#g" \
  -e "s#Host: \"host\.docker\.internal\"#Host: \"${POSTGRES_ADDR}\"#g" \
  -e "s#Host: http://175\.24\.130\.226#Host: ${POSTGRES_ADDR}#g" \
  -e "s#Host: https://175\.24\.130\.226#Host: ${POSTGRES_ADDR}#g" \
  -e "s#Host: http://host\.docker\.internal#Host: ${POSTGRES_ADDR}#g" \
  -e "s#Host: https://host\.docker\.internal#Host: ${POSTGRES_ADDR}#g" \
  -e "s#Host: 175\.24\.130\.226#Host: ${POSTGRES_ADDR}#g" \
  -e "s#Host: host\.docker\.internal#Host: ${POSTGRES_ADDR}#g" \
  -e "s#Port: \"35432\"#Port: \"${POSTGRES_PORT}\"#g" \
  -e "s#port=35432#port=${POSTGRES_PORT}#g" \
  -e "s#host=http://175\.24\.130\.226 #host=${POSTGRES_ADDR} #g" \
  -e "s#host=https://175\.24\.130\.226 #host=${POSTGRES_ADDR} #g" \
  -e "s#host=http://host\.docker\.internal #host=${POSTGRES_ADDR} #g" \
  -e "s#host=https://host\.docker\.internal #host=${POSTGRES_ADDR} #g" \
  -e "s#host=175\.24\.130\.226 #host=${POSTGRES_ADDR} #g" \
  -e "s#host=host\.docker\.internal #host=${POSTGRES_ADDR} #g" \
  -e "s#@http://175\.24\.130\.226:35432#@${POSTGRES_ADDR}:${POSTGRES_PORT}#g" \
  -e "s#@https://175\.24\.130\.226:35432#@${POSTGRES_ADDR}:${POSTGRES_PORT}#g" \
  -e "s#@http://host\.docker\.internal:35432#@${POSTGRES_ADDR}:${POSTGRES_PORT}#g" \
  -e "s#@https://host\.docker\.internal:35432#@${POSTGRES_ADDR}:${POSTGRES_PORT}#g" \
  -e "s#@175\.24\.130\.226:35432#@${POSTGRES_ADDR}:${POSTGRES_PORT}#g" \
  -e "s#@host\.docker\.internal:35432#@${POSTGRES_ADDR}:${POSTGRES_PORT}#g" \
  -e "s#localhost:34317#${OTEL_ADDR}#g" \
  -e "s#175\.24\.130\.226:34317#${OTEL_ADDR}#g" \
  -e "s#host\.docker\.internal:34317#${OTEL_ADDR}#g" \
  "$dst"
}

show_config_hint() {
  local name="$1"
  local file="$2"
  log "config ready: ${name} => ${file}"
  grep -E '^(Name:|Host:|Port:|ListenOn:|Endpoint:|Path:|MetricsPath:|Key:|Mode:)' "$file" || true
}

api_pid=""
rpc_pid=""

stop_all() {
  set +e
  log "stopping children..."
  if [[ -n "$api_pid" ]] && kill -0 "$api_pid" 2>/dev/null; then
    kill "$api_pid" 2>/dev/null || true
  fi
  if [[ -n "$rpc_pid" ]] && kill -0 "$rpc_pid" 2>/dev/null; then
    kill "$rpc_pid" 2>/dev/null || true
  fi
  wait ${api_pid:-} ${rpc_pid:-} 2>/dev/null || true
}

trap 'log "received TERM/INT"; stop_all; exit 0' TERM INT

log "service=${SERVICE_NAME}"
log "api_enabled=${API_ENABLED} rpc_enabled=${RPC_ENABLED}"
log "API_WORKDIR=${API_WORKDIR}"
log "RPC_WORKDIR=${RPC_WORKDIR}"
log "API_BIN=${API_BIN}"
log "RPC_BIN=${RPC_BIN}"
log "ETCD_ADDR=${ETCD_ADDR}"
log "REDIS_ADDR=${REDIS_ADDR}"
log "POSTGRES_ADDR=${POSTGRES_ADDR}:${POSTGRES_PORT}"
log "KAFKA_ADDR=${KAFKA_ADDR}"
log "MINIO_ADDR=${MINIO_ADDR}"
log "OTEL_ADDR=${OTEL_ADDR}"

if [[ "$RPC_ENABLED" == "1" ]]; then
  rpc_config_tmp="/tmp/service-configs/${SERVICE_NAME}-rpc.yaml"
  log "patch rpc config: ${RPC_WORKDIR}/${RPC_CONFIG}"
  patch_config "$RPC_WORKDIR/$RPC_CONFIG" "$rpc_config_tmp"
  show_config_hint "rpc" "$rpc_config_tmp"
  (
    cd "$RPC_WORKDIR"
    log "starting rpc: ${RPC_BIN} -f ${rpc_config_tmp}"
    exec "$RPC_BIN" -f "$rpc_config_tmp"
  ) &
  rpc_pid="$!"
  log "rpc started pid=${rpc_pid}"
fi

if [[ "$API_ENABLED" == "1" ]]; then
  api_config_tmp="/tmp/service-configs/${SERVICE_NAME}-api.yaml"
  log "patch api config: ${API_WORKDIR}/${API_CONFIG}"
  patch_config "$API_WORKDIR/$API_CONFIG" "$api_config_tmp"
  show_config_hint "api" "$api_config_tmp"
  (
    cd "$API_WORKDIR"
    log "starting api: ${API_BIN} -f ${api_config_tmp}"
    exec "$API_BIN" -f "$api_config_tmp"
  ) &
  api_pid="$!"
  log "api started pid=${api_pid}"
fi

if [[ -z "$api_pid" && -z "$rpc_pid" ]]; then
  log "no process configured for service=${SERVICE_NAME}"
  exit 1
fi

while true; do
  if [[ -n "$rpc_pid" ]] && ! kill -0 "$rpc_pid" 2>/dev/null; then
    log "rpc exited pid=${rpc_pid}"
    wait "$rpc_pid" || true
    stop_all
    exit 1
  fi

  if [[ -n "$api_pid" ]] && ! kill -0 "$api_pid" 2>/dev/null; then
    log "api exited pid=${api_pid}"
    wait "$api_pid" || true
    stop_all
    exit 1
  fi

  sleep 1
done
EOF

  cat > "$DOCKERIGNORE_FILE" <<'EOF'
.git
.idea
.vscode
log
volumes
data
.manage-logs
tmp
dist
bin
*.zip
*.exe
EOF

  chmod +x "$ENTRYPOINT_FILE"
  info "generated runtime helper: $ENTRYPOINT_FILE"
  info "generated dockerignore: $DOCKERIGNORE_FILE"
}

ensure_base_dirs() {
  ensure_dir "$ROOT_DIR/log"
  ensure_dir "$ROOT_DIR/service/like/rpc/data"
  ensure_dir "$LOG_ROOT_DIR"
  ensure_dir "$LOG_DIR"
}

preflight() {
  section "preflight"

  command_exists docker || die "docker command not found"
  info "docker binary: $(command -v docker)"

  if ! docker version >/dev/null 2>&1; then
    die "docker daemon is not available"
  fi

  info "docker is available"
  info "docker context: $(docker context show 2>/dev/null || echo default)"

  if [ -f "$ROOT_DIR/docker-compose.yaml" ]; then
    if docker compose version >/dev/null 2>&1; then
      info "docker compose is available"
    else
      warn "docker-compose.yaml found, but 'docker compose' is unavailable"
    fi
  fi

  resolve_infra_env_defaults

  info "current user: $(whoami)"
  info "root dir: $ROOT_DIR"
  info "dockerfile: $DOCKERFILE"
  info "log dir: $LOG_DIR"
  info "NODE_ROLE=$NODE_ROLE"
  info "INFRA_HOST=${INFRA_HOST:-<empty>}"
  info "ETCD_ADDR=${ETCD_ADDR:-<empty>}"
  info "REDIS_ADDR=${REDIS_ADDR:-<empty>}"
  info "POSTGRES_ADDR=${POSTGRES_ADDR:-<empty>}:${POSTGRES_PORT:-<empty>}"
  info "KAFKA_ADDR=${KAFKA_ADDR:-<empty>}"
  info "MINIO_ADDR=${MINIO_ADDR:-<empty>}"
  info "OTEL_ADDR=${OTEL_ADDR:-<empty>}"
  info "DOCKER_BUILDKIT=$DOCKER_BUILDKIT"
  info "DEBUG_BUILD=$DEBUG_BUILD"

  if [ -f "$HOME/.docker/config.json" ]; then
    info "docker client config exists: $HOME/.docker/config.json"
  else
    warn "docker client config not found: $HOME/.docker/config.json"
  fi
}

show_daemon_proxy() {
  section "docker daemon proxy"
  systemctl show --property=Environment docker 2>/dev/null || true
}

show_docker_info_summary() {
  section "docker info summary"
  docker info 2>/dev/null | sed -n '1,80p' || true
}

start_infra() {
  [ "$NODE_ROLE" = "infra" ] || die "start_infra 只能在 NODE_ROLE=infra 的机器上执行"

  if [ -f "$ROOT_DIR/docker-compose.yaml" ]; then
    info "starting infra services: ${INFRA_SERVICES[*]}"
    local logfile="$LOG_DIR/infra-start.log"
    run_logged "docker compose up infra" "$logfile" \
      bash -lc "cd '$ROOT_DIR' && docker compose up -d ${INFRA_SERVICES[*]}"
  else
    die "docker-compose.yaml not found: $ROOT_DIR/docker-compose.yaml"
  fi
}

stop_infra() {
  [ "$NODE_ROLE" = "infra" ] || die "stop_infra 只能在 NODE_ROLE=infra 的机器上执行"

  if [ -f "$ROOT_DIR/docker-compose.yaml" ]; then
    info "stopping infra services: ${INFRA_SERVICES[*]}"
    local logfile="$LOG_DIR/infra-stop.log"
    run_logged "docker compose stop infra" "$logfile" \
      bash -lc "cd '$ROOT_DIR' && docker compose stop ${INFRA_SERVICES[*]}"
  else
    die "docker-compose.yaml not found: $ROOT_DIR/docker-compose.yaml"
  fi
}

status_infra() {
  [ "$NODE_ROLE" = "infra" ] || die "status_infra 只能在 NODE_ROLE=infra 的机器上执行"

  section "infra status"
  bash -lc "cd '$ROOT_DIR' && docker compose ps ${INFRA_SERVICES[*]}" || true
}

resolve_build_paths() {
  local service="$1"
  case "$service" in
    article)  echo "./service/article/api|./service/article/rpc" ;;
    comment)  echo "./service/comment/api|./service/comment/rpc" ;;
    like)     echo "./service/like/api|./service/like/rpc" ;;
    follow)   echo "./service/follow/api|./service/follow/rpc" ;;
    favorite) echo "./service/favorite/api|./service/favorite/rpc" ;;
    message)  echo "./service/message/api|./service/message/rpc" ;;
    task)     echo "./service/task/api|./service/task/rpc" ;;
    user)     echo "./service/user/user/api|./service/user/user/rpc" ;;
    admin)    echo "./service/user/admin/api|./service/user/admin/rpc" ;;
    hot)      echo "./service/hot/api|./service/hot/rpc" ;;
    points)   echo "|./service/points/rpc" ;;
    security) echo "|./service/security/rpc" ;;
    *) die "unknown service: $service" ;;
  esac
}

build_one() {
  local service="$1"
  local api_build rpc_build map
  local build_log="$LOG_DIR/build-${service}.log"

  map="$(resolve_build_paths "$service")"
  api_build="${map%%|*}"
  rpc_build="${map##*|}"

  section "build service=${service}"
  info "image=${IMAGE_PREFIX}-${service}:latest"
  info "api_build=${api_build:-<none>}"
  info "rpc_build=${rpc_build:-<none>}"
  info "build log => $build_log"

  run_logged "docker build ${service}" "$build_log" \
    env DOCKER_BUILDKIT="$DOCKER_BUILDKIT" docker build \
      --network host \
      --progress="${BUILD_PROGRESS}" \
      -f "$DOCKERFILE" \
      -t "${IMAGE_PREFIX}-${service}:latest" \
      --build-arg HTTP_PROXY="${HTTP_PROXY:-}" \
      --build-arg HTTPS_PROXY="${HTTPS_PROXY:-}" \
      --build-arg NO_PROXY="${NO_PROXY:-}" \
      --build-arg http_proxy="${http_proxy:-${HTTP_PROXY:-}}" \
      --build-arg https_proxy="${https_proxy:-${HTTPS_PROXY:-}}" \
      --build-arg no_proxy="${no_proxy:-${NO_PROXY:-}}" \
      --build-arg API_BUILD_PATH="$api_build" \
      --build-arg RPC_BUILD_PATH="$rpc_build" \
      --build-arg DEBUG_BUILD="$DEBUG_BUILD" \
      "$ROOT_DIR"
}

ensure_image() {
  local service="$1"
  if ! docker image inspect "${IMAGE_PREFIX}-${service}:latest" >/dev/null 2>&1; then
    warn "image not found, auto build: ${service}"
    build_one "$service"
  else
    info "image exists: ${IMAGE_PREFIX}-${service}:latest"
  fi
}

print_container_inspect_summary() {
  local name="$1"
  section "inspect summary: ${name}"
  docker inspect \
    --format 'Name={{.Name}}
Image={{.Config.Image}}
Status={{.State.Status}}
Running={{.State.Running}}
Restarting={{.State.Restarting}}
ExitCode={{.State.ExitCode}}
Error={{.State.Error}}
StartedAt={{.State.StartedAt}}
FinishedAt={{.State.FinishedAt}}
RestartCount={{.RestartCount}}
Health={{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}
IP={{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' \
    "$name" 2>/dev/null || true
}

print_container_logs_tail() {
  local name="$1"
  section "container logs tail: ${name}"
  docker logs --tail "$LOG_TAIL_LINES" "$name" 2>&1 || true
}

wait_container_ready() {
  local name="$1"
  local service="$2"
  local i status running health exit_code restart_count

  section "wait container ready: ${service}"
  for ((i=1; i<=START_WAIT_SECONDS; i++)); do
    if ! docker inspect "$name" >/dev/null 2>&1; then
      error "container not found while waiting: $name"
      return 1
    fi

    status="$(docker inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo unknown)"
    running="$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null || echo false)"
    health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' "$name" 2>/dev/null || echo unknown)"
    exit_code="$(docker inspect -f '{{.State.ExitCode}}' "$name" 2>/dev/null || echo unknown)"
    restart_count="$(docker inspect -f '{{.RestartCount}}' "$name" 2>/dev/null || echo unknown)"

    info "wait[$i/$START_WAIT_SECONDS] service=${service} status=${status} running=${running} health=${health} exit_code=${exit_code} restart_count=${restart_count}"

    if [ "$status" = "exited" ] || [ "$status" = "dead" ]; then
      error "container failed during startup: $name"
      print_container_inspect_summary "$name"
      print_container_logs_tail "$name"
      return 1
    fi

    if [ "$running" = "true" ]; then
      if [ "$health" = "healthy" ] || [ "$health" = "no-healthcheck" ] || [ "$health" = "starting" ]; then
        info "container considered started: $name"
        docker port "$name" || true
        return 0
      fi
    fi

    sleep 1
  done

  warn "startup wait timeout reached: $name"
  print_container_inspect_summary "$name"
  print_container_logs_tail "$name"
  return 0
}

run_container() {
  local service="$1"
  local image="$2"
  local api_enabled="$3"
  local rpc_enabled="$4"
  local api_workdir="$5"
  local rpc_workdir="$6"
  local api_config="$7"
  local rpc_config="$8"
  shift 8

  local -a port_args=("$@")
  local -a volume_args=("-v" "$ROOT_DIR/log:/app/log")
  local run_log="$LOG_DIR/run-${service}.log"
  local name="${IMAGE_PREFIX}-${service}"

  if [ "$service" = "like" ]; then
    volume_args+=("-v" "$ROOT_DIR/service/like/rpc/data:/app/service/like/rpc/data")
  fi

  section "start service=${service}"
  info "image=${image}"
  info "api_enabled=${api_enabled} rpc_enabled=${rpc_enabled}"
  info "api_workdir=${api_workdir:-<none>}"
  info "rpc_workdir=${rpc_workdir:-<none>}"
  info "api_config=${api_config:-<none>}"
  info "rpc_config=${rpc_config:-<none>}"
  info "ports=${port_args[*]:-<none>}"
  info "run log => $run_log"

  if docker ps -a --format '{{.Names}}' | grep -q "^${name}$"; then
    warn "removing old container: ${name}"
    docker rm -f "${name}" >/dev/null 2>&1 || true
  fi

  run_logged "docker run ${service}" "$run_log" \
    docker run -d \
      --name "${name}" \
      --restart always \
      "${port_args[@]}" \
      "${volume_args[@]}" \
      -e SERVICE_NAME="$service" \
      -e API_ENABLED="$api_enabled" \
      -e RPC_ENABLED="$rpc_enabled" \
      -e API_WORKDIR="$api_workdir" \
      -e RPC_WORKDIR="$rpc_workdir" \
      -e API_CONFIG="$api_config" \
      -e RPC_CONFIG="$rpc_config" \
      -e API_BIN="/app/bin/api" \
      -e RPC_BIN="/app/bin/rpc" \
      -e ETCD_ADDR="$ETCD_ADDR" \
      -e REDIS_ADDR="$REDIS_ADDR" \
      -e POSTGRES_ADDR="$POSTGRES_ADDR" \
      -e POSTGRES_PORT="$POSTGRES_PORT" \
      -e KAFKA_ADDR="$KAFKA_ADDR" \
      -e MINIO_ADDR="$MINIO_ADDR" \
      -e BEANSTALKD1_ADDR="$BEANSTALKD1_ADDR" \
      -e BEANSTALKD2_ADDR="$BEANSTALKD2_ADDR" \
      -e OTEL_ADDR="$OTEL_ADDR" \
      "$image"

  wait_container_ready "$name" "$service"
  print_container_inspect_summary "$name"
  print_container_logs_tail "$name"
}

start_one() {
  local service="$1"

  [ "$NODE_ROLE" = "app" ] || die "start_one 只能在 NODE_ROLE=app 的机器上执行"
  ensure_image "$service"

  case "$service" in
    article)
      run_container "article" "${IMAGE_PREFIX}-article:latest" "1" "1" \
        "/app/service/article/api" "/app/service/article/rpc" \
        "etc/article-api.yaml" "etc/article.yaml" \
        -p 18889:8889 -p 19001:9001
      ;;
    comment)
      run_container "comment" "${IMAGE_PREFIX}-comment:latest" "1" "1" \
        "/app/service/comment/api" "/app/service/comment/rpc" \
        "etc/commentservice.yaml" "etc/comment.yaml" \
        -p 18888:8888 -p 19002:9001
      ;;
    like)
      run_container "like" "${IMAGE_PREFIX}-like:latest" "1" "1" \
        "/app/service/like/api" "/app/service/like/rpc" \
        "etc/likecenter.yaml" "etc/like.yaml" \
        -p 18887:8887 -p 18082:8082
      ;;
    follow)
      run_container "follow" "${IMAGE_PREFIX}-follow:latest" "1" "1" \
        "/app/service/follow/api" "/app/service/follow/rpc" \
        "etc/followcenter.yaml" "etc/follow.yaml" \
        -p 18891:8891 -p 18086:8082
      ;;
    favorite)
      run_container "favorite" "${IMAGE_PREFIX}-favorite:latest" "1" "1" \
        "/app/service/favorite/api" "/app/service/favorite/rpc" \
        "etc/favorite.yaml" "etc/favorite.yaml" \
        -p 18890:8890 -p 18088:8082
      ;;
    message)
      run_container "message" "${IMAGE_PREFIX}-message:latest" "1" "1" \
        "/app/service/message/api" "/app/service/message/rpc" \
        "etc/messagecenter.yaml" "etc/message.yaml" \
        -p 18892:8892 -p 18087:8082
      ;;
    task)
      run_container "task" "${IMAGE_PREFIX}-task:latest" "1" "1" \
        "/app/service/task/api" "/app/service/task/rpc" \
        "etc/task.yaml" "etc/task.yaml" \
        -p 18886:8888 -p 19005:9005
      ;;
    user)
      run_container "user" "${IMAGE_PREFIX}-user:latest" "1" "1" \
        "/app/service/user/user/api" "/app/service/user/user/rpc" \
        "etc/usercenter.yaml" "etc/user.yaml" \
        -p 18885:8888 -p 19004:9004
      ;;
    admin)
      run_container "admin" "${IMAGE_PREFIX}-admin:latest" "1" "1" \
        "/app/service/user/admin/api" "/app/service/user/admin/rpc" \
        "etc/admincenter.yaml" "etc/admin.yaml" \
        -p 18884:8889 -p 18081:8081
      ;;
    hot)
      run_container "hot" "${IMAGE_PREFIX}-hot:latest" "1" "1" \
        "/app/service/hot/api" "/app/service/hot/rpc" \
        "etc/hotcenter.yaml" "etc/hot.yaml" \
        -p 18893:8893 -p 18083:8083
      ;;
    points)
      run_container "points" "${IMAGE_PREFIX}-points:latest" "0" "1" \
        "" "/app/service/points/rpc" \
        "" "etc/points.yaml" \
        -p 18084:8082
      ;;
    security)
      run_container "security" "${IMAGE_PREFIX}-security:latest" "0" "1" \
        "" "/app/service/security/rpc" \
        "" "etc/security.yaml" \
        -p 18085:8081
      ;;
    *)
      die "unknown service: $service"
      ;;
  esac
}

stop_one() {
  local service="$1"
  local name="${IMAGE_PREFIX}-${service}"

  section "stop service=${service}"
  docker rm -f "${name}" >/dev/null 2>&1 || true
  info "stopped: ${name}"
}

status_one() {
  local service="$1"
  local name="${IMAGE_PREFIX}-${service}"

  section "status service=${service}"

  if ! docker inspect "$name" >/dev/null 2>&1; then
    warn "container not found: $name"
    return 0
  fi

  docker ps -a --filter "name=^${name}$"
  print_container_inspect_summary "$name"
  section "port mapping: ${name}"
  docker port "$name" || true
  section "recent logs: ${name}"
  docker logs --tail "$STATUS_TAIL_LINES" "$name" 2>&1 || true
}

logs_one() {
  docker logs -f "${IMAGE_PREFIX}-${1}"
}

doctor_one() {
  local service="${1:-all}"

  preflight
  show_daemon_proxy
  show_docker_info_summary

  section "images"
  docker images | grep "${IMAGE_PREFIX}" || true

  section "containers"
  docker ps -a | grep "${IMAGE_PREFIX}" || true

  section "networks"
  docker network ls || true

  if [ "$service" != "all" ] && service_exists "$service"; then
    status_one "$service"
  fi
}

build_target() {
  local target="$1"

  [ "$NODE_ROLE" = "app" ] || die "build 只能在 NODE_ROLE=app 的机器上执行"

  if [ "$target" = "all" ]; then
    local s
    for s in "${ALL_SERVICES[@]}"; do
      build_one "$s"
    done
  else
    service_exists "$target" || die "unknown service: $target"
    build_one "$target"
  fi
}

start_target() {
  local target="$1"

  case "$NODE_ROLE" in
    infra)
      start_infra
      ;;
    app)
      if [ "$target" = "all" ]; then
        local s
        for s in "${ALL_SERVICES[@]}"; do
          start_one "$s"
        done
      else
        service_exists "$target" || die "unknown service: $target"
        start_one "$target"
      fi
      ;;
    *)
      die "unsupported NODE_ROLE=$NODE_ROLE"
      ;;
  esac
}

stop_target() {
  local target="$1"

  case "$NODE_ROLE" in
    infra)
      stop_infra
      ;;
    app)
      if [ "$target" = "all" ]; then
        local s
        for s in "${ALL_SERVICES[@]}"; do
          stop_one "$s"
        done
      else
        service_exists "$target" || die "unknown service: $target"
        stop_one "$target"
      fi
      ;;
    *)
      die "unsupported NODE_ROLE=$NODE_ROLE"
      ;;
  esac
}

restart_target() {
  local target="$1"

  case "$NODE_ROLE" in
    infra)
      stop_infra
      start_infra
      ;;
    app)
      if [ "$target" = "all" ]; then
        local s
        for s in "${ALL_SERVICES[@]}"; do
          stop_one "$s"
        done
        start_target all
      else
        service_exists "$target" || die "unknown service: $target"
        stop_one "$target"
        start_target "$target"
      fi
      ;;
    *)
      die "unsupported NODE_ROLE=$NODE_ROLE"
      ;;
  esac
}

status_target() {
  local target="$1"

  case "$NODE_ROLE" in
    infra)
      status_infra
      ;;
    app)
      if [ "$target" = "all" ]; then
        local s
        for s in "${ALL_SERVICES[@]}"; do
          status_one "$s"
        done
      else
        service_exists "$target" || die "unknown service: $target"
        status_one "$target"
      fi
      ;;
    *)
      die "unsupported NODE_ROLE=$NODE_ROLE"
      ;;
  esac
}

doctor_target() {
  local target="$1"

  case "$NODE_ROLE" in
    infra)
      preflight
      show_daemon_proxy
      show_docker_info_summary
      status_infra
      ;;
    app)
      if [ "$target" = "all" ]; then
        doctor_one all
      else
        service_exists "$target" || die "unknown service: $target"
        doctor_one "$target"
      fi
      ;;
    *)
      die "unsupported NODE_ROLE=$NODE_ROLE"
      ;;
  esac
}

main() {
  local action="${1:-}"
  local target="${2:-all}"

  ensure_runtime_files
  ensure_base_dirs
  chmod +x "$ROOT_DIR/manage.sh" || true

  info "ROOT_DIR=$ROOT_DIR"
  info "DOCKERFILE=$DOCKERFILE"
  info "LOG_DIR=$LOG_DIR"
  info "NODE_ROLE=$NODE_ROLE"
  info "INFRA_HOST=${INFRA_HOST:-<empty>}"
  info "BUILD_PROGRESS=$BUILD_PROGRESS"
  info "START_WAIT_SECONDS=$START_WAIT_SECONDS"
  info "STATUS_TAIL_LINES=$STATUS_TAIL_LINES"
  info "LOG_TAIL_LINES=$LOG_TAIL_LINES"
  info "DEBUG_BUILD=$DEBUG_BUILD"
  info "DOCKER_BUILDKIT=$DOCKER_BUILDKIT"

  case "$action" in
    build)
      preflight
      build_target "$target"
      ;;
    start)
      preflight
      start_target "$target"
      ;;
    stop)
      preflight
      stop_target "$target"
      ;;
    restart)
      preflight
      restart_target "$target"
      ;;
    status)
      preflight
      status_target "$target"
      ;;
    logs)
      preflight
      if [ "$NODE_ROLE" = "infra" ]; then
        bash -lc "cd '$ROOT_DIR' && docker compose logs -f --tail=200 ${INFRA_SERVICES[*]}"
      else
        if [ "$target" = "all" ]; then
          die "logs does not support target=all when NODE_ROLE=app"
        fi
        service_exists "$target" || die "unknown service: $target"
        logs_one "$target"
      fi
      ;;
    doctor)
      doctor_target "$target"
      ;;
    *)
      echo "Usage:"
      echo
      echo "  # infra 机器"
      echo "  NODE_ROLE=infra ./manage.sh start"
      echo "  NODE_ROLE=infra ./manage.sh stop"
      echo "  NODE_ROLE=infra ./manage.sh restart"
      echo "  NODE_ROLE=infra ./manage.sh status"
      echo "  NODE_ROLE=infra ./manage.sh logs"
      echo
      echo "  # app 机器（先在脚本顶部填写 INFRA_HOST）"
      echo "  NODE_ROLE=app ./manage.sh build all|service"
      echo "  NODE_ROLE=app ./manage.sh start all|service"
      echo "  NODE_ROLE=app ./manage.sh stop all|service"
      echo "  NODE_ROLE=app ./manage.sh restart all|service"
      echo "  NODE_ROLE=app ./manage.sh status all|service"
      echo "  NODE_ROLE=app ./manage.sh logs service"
      echo "  NODE_ROLE=app ./manage.sh doctor all|service"
      echo
      echo "Optional env:"
      echo "  BUILD_PROGRESS=plain"
      echo "  START_WAIT_SECONDS=20"
      echo "  STATUS_TAIL_LINES=30"
      echo "  LOG_TAIL_LINES=80"
      echo "  DEBUG_BUILD=0"
      echo "  DOCKER_BUILDKIT=1"
      echo "  ETCD_ADDR=${INFRA_HOST}:32379"
      echo "  REDIS_ADDR=${INFRA_HOST}:36379"
      echo "  POSTGRES_ADDR=${INFRA_HOST}"
      echo "  POSTGRES_PORT=35432"
      echo "  KAFKA_ADDR=${INFRA_HOST}:39092"
      echo "  MINIO_ADDR=${INFRA_HOST}:39000"
      echo "  OTEL_ADDR=${INFRA_HOST}:34317"
      exit 1
      ;;
  esac
}

main "$@"
