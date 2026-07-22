#!/usr/bin/env bash
set -Eeuo pipefail

export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy

readonly config_file=/etc/new-api-pg-idrive-backup.conf

if [[ ! -r "$config_file" ]]; then
  echo "$(date -Is) backup configuration is missing: $config_file" >&2
  exit 1
fi

# The host-specific file contains only paths and the remote prefix, never credentials.
# Credentials are held by the root-owned MinIO Client configuration.
# shellcheck source=/etc/new-api-pg-idrive-backup.conf
source "$config_file"

: "${APP_DIR:?APP_DIR is required}"
: "${COMPOSE_FILE:?COMPOSE_FILE is required}"
: "${REMOTE_BUCKET_PREFIX:?REMOTE_BUCKET_PREFIX is required}"

readonly lock_file="${LOCK_FILE:-/var/lock/new-api-pg-idrive-backup.lock}"

mc_with_network() {
  if [[ -n "${MC_PROXY_URL:-}" ]]; then
    HTTP_PROXY="$MC_PROXY_URL" \
      HTTPS_PROXY="$MC_PROXY_URL" \
      NO_PROXY=127.0.0.1,localhost \
      mc "$@"
  else
    mc "$@"
  fi
}

exec 9>"$lock_file"
flock -n 9 || {
  echo "$(date -Is) another backup is already running; exiting"
  exit 0
}

readonly timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
readonly host="$(hostname)"
readonly work_dir="$(mktemp -d "/var/tmp/new-api-pg-backup-${timestamp}.XXXXXX")"
readonly archive="/var/tmp/new-api-postgres-${host}-${timestamp}.tar.gz"
readonly archive_name="$(basename "$archive")"
readonly remote_prefix="${REMOTE_BUCKET_PREFIX}/${host}/${timestamp}"

cleanup() {
  rm -rf "$work_dir" "$archive" "${archive}.sha256"
}
trap cleanup EXIT

chmod 700 "$work_dir"
cd "$APP_DIR"

echo "$(date -Is) backup started: host=${host} timestamp=${timestamp}"

docker compose --project-directory "$APP_DIR" -f "$COMPOSE_FILE" ps postgres >/dev/null
docker compose --project-directory "$APP_DIR" -f "$COMPOSE_FILE" exec -T postgres \
  sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -Z9 --no-owner --no-acl' \
  > "$work_dir/database.dump"
docker compose --project-directory "$APP_DIR" -f "$COMPOSE_FILE" exec -T postgres \
  sh -c 'pg_dumpall -U "$POSTGRES_USER" --globals-only' \
  > "$work_dir/globals.sql"

test -s "$work_dir/database.dump"
test -s "$work_dir/globals.sql"
docker compose --project-directory "$APP_DIR" -f "$COMPOSE_FILE" exec -T postgres \
  pg_restore -l < "$work_dir/database.dump" >/dev/null

cat > "$work_dir/README.txt" <<EOF
New API PostgreSQL backup
created_at=${timestamp}
host=${host}
format=database.dump is a pg_dump custom archive; restore with pg_restore
globals=globals.sql from pg_dumpall --globals-only
EOF

(
  cd "$work_dir"
  sha256sum database.dump globals.sql README.txt > SHA256SUMS
)
tar -C "$work_dir" -czf "$archive" database.dump globals.sql README.txt SHA256SUMS
(
  cd "$(dirname "$archive")"
  sha256sum "$archive_name" > "${archive_name}.sha256"
)

mc_with_network cp --quiet "$archive" "${archive}.sha256" "${remote_prefix}/"
mc_with_network stat "${remote_prefix}/${archive_name}" >/dev/null
mc_with_network stat "${remote_prefix}/${archive_name}.sha256" >/dev/null

echo "$(date -Is) backup uploaded: ${remote_prefix}/${archive_name} size=$(stat -c %s "$archive")"
echo "$(date -Is) backup finished"
