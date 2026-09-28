#!/bin/bash
# AOC-026: the Cloudflare half of the cache policy (docs/architecture.md § Caching).
#
#   check            read only: what is live, and what apply would change
#   apply --dry-run  every read apply makes, and prints each write instead of making it
#   apply            raise the zone's min_tls_version to 1.2 and add the one Cache Rule
#   purge <url>      purge one URL from the edge cache
#   rollback         remove the Cache Rule and put min_tls_version back to 1.0
#
# Every write looks first: apply and rollback are safe to re-run, a run with nothing to do writes
# nothing, and anything this script did not make (another rule in the phase) stops it before it
# writes. Success is an HTTP 2xx whose body, if JSON, does not say success:false — not "a JSON body
# with success:true", which is the misreading that stopped rollback.sh in production (AOC-041).
# The Python must run on macOS's /usr/bin/python3 (3.9): no backslash in an f-string, no match.
#
# Cloudflare writes are refused from the assistant, so Pierre runs apply, purge and rollback.
set -euo pipefail
set -a; source ~/.config/aoc-codex/cloudflare.env; set +a
API=https://api.cloudflare.com/client/v4
ZONE=9a60a586d20fe4ed78079b158b19cb1e          # aoc-codex.app
EP="$API/zones/$ZONE/rulesets/phases/http_request_cache_settings/entrypoint"
DESC="AOC-026 cache aoc-codex.app by the origin Cache-Control"
DRY=0

# call <label> <curl args...> : one API call. Prints ✅ or the status and errors; stops on failure.
# Leaves the body in $OUT.
call() {
  local label=$1; shift
  if [ "$DRY" = 1 ]; then echo "   🔸 would: $label"; OUT='{}'; return 0; fi
  local raw code
  raw=$(curl -s -w '\n%{http_code}' -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$@")
  code=${raw##*$'\n'}; OUT=${raw%$'\n'*}
  if printf '%s' "$OUT" | python3 -c 'import sys,json
body=sys.stdin.read()
if not sys.argv[1].startswith("2"): sys.exit(1)
try: d=json.loads(body)
except ValueError: sys.exit(0)
sys.exit(0 if not isinstance(d, dict) or d.get("success", True) else 1)' "$code"; then
    echo "   ✅ $label"
  else
    echo "   ❌ $label (HTTP $code)"; printf '%s' "$OUT" | python3 -c 'import sys,json
body=sys.stdin.read()
try: print("     ", json.loads(body).get("errors"))
except Exception: print("      body:", repr(body[:200]))'
    exit 1
  fi
}

# tls_version: the zone's min_tls_version. Fails if the API does not answer.
tls_version() {
  curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/zones/$ZONE/settings/min_tls_version" \
    | python3 -c 'import sys,json
d=json.load(sys.stdin)
if not d.get("success"): sys.exit("   ❌ min_tls_version did not answer: %s" % d.get("errors"))
print(d["result"]["value"])'
}

# cache_rules: the cache-settings phase, as "none" (no entrypoint yet) or one line per rule,
# "<ruleset id> <rule id> <ours|OTHER> <description>". Fails on any other API failure — an
# unanswered read must never look like "no rules".
cache_rules() {
  curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$EP" | python3 -c 'import sys,json
d=json.load(sys.stdin)
if not d.get("success"):
    if any(e.get("code") == 10003 for e in d.get("errors") or []): print("none"); sys.exit(0)
    sys.exit("   ❌ the cache rules did not answer: %s" % d.get("errors"))
rs=d["result"]; rules=rs.get("rules") or []
if not rules: print("none")
for r in rules:
    print("%s %s %s %s" % (rs["id"], r["id"], "ours" if r.get("description") == sys.argv[1] else "OTHER", r.get("description", "")))' "$DESC"
}

rule_json() {
  python3 -c 'import json,sys
print(json.dumps({"rules": [{
  "description": sys.argv[1],
  "expression": "(http.host eq \"aoc-codex.app\")",
  "action": "set_cache_settings",
  "action_parameters": {
    "cache": True,
    "edge_ttl": {"mode": "bypass_by_default"},
    "browser_ttl": {"mode": "respect_origin"}
  },
  "enabled": True
}]}))' "$DESC"
}

show() {
  local tls rules
  tls=$(tls_version); rules=$(cache_rules)
  echo "   min_tls_version: $tls"
  echo "   cache rules:     $(printf '%s' "$rules" | awk '{ if ($1 == "none") print "none"; else { $1=""; $2=""; print } }' | paste -sd ';' -)"
  echo "   page:            $(curl -sS -o /dev/null -D - https://aoc-codex.app/ | grep -i '^cf-cache-status' | tr -d '\r')"
}

case "${1:-}" in
  check)
    echo "aoc-codex.app, now:"; show ;;

  apply)
    [ "${2:-}" = "--dry-run" ] && DRY=1
    echo "apply AOC-026 to aoc-codex.app$([ $DRY = 1 ] && echo ' (dry run: reads only)'):"
    # Both reads happen before either write, so a surprise stops the script with nothing changed.
    TLS=$(tls_version); RULES=$(cache_rules)
    if printf '%s\n' "$RULES" | grep -q ' OTHER '; then
      echo "   ❌ the cache-settings phase holds rules this script did not make — nothing was changed:"
      printf '%s\n' "$RULES" | grep ' OTHER ' | sed 's/^/      /'; exit 1
    fi
    case "$TLS" in
      1.0|1.1) call "1/2 min_tls_version $TLS -> 1.2" -X PATCH "$API/zones/$ZONE/settings/min_tls_version" \
                 -H 'Content-Type: application/json' --data '{"value":"1.2"}' ;;
      1.2|1.3) echo "   ✅ 1/2 min_tls_version is already $TLS" ;;
      *) echo "   ❌ 1/2 min_tls_version is $TLS, which this script does not know — stopping"; exit 1 ;;
    esac
    if [ "$RULES" = none ]; then
      call "2/2 add the Cache Rule" -X PUT "$EP" -H 'Content-Type: application/json' --data "$(rule_json)"
    else
      echo "   ✅ 2/2 the Cache Rule is already there"
    fi
    [ $DRY = 1 ] || { echo "now:"; show; } ;;

  purge)
    URL=${2:?usage: purge <url>}
    echo "purge $URL:"
    call "purge" -X POST "$API/zones/$ZONE/purge_cache" -H 'Content-Type: application/json' \
      --data "$(python3 -c 'import json,sys; print(json.dumps({"files": [sys.argv[1]]}))' "$URL")" ;;

  rollback)
    echo "rollback AOC-026 on aoc-codex.app:"
    TLS=$(tls_version); RULES=$(cache_rules)
    OURS=$(printf '%s\n' "$RULES" | grep ' ours ' || true)
    if [ -n "$OURS" ]; then
      set -- $OURS
      call "1/2 remove the Cache Rule" -X DELETE "$API/zones/$ZONE/rulesets/$1/rules/$2"
    else
      echo "   ✅ 1/2 no AOC-026 Cache Rule"
    fi
    if [ "$TLS" = 1.2 ]; then
      call "2/2 min_tls_version 1.2 -> 1.0" -X PATCH "$API/zones/$ZONE/settings/min_tls_version" \
        -H 'Content-Type: application/json' --data '{"value":"1.0"}'
    else
      echo "   ✅ 2/2 min_tls_version is $TLS, not what apply set — left alone"
    fi
    echo "now:"; show ;;

  *) sed -n '2,12p' "$0"; exit 2 ;;
esac
