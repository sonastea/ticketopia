# Backups and restore checks

Production MariaDB is backed up daily by Coolify to Cloudflare R2, and a daily
restore check on the server proves the newest backup can be restored. Only
MariaDB needs backups: Redis holds expiring cache entries the app rebuilds.

A backup is not proven until it restores. The check restores the newest backup
into a disposable MariaDB, verifies it, and alerts when it fails or stops
running.

## 1. Off-site storage: Cloudflare R2

1. In Cloudflare, create a private R2 bucket (for example
   `ticketopia-backups`). Leave public access off: dumps hold account emails,
   saved events, posts and token hashes.
2. Create an R2 API token with **Object Read & Write** limited to that bucket,
   and note the access key, secret and the S3 endpoint
   `https://<account-id>.r2.cloudflarestorage.com`.
3. In Coolify, add an S3 storage with that endpoint, bucket, region `auto`, key
   and secret, and validate it.

## 2. Scheduled backups in Coolify

On the MariaDB resource, open **Backups** and add a schedule:

- Frequency `0 3 * * *` (daily at 3:00 in the server's timezone).
- **Specific databases**: `ticketopia`.
- **S3 storage**: enable it with the R2 storage.
- **Retention**: keep 14 backups locally and 14 in S3.

Select **Back up now**, then confirm the execution shows **Success**, a non-zero
size and a successful S3 upload. Coolify notifies on the first failure and then
daily while backups keep failing. See
[Coolify's backup guide](https://coolify.io/docs/databases/backups).

## 3. Daily restore check

`scripts/backup/verify-restore.sh` restores one backup into a throwaway
`mariadb:12.3.3` container with no network and checks that:

- the file is recent (`MAX_AGE_HOURS`, default 36), non-empty and, when
  compressed, a valid gzip file;
- it imports, with the core tables present (`accounts`, `events`,
  `saved_events`, `event_reminders`, `discussion_posts` and migration state);
- the schema version is applied (and equals `EXPECT_SCHEMA` when set, or the
  app's version when run from a checkout), and the migration state is present
  and clean;
- there are at least `MIN_ACCOUNTS` accounts (default 1), and
  `mariadb-check` reports every table OK.

It then removes the container, prints the version and row counts, and reports
the outcome to an [Uptime Kuma](https://github.com/louislam/uptime-kuma) Push
monitor (`KUMA_PUSH_URL`) or a healthchecks.io-style URL (`HEALTHCHECK_URL`).
It needs only `sh`, Docker and, for alerts, `curl`; it touches no other database.

On the Coolify server:

1. Copy the script, for example to `/opt/ticketopia/verify-restore.sh`.
2. Find the MariaDB backup directory under `/data/coolify/backups` (the
   **Executions** page shows the file) and run the script once by hand:

   ```sh
   BACKUP_DIR=/data/coolify/backups/<…>/<database> sh /opt/ticketopia/verify-restore.sh
   ```

3. In Uptime Kuma, add a monitor of type **Push** (for example "Ticketopia
   restore check") with a **Heartbeat Interval** of `90000` seconds (25 hours,
   so a slightly late daily run is not flagged) and **Retries** `0`, attach your
   notification, save, and copy its **Push URL**. The server must be able to
   reach Kuma's `/api/push/…` path. Each run reports `status=up` with the
   summary or `status=down` with the reason; a run that never happens turns the
   monitor down after the interval. Kuma's example parameters in the copied URL
   are replaced.
4. Add a root crontab entry an hour after the backup:

   ```cron
   0 4 * * * BACKUP_DIR=/data/coolify/backups/<…>/<database> KUMA_PUSH_URL='https://<kuma>/api/push/<token>' /bin/sh /opt/ticketopia/verify-restore.sh >>/var/log/ticketopia-restore-check.log 2>&1
   ```

   Alternatively, set `HEALTHCHECK_URL` to a [healthchecks.io](https://healthchecks.io)
   check (1-day period): it is pinged on success and with `/fail` on failure.

Leave `EXPECT_SCHEMA` unset on the server so deploys that migrate the schema do
not fail the check; it still requires an applied, clean schema.

Now and then, check the off-site copy itself: download the newest object from
the R2 bucket (in the Cloudflare dashboard, or with any S3 client using the R2
endpoint), not from Coolify, which serves its local copy while one exists. Then
run `make backup-verify file=<downloaded file>` on any machine with Docker.

## Restoring for real

1. Take a fresh backup (**Back up now**) so the current state is kept.
2. Stop the Ticketopia application so nothing writes during the restore.
3. Empty the database first, so tables from a newer schema cannot linger and
   break later migrations (grants on `ticketopia.*` are kept), then import. The
   password is read inside the container, so nothing prompts mid-pipe:

   ```sh
   docker exec <mariadb-container> sh -c 'MYSQL_PWD="$MARIADB_ROOT_PASSWORD" exec mariadb --user=root -e "DROP DATABASE ticketopia; CREATE DATABASE ticketopia"'
   gzip -dc <backup>.gz | docker exec -i <mariadb-container> sh -c 'MYSQL_PWD="$MARIADB_ROOT_PASSWORD" exec mariadb --user=root ticketopia'
   ```

   For an uncompressed dump, use `docker exec -i … < <backup>` instead of
   `gzip -dc`. Coolify's [database restore](https://coolify.io/docs/databases/restore)
   is an alternative; empty the database first there too.
4. If the backup predates the deployed image's schema, run the image's
   `migrate` subcommand with the migration credentials, as in
   [deployment](deployment.md#environment-template): the app only starts on its
   exact schema version (`SupportedSchemaVersion`). A single-database dump holds
   no MariaDB users or grants, so on a new server provision the runtime and
   migration users and grants first.
5. Start the application and check `/readyz`.
