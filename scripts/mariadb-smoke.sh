#!/bin/sh
# Requires the disposable Compose database, generated certs, and built image.
# Temporarily PAUSES that local database. Never use against a shared deployment.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
image=${TICKETOPIA_TEST_IMAGE:-ticketopia:local}
app="ticketopia-db-smoke-$$"
database=$(docker compose -f "$root/deploy/mariadb/compose.yaml" ps -q mariadb)
test -n "$database"
started=false
paused=false
cleanup() {
  if [ "$paused" = true ]; then docker unpause "$database" >/dev/null; fi
  if [ "$started" = true ]; then docker rm -f "$app" >/dev/null; fi
}
trap cleanup EXIT HUP INT TERM

docker run --rm --network ticketopia-db_default \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  -v "$root/.local/mariadb/certs/ca.crt:/etc/db/ca.crt:ro" \
  -e PERSISTENCE_MODE=mariadb -e DB_HOST=mariadb -e DB_NAME=ticketopia \
  -e DB_TLS_CA_FILE=/etc/db/ca.crt \
  -e DB_MIGRATION_USER=ticketopia_migration -e DB_MIGRATION_PASSWORD=local-migration-only \
  "$image" migrate
docker compose -f "$root/deploy/mariadb/compose.yaml" exec -T \
  -e MYSQL_PWD=local-root-only mariadb mariadb -uroot < "$root/deploy/mariadb/runtime-grants.sql"

# A bad runtime password must fail startup, not start discovery in memory, and
# neither the password nor a DSN may appear in the resulting application log.
if failure=$(docker run --rm --network ticketopia-db_default \
  -v "$root/.local/mariadb/certs/ca.crt:/etc/db/ca.crt:ro" \
  -e PERSISTENCE_MODE=mariadb -e DB_HOST=mariadb -e DB_NAME=ticketopia \
  -e DB_TLS_CA_FILE=/etc/db/ca.crt -e DB_USER=ticketopia_runtime \
  -e DB_PASSWORD=canary-database-password -e DB_STARTUP_TIMEOUT=500ms "$image" 2>&1); then
  echo "bad credentials did not fail startup" >&2
  exit 1
fi
echo "$failure" | grep -q 'Persistence initialization failed'
if echo "$failure" | grep -E 'canary-database-password|@tcp\('; then
  echo "startup error leaked credentials" >&2
  exit 1
fi

docker run -d --name "$app" --network ticketopia-db_default \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  -v "$root/.local/mariadb/certs/ca.crt:/etc/db/ca.crt:ro" \
  -e PERSISTENCE_MODE=mariadb -e DB_HOST=mariadb -e DB_NAME=ticketopia \
  -e DB_TLS_CA_FILE=/etc/db/ca.crt -e DB_USER=ticketopia_runtime -e DB_PASSWORD=local-runtime-only \
  -e IP_GEOLOCATION_ENABLED=false "$image" >/dev/null
started=true
test "$(docker inspect -f '{{.Config.User}}' "$app")" = 65532:65532
ready() {
  attempt=0
  until docker exec "$app" /healthcheck -url http://127.0.0.1:8080/readyz >/dev/null 2>&1; do
    attempt=$((attempt+1))
    if [ "$attempt" -ge 30 ]; then docker logs "$app"; return 1; fi
    sleep 0.1
  done
}
ready
docker exec "$app" /healthcheck
docker exec "$app" /healthcheck -url http://127.0.0.1:8080/
docker exec "$app" /healthcheck -url http://127.0.0.1:8080/assets/app.css
docker pause "$database" >/dev/null
paused=true
if docker exec "$app" /healthcheck -url http://127.0.0.1:8080/readyz; then
  echo "database outage did not gate readiness" >&2
  exit 1
fi
docker exec "$app" /healthcheck
docker unpause "$database" >/dev/null
paused=false
ready
docker stop --time 10 "$app" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$app")" = 0
# Same app configuration, new process/pool/cache, same durable schema.
docker start "$app" >/dev/null
ready
docker stop --time 10 "$app" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$app")" = 0
if docker logs "$app" 2>&1 | grep -E 'local-runtime-only|local-migration-only|@tcp\('; then
  echo "credential redaction failed" >&2
  exit 1
fi
echo "MariaDB migration/non-root/readiness/restart/shutdown smoke checks passed"
