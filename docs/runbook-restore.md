# Runbook — back up and restore the production database

**Written while doing it, 2026-09-17 (AOC-006). Every command below was run.**

Read this at 2 a.m. with no context. It assumes nothing except a Mac with Homebrew.

> ✅ **THERE IS NOW A SCHEDULED BACKUP (AOC-030).** A second Railway service dumps to R2 daily
> over the private network, and a GitHub Action goes red if it stops. **Section 6 is the path you
> want in a real disaster** — it restores from a dump the *job* made.
>
> Railway's scheduled backups remain a **Pro-plan** feature and this project is on Hobby, so
> sections 1–3 below (the by-hand dump) are still the right thing before a risky migration, and
> still the fallback if the bucket is unreachable.
>
> ⚠️ This database holds the only structured copy of the AoC>TV dataset that will ever exist
> (the source host lapses ~February 2027).

---

## 0. What you need once

```bash
brew install libpq          # Postgres client 18.6 — MUST be >= the server
brew install colima docker docker-compose   # only needed for a restore
colima start                # ⚠️ installing it does not START it, and `make db-up` dies without it
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

⚠️ **That username is the Postgres service's INSTANCE ID.** If it ever changes: Railway dashboard →
Postgres service → `CMD+K` → **Copy Service Instance ID** (*not* "Copy Service ID", a different
value).

**What each username actually does**, because "it let me in" is not the same as "I am in the right
container", and an earlier version of this page said all three were refused:

| Username | Result, measured 2026-09-17 |
|---|---|
| Postgres **service instance ID** | ✅ what you want — you are in the Postgres container, which has `pg_dump` 18.6 |
| `api` **service domain** (`…up.railway.app`) | ⚠️ **authenticates**, but lands you in the `api` container — a `scratch` image with **no shell and no `pg_dump`** — and forwarding on to Postgres from there was refused |
| **service** ID | ❌ refused; Railway's docs say so explicitly |
| **deployment** ID | ❌ refused: *"No target found … Use a service domain as the SSH username."* |

So if your login succeeds and there is still no database on port 15432, you are in the wrong
container — do not go looking at your key.

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
the only off-Railway copy. Two copies in one place is one copy. (**AOC-030** makes this automatic.)

## 4. Restore into the local database

⚠️ **Start here if production is already gone.** Sections 1–3 take a *new* dump; a real disaster
restores an *old* one, and `$TS` from section 3 does not exist on that path. Pick the file first:

```bash
ls -lt ~/AoC-backups/*.dump                    # what you have, newest first
DUMP=$(ls -t ~/AoC-backups/*.dump | head -1)   # or set it by hand from that listing
echo "restoring: $DUMP"
[ -s "$DUMP" ] || echo "THAT FILE IS EMPTY OR MISSING — pick another one"
```

**Look at the dates before trusting the newest.** If the newest dump is corrupt, an older one is
the whole fallback, and `pg_restore -l "$DUMP" | head` is how you find out which.

Never restore over `aoc_dev` — you lose whatever you were working on. Use a separate database.

```bash
cd /Volumes/SSD_pierre/Dev/Age\ of\ Conan/aoc_api
colima start                                   # the container runtime; a no-op if already up
make db-up                                     # Postgres 18 on localhost:5433

docker compose exec -T db psql -U aoc -d postgres \
  -c 'DROP DATABASE IF EXISTS aoc_prod_restore WITH (FORCE)' \
  -c 'CREATE DATABASE aoc_prod_restore'

PGPASSWORD=aoc pg_restore -h 127.0.0.1 -p 5433 -U aoc -d aoc_prod_restore \
  --no-owner --no-acl "$DUMP"
echo "exit: $?"          # 0 = restored
```

⚠️ **Watch the exit code, not the absence of output.** A `pg_restore` that never connected prints
almost nothing and leaves an empty database — which then "matches" an empty production and looks
like success. That happened during this rehearsal.

## 5. Prove the restore actually worked

⚠️ **Two databases, two different passwords — write them out, do not loop.** Production's password
is in `$PGPASSWORD` from section 2; the local one is `aoc`. An earlier version of this page looped
over both with `PGPASSWORD=${PGPASSWORD:-aoc}`, which sends **production's** password to the local
database and fails authentication — on the step whose entire job is to prove the restore worked.
(It also used `set -- $db` to split a string, which **does not word-split in zsh**, the shell macOS
has shipped since Catalina. Both were found by AOC-006 verify round 1 and both are why this is now
two plain commands.)

```bash
TABLES='SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relkind = '"'"'r'"'"' AND n.nspname = '"'"'public'"'"''

# production, through the tunnel — uses $PGPASSWORD from section 2
psql -h 127.0.0.1 -p 15432 -U postgres -d railway -tAc "$TABLES"

# the restored copy, locally — its password is 'aoc', not production's
PGPASSWORD=aoc psql -h 127.0.0.1 -p 5433 -U aoc -d aoc_prod_restore -tAc "$TABLES"
```

The two numbers must match. ⚠️ **Two zeroes are not a match** — an empty restore "matches" an empty
production, which is exactly the false pass this rehearsal hit. If both print `0`, something is
wrong with the restore *or* production is genuinely empty; check which before believing it.

For a database with real content, compare the rows themselves, not just the counts. Run the same
query on both sides — again with each side's own password — and the hashes must be equal:

```bash
HASH="SELECT md5(string_agg(id||note, '|' ORDER BY id)) FROM schema_probe"

psql -h 127.0.0.1 -p 15432 -U postgres -d railway -tAc "$HASH"
PGPASSWORD=aoc psql -h 127.0.0.1 -p 5433 -U aoc -d aoc_prod_restore -tAc "$HASH"
```

(`schema_probe` is the table that exists today. After AOC-011 the table to hash is `items`.)

---

## 6. Restore from the AUTOMATED backup (AOC-030) — the real disaster path

⭐ **Start here when production is gone.** Sections 1–3 take a *new* dump, which requires a
production that still answers. This section needs only the bucket.

### What you need

The **read-only** R2 credentials — `R2_READONLY_ACCESS_KEY_ID` / `R2_READONLY_SECRET_ACCESS_KEY`
in `~/.config/aoc-codex/r2.env`, plus `R2_S3_ENDPOINT` and the backups bucket name. Use the
read-only pair: nothing in a restore should be able to damage the thing you are restoring from.

⚠️ **`rclone` is not installed on this machine** (checked 2026-09-22) and there is no
`~/.config/rclone/rclone.conf`. Install it, or use the S3 API directly — the bucket is plain S3.
`brew install rclone`, then configure a remote from the values in `r2.env`:

```bash
set -a; . ~/.config/aoc-codex/r2.env; set +a
export RCLONE_CONFIG_R2RO_TYPE=s3
export RCLONE_CONFIG_R2RO_PROVIDER=Cloudflare
export RCLONE_CONFIG_R2RO_ENDPOINT="$R2_S3_ENDPOINT"
export RCLONE_CONFIG_R2RO_ACCESS_KEY_ID="$R2_READONLY_ACCESS_KEY_ID"
export RCLONE_CONFIG_R2RO_SECRET_ACCESS_KEY="$R2_READONLY_SECRET_ACCESS_KEY"
export RCLONE_S3_NO_CHECK_BUCKET=true     # the token is bucket-scoped; see below
```

⚠️ **`RCLONE_S3_NO_CHECK_BUCKET=true` is required, not optional.** A bucket-scoped token cannot
`CreateBucket`, and rclone tries to ensure the bucket exists first. Without it you get a `403` that
looks like a bad credential and is not one (AOC-007).

### List what exists, newest last

```bash
rclone lsl "r2ro:$R2_BACKUP_BUCKET/prod/" | sort -k2
```

**Look at the dates and the sizes before trusting the newest.** If the newest dump is corrupt, an
older one is the whole fallback. A row that is suspiciously small is a failed job, not a backup.

### Fetch it and CHECK IT BEFORE USING IT

```bash
mkdir -p tmp/dumps
NEWEST="$(rclone lsf --format p "r2ro:$R2_BACKUP_BUCKET/prod/" | sort | tail -1)"
echo "fetching: $NEWEST"
rclone copyto "r2ro:$R2_BACKUP_BUCKET/prod/$NEWEST" "tmp/dumps/$NEWEST"

export PATH="/opt/homebrew/opt/libpq/bin:$PATH"
pg_restore -l "tmp/dumps/$NEWEST" | head
```

⚠️ **A dump that cannot be listed is not a backup.** If `pg_restore -l` errors or prints an empty
table of contents, go to the next-oldest file — do not try to restore it.

### Restore it

`make db-restore` already picks the newest file in `tmp/dumps/`, which is what you just put there:

```bash
make db-restore
```

Then **prove it** with section 5's two queries. ⚠️ **Two zeroes are not a match.**

### ⚠️ The exit code, again

Same warning as section 4, and it is the one that has actually bitten: **watch the exit code, not
the absence of output.** A `pg_restore` that never connected prints almost nothing and leaves an
empty database, which then "matches" an empty production and looks like success.

---

## The alarm, and how it can itself go silent

`.github/workflows/backup-freshness.yml` runs daily and fails when the newest object under `prod/`
is older than 48 h, smaller than 1 KB, or absent. **Its failure email is the alarm** — there is
nothing else subscribed.

**To prove the alarm still works** (do this during the six-monthly drill — a check nobody has seen
fail is a check nobody has tested):

> Actions → **Backup freshness** → *Run workflow* → set **prefix** to `does-not-exist/` → it must
> go **red** with `NO OBJECTS`. Then run it again with the prefix blank and confirm it goes green.

⚠️⚠️ **GitHub disables scheduled workflows in a repository with no activity for 60 days.** This
project is touched in bursts months apart, so that is a real state, not a theoretical one. When it
happens GitHub emails the repository owner and the Actions tab shows the workflow as disabled —
**re-enable it there**, and the daily check resumes. Any push also resets the clock.

This is the one hole in the dead man's switch and it is written here on purpose: nobody should
discover it at the same moment they discover a missing backup.

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

Next due: **2027-03-17** — six months after the 2026-09-17 rehearsal. (This said *2026-03-17* until
2026-09-18: a date six months in the **past**, which a reader would either trip over or, worse,
treat as already handled.)

✅ **Something fires on that date now.** `.github/workflows/restore-drill-reminder.yml` runs on
17 March and 17 September and **opens an issue** labelled `restore-drill`, with the checklist. It
skips opening a second one while the first is still open. A reminder that depends on someone
remembering to look at a runbook is not a reminder; this one arrives by itself.

⚠️ **Use a dump the JOB produced, not a hand-made one** (section 6). The automated path is the one
that has to work, and rehearsing the manual path proves nothing about it.

⚠️ **This reminder is subject to the same 60-day dormancy rule as the alarm** — see § The alarm
can itself go silent, below.
