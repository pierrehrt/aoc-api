#!/bin/bash
# AOC-026: the Cloudflare half of the cache policy (docs/architecture.md § Caching).
#
#   check            read only: what is live, and whether it is what apply wants
#   apply --dry-run  every read apply makes, and prints each write instead of making it
#   apply            raise the zone's min_tls_version to 1.2 and bring the Cache Rules to the two below
#   purge <url>      purge one URL from the edge cache
#   rollback         remove AOC-026's Cache Rules and put min_tls_version back to 1.0
#
# Every write looks first: apply and rollback are safe to re-run, a run with nothing to do writes
# nothing, and anything this script did not make (a rule whose description does not start with
# "AOC-026 ") stops it before it writes. Success is an HTTP 2xx whose body, if JSON, does not say
# success:false — not "a JSON body with success:true", which is the misreading that stopped
# rollback.sh in production (AOC-041).
# The Python must run on macOS's /usr/bin/python3 (3.9): no backslash in an f-string, no match.
#
# Cloudflare writes are refused from the assistant, so Pierre runs apply, purge and rollback.
set -euo pipefail
set -a; source ~/.config/aoc-codex/cloudflare.env; set +a
API=https://api.cloudflare.com/client/v4
ZONE=9a60a586d20fe4ed78079b158b19cb1e          # aoc-codex.app
EP="$API/zones/$ZONE/rulesets/phases/http_request_cache_settings/entrypoint"
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

# wanted: the cache-settings phase as this script wants it, IN ORDER.
#   1. every request to the apex is eligible, cached by the origin's Cache-Control, bypassed
#      without one — the origin decides, the edge never guesses;
#   2. a request carrying HX-Request: true or any Authorization is never answered from cache
#      (AOC-026 verify round 1, B2). The origin's no-store only stops a response being STORED; a
#      HIT never reaches the origin, so without this an HTMX GET would be handed the cached page.
#      Not on Cookie: Cloudflare's own bot cookies ride on every request.
# Rule 2 comes after rule 1 because a later matching rule overrides an earlier one's setting.
wanted() {
  python3 -c 'import json
host = "http.host eq \"aoc-codex.app\""
print(json.dumps([
  {"description": "AOC-026 cache aoc-codex.app by the origin Cache-Control",
   "expression": "(" + host + ")",
   "action": "set_cache_settings",
   "action_parameters": {"cache": True, "edge_ttl": {"mode": "bypass_by_default"},
                         "browser_ttl": {"mode": "respect_origin"}},
   "enabled": True},
  {"description": "AOC-026 never answer an HTMX or authorised request from cache",
   "expression": "(" + host + " and (any(http.request.headers[\"hx-request\"][*] eq \"true\")"
                 " or any(http.request.headers[\"authorization\"][*] ne \"\")))",
   "action": "set_cache_settings",
   "action_parameters": {"cache": False},
   "enabled": True},
]))'
}

# phase: the cache-settings phase as JSON, {"id": null, "rules": []} when the zone has no entrypoint
# yet. Fails if the API does not answer: an unanswered read must never look like "no rules".
phase() {
  curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$EP" | python3 -c 'import sys,json
d=json.load(sys.stdin)
if not d.get("success"):
    if any(e.get("code") == 10003 for e in d.get("errors") or []):
        print(json.dumps({"id": None, "rules": []})); sys.exit(0)
    sys.exit("   ❌ the cache rules did not answer: %s" % d.get("errors"))
print(json.dumps({"id": d["result"]["id"], "rules": d["result"].get("rules") or []}))'
}

# compare <phase>: the first line is one word — none, same, differs or other — and the lines after
# it say what is there. "Ours" is every rule whose description starts with "AOC-026 ".
compare() {
  python3 -c 'import sys,json
rules=json.loads(sys.argv[1])["rules"]; want=json.loads(sys.argv[2])
keys=("description","expression","action","action_parameters","enabled")
descs=[r.get("description") or "(no description)" for r in rules]
other=[d for d in descs if not d.startswith("AOC-026 ")]
if other:
    print("other")
    for d in other: print("      " + d)
elif not rules:
    print("none")
elif [dict((k, r.get(k)) for k in keys) for r in rules] == want:
    print("same")
else:
    print("differs")
    print("      live:   " + "; ".join(descs))
    print("      wanted: " + "; ".join(w["description"] for w in want))' "$1" "$(wanted)"
}

show() {
  local ph
  ph=$(phase)
  echo "   min_tls_version: $(tls_version)"
  echo "   cache rules:     $(python3 -c 'import sys,json
print("; ".join(r.get("description") or "(no description)" for r in json.loads(sys.argv[1])["rules"]) or "none")' "$ph")"
  echo "   as apply wants:  $(compare "$ph" | head -1)"
  echo "   page:            $(curl -sS -o /dev/null -D - https://aoc-codex.app/ | grep -i '^cf-cache-status' | tr -d '\r')"
}

case "${1:-}" in
  check)
    echo "aoc-codex.app, now:"; show ;;

  apply)
    [ "${2:-}" = "--dry-run" ] && DRY=1
    echo "apply AOC-026 to aoc-codex.app$([ $DRY = 1 ] && echo ' (dry run: reads only)'):"
    # Every read happens before any write, so a surprise stops the script with nothing changed.
    TLS=$(tls_version); PH=$(phase); STATE=$(compare "$PH")
    case "$STATE" in
      other*)
        echo "   ❌ the cache-settings phase holds rules this script did not make — nothing was changed:"
        printf '%s\n' "$STATE" | tail -n +2; exit 1 ;;
    esac
    case "$TLS" in
      1.0|1.1) call "1/2 min_tls_version $TLS -> 1.2" -X PATCH "$API/zones/$ZONE/settings/min_tls_version" \
                 -H 'Content-Type: application/json' --data '{"value":"1.2"}' ;;
      1.2|1.3) echo "   ✅ 1/2 min_tls_version is already $TLS" ;;
      *) echo "   ❌ 1/2 min_tls_version is $TLS, which this script does not know — stopping"; exit 1 ;;
    esac
    # One PUT of the whole phase: atomic, so there is no moment with one rule and not the other.
    case "$STATE" in
      same) echo "   ✅ 2/2 the Cache Rules are already as wanted" ;;
      none) call "2/2 add the Cache Rules" -X PUT "$EP" -H 'Content-Type: application/json' \
              --data "{\"rules\": $(wanted)}" ;;
      differs*) printf '%s\n' "$STATE" | tail -n +2
              call "2/2 bring AOC-026's Cache Rules to the wanted set" -X PUT "$EP" \
                -H 'Content-Type: application/json' --data "{\"rules\": $(wanted)}" ;;
    esac
    [ $DRY = 1 ] || { echo "now:"; show; } ;;

  purge)
    URL=${2:?usage: purge <url>}
    echo "purge $URL:"
    call "purge" -X POST "$API/zones/$ZONE/purge_cache" -H 'Content-Type: application/json' \
      --data "$(python3 -c 'import json,sys; print(json.dumps({"files": [sys.argv[1]]}))' "$URL")" ;;

  rollback)
    echo "rollback AOC-026 on aoc-codex.app:"
    TLS=$(tls_version); PH=$(phase); STATE=$(compare "$PH")
    case "$STATE" in
      other*)
        echo "   ❌ the cache-settings phase holds rules this script did not make — nothing was changed:"
        printf '%s\n' "$STATE" | tail -n +2; exit 1 ;;
      none) echo "   ✅ 1/2 no AOC-026 Cache Rules" ;;
      *) RSID=$(python3 -c 'import sys,json; print(json.loads(sys.argv[1])["id"])' "$PH")
         for RID in $(python3 -c 'import sys,json; print(" ".join(r["id"] for r in json.loads(sys.argv[1])["rules"]))' "$PH"); do
           call "1/2 remove Cache Rule $RID" -X DELETE "$API/zones/$ZONE/rulesets/$RSID/rules/$RID"
         done ;;
    esac
    if [ "$TLS" = 1.2 ]; then
      call "2/2 min_tls_version 1.2 -> 1.0" -X PATCH "$API/zones/$ZONE/settings/min_tls_version" \
        -H 'Content-Type: application/json' --data '{"value":"1.0"}'
    else
      echo "   ✅ 2/2 min_tls_version is $TLS, not what apply set — left alone"
    fi
    echo "now:"; show ;;

  *) sed -n '2,17p' "$0"; exit 2 ;;
esac
