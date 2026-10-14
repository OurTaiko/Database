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
#   /etc/ourtaiko-database/postgres-tls/  server.crt, server.key, pg_hba.conf (owner uid 70)
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

# Published on 5432 for remote database tools. pg_hba.conf (see
# pg_hba.conf.example) admits outside connections only over TLS, only for the
# viewer and editor roles and only from listed addresses (the cloud security
# group must allow them too); the owner role works
# inside the Docker network alone. The TLS key pair is self-signed.
postgres() {
  docker rm -f ourtaiko-database-postgres >/dev/null 2>&1 || true
  docker run -d --name ourtaiko-database-postgres --network "$net" --network-alias postgres \
    --restart unless-stopped "${log[@]}" \
    --env-file "$conf/postgres.env" \
    -e POSTGRES_DB=ourtaiko_database -e POSTGRES_USER=ourtaiko_database \
    -e POSTGRES_INITDB_ARGS='--encoding=UTF8 --locale=C.UTF-8' \
    -v ourtaiko-database-postgres:/var/lib/postgresql \
    -v "$conf/postgres-tls:/etc/postgresql/tls:ro" \
    -p 5432:5432 \
    --health-cmd 'pg_isready -U ourtaiko_database -d ourtaiko_database' --health-interval 5s --health-retries 20 \
    postgres:18-alpine \
    -c ssl=on -c ssl_cert_file=/etc/postgresql/tls/server.crt -c ssl_key_file=/etc/postgresql/tls/server.key \
    -c hba_file=/etc/postgresql/tls/pg_hba.conf -c password_encryption=scram-sha-256 >/dev/null
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

services=("$@")
[ ${#services[@]} -gt 0 ] || services=(postgres api web)
for s in "${services[@]}"; do
  case "$s" in
    postgres|api|web) "$s"; echo "$s: running" ;;
    *) echo "unknown service $s" >&2; exit 1 ;;
  esac
done
