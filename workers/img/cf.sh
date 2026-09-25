# Shared by deploy.sh, switch.sh and rollback.sh. Sourced, not run.
# Reads CLOUDFLARE_API_TOKEN from ~/.config/aoc-codex/cloudflare.env and never prints it.
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
