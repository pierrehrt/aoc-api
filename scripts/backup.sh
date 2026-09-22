#!/usr/bin/env bash
#
# The scheduled production backup (AOC-030). Runs in the Railway cron service built from
# Dockerfile.backup. Four steps: dump, check the dump, upload, check the upload.
#
# ⭐ THE RULE THIS SCRIPT IS WRITTEN AGAINST: a backup fails QUIETLY, and the day you find out
# is the day you needed it. So every step that could go wrong ends in a non-zero exit and a
# sentence a person can act on at 03:00. There is no path through this file that reports
# success without having looked.
#
# ⚠️ It never prints DATABASE_URL or an R2 secret. Only the HOST is echoed, the way the
# Makefile's require_db does, because the URL carries the password.
#
# Env it requires (all set as Railway variables on the backup service):
#   DATABASE_URL            ${{Postgres.DATABASE_URL}} — a REFERENCE, never a copied string
#   R2_S3_ENDPOINT          https://<account-id>.r2.cloudflarestorage.com
#   R2_BACKUP_BUCKET        the backups bucket (NOT the tooltip bucket)
#   R2_ACCESS_KEY_ID        write-scoped token for that bucket
#   R2_SECRET_ACCESS_KEY
# Optional:
#   BACKUP_PREFIX           default "prod/"
#   MIN_DUMP_BYTES          default 1024 — a floor, not a size check; see below

set -euo pipefail

# ⚠️ pipefail is the point. Without it `pg_dump ... | something` reports the exit code of the
# LAST command, so a pg_dump that died mid-stream reads as a success and uploads a truncated
# object. That is AOC-029's defect in a different file.

BACKUP_PREFIX="${BACKUP_PREFIX:-prod/}"
MIN_DUMP_BYTES="${MIN_DUMP_BYTES:-1024}"

die() { echo "BACKUP FAILED: $*" >&2; exit 1; }

require() {
  local name="$1"
  [ -n "${!name:-}" ] || die "$name is not set. Refusing to guess."
}

for v in DATABASE_URL R2_S3_ENDPOINT R2_BACKUP_BUCKET R2_ACCESS_KEY_ID R2_SECRET_ACCESS_KEY; do
  require "$v"
done

# Host only — never the URL, which carries the password.
DB_HOST="$(printf '%s' "$DATABASE_URL" | sed -E 's#^[^@]*@##; s#[/?].*$##')"
echo "▶ backup starting"
echo "  database : $DB_HOST"
echo "  bucket   : ${R2_BACKUP_BUCKET}/${BACKUP_PREFIX}"

# ⚠️ Guard against the tooltip bucket. Opposite retention policy: tooltips are kept forever and
# are read by the site; dumps are private and expired. Writing dumps into the tooltip bucket
# would put them under a lifecycle rule meant for something else, and put 4,645 images under
# one meant for dumps.
if [ "${R2_BACKUP_BUCKET}" = "aoc-codex-enam" ]; then
  die "R2_BACKUP_BUCKET is the TOOLTIP bucket (aoc-codex-enam). The backups bucket is a different one (AOC-030)."
fi

TS="$(date -u +%Y%m%dT%H%M%SZ)"
NAME="aoc-prod-${TS}.dump"
WORKDIR="$(mktemp -d)"
DUMP="${WORKDIR}/${NAME}"
trap 'rm -rf "$WORKDIR"' EXIT

# ---- 1. dump ----------------------------------------------------------------
# -Fc: the custom format docs/runbook-restore.md already restores from and `make db-restore`
# already handles. Changing it here means changing the runbook in the same commit.
echo "▶ 1/4 pg_dump -Fc"
pg_dump "$DATABASE_URL" \
  --format=custom \
  --no-owner \
  --no-acl \
  --file="$DUMP" \
  || die "pg_dump exited non-zero. NOTHING was uploaded. Check the client/server versions first: this image pins $(pg_dump --version | awk '{print $3}'), production must not be newer."

# ---- 2. check the dump BEFORE uploading -------------------------------------
# Upload only what has been looked at. A truncated or empty object in the bucket is worse than
# no object, because the freshness check would see it and go green.
echo "▶ 2/4 verifying the dump locally"

[ -f "$DUMP" ] || die "pg_dump exited 0 but produced no file at $DUMP."

SIZE="$(wc -c < "$DUMP" | tr -d ' ')"
[ "$SIZE" -ge "$MIN_DUMP_BYTES" ] \
  || die "the dump is ${SIZE} bytes, below the ${MIN_DUMP_BYTES}-byte floor. That is not a database."

# ⭐ The check that actually matters. A dump that cannot be listed is not a backup — the
# runbook's own rule. This also catches a well-formed dump of NOTHING: a database with no
# tables produces a valid file with an empty TOC, and two zeroes are not a match (AOC-006).
TOC_ENTRIES="$(pg_restore -l "$DUMP" | grep -cvE '^;|^$' || true)"
[ "$TOC_ENTRIES" -gt 0 ] \
  || die "the dump has an EMPTY table of contents — it restores to nothing. Either the database is empty or pg_dump wrote a stub. Not uploading it."

echo "  ${SIZE} bytes, ${TOC_ENTRIES} TOC entries"

# ---- 3. upload --------------------------------------------------------------
# rclone is configured entirely from env, so no config file holds a credential and nothing is
# written to disk. The variable names stay ours (R2_*); these are rclone's internal spellings.
export RCLONE_CONFIG_R2_TYPE=s3
export RCLONE_CONFIG_R2_PROVIDER=Cloudflare
export RCLONE_CONFIG_R2_ENDPOINT="$R2_S3_ENDPOINT"
export RCLONE_CONFIG_R2_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID"
export RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY"

# ⚠️ REQUIRED, and it is not an optimisation. A bucket-scoped token cannot CreateBucket, and
# rclone tries to ensure the bucket exists before its first PUT — so without this the upload
# fails at CreateBucket with 403 and never reaches PutObject. Measured in AOC-007 and written
# into docs/architecture.md § Object storage.
export RCLONE_S3_NO_CHECK_BUCKET=true

REMOTE="r2:${R2_BACKUP_BUCKET}/${BACKUP_PREFIX}${NAME}"
echo "▶ 3/4 uploading to ${BACKUP_PREFIX}${NAME}"
rclone copyto "$DUMP" "$REMOTE" --stats-one-line \
  || die "the upload failed. The dump was good; the bucket did not get it. Yesterday's object is untouched."

# ---- 4. check the upload ----------------------------------------------------
# A 200 is not evidence the bytes arrived. Ask the bucket what it actually holds.
echo "▶ 4/4 verifying the object in the bucket"

# `lsf --format s` prints just the byte count, so this needs no JSON parser — and therefore no
# Python in an image that otherwise carries only Postgres and rclone.
REMOTE_SIZE="$(rclone lsf --format s "$REMOTE" 2>/dev/null | head -1 | tr -d ' \r' || true)"

[ -n "$REMOTE_SIZE" ] \
  || die "uploaded, but the object cannot be read back from the bucket. Treat this run as failed."

case "$REMOTE_SIZE" in
  ''|*[!0-9]*) die "the bucket reported a non-numeric size (${REMOTE_SIZE}) for the object. Treat this run as failed." ;;
esac

[ "$REMOTE_SIZE" = "$SIZE" ] \
  || die "size mismatch: local ${SIZE} bytes, bucket ${REMOTE_SIZE} bytes. The object is truncated."

echo "✅ backup complete: ${BACKUP_PREFIX}${NAME} (${SIZE} bytes, ${TOC_ENTRIES} TOC entries)"
