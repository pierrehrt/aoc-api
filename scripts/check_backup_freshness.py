#!/usr/bin/env python3
"""The dead man's switch for the scheduled backup (AOC-030).

Lists the backup bucket and FAILS when the newest object is too old, too small, or absent.
Run daily by .github/workflows/backup-freshness.yml; its failure notification IS the alarm.

⭐ WHY THIS EXISTS AND A DEPLOY NOTIFICATION DOES NOT REPLACE IT.
The failure that actually happens is that the cron never fires at all. There is no deploy, no
error and no log line for that — the only evidence is an absence, and absence is exactly what
this project keeps finding read as success (AOC-006's restore "matched" an empty production;
bin/gate spent three tickets removing the same shape). So the check is phrased the other way
round: it must SEE a recent, plausible object, or it goes red.

⚠️ It reads with a READ-ONLY credential. A checker that can delete what it is checking is one
typo away from being the outage.

Why Python stdlib rather than rclone, when Dockerfile.backup uses rclone: this makes one
ListObjectsV2 GET, which is ~40 lines to sign (the pattern armory_snapshot/upload_tooltips.py
already uses against this same account), whereas the backup job PUTs a file that will one day
need multipart — which is worth a pinned binary. Each side uses the cheaper tool for its own
job; neither is a second way of doing the same thing.

Usage:
    check_backup_freshness.py                     # uses the env below
    check_backup_freshness.py --max-age-hours 48 --min-bytes 1024
    check_backup_freshness.py --prefix does-not-exist/   # how criterion 7 is proven

Env:
    R2_S3_ENDPOINT, R2_BACKUP_BUCKET,
    R2_READONLY_ACCESS_KEY_ID, R2_READONLY_SECRET_ACCESS_KEY
    BACKUP_PREFIX (default "prod/")
"""
import argparse
import datetime as dt
import hashlib
import hmac
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

S3_NS = {"s3": "http://s3.amazonaws.com/doc/2006-03-01/"}
REGION = "auto"          # R2 signs with "auto", not a real AWS region
SERVICE = "s3"


class Obj:
    """One object in the bucket. Kept tiny so the tests can build them by hand."""

    def __init__(self, key, size, last_modified):
        self.key = key
        self.size = size
        self.last_modified = last_modified

    def __repr__(self):
        return f"Obj({self.key!r}, {self.size}, {self.last_modified.isoformat()})"


# ---------------------------------------------------------------------------
# The decision. A pure function of (objects, now) so it can be tested without a network,
# a credential or a clock — a script that decides whether the backup happened is exactly the
# code this project does not trust to reasoning alone (DECISIONS.md, 2026-09-15).
# ---------------------------------------------------------------------------
def judge(objects, now, max_age_hours, min_bytes, prefix=""):
    """Return (ok: bool, lines: list[str]). Never raises."""
    if not objects:
        return False, [
            f"NO OBJECTS under {prefix!r}.",
            "The bucket is empty, the prefix is wrong, or the job has never run.",
            "An empty listing is a FAILURE here, not a vacuous pass.",
        ]

    newest = max(objects, key=lambda o: o.last_modified)
    age = now - newest.last_modified
    age_h = age.total_seconds() / 3600.0

    lines = [
        f"newest object : {newest.key}",
        f"age           : {age_h:.1f} h  (limit {max_age_hours} h)",
        f"size          : {newest.size} bytes  (floor {min_bytes})",
        f"objects held  : {len(objects)}",
    ]

    problems = []
    if age_h > max_age_hours:
        problems.append(
            f"STALE: the newest backup is {age_h:.1f} h old, older than the {max_age_hours} h limit. "
            "The cron has stopped firing, or the job is failing."
        )
    # ⚠️ A future timestamp is not "fresh" — it means a clock is wrong somewhere, and a clock
    # that is wrong is how a stale backup reads as recent.
    if age.total_seconds() < -300:
        problems.append(
            f"IMPOSSIBLE: the newest object is dated {-age_h:.1f} h in the FUTURE. "
            "Do not trust this listing; check the clock on whatever wrote it."
        )
    if newest.size < min_bytes:
        problems.append(
            f"TOO SMALL: {newest.size} bytes is below the {min_bytes}-byte floor. "
            "A job that uploaded an empty file or an error message must not read as success."
        )

    return (not problems), lines + problems


# ---------------------------------------------------------------------------
# SigV4 — enough of it for one ListObjectsV2 GET.
# ---------------------------------------------------------------------------
def _sign(key, msg):
    return hmac.new(key, msg.encode(), hashlib.sha256).digest()


def _signing_key(secret, datestamp):
    k = _sign(f"AWS4{secret}".encode(), datestamp)
    k = _sign(k, REGION)
    k = _sign(k, SERVICE)
    return _sign(k, "aws4_request")


def list_objects(endpoint, bucket, prefix, access_key, secret_key, timeout=30):
    """Every object under prefix, following continuation tokens."""
    host = urllib.parse.urlparse(endpoint).netloc
    out, token = [], None

    while True:
        params = {"list-type": "2", "prefix": prefix, "max-keys": "1000"}
        if token:
            params["continuation-token"] = token
        # The canonical query string is sorted and percent-encoded.
        canonical_qs = "&".join(
            f"{urllib.parse.quote(k, safe='-_.~')}={urllib.parse.quote(v, safe='-_.~')}"
            for k, v in sorted(params.items())
        )

        now = dt.datetime.now(dt.timezone.utc)
        amzdate = now.strftime("%Y%m%dT%H%M%SZ")
        datestamp = now.strftime("%Y%m%d")
        payload_hash = hashlib.sha256(b"").hexdigest()

        canonical_uri = f"/{urllib.parse.quote(bucket, safe='')}"
        canonical_headers = (
            f"host:{host}\n"
            f"x-amz-content-sha256:{payload_hash}\n"
            f"x-amz-date:{amzdate}\n"
        )
        signed_headers = "host;x-amz-content-sha256;x-amz-date"
        canonical_request = (
            f"GET\n{canonical_uri}\n{canonical_qs}\n"
            f"{canonical_headers}\n{signed_headers}\n{payload_hash}"
        )

        scope = f"{datestamp}/{REGION}/{SERVICE}/aws4_request"
        string_to_sign = (
            f"AWS4-HMAC-SHA256\n{amzdate}\n{scope}\n"
            f"{hashlib.sha256(canonical_request.encode()).hexdigest()}"
        )
        signature = hmac.new(
            _signing_key(secret_key, datestamp), string_to_sign.encode(), hashlib.sha256
        ).hexdigest()

        req = urllib.request.Request(
            f"{endpoint.rstrip('/')}{canonical_uri}?{canonical_qs}",
            method="GET",
            headers={
                "Host": host,
                "x-amz-content-sha256": payload_hash,
                "x-amz-date": amzdate,
                "Authorization": (
                    f"AWS4-HMAC-SHA256 Credential={access_key}/{scope}, "
                    f"SignedHeaders={signed_headers}, Signature={signature}"
                ),
            },
        )
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read()

        root = ET.fromstring(body)
        for c in root.findall("s3:Contents", S3_NS):
            key = c.findtext("s3:Key", default="", namespaces=S3_NS)
            size = int(c.findtext("s3:Size", default="0", namespaces=S3_NS))
            lm = c.findtext("s3:LastModified", default="", namespaces=S3_NS)
            # "2026-09-22T03:00:01.000Z" → aware datetime
            stamp = dt.datetime.fromisoformat(lm.replace("Z", "+00:00"))
            out.append(Obj(key, size, stamp))

        if root.findtext("s3:IsTruncated", default="false", namespaces=S3_NS) != "true":
            return out
        token = root.findtext("s3:NextContinuationToken", default="", namespaces=S3_NS)
        if not token:
            return out


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--max-age-hours", type=float, default=48.0)
    ap.add_argument("--min-bytes", type=int, default=1024)
    ap.add_argument("--prefix", default=os.environ.get("BACKUP_PREFIX", "prod/"))
    args = ap.parse_args()

    missing = [
        n
        for n in (
            "R2_S3_ENDPOINT",
            "R2_BACKUP_BUCKET",
            "R2_READONLY_ACCESS_KEY_ID",
            "R2_READONLY_SECRET_ACCESS_KEY",
        )
        if not os.environ.get(n)
    ]
    if missing:
        # ⚠️ Not "skip". A check that cannot look must not be able to pass — that is the exact
        # defect AOC-032 removed from bin/gate.
        print(f"CANNOT CHECK: these are not set: {', '.join(missing)}", file=sys.stderr)
        print("A check that could not look is a FAILURE, not a pass.", file=sys.stderr)
        return 2

    endpoint = os.environ["R2_S3_ENDPOINT"]
    bucket = os.environ["R2_BACKUP_BUCKET"]
    print(f"checking {bucket}/{args.prefix}")

    try:
        objects = list_objects(
            endpoint,
            bucket,
            args.prefix,
            os.environ["R2_READONLY_ACCESS_KEY_ID"],
            os.environ["R2_READONLY_SECRET_ACCESS_KEY"],
        )
    except urllib.error.HTTPError as e:
        print(f"CANNOT CHECK: R2 answered {e.code} {e.reason}", file=sys.stderr)
        return 2
    except Exception as e:  # noqa: BLE001 — any failure to look is a failure, loudly
        print(f"CANNOT CHECK: {type(e).__name__}: {e}", file=sys.stderr)
        return 2

    ok, lines = judge(
        objects,
        dt.datetime.now(dt.timezone.utc),
        args.max_age_hours,
        args.min_bytes,
        args.prefix,
    )
    for line in lines:
        print(("  " if ok else "  !! ") + line)

    if ok:
        print("✅ the backup is fresh")
        return 0
    print("❌ BACKUP ALARM — see above", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
