#!/bin/sh
# Restores a Ticketopia MariaDB backup into a disposable container and checks
# it is usable. See docs/backups.md. Needs Docker; touches no other database.
#
# usage: verify-restore.sh [BACKUP_FILE]
#   Without a file, the newest regular file in BACKUP_DIR is used.
# Environment:
#   BACKUP_DIR       directory of backups, for example the database's directory
#                    under /data/coolify/backups (used without BACKUP_FILE)
#   MAX_AGE_HOURS    fail when the backup is older (default 36; 0 disables)
#   EXPECT_SCHEMA    required schema version (default: the app's, when run
#                    from a checkout; otherwise any applied version)
#   MIN_ACCOUNTS     fail with fewer accounts (default 1)
#   DATABASE         database name inside the dump (default ticketopia)
#   IMAGE            MariaDB image (default mariadb:12.3.3, as deployed)
#   HEALTHCHECK_URL  optional: pinged on success, and with /fail on failure
#                    (healthchecks.io style), so a missed or failed run alerts
set -eu

image=${IMAGE:-mariadb:12.3.3}
database=${DATABASE:-ticketopia}
max_age=${MAX_AGE_HOURS:-36}
min_accounts=${MIN_ACCOUNTS:-1}
container="ticketopia-restore-check-$$"
password="restore-check-$$-$(date +%s)"
status_file=$(mktemp)

notify() {
	[ -n "${HEALTHCHECK_URL:-}" ] || return 0
	curl -fsS -m 10 --retry 3 --data-raw "$2" "$HEALTHCHECK_URL$1" >/dev/null 2>&1 || true
}
fail() {
	echo "restore check FAILED: $*" >&2
	notify /fail "$*"
	exit 1
}
cleanup() {
	docker rm -f "$container" >/dev/null 2>&1 || true
	rm -f "$status_file"
}
trap cleanup EXIT INT TERM

file=${1:-}
if [ -z "$file" ]; then
	[ -n "${BACKUP_DIR:-}" ] || fail "pass a backup file or set BACKUP_DIR"
	[ -d "$BACKUP_DIR" ] || fail "BACKUP_DIR $BACKUP_DIR is not a directory"
	file=$(find "$BACKUP_DIR" -type f ! -name '.*' -exec ls -t {} + 2>/dev/null | head -n 1)
	[ -n "$file" ] || fail "no backups in $BACKUP_DIR"
fi
[ -f "$file" ] && [ -s "$file" ] || fail "backup $file is missing or empty"

# Fresh: Coolify backs up daily, so an old newest file means backups stopped.
if [ "$max_age" != 0 ]; then
	now=$(date +%s)
	modified=$(stat -c %Y "$file" 2>/dev/null || stat -f %m "$file")
	age=$(( (now - modified) / 3600 ))
	[ "$age" -le "$max_age" ] || fail "newest backup $file is ${age}h old (limit ${max_age}h)"
fi

# Gzip by content, not name: all-database dumps are compressed.
if gzip -t "$file" 2>/dev/null; then
	read_dump() { gzip -dc "$file"; }
else
	head -c 2 "$file" | od -An -c | grep -q '037 213' && fail "backup $file is a damaged gzip file"
	read_dump() { cat "$file"; }
fi

if [ -z "${EXPECT_SCHEMA:-}" ] && [ -f internal/persistence/pool.go ]; then
	EXPECT_SCHEMA=$(sed -n 's/^const SupportedSchemaVersion = \([0-9][0-9]*\)$/\1/p' internal/persistence/pool.go)
fi

echo "restore check: $file ($(wc -c <"$file" | tr -d ' ') bytes) into $image"
docker run -d --name "$container" --network none --tmpfs /var/lib/mysql \
	-e MARIADB_ROOT_PASSWORD="$password" "$image" >/dev/null || fail "could not start $image"
sql() { docker exec -i -e MYSQL_PWD="$password" "$container" mariadb --user=root "$@"; }
tries=0
until docker exec "$container" healthcheck.sh --connect --innodb_initialized >/dev/null 2>&1; do
	tries=$((tries + 1))
	[ "$tries" -lt 90 ] || fail "disposable MariaDB did not start"
	sleep 1
done

# A single-database dump has no CREATE DATABASE/USE; an all-database dump
# switches databases itself.
sql -e "CREATE DATABASE IF NOT EXISTS \`$database\`" || fail "could not create $database"
# sh has no pipefail: record a failed read, which the import's status hides.
{ read_dump || echo "read failed" >"$status_file"; } | sql "$database" || fail "import failed"
[ ! -s "$status_file" ] || fail "could not read the whole backup"

value() { sql -N -B "$database" -e "$1" 2>/dev/null | head -n 1; }
for table in schema_state goose_db_version accounts events saved_events event_reminders discussion_posts; do
	[ "$(value "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='$table'")" = 1 ] ||
		fail "table $table is missing after restore"
done
version=$(value "SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied=1")
[ -n "$version" ] && [ "$version" -gt 0 ] || fail "no applied schema version"
if [ -n "${EXPECT_SCHEMA:-}" ] && [ "$version" != "$EXPECT_SCHEMA" ]; then
	fail "schema version $version, expected $EXPECT_SCHEMA"
fi
# The app needs the single migration-state row, clean.
[ "$(value "SELECT COUNT(*) FROM schema_state WHERE id=1 AND NOT dirty")" = 1 ] ||
	fail "schema_state is missing or dirty (an interrupted migration was backed up)"
accounts=$(value "SELECT COUNT(*) FROM accounts")
[ "$accounts" -ge "$min_accounts" ] || fail "$accounts accounts, expected at least $min_accounts"
check=$(docker exec -e MYSQL_PWD="$password" "$container" mariadb-check --user=root --quick "$database" 2>&1) || fail "table check failed: $check"
echo "$check" | grep -v ' OK$' | grep -q . && fail "table check reported problems: $(echo "$check" | grep -v ' OK$' | head -n 3)"

counts=$(value "SELECT CONCAT('accounts ', (SELECT COUNT(*) FROM accounts), ', events ', (SELECT COUNT(*) FROM events), ', saves ', (SELECT COUNT(*) FROM saved_events), ', reminders ', (SELECT COUNT(*) FROM event_reminders), ', posts ', (SELECT COUNT(*) FROM discussion_posts))")
summary="restore check passed: $(basename "$file"), schema $version, $counts"
echo "$summary"
notify "" "$summary"
