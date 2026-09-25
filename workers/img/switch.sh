#!/bin/bash
# Move img.aoc-codex.app from the R2 custom domain to the img Worker. Undo with rollback.sh.
# The hostname answers nothing for the seconds between steps 2 and 4.
cd "$(dirname "$0")"; source ./cf.sh
echo "switch $HOST to Worker $SCRIPT:"
cf "1/4 the Worker exists" "$API/accounts/$ACCOUNT/workers/scripts/$SCRIPT/settings"
cf "2/4 detach the R2 custom domain" -X DELETE "$API/accounts/$ACCOUNT/r2/buckets/$BUCKET/domains/custom/$HOST"
# The R2 binding owns a proxied CNAME to public.r2.dev. Wait for it to go; remove it only if it is
# still exactly that record, and stop on anything else rather than delete a record we did not make.
for i in $(seq 1 12); do
  REC=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "$API/zones/$ZONE/dns_records?name=$HOST" \
    | python3 -c 'import sys,json; r=json.load(sys.stdin)["result"]; print(" ".join(f"{x[\"id\"]}:{x[\"type\"]}:{x[\"content\"]}" for x in r))')
  [ -z "$REC" ] && break; sleep 5
done
if [ -n "$REC" ]; then
  set -- $REC
  if [ $# -eq 1 ] && [ "${1#*:}" = "CNAME:public.r2.dev" ]; then
    cf "3/4 remove the leftover R2 CNAME" -X DELETE "$API/zones/$ZONE/dns_records/${1%%:*}"
  else
    echo "   ❌ 3/4 $HOST still has DNS records this script did not expect: $REC"; echo "      run rollback.sh"; exit 1
  fi
else
  echo "   ✅ 3/4 the R2 CNAME is gone"
fi
cf "4/4 attach the Worker as $HOST" -X PUT "$API/accounts/$ACCOUNT/workers/domains" -H 'Content-Type: application/json' \
  --data "{\"environment\":\"production\",\"hostname\":\"$HOST\",\"service\":\"$SCRIPT\",\"zone_id\":\"$ZONE\"}"
echo "done. check: curl -sI https://$HOST/armory/<file> | grep -i x-aoc-cache"
