# Runbook — back up and restore the production database

**Written while doing it, 2026-09-17 (AOC-006). Every command below was run.**

Read this at 2 a.m. with no context. It assumes nothing except a Mac with Homebrew.

> ⚠️ **THIS DATABASE HAS NO OTHER BACKUP.** Railway's scheduled backups are a **Pro-plan**
> feature and this project is on Hobby. The dumps this runbook produces are the **only** copies
> that exist. It will also hold the only structured copy of the AoC>TV dataset that will ever
> exist (the source host lapses ~February 2027).

---

## 0. What you need once

```bash
brew install libpq          # Postgres client 18.6 — MUST be >= the server
brew install colima docker docker-compose   # only needed for a restore
```

⚠️ **`libpq` is keg-only: its binaries are NOT on `PATH`.** Every command below starts by fixing
that. This matters more than it looks — a `pg_dump` *older* than the server refuses to run at
all, and before this was pinned the machine simply happened to have a new enough one.

```bash
export PATH="/opt/homebrew/opt/libpq/bin:$PATH"
pg_dump --version    # must print 18.x — production is PostgreSQL 18.6
```

**SSH key** — already registered (`~/.ssh/railway_aoc_ed25519`). To make a new one:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/railway_aoc_ed25519 -N "" -C "aoc-codex"
cat ~/.ssh/railway_aoc_ed25519.pub     # paste into Railway → Account Settings → SSH Keys
```

---

## 1. Open the tunnel to production

Railway's Postgres has **no public endpoint on purpose**. Reach it over SSH instead.

```bash
ssh -i ~/.ssh/railway_aoc_ed25519 -f -N -L 15432:127.0.0.1:5432 \
    5a0e6a81-37de-4f92-9cc6-3c7b45cf7005@ssh.railway.com
```

⚠️ **That username is the Postgres service's INSTANCE ID, and nothing else works.**
Measured, all refused: the service ID, the deployment ID, and the `api` service's domain.
If it ever changes: Railway dashboard → Postgres service → `CMD+K` → **Copy Service Instance ID**
(*not* "Copy Service ID", a different value).

⚠️ **Forward to `127.0.0.1:5432`, the container's own loopback — NOT to
`postgres.railway.internal`.** Railway's docs say private-network targets are allowed; they were
refused every way tried (2026-09-17). The loopback of the Postgres container itself works.

Check it is up:

```bash
nc -z 127.0.0.1 15432 && echo "tunnel up"
```

Close it when finished:

```bash
pkill -f "15432:127.0.0.1:5432"
```

## 2. Get the password

Railway dashboard → **Postgres** service → **Variables** → `PGPASSWORD`.
(Or ask Claude, which reads it through the Railway MCP.)

```bash
export PGPASSWORD='<paste it here — never commit this>'
```

⚠️ Do not put it in a file in this repo, and do not paste it into a commit message.

## 3. Take the backup

```bash
mkdir -p ~/AoC-backups && chmod 700 ~/AoC-backups
TS=$(date -u +%Y%m%dT%H%M%SZ)

# The one you restore FROM (compressed, selective restore possible)
pg_dump -h 127.0.0.1 -p 15432 -U postgres -d railway -Fc --no-owner --no-acl \
        -f ~/AoC-backups/aoc-prod-$TS.dump

# The one you can READ in a text editor when you need to know what was in it
pg_dump -h 127.0.0.1 -p 15432 -U postgres -d railway -Fp --no-owner --no-acl \
        -f ~/AoC-backups/aoc-prod-$TS.sql
```

**Verify the dump before trusting it** — a dump that cannot be listed is not a backup:

```bash
pg_restore -l ~/AoC-backups/aoc-prod-$TS.dump | head
```

⭐ **Copy it somewhere that is not this laptop.** Right now `~/AoC-backups` on Pierre's machine is
the only off-Railway copy. Two copies in one place is one copy.

## 4. Restore into the local database

Never restore over `aoc_dev` — you lose whatever you were working on. Use a separate database.

```bash
cd /Volumes/SSD_pierre/Dev/Age\ of\ Conan/aoc_api
make db-up                                     # Postgres 18 on localhost:5433

docker compose exec -T db psql -U aoc -d postgres \
  -c 'DROP DATABASE IF EXISTS aoc_prod_restore WITH (FORCE)' \
  -c 'CREATE DATABASE aoc_prod_restore'

PGPASSWORD=aoc pg_restore -h 127.0.0.1 -p 5433 -U aoc -d aoc_prod_restore \
  --no-owner --no-acl ~/AoC-backups/aoc-prod-$TS.dump
echo "exit: $?"          # 0 = restored
```

⚠️ **Watch the exit code, not the absence of output.** A `pg_restore` that never connected prints
almost nothing and leaves an empty database — which then "matches" an empty production and looks
like success. That happened during this rehearsal.

## 5. Prove the restore actually worked

```bash
# object counts, both sides
for db in "15432 postgres railway" "5433 aoc aoc_prod_restore"; do
  set -- $db
  PGPASSWORD=${PGPASSWORD:-aoc} psql -h 127.0.0.1 -p $1 -U $2 -d $3 -tAc \
    "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
     WHERE c.relkind='r' AND n.nspname='public'"
done
```

For a database with real content, compare the rows themselves, not just the counts:

```sql
SELECT md5(string_agg(id||note,'|' ORDER BY id)) FROM schema_probe;
```

Run it on both sides; the hashes must be equal.

---

## What this rehearsal did and did NOT prove (2026-09-17)

| Proven | How |
|---|---|
| The tunnel reaches production | `/health` fetched through it → 200, commit `fbcf227` |
| `pg_dump` runs against production 18.6 | two dumps taken, 22 s each |
| The dump is well-formed | `pg_restore -l` lists its TOC |
| `pg_restore` restores it | exit 0; schemas/extensions/tables match production |
| **Dump→restore preserves data** | round-trip of local `aoc_dev`: 1 row in, 1 row out, **md5 identical** |

⛔ **NOT proven: how long a real restore takes.** ⭐ **Production was EMPTY on 2026-09-17** — zero
tables, no `goose_db_version`; migrations have only ever been applied locally. The 22 s above is
tunnel latency, not data. **This rehearsal must be repeated after AOC-011 imports the 4,648 items**,
and the timing recorded then. A restore time nobody has measured is a restore time you discover
during the outage.

⛔ **NOT covered:** restoring into a non-empty database, a backup taken mid-migration, and a
corrupt newest backup (there is currently no older one to fall back to).

## Re-rehearse every 6 months

Next due: **2026-03-17**. There is no automated reminder yet.
