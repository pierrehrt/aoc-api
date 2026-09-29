#!/bin/bash
# Tests for switch.sh and rollback.sh: the real scripts, run against a fake `curl` that plays the
# Cloudflare API from a small state file, so every starting state can be driven without touching
# Cloudflare. Run by `make worker-test`.
#
# Why it exists: on 2026-09-28 switch.sh stopped halfway in production — a 3.12-only f-string, run
# by macOS's /usr/bin/python3 (3.9) — and took img.aoc-codex.app off DNS (AOC-041). So the scripts
# run here under the interpreter in $PYTHON (default: python3 on PATH); on a Mac, run
#   PYTHON=/usr/bin/python3 make worker-test
# to test the one that Pierre's shell actually uses.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
PYTHON=${PYTHON:-$(command -v python3)}
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/home/.config/aoc-codex"
echo 'CLOUDFLARE_API_TOKEN=test-token' > "$work/home/.config/aoc-codex/cloudflare.env"
ln -s "$PYTHON" "$work/bin/python3"
printf '#!/bin/sh\nexit 0\n' > "$work/bin/sleep"; chmod +x "$work/bin/sleep"

# The fake curl. State: r2 (custom domain attached), worker (Workers domain attached), script (the
# Worker exists), dns (records on the host), linger / wlinger (detaching R2 / the Worker leaves its
# record in the next n DNS answers; 99 = for good), appear (records that show up when R2 is
# detached, as if someone added one mid-run), broken ({path: n} — that GET answers n times, then
# success:false, so a lookup can fail partway through a run), reply ({"METHOD path": [status,
# body]} — that call still takes effect but answers exactly this, so a script that carries on past
# a failure reply shows up in the writes that follow). With -w it appends the status, as
# curl does. Detaching a Workers domain answers 200 with an empty body: live, the reply was not
# JSON and the detach took effect (2026-09-28); the status itself was not captured. Writes refuse to overwrite, as a conservative stand-in for
# Cloudflare: attaching over a live record or binding fails.
cat > "$work/bin/curl" <<'PY'
#!/usr/bin/env python3
import json, os, sys, urllib.parse
args = sys.argv[1:]; method = "GET"; url = ""; wfmt = ""
for i, a in enumerate(args):
    if a == "-X": method = args[i + 1]
    if a == "-w": wfmt = args[i + 1]
    if a.startswith("https://"): url = a
path = urllib.parse.urlparse(url).path.replace("/client/v4", "", 1)
query = urllib.parse.parse_qs(urllib.parse.urlparse(url).query)
st = json.load(open(os.environ["STUB_STATE"]))
open(os.environ["STUB_LOG"], "a").write("%s %s\n" % (method, path))
HOST, ACC, ZONE = "img.aoc-codex.app", "acct", "9a60a586d20fe4ed78079b158b19cb1e"
R2 = "/accounts/%s/r2/buckets/aoc-codex-enam/domains/custom" % ACC
WD = "/accounts/%s/workers/domains" % ACC
DNS = "/zones/%s/dns_records" % ZONE
def ok(result=None): return {"success": True, "errors": [], "result": result}
def no(msg): return {"success": False, "errors": [{"message": msg}], "result": None}
EMPTY = object()
def answer(out):
    status, body = (200, "") if out is EMPTY else (200 if out["success"] else 400, json.dumps(out))
    sys.stdout.write(body + ("\n%d" % status if "%{http_code}" in wfmt else ""))
seen = st.setdefault("seen", {}); seen[path] = seen.get(path, 0) + 1
if method == "GET" and path in st["broken"] and seen[path] > st["broken"][path]:
    out = no("simulated outage")
elif method == "GET" and path == "/zones/" + ZONE:
    out = ok({"account": {"id": ACC}})
elif method == "GET" and path == "/accounts/%s/workers/scripts/aoc-img/settings" % ACC:
    out = ok({}) if st["script"] else no("script not found")
elif method == "GET" and path == R2:
    out = ok({"domains": [{"domain": HOST}] if st["r2"] else []})
elif method == "DELETE" and path == R2 + "/" + HOST:
    if not st["r2"]: out = no("domain not found")
    else:
        st["r2"] = False; out = ok({})
        for r in st["dns"]:
            if r["content"] == "public.r2.dev": r["left"] = st["linger"]
        st["dns"] = [r for r in st["dns"] if r.get("left", 1) > 0] + st["appear"]
elif method == "POST" and path == R2:
    if st["r2"] or st["worker"] or st["dns"]: out = no("hostname in use")
    else:
        st["r2"] = True; st["dns"] = [{"id": "r2cname", "type": "CNAME", "content": "public.r2.dev"}]; out = ok({})
elif method == "GET" and path == WD:
    hits = [{"id": "wd1", "hostname": HOST}] if st["worker"] and query.get("hostname") == [HOST] else []
    out = ok(hits)
elif method == "PUT" and path == WD:
    if st["r2"] or st["worker"] or st["dns"]: out = no("hostname in use")
    else:
        st["worker"] = True; st["dns"] = [{"id": "wkaaaa", "type": "AAAA", "content": "100::"}]; out = ok({})
elif method == "DELETE" and path == WD + "/wd1":
    if not st["worker"]: out = no("not found")
    else:
        st["worker"] = False; out = EMPTY
        for r in st["dns"]:
            if r["id"] == "wkaaaa": r["left"] = st["wlinger"]
        st["dns"] = [r for r in st["dns"] if r.get("left", 1) > 0]
elif method == "GET" and path == DNS:
    out = ok([dict((k, v) for k, v in r.items() if k != "left") for r in st["dns"]] if query.get("name") == [HOST] else [])
    for r in st["dns"]:
        if "left" in r: r["left"] -= 1
    st["dns"] = [r for r in st["dns"] if r.get("left", 1) > 0]
elif method == "DELETE" and path.startswith(DNS + "/"):
    rid = path.rsplit("/", 1)[1]; before = len(st["dns"])
    st["dns"] = [r for r in st["dns"] if r["id"] != rid]
    out = ok({}) if len(st["dns"]) < before else no("record not found")
else:
    out = no("fake curl: no route for %s %s" % (method, path))
json.dump(st, open(os.environ["STUB_STATE"], "w"))
if "%s %s" % (method, path) in st["reply"]:
    status, body = st["reply"]["%s %s" % (method, path)]
    sys.stdout.write(body + ("\n%d" % status if "%{http_code}" in wfmt else ""))
else:
    answer(out)
PY
chmod +x "$work/bin/curl"

pass=0; fail=0
CNAME='{"id":"r2cname","type":"CNAME","content":"public.r2.dev"}'
WREC='{"id":"wkaaaa","type":"AAAA","content":"100::"}'
OTHER='{"id":"x","type":"A","content":"192.0.2.1"}'
R2P='"/accounts/acct/r2/buckets/aoc-codex-enam/domains/custom"'
WDP='"/accounts/acct/workers/domains"'
DNSP='"/zones/9a60a586d20fe4ed78079b158b19cb1e/dns_records"'
# run <name> <script> <state> <want exit> <want r2> <want worker> <want writes, in order, e.g. "DELETE PUT">
run() {
  local name=$1 script=$2 state=$3 want_rc=$4 want_r2=$5 want_worker=$6 want_writes=$7
  printf '%s' "$state" > "$work/state.json"; : > "$work/log"
  local out rc got
  out=$(cd "$here" && HOME="$work/home" PATH="$work/bin:$PATH" STUB_STATE="$work/state.json" STUB_LOG="$work/log" \
    /bin/bash "./$script" 2>&1); rc=$?
  got=$("$PYTHON" -c 'import json,sys
st=json.load(open(sys.argv[1]))
w=[l.split()[0] for l in open(sys.argv[2]) if l.split()[0] in ("PUT","POST","DELETE")]
print("%d %s %s %s" % (int(sys.argv[3]), str(st["r2"]).lower(), str(st["worker"]).lower(), " ".join(w) or "-"))' \
    "$work/state.json" "$work/log" "$rc")
  local want="$want_rc $want_r2 $want_worker ${want_writes:--}"
  if [ "$got" = "$want" ]; then pass=$((pass + 1)); echo "ok   $name"
  else
    fail=$((fail + 1)); echo "FAIL $name"; echo "     got  exit/r2/worker/writes: $got"; echo "     want exit/r2/worker/writes: $want"
    printf '%s\n' "$out" | sed 's/^/     | /'
  fi
}
# s <r2> <worker> <script> <dns records> <linger> <wlinger> <broken> [appear] [reply]
s() { printf '{"r2":%s,"worker":%s,"script":%s,"dns":[%s],"linger":%s,"wlinger":%s,"broken":{%s},"appear":[%s],"reply":{%s}}' \
  "$1" "$2" "$3" "$4" "$5" "$6" "$7" "${8:-}" "${9:-}"; }
BAD200='"DELETE /accounts/acct/workers/domains/wd1":[200,"{\"success\":false,\"errors\":[]}"]'
BAD500='"DELETE /accounts/acct/workers/domains/wd1":[500,""]'

echo "python3 under test: $("$PYTHON" --version 2>&1)"
run "switch: from the R2 custom domain"               switch.sh   "$(s true  false true  "$CNAME" 0 0 '')" 0 false true "DELETE PUT"
run "switch: R2 leaves its CNAME behind"               switch.sh   "$(s true  false true  "$CNAME" 99 0 '')" 0 false true "DELETE DELETE PUT"
run "switch: R2's CNAME takes a few looks to go"      switch.sh   "$(s true  false true  "$CNAME" 3 0 '')" 0 false true "DELETE PUT"
run "switch: a record appears mid-run, stops"          switch.sh   "$(s true  false true  "$CNAME" 0 0 '' "$OTHER")" 1 false false "DELETE"
run "switch: re-run after stopping halfway"            switch.sh   "$(s false false true  ''       0 0 '')" 0 false true "PUT"
run "switch: already on the Worker, writes nothing"    switch.sh   "$(s false true  true  "$WREC"  0 0 '')" 0 false true ""
run "switch: a record it did not make, writes nothing" switch.sh   "$(s true  false true  "$OTHER" 0 0 '')" 1 true  false ""
run "switch: R2's CNAME plus another, writes nothing"  switch.sh   "$(s true  false true  "$CNAME,$OTHER" 0 0 '')" 1 true false ""
run "switch: no Worker script, writes nothing"         switch.sh   "$(s true  false false "$CNAME" 0 0 '')" 1 true  false ""
run "switch: R2 list unanswered, writes nothing"       switch.sh   "$(s true  false true  "$CNAME" 0 0 "$R2P:0")" 1 true false ""
run "switch: Workers list unanswered, writes nothing"  switch.sh   "$(s true  false true  "$CNAME" 0 0 "$WDP:0")" 1 true false ""
run "switch: DNS list unanswered, writes nothing"      switch.sh   "$(s true  false true  "$CNAME" 0 0 "$DNSP:0")" 1 true false ""
run "switch: DNS dies after the detach, no Worker"     switch.sh   "$(s true  false true  "$CNAME" 0 0 "$DNSP:1")" 1 false false "DELETE"
run "rollback: from the Worker"                        rollback.sh "$(s false true  true  "$WREC"  0 0 '')" 0 true  false "DELETE POST"
run "rollback: re-run after stopping halfway"          rollback.sh "$(s false false true  ''       0 0 '')" 0 true  false "POST"
run "rollback: already on R2, writes nothing"          rollback.sh "$(s true  false true  "$CNAME" 0 0 '')" 0 true  false ""
run "rollback: a record it did not make, nothing"      rollback.sh "$(s false true  true  "$WREC,$OTHER" 0 0 '')" 1 false true ""
run "rollback: Worker's record takes a few looks"     rollback.sh "$(s false true  true  "$WREC"  0 3 '')" 0 true  false "DELETE POST"
run "rollback: Worker's record lingers, no attach"     rollback.sh "$(s false true  true  "$WREC"  0 99 '')" 1 false false "DELETE"
run "rollback: detach says 200 success:false, stops"  rollback.sh "$(s false true  true  "$WREC"  0 0 '' '' "$BAD200")" 1 false false "DELETE"
run "rollback: detach says 500 and no body, stops"     rollback.sh "$(s false true  true  "$WREC"  0 0 '' '' "$BAD500")" 1 false false "DELETE"
run "rollback: Workers list unanswered, nothing"       rollback.sh "$(s false true  true  "$WREC"  0 0 "$WDP:0")" 1 false true ""
run "rollback: R2 list unanswered, nothing"            rollback.sh "$(s false true  true  "$WREC"  0 0 "$R2P:0")" 1 false true ""
run "rollback: DNS dies after the detach, no attach"   rollback.sh "$(s false true  true  "$WREC"  0 0 "$DNSP:1")" 1 false false "DELETE"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
