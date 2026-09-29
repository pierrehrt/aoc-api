#!/bin/bash
# The pre-deploy step of a release that carries a migration (docs/architecture.md § Migrations,
# runbook-restore.md §§ 1–3), as ONE command Pierre runs:
#
#   check    open the tunnel, show goose's status against production, close it — writes nothing
#   apply    open the tunnel, take a verified pg_dump to ~/AoC-backups, run `goose up`, show the
#            status, close the tunnel
#
# Why a script: the runbook's steps are right, but four commands with a password in the middle are
# four chances to run them out of order (AOC-041's lesson). The password is read from Railway with
# the project token and never printed. The tunnel is closed on every exit path.
#
# ⚠️ Read the port, not the reassurance: the Makefile prints "(local)" for 127.0.0.1:15432, and
# that IS production behind the tunnel (runbook § 1).
set -euo pipefail

KEY=~/.ssh/railway_aoc_ed25519
PG_INSTANCE=5a0e6a81-37de-4f92-9cc6-3c7b45cf7005   # the Postgres service's INSTANCE id (runbook § 1)
PORT=15432
REPO="$(cd "$(dirname "$0")/.." && pwd)"
MODE=${1:-}
[ "$MODE" = check ] || [ "$MODE" = apply ] || { sed -n '2,15p' "$0"; exit 2; }

set -a; . ~/.config/aoc-codex/railway.env; set +a

close_tunnel() { pkill -f "$PORT:127.0.0.1:5432" 2>/dev/null || true; }
trap close_tunnel EXIT

echo "▶ 1/4 password from Railway (not printed)"
PGPASSWORD="$(railway variables -s Postgres -e production --json 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin)["PGPASSWORD"])')"
[ -n "$PGPASSWORD" ] || { echo "   ❌ could not read PGPASSWORD from the Postgres service"; exit 1; }
export PGPASSWORD

echo "▶ 2/4 tunnel"
close_tunnel
ssh -i "$KEY" -f -N -L "$PORT:127.0.0.1:5432" "$PG_INSTANCE@ssh.railway.com"
for i in $(seq 1 20); do nc -z 127.0.0.1 "$PORT" 2>/dev/null && break; sleep 0.5; done
nc -z 127.0.0.1 "$PORT" || { echo "   ❌ the tunnel did not come up on $PORT"; exit 1; }
export DATABASE_URL="postgres://postgres:${PGPASSWORD}@127.0.0.1:${PORT}/railway?sslmode=disable"
echo "   ✅ up — this is PRODUCTION on port $PORT"

echo "▶ 3/4 goose status BEFORE"
( cd "$REPO" && make -s migrate-status 2>&1 | grep -vi 'reassur\|(local)' )

if [ "$MODE" = check ]; then echo "check only — nothing written"; exit 0; fi

echo "▶ 4/4 backup, then migrate"
mkdir -p ~/AoC-backups && chmod 700 ~/AoC-backups
TS=$(date -u +%Y%m%dT%H%M%SZ)
DUMP=~/AoC-backups/aoc-prod-$TS.dump
pg_dump -h 127.0.0.1 -p "$PORT" -U postgres -d railway -Fc --no-owner --no-acl -f "$DUMP"
# A dump that cannot be listed is not a backup (AOC-006); a size alone is not evidence.
TABLES=$(pg_restore -l "$DUMP" | grep -c 'TABLE DATA' || true)
SIZE=$(stat -f %z "$DUMP")
[ "$SIZE" -gt 100000 ] && [ "$TABLES" -gt 20 ] || { echo "   ❌ backup looks wrong: $SIZE bytes, $TABLES tables — NOT migrating"; exit 1; }
echo "   ✅ backup $DUMP — $SIZE bytes, $TABLES tables with data"
( cd "$REPO" && make -s migrate-up 2>&1 | grep -vi 'reassur\|(local)' )
echo "▶ goose status AFTER"
( cd "$REPO" && make -s migrate-status 2>&1 | grep -vi 'reassur\|(local)' )
echo "done — the tunnel closes now. Paste this whole output to Claude."
