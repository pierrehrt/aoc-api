#!/bin/bash
# Put img.aoc-codex.app back on the R2 custom domain, as it was before switch.sh.
# Safe to re-run, like switch.sh: every step looks before it writes.
cd "$(dirname "$0")"; source ./cf.sh
echo "rollback $HOST to the R2 custom domain:"
WD=$(worker_domain)
R2=$(r2_domain)
if [ -z "$WD" ] && [ "$R2" = attached ]; then echo "   ✅ $HOST is already on the R2 custom domain — nothing to do"; exit 0; fi
# Before the first write: the host may carry the Worker's own record (a read-only AAAA 100::) or
# nothing. Anything else is not ours to take down, so stop while the hostname still answers.
REC=$(dns_records)
if [ -n "$REC" ] && [ "${REC#*:}" != "AAAA:100::" ]; then
  echo "   ❌ $HOST has DNS records this script did not expect: $REC — nothing was changed"; exit 1
fi
if [ -n "$WD" ]; then cf "1/3 detach the Worker from $HOST" -X DELETE "$API/accounts/$ACCOUNT/workers/domains/$WD"
else echo "   ✅ 1/3 no Worker attached to $HOST"; fi
# The Worker's record goes when the Worker is detached, not instantly. Wait for it rather than
# attach the bucket over it; if it outlasts the wait, re-running this script finishes the job.
REC=$(wait_dns_clear)
if [ -n "$REC" ]; then
  echo "   ❌ 2/3 $HOST still has DNS records: $REC — wait a minute and run rollback.sh again"; exit 1
fi
echo "   ✅ 2/3 no DNS record left on $HOST"
cf "3/3 re-attach the R2 custom domain" -X POST "$API/accounts/$ACCOUNT/r2/buckets/$BUCKET/domains/custom" \
  -H 'Content-Type: application/json' --data "{\"domain\":\"$HOST\",\"zoneId\":\"$ZONE\",\"enabled\":true,\"minTLS\":\"1.2\"}"
