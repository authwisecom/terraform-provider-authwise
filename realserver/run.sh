#!/usr/bin/env bash
# The real-server stack without docker (tf#86), for the CI job: the
# runner's job pod has no docker daemon, so the released images' binaries
# are extracted with crane and run here, beside postgres and valkey, which
# the job runs as services on localhost. Same configs as docker-compose.yaml.
#
#   run.sh up     extract, start everything, wait until it answers
#   run.sh down   stop what up started
#
# Needs REALSERVER_REGISTRY, and REALSERVER_REGISTRY_USER and
# REALSERVER_REGISTRY_PASSWORD to log in to it. Logs go to .data/logs and
# are never printed whole: the job's log is public.
set -euo pipefail

export REALSERVER_DIR="$(cd "$(dirname "$0")" && pwd)"
export REALSERVER_DATA="$REALSERVER_DIR/.data"
export REALSERVER_CERTS="$REALSERVER_DATA/certs"
bin="$REALSERVER_DATA/bin"
logs="$REALSERVER_DATA/logs"
# shellcheck source=versions.env
set -a; . "$REALSERVER_DIR/versions.env"; set +a

crane() { go run github.com/google/go-containerregistry/cmd/crane@v0.20.6 "$@"; }

extract() { # image binary
  crane export --platform linux/amd64 "$REALSERVER_REGISTRY/authwise/$1" - | tar -x -C "$bin" --strip-components=2 "usr/bin/$2"
}

start() { # name, then the command
  local name=$1; shift
  "$@" >"$logs/$name.log" 2>&1 &
  echo $! >"$REALSERVER_DATA/$name.pid"
}

once() { # name, then the command: a one-shot that must succeed
  local name=$1; shift
  if ! "$@" >"$logs/$name.log" 2>&1; then
    echo "realserver: $name failed; its last lines:" >&2
    tail -n 15 "$logs/$name.log" | grep -v -i -E 'secret|password|token' >&2 || true
    exit 1
  fi
}

wait_port() { # name port
  for _ in $(seq 1 60); do
    if (exec 3<>"/dev/tcp/127.0.0.1/$2") 2>/dev/null; then return 0; fi
    sleep 1
  done
  echo "realserver: $1 is not listening on $2; its last lines:" >&2
  tail -n 15 "$logs/$1.log" | grep -v -i -E 'secret|password|token' >&2 || true
  exit 1
}

up() {
  : "${REALSERVER_REGISTRY:?set REALSERVER_REGISTRY}"
  mkdir -p "$bin" "$logs"
  printf '%s' "${REALSERVER_REGISTRY_PASSWORD:?set REALSERVER_REGISTRY_PASSWORD}" |
    crane auth login "$REALSERVER_REGISTRY" -u "${REALSERVER_REGISTRY_USER:?set REALSERVER_REGISTRY_USER}" --password-stdin >/dev/null 2>&1

  extract "kit/profiles/workplace-engine:$KIT_VERSION" engine
  extract "kit/profiles/workplace-api:$KIT_VERSION" api
  extract "kit/profiles/workplace-tenancyadmin:$KIT_VERSION" tenancyadmin
  extract "guard-control:$GUARD_CONTROL_VERSION" guard-control
  extract "platform-stub:$PLATFORM_STUB_VERSION" platform-stub
  extract "id:$ID_VERSION" id
  "$REALSERVER_DIR/certs.sh"

  # postgres and valkey are the job's services; they start with it.
  wait_port postgres 5432
  wait_port valkey 6379
  sleep 2

  export DB_HOSTS=localhost DB_PORT=5432 DB_USERNAME=kit DB_PASSWORD=kit DB_NAME=authwise_id DB_SSL_MODE=disable
  once id-setup env DB_OWNER_HOSTS=localhost DB_OWNER_PORT=5432 DB_OWNER_USERNAME=kit DB_OWNER_PASSWORD=kit DB_OWNER_SSL_MODE=disable \
    "$bin/id" setup --config "$REALSERVER_DIR/id.yaml"
  start id env SPACE_ID=tf-realserver \
    GRPC_SERVER_TLS_CERT_PATH="$REALSERVER_CERTS/server.pem" GRPC_SERVER_TLS_CA_PATH="$REALSERVER_CERTS/ca.pem" GRPC_SERVER_TLS_KEY_PATH="$REALSERVER_CERTS/server.key" \
    "$bin/id" run --config "$REALSERVER_DIR/id.yaml"
  start platform-stub env PLATFORM_STUB_REDIS_ADDR=localhost:6379 PLATFORM_SERVER_HOST=0.0.0.0 PLATFORM_SERVER_PORT=9989 \
    PLATFORM_SERVER_TLS_CERT_PATH="$REALSERVER_CERTS/server.pem" PLATFORM_SERVER_TLS_CA_PATH="$REALSERVER_CERTS/ca.pem" PLATFORM_SERVER_TLS_KEY_PATH="$REALSERVER_CERTS/server.key" \
    "$bin/platform-stub"
  wait_port platform-stub 9989

  once kit-migrate "$bin/engine" migrate --config "$REALSERVER_DIR/kit/migrate.yaml"
  start kit-engine "$bin/engine" run --config "$REALSERVER_DIR/kit/engine.yaml"
  start kit-api "$bin/api" run --config "$REALSERVER_DIR/kit/api.yaml"
  start kit-tenancyadmin "$bin/tenancyadmin" run --config "$REALSERVER_DIR/kit/tenancyadmin.yaml"
  once kit-mint "$bin/engine" admin client-credentials --config "$REALSERVER_DIR/kit/migrate.yaml" \
    --client-id c-90 --output json --out "$REALSERVER_DATA/client.json" --rotate --no-event
  wait_port kit-engine 28443
  wait_port kit-api 28444
  wait_port kit-tenancyadmin 9443
  wait_port id 9988

  once guard-control-migrate "$bin/guard-control" migrate --config "$REALSERVER_DIR/guard-control.yaml"
  start guard-control "$bin/guard-control" run --config "$REALSERVER_DIR/guard-control.yaml"
  wait_port guard-control 28445
  echo "realserver: up (kit $KIT_VERSION, guard-control $GUARD_CONTROL_VERSION)"
}

down() {
  for f in "$REALSERVER_DATA"/*.pid; do
    [ -e "$f" ] && kill "$(cat "$f")" 2>/dev/null || true
    rm -f "$f"
  done
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  *) echo "usage: $0 up|down" >&2; exit 2 ;;
esac
