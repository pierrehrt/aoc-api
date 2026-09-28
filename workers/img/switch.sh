#!/bin/bash
# Move img.aoc-codex.app from the R2 custom domain to the img Worker. Undo with rollback.sh.
# The hostname answers nothing for the seconds between steps 2 and 4.
# Safe to re-run: every step looks before it writes, so a switch that stopped halfway is finished
# rather than failed on the step that already happened (AOC-041, 2026-09-28).
cd "$(dirname "$0")"; source ./cf.sh
echo "switch $HOST to Worker $SCRIPT:"
cf "1/4 the Worker exists" "$API/accounts/$ACCOUNT/workers/scripts/$SCRIPT/settings"
WD=$(worker_domain)
if [ -n "$WD" ]; then echo "   ✅ $HOST is already served by the Worker — nothing to do"; exit 0; fi
# Before the first write: the host may carry R2's CNAME or nothing. Anything else is not ours to
# take down, so stop while the hostname still answers.
REC=$(dns_records)
if [ -n "$REC" ] && [ "${REC#*:}" != "CNAME:public.r2.dev" ]; then
  echo "   ❌ $HOST has DNS records this script did not expect: $REC — nothing was changed"; exit 1
fi
R2=$(r2_domain)
if [ "$R2" = attached ]; then
  cf "2/4 detach the R2 custom domain" -X DELETE "$API/accounts/$ACCOUNT/r2/buckets/$BUCKET/domains/custom/$HOST"
else
  echo "   ✅ 2/4 no R2 custom domain on $HOST"
fi
# The R2 binding owns a proxied CNAME to public.r2.dev. Wait for it to go; remove it only if it is
# still exactly that record, and stop on anything else rather than delete a record we did not make.
REC=$(wait_dns_clear)
if [ -n "$REC" ]; then
  set -- $REC
  if [ $# -eq 1 ] && [ "${1#*:}" = "CNAME:public.r2.dev" ]; then
    cf "3/4 remove the leftover R2 CNAME" -X DELETE "$API/zones/$ZONE/dns_records/${1%%:*}"
  else
    echo "   ❌ 3/4 $HOST still has DNS records this script did not expect: $REC"; echo "      run rollback.sh"; exit 1
  fi
else
  echo "   ✅ 3/4 no DNS record left on $HOST"
fi
cf "4/4 attach the Worker as $HOST" -X PUT "$API/accounts/$ACCOUNT/workers/domains" -H 'Content-Type: application/json' \
  --data "{\"environment\":\"production\",\"hostname\":\"$HOST\",\"service\":\"$SCRIPT\",\"zone_id\":\"$ZONE\"}"
echo "done. check: curl -sI https://$HOST/armory/<file> | grep -i x-aoc-cache"
