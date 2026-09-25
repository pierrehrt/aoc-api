#!/bin/bash
# Put img.aoc-codex.app back on the R2 custom domain, as it was before switch.sh.
cd "$(dirname "$0")"; source ./cf.sh
echo "rollback $HOST to the R2 custom domain:"
ID=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/accounts/$ACCOUNT/workers/domains?hostname=$HOST" \
  | python3 -c 'import sys,json; r=json.load(sys.stdin).get("result") or []; print(r[0]["id"] if r else "")')
if [ -n "$ID" ]; then cf "1/2 detach the Worker from $HOST" -X DELETE "$API/accounts/$ACCOUNT/workers/domains/$ID"
else echo "   ✅ 1/2 no Worker attached to $HOST"; fi
cf "2/2 re-attach the R2 custom domain" -X POST "$API/accounts/$ACCOUNT/r2/buckets/$BUCKET/domains/custom" \
  -H 'Content-Type: application/json' --data "{\"domain\":\"$HOST\",\"zoneId\":\"$ZONE\",\"enabled\":true,\"minTLS\":\"1.2\"}"
