#!/usr/bin/env bash
# Starts or recreates the OurTaikoDatabase containers on a Docker host without
# docker compose (sakura runs Docker 20.10). Images are built elsewhere and
# loaded with docker load; nothing here pulls.
#
#   run.sh                  every service, in dependency order
#   run.sh api web          only these
#
# Private configuration (root-only):
#   /etc/ourtaiko-database/postgres.env   POSTGRES_PASSWORD
#   /etc/ourtaiko-database/api.env        see ../api.env.example
# CloudBeaver keeps its workspace in /opt/ourtaiko-database/cloudbeaver.
set -euo pipefail
net=ourtaiko-database
conf=/etc/ourtaiko-database
log=(--log-driver json-file --log-opt max-size=10m --log-opt max-file=3)

docker network inspect "$net" >/dev/null 2>&1 || docker network create "$net" >/dev/null

wait_healthy() {
  for _ in $(seq 1 60); do
    [ "$(docker inspect -f '{{.State.Health.Status}}' "$1")" = healthy ] && return 0
    sleep 2
  done
  echo "$1 did not become healthy" >&2
  docker logs --tail 30 "$1" >&2
  return 1
}

postgres() {
  docker rm -f ourtaiko-database-postgres >/dev/null 2>&1 || true
  docker run -d --name ourtaiko-database-postgres --network "$net" --network-alias postgres \
    --restart unless-stopped "${log[@]}" \
    --env-file "$conf/postgres.env" \
    -e POSTGRES_DB=ourtaiko_database -e POSTGRES_USER=ourtaiko_database \
    -e POSTGRES_INITDB_ARGS='--encoding=UTF8 --locale=C.UTF-8' \
    -v ourtaiko-database-postgres:/var/lib/postgresql \
    --health-cmd 'pg_isready -U ourtaiko_database -d ourtaiko_database' --health-interval 5s --health-retries 20 \
    postgres:18-alpine >/dev/null
  wait_healthy ourtaiko-database-postgres
}

api() {
  docker rm -f ourtaiko-database-api >/dev/null 2>&1 || true
  docker run -d --name ourtaiko-database-api --network "$net" --network-alias api \
    --restart unless-stopped "${log[@]}" \
    --env-file "$conf/api.env" \
    -e LISTEN_ADDR=0.0.0.0:8081 -e STORAGE_DIR=/tmp/staging -e STORAGE_BACKEND=s3 \
    --read-only --tmpfs /tmp:rw,noexec,nosuid,size=1g \
    --cap-drop ALL --security-opt no-new-privileges:true \
    --health-cmd 'curl -fsS http://127.0.0.1:8081/readyz' --health-interval 5s --health-retries 20 --health-start-period 20s \
    ourtaiko-database-api:release >/dev/null
  wait_healthy ourtaiko-database-api
}

web() {
  docker rm -f ourtaiko-database-web >/dev/null 2>&1 || true
  docker run -d --name ourtaiko-database-web --network "$net" \
    --restart unless-stopped "${log[@]}" \
    -p 127.0.0.1:8090:8080 \
    --read-only --tmpfs /tmp:rw,noexec,nosuid,size=64m \
    --security-opt no-new-privileges:true \
    --health-cmd 'wget -qO- http://127.0.0.1:8080/readyz' --health-interval 5s --health-retries 20 \
    ourtaiko-database-web:release >/dev/null
  wait_healthy ourtaiko-database-web
}

# The first visit runs CloudBeaver's setup wizard, which creates the
# administrator. Finish it before the public site is reachable.
cloudbeaver() {
  docker rm -f ourtaiko-database-cloudbeaver >/dev/null 2>&1 || true
  docker run -d --name ourtaiko-database-cloudbeaver --network "$net" \
    --restart unless-stopped "${log[@]}" \
    -p 127.0.0.1:8978:8978 \
    -v /opt/ourtaiko-database/cloudbeaver:/opt/cloudbeaver/workspace \
    -e JAVA_OPTS='-Xms128m -Xmx512m' \
    -e CB_SERVER_NAME='OurTaiko Database' \
    -e CB_SERVER_URL=https://postgre-database.ourtaiko.org \
    -e CLOUDBEAVER_APP_FORWARD_PROXY=true \
    -e CLOUDBEAVER_APP_SUPPORTS_CUSTOM_CONNECTIONS=false \
    -e CLOUDBEAVER_APP_GRANT_CONNECTIONS_ACCESS_TO_ANONYMOUS_TEAM=false \
    -e CLOUDBEAVER_APP_ANONYMOUS_ACCESS_ENABLED=false \
    -e CLOUDBEAVER_CORE_LOCALIZATION=zh \
    ourtaiko-database-cloudbeaver:pinned >/dev/null
}

services=("$@")
[ ${#services[@]} -gt 0 ] || services=(postgres api web cloudbeaver)
for s in "${services[@]}"; do
  case "$s" in
    postgres|api|web|cloudbeaver) "$s"; echo "$s: running" ;;
    *) echo "unknown service $s" >&2; exit 1 ;;
  esac
done
