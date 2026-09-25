#!/bin/bash
# Upload the img Worker with its R2 binding, and enable it on workers.dev for a pre-switch check.
# Does NOT touch img.aoc-codex.app — that is switch.sh.
cd "$(dirname "$0")"; source ./cf.sh
echo "deploy $SCRIPT:"
cf "upload script with binding BUCKET -> $BUCKET" -X PUT "$API/accounts/$ACCOUNT/workers/scripts/$SCRIPT" \
  -F "metadata={\"main_module\":\"worker.mjs\",\"compatibility_date\":\"2025-09-01\",\"bindings\":[{\"type\":\"r2_bucket\",\"name\":\"BUCKET\",\"bucket_name\":\"$BUCKET\"}]};type=application/json" \
  -F 'worker.mjs=@worker.mjs;type=application/javascript+module'
cf "enable on workers.dev" -X POST "$API/accounts/$ACCOUNT/workers/scripts/$SCRIPT/subdomain" \
  -H 'Content-Type: application/json' --data '{"enabled":true,"previews_enabled":false}'
SUB=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/accounts/$ACCOUNT/workers/subdomain" | python3 -c 'import sys,json;print(json.load(sys.stdin)["result"]["subdomain"])')
echo "check: https://$SCRIPT.$SUB.workers.dev/armory/<file>   (no edge cache on workers.dev)"
