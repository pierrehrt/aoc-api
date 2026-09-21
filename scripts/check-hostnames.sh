#!/usr/bin/env bash
# check-hostnames.sh — gather evidence for the public hostnames of aoc-codex.app.
#
# Written for AOC-014's acceptance criteria, kept in the repo because watch mode
# (product_management/workflows/6-watch.md), AOC-026's cache rules and every release
# re-ask exactly these questions. It only ever READS: no DNS change, no Railway call,
# no credential. Safe to run from anywhere, any time.
#
# Usage:  scripts/check-hostnames.sh [canonical-host]
#         canonical-host defaults to aoc-codex.app, the apex (AOC-014, 2026-09-21).
#
# Exit 0 if every check passed, 1 otherwise. Each line is PASS / FAIL / INFO so the
# output can be pasted into a ticket as evidence.

set -uo pipefail

ZONE=aoc-codex.app
CANON=${1:-$ZONE}
case "$CANON" in
  "$ZONE")      OTHER="www.$ZONE" ;;
  "www.$ZONE")  OTHER="$ZONE" ;;
  *) echo "canonical host must be $ZONE or www.$ZONE, got: $CANON" >&2; exit 2 ;;
esac
IMG=img.$ZONE
RESOLVER=1.1.1.1

# Two real keys from armory_snapshot/tooltips_upload_manifest.csv, with their true
# byte counts. The bracketed one is deliberate: it is the URL-encoding trap.
KEY_PLAIN='armory/a_fathers_devotion.jpg';                    BYTES_PLAIN=18505
KEY_ENC='armory/%5Bcenterpiece_legion_commander%5D.jpg';      BYTES_ENC=5286

fails=0
pass() { printf '  \033[32mPASS\033[0m  %s\n' "$*"; }
fail() { printf '  \033[31mFAIL\033[0m  %s\n' "$*"; fails=$((fails+1)); }
info() { printf '  ----  %s\n' "$*"; }
head_() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# probe URL -> prints "code|content_type|redirect_url".
# The separator is '|', not a tab: a tab is IFS-whitespace, so bash's `read` collapses
# two adjacent tabs into one delimiter and an empty Content-Type (every 301 has one)
# shifts the redirect target into $ctype and leaves $loc empty. Measured, not guessed.
probe() { curl -sS -o /dev/null -w '%{http_code}|%{content_type}|%{redirect_url}' --max-time 20 "$1" 2>/dev/null; }
hdr()   { curl -sS -o /dev/null -D- --max-time 20 "$1" 2>/dev/null | tr -d '\r'; }
# Same as probe(), but pinned to an address so a stale local resolver cannot answer for it.
probe_at() { if [ -n "$2" ]; then curl -sS -o /dev/null -w '%{http_code}|%{content_type}|%{redirect_url}' --max-time 20 --resolve "$1:443:$2" --resolve "$1:80:$2" "$3" 2>/dev/null; else probe "$3"; fi; }

head_ "1. Zone nameservers (criterion 1)"
ns=$(dig +short NS "$ZONE" @$RESOLVER | sort | tr '\n' ' ')
case "$ns" in
  *ns.cloudflare.com*) pass "NS on Cloudflare: $ns" ;;
  *)                   fail "NS not on Cloudflare: ${ns:-<empty>}" ;;
esac

head_ "2. DNS and TLS on both site hostnames (criteria 2, 8)"
for h in "$CANON" "$OTHER"; do
  a=$(dig +short A "$h" @$RESOLVER | grep -E '^[0-9]' | tr '\n' ' ')
  [ -n "$a" ] && pass "$h resolves: $a" || fail "$h does not resolve"
  # Ask an authoritative-ish resolver directly, then pin curl to that address.
  # ⚠️ The trap this avoids: this machine's OWN resolver caches the pre-proxy address for
  # the record's old TTL, so right after flipping the orange cloud `curl` still reaches
  # Railway and the check reports "not proxied" when the change was in fact applied.
  # Measured on 2026-09-21: dig said 104.21.34.205, dscacheutil still said 69.46.46.106.
  ip=$(dig +short A "$h" @$RESOLVER | grep -E '^[0-9]' | head -1)
  if [ -n "$ip" ] && curl -sS -o /dev/null -D- --max-time 20 --resolve "$h:443:$ip" "https://$h/health" 2>/dev/null | tr -d '\r' | grep -qi '^server: cloudflare'; then
    pass "$h is PROXIED by Cloudflare (server: cloudflare via $ip)"
  else
    fail "$h is NOT proxied by Cloudflare — AOC-026's cache rules need the orange cloud"
  fi
  sysip=$(dscacheutil -q host -a name "$h" 2>/dev/null | awk '/^ip_address/{print $2; exit}')
  if [ -n "$sysip" ] && [ -n "$ip" ] && [ "$sysip" != "$ip" ]; then
    info "  NOTE: this machine still resolves $h to $sysip, authoritative says $ip — stale local cache, not a fault"
  fi
  if curl -sS -o /dev/null --max-time 20 "https://$h/health" 2>/dev/null; then
    pass "$h serves valid TLS (certificate verified by curl)"
  else
    fail "$h TLS invalid or absent: $(curl -sS -o /dev/null --max-time 20 "https://$h/health" 2>&1 | head -1)"
  fi
done

head_ "3. The canonical host serves the application (criteria 3, 4)"
IFS='|' read -r code ctype _ <<<"$(probe "https://$CANON/health")"
[ "$code" = 200 ] && pass "https://$CANON/health -> 200" || fail "https://$CANON/health -> $code"
body=$(curl -sS --max-time 20 "https://$CANON/health" 2>/dev/null)
case "$body" in *'"status":"ok"'*) pass "health body: $body" ;; *) fail "health body unexpected: ${body:-<empty>}" ;; esac

IFS='|' read -r code ctype _ <<<"$(probe "https://$CANON/")"
if [ "$code" = 200 ] && [ "${ctype#text/html}" != "$ctype" ]; then
  pass "https://$CANON/ -> 200 $ctype (server-rendered HTML)"
else
  fail "https://$CANON/ -> $code $ctype (expected 200 text/html)"
fi
# Server-rendered means the markup is in the first response, not fetched by a script.
n=$(curl -sS --max-time 20 "https://$CANON/" 2>/dev/null | grep -ciE '<(h1|main|table|article|ul)\b')
[ "${n:-0}" -gt 0 ] && pass "  markup present without JavaScript ($n structural tags)" \
                    || info "  no structural tags yet — the site is still the EP-01 skeleton"

IFS='|' read -r code ctype _ <<<"$(probe "https://$CANON/v1/items")"
case "$ctype" in
  application/json*) pass "https://$CANON/v1/items -> $code $ctype (JSON, same origin)" ;;
  *) info "https://$CANON/v1/items -> $code $ctype — endpoint lands in AOC-012, not this ticket" ;;
esac

head_ "4. The non-canonical host 301s to the canonical one (criterion 5)"
oip=$(dig +short A "$OTHER" @$RESOLVER | grep -E '^[0-9]' | head -1)
IFS='|' read -r code ctype loc <<<"$(probe_at "$OTHER" "$oip" "https://$OTHER/health")"
if [ "$code" = 301 ] && [ "${loc#https://$CANON}" != "$loc" ]; then
  pass "https://$OTHER/health -> 301 $loc"
elif [ "$code" = 301 ]; then
  fail "https://$OTHER/health -> 301 but to $loc (expected https://$CANON/...)"
else
  fail "https://$OTHER/health -> $code (expected a 301 to $CANON, not a second indexable copy)"
fi

head_ "5. Tooltip images on $IMG (criterion 6)"
for pair in "$KEY_PLAIN:$BYTES_PLAIN" "$KEY_ENC:$BYTES_ENC"; do
  key=${pair%:*}; want=${pair##*:}
  h=$(hdr "https://$IMG/$key")
  code=$(printf '%s' "$h" | awk 'NR==1{print $2}')
  ct=$(printf  '%s' "$h" | awk -F': ' 'tolower($1)=="content-type"{print $2}')
  cl=$(printf  '%s' "$h" | awk -F': ' 'tolower($1)=="content-length"{print $2}')
  cc=$(printf  '%s' "$h" | awk -F': ' 'tolower($1)=="cache-control"{print $2}')
  if [ "$code" = 200 ] && [ "$ct" = image/jpeg ]; then
    pass "$key -> 200 $ct"
  else
    fail "$key -> $code ${ct:-<no content-type>}"
  fi
  # The byte count is what proves it is the RIGHT image, not merely an image.
  if [ -n "$cl" ]; then
    [ "$cl" = "$want" ] && pass "  $cl bytes, matches the upload manifest" \
                        || fail "  $cl bytes, manifest says $want"
  else
    info "  no content-length header to compare against the manifest"
  fi
  case "$cc" in
    *max-age=31536000*) pass "  cache-control: $cc" ;;
    "")                 fail "  no cache-control header" ;;
    *)                  fail "  cache-control too short: $cc" ;;
  esac
done

head_ "6. Plain HTTP is upgraded, never served (criterion 7)"
# .app is HSTS-preloaded, so browsers upgrade on their own. curl and bots do not,
# which is exactly why this is measured rather than assumed.
for h in "$CANON" "$OTHER" "$IMG"; do
  case "$h" in "$IMG") path="/$KEY_PLAIN" ;; *) path=/health ;; esac
  IFS='|' read -r code ctype loc <<<"$(probe "http://$h$path")"
  case "$code" in
    301|302|307|308) pass "http://$h$path -> $code $loc" ;;
    000)             fail "http://$h$path -> no answer" ;;
    *)               fail "http://$h$path -> $code ${ctype} — SERVED in plaintext, not upgraded" ;;
  esac
done

head_ "7. The bucket is reachable only through our own name (criterion 10)"
# A bare prefix must not list the bucket's contents.
IFS='|' read -r code ctype _ <<<"$(probe "https://$IMG/armory/")"
[ "$code" = 200 ] && fail "https://$IMG/armory/ -> 200 — the bucket appears world-listable" \
                  || pass "https://$IMG/armory/ -> $code (not listable)"
info "r2.dev development URL: checked out-of-band via the Cloudflare API (domains/managed -> enabled:false), not from here"

head_ "Result"
if [ "$fails" -eq 0 ]; then
  printf '  \033[32mall checks passed\033[0m (canonical: %s)\n\n' "$CANON"; exit 0
else
  printf '  \033[31m%d check(s) failed\033[0m (canonical: %s)\n\n' "$fails" "$CANON"; exit 1
fi
