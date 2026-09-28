# Shared by deploy.sh, switch.sh and rollback.sh. Sourced, not run.
# Reads CLOUDFLARE_API_TOKEN from ~/.config/aoc-codex/cloudflare.env and never prints it.
# ⚠️ The Python here must run on macOS's /usr/bin/python3 (3.9): no backslash inside an f-string,
# no match statements. A 3.12-only f-string stopped switch.sh halfway in production (AOC-041,
# 2026-09-28); scripts.test.sh runs every script under whichever python3 it is given.
set -euo pipefail
set -a; source ~/.config/aoc-codex/cloudflare.env; set +a
API=https://api.cloudflare.com/client/v4
ZONE=9a60a586d20fe4ed78079b158b19cb1e          # aoc-codex.app
HOST=img.aoc-codex.app
BUCKET=aoc-codex-enam
SCRIPT=aoc-img
ACCOUNT=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/zones/$ZONE" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["result"]["account"]["id"])')

# cf <label> <curl args...> : run one API call, print success or the errors, stop on failure.
cf() {
  local label=$1; shift
  local out; out=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$@")
  if printf '%s' "$out" | python3 -c 'import sys,json; d=json.load(sys.stdin); sys.exit(0 if d.get("success") else 1)' 2>/dev/null; then
    echo "   ✅ $label"; CF_OUT=$out
  else
    echo "   ❌ $label"; printf '%s' "$out" | python3 -c 'import sys,json
try: d=json.load(sys.stdin); print("     ", d.get("errors"))
except Exception: print("      (not JSON)")'
    exit 1
  fi
}

# Read-only lookups, so switch.sh and rollback.sh can look before every write and be re-run safely.
# Each one FAILS when the API does not answer: an unanswered lookup must never read as "nothing
# there". Call them as VAR=$(lookup), never inside [ ], where set -e cannot see the failure.

# r2_domain: "attached" or "absent" — is $HOST a custom domain of $BUCKET?
r2_domain() {
  curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/accounts/$ACCOUNT/r2/buckets/$BUCKET/domains/custom" \
    | python3 -c 'import sys,json
d=json.load(sys.stdin)
if not d.get("success"): sys.exit("   ❌ the R2 custom-domain list did not answer: %s" % d.get("errors"))
print("attached" if any(x.get("domain") == sys.argv[1] for x in d["result"]["domains"]) else "absent")' "$HOST"
}

# worker_domain: the id of the Workers domain on $HOST, or nothing.
worker_domain() {
  curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/accounts/$ACCOUNT/workers/domains?hostname=$HOST" \
    | python3 -c 'import sys,json
d=json.load(sys.stdin)
if not d.get("success"): sys.exit("   ❌ the Workers domain list did not answer: %s" % d.get("errors"))
r=[x for x in d["result"] if x.get("hostname") == sys.argv[1]]
print(r[0]["id"] if r else "")' "$HOST"
}

# dns_records: $HOST's DNS records as "id:type:content", space-separated, or nothing.
dns_records() {
  curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/zones/$ZONE/dns_records?name=$HOST" \
    | python3 -c 'import sys,json
d=json.load(sys.stdin)
if not d.get("success"): sys.exit("   ❌ the DNS record list did not answer: %s" % d.get("errors"))
print(" ".join("%s:%s:%s" % (x["id"], x["type"], x["content"]) for x in d["result"]))'
}

# wait_dns_clear: give $HOST's records up to a minute to go (a detached binding removes its own
# record, not instantly); prints whatever is still there. `|| return 1` because this runs inside
# $( ), where bash does not apply set -e.
wait_dns_clear() {
  local rec i
  for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
    rec=$(dns_records) || return 1
    [ -z "$rec" ] && return 0
    sleep 5
  done
  printf '%s' "$rec"
}
