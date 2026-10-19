#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

load_root_env() {
  if [ -f "$ROOT_DIR/.env" ]; then
    set -a
    # shellcheck disable=SC1091
    . "$ROOT_DIR/.env"
    set +a
  fi
}

load_root_env
PUBLIC_HOST="${PUBLIC_HOST:-141.11.46.61}"
DOCKER_NETWORK="${DOCKER_NETWORK:-sea-ridethewind}"
CONTAINER_PREFIX="${CONTAINER_PREFIX:-Sea}"
POSTGRES_USER="${POSTGRES_USER:-admin}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"
POSTGRES_DB="${POSTGRES_DB:-first_db}"
export PUBLIC_HOST
export DOCKER_NETWORK
export CONTAINER_PREFIX
export POSTGRES_USER
export POSTGRES_PASSWORD
export POSTGRES_DB

INFRA_SERVICES=(
  etcd
  postgres
  redis
  beanstalkd1
  beanstalkd2
  neo4j
  kafka
  minio
  milvus
  postgres-exporter
  redis-exporter
  kafka-exporter
  jaeger
  prometheus
  grafana
  node-exporter
  cadvisor
  filebeat
)

APP_SERVICES=(
  article
  comment
  like
  follow
  favorite
  message
  task
  user
  admin
  hot
  points
  security
)

timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

log() {
  echo "[$(timestamp)] $*"
}

docker_compose() {
  sudo env \
    POSTGRES_USER="$POSTGRES_USER" \
    POSTGRES_PASSWORD="$POSTGRES_PASSWORD" \
    POSTGRES_DB="$POSTGRES_DB" \
    docker compose -f "$ROOT_DIR/docker-compose.yaml" "$@"
}

manage() {
  sudo env \
    HOME=/home/ubuntu \
    PATH=/home/ubuntu/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOPATH=/home/ubuntu/go \
    GOMODCACHE=/home/ubuntu/go/pkg/mod \
    GOCACHE=/home/ubuntu/.cache/go-build \
    GO_BIN=/home/ubuntu/go/bin/go \
    NODE_ROLE=app \
    INFRA_HOST="$PUBLIC_HOST" \
    ETCD_ADDR="${ETCD_ADDR:-etcd:2379}" \
    REDIS_ADDR="${REDIS_ADDR:-redis:6379}" \
    POSTGRES_ADDR="${POSTGRES_ADDR:-postgres}" \
    POSTGRES_PORT="${POSTGRES_PORT:-5432}" \
    KAFKA_ADDR="${KAFKA_ADDR:-kafka:9092}" \
    MINIO_ADDR="${MINIO_ADDR:-minio:9000}" \
    BEANSTALKD1_ADDR="${BEANSTALKD1_ADDR:-beanstalkd1:11300}" \
    BEANSTALKD2_ADDR="${BEANSTALKD2_ADDR:-beanstalkd2:11300}" \
    OTEL_ADDR="${OTEL_ADDR:-jaeger:4317}" \
    DOCKER_NETWORK="$DOCKER_NETWORK" \
    CONTAINER_PREFIX="$CONTAINER_PREFIX" \
    "$ROOT_DIR/manage.sh" "$@"
}

wait_compose_container() {
  local service="$1"
  local wait_seconds="${2:-60}"
  local i status health container_id

  for ((i=1; i<=wait_seconds; i++)); do
    container_id="$(docker_compose ps -q "$service" 2>/dev/null || true)"
    if [[ -z "$container_id" ]]; then
      sleep 1
      continue
    fi

    status="$(sudo docker inspect -f '{{.State.Status}}' "$container_id" 2>/dev/null || true)"
    health="$(sudo docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' "$container_id" 2>/dev/null || true)"

    if [[ "$status" == "running" && ( "$health" == "healthy" || "$health" == "no-healthcheck" || "$health" == "starting" ) ]]; then
      log "container ready: $service status=$status health=$health"
      return 0
    fi

    if [[ "$status" == "exited" || "$status" == "dead" ]]; then
      log "container failed: $service"
      docker_compose logs --tail 120 "$service" || true
      return 1
    fi

    sleep 1
  done

  log "container wait timeout: $service"
  docker_compose ps "$service" || true
  docker_compose logs --tail 120 "$service" || true
  return 1
}

start_infra() {
  local service
  for service in "${INFRA_SERVICES[@]}"; do
    log "compose up: $service"
    docker_compose up -d "$service"
    wait_compose_container "$service"
  done
}

stop_infra() {
  local idx service
  for ((idx=${#INFRA_SERVICES[@]}-1; idx>=0; idx--)); do
    service="${INFRA_SERVICES[$idx]}"
    log "compose stop: $service"
    docker_compose stop "$service" || true
  done
}

build_apps() {
  local service
  for service in "${APP_SERVICES[@]}"; do
    log "build app: $service"
    manage build "$service"
  done
}

start_apps() {
  local service
  for service in "${APP_SERVICES[@]}"; do
    log "start app: $service"
    manage start "$service"
  done
}

stop_apps() {
  local idx service
  for ((idx=${#APP_SERVICES[@]}-1; idx>=0; idx--)); do
    service="${APP_SERVICES[$idx]}"
    log "stop app: $service"
    manage stop "$service" || true
  done
}

status_all() {
  sudo docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
}

case "${1:-}" in
  build)
    build_apps
    ;;
  start)
    start_infra
    start_apps
    ;;
  deploy)
    start_infra
    build_apps
    start_apps
    ;;
  stop)
    stop_apps
    stop_infra
    ;;
  restart)
    stop_apps
    stop_infra
    start_infra
    start_apps
    ;;
  status)
    status_all
    ;;
  *)
    echo "Usage: $0 {build|start|deploy|stop|restart|status}"
    exit 1
    ;;
esac
