#!/bin/sh
# Local Compose fixtures only. Never source production credentials into this flow.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"

for tool in docker go openssl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "make db-setup requires $tool" >&2
    exit 1
  fi
done

compose() {
  docker compose -f "$root/deploy/mariadb/compose.yaml" "$@"
}
admin_sql() {
  compose exec -T mariadb sh -c 'MYSQL_PWD="$MARIADB_ROOT_PASSWORD" exec mariadb --user=root'
}

echo "Preparing the local MariaDB database (localhost:3307/ticketopia)..."
sh scripts/mariadb-certs.sh
compose up -d --wait --wait-timeout 60 mariadb
admin_sql < deploy/mariadb/init.sql

# Explicit values take precedence over inherited settings and godotenv's .env
# defaults. Runtime and migration credentials stay separate and local-only.
export PERSISTENCE_MODE=mariadb DB_HOST=localhost DB_PORT=3307 DB_NAME=ticketopia
export DB_TLS_MODE=verify-full DB_TLS_CA_FILE="$root/.local/mariadb/certs/ca.crt"
export DB_USER=ticketopia_runtime DB_PASSWORD=local-runtime-only
export DB_MIGRATION_USER=ticketopia_migration DB_MIGRATION_PASSWORD=local-migration-only

echo "Applying migrations from the current source..."
go run ./cmd/ticketopia migrate

echo "Applying runtime grants..."
admin_sql < deploy/mariadb/runtime-grants.sql

echo "Checking runtime TLS, reads and required write permissions..."
compose exec -T -e MYSQL_PWD=local-runtime-only mariadb mariadb \
  --host=localhost --protocol=tcp --user=ticketopia_runtime \
  --ssl-ca=/certs/ca.crt --ssl-verify-server-cert \
  --database=ticketopia --batch \
  < deploy/mariadb/runtime-check.sql

echo "Local migrations and runtime permissions are ready."
