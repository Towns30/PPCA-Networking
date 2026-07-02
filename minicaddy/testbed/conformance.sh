#!/usr/bin/env bash
# Core conformance checks for minicaddy. This is the grading FLOOR — also diff
# against real Caddy and read the parser/keep-alive code by hand.
#
# Usage: ./conformance.sh              (assumes :8080 static, :8081 proxy w/ auth)
#        ./conformance.sh HOST PORT PROXYPORT
set -u
HOST=${1:-127.0.0.1}
PORT=${2:-8080}
PXP=${3:-8081}
BASE="http://$HOST:$PORT"
PROXY="http://$HOST:$PXP"
AUTH="admin:secret"

pass=0; fail=0
ok()   { echo "  PASS: $1"; pass=$((pass+1)); }
no()   { echo "  FAIL: $1"; fail=$((fail+1)); }
check(){ local d="$1"; shift; if "$@"; then ok "$d"; else no "$d"; fi; }

# Prepare a known static file.
ROOT=$(dirname "$0")/../www
printf 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789' > "$ROOT/_conf.txt"
trap 'rm -f "$ROOT/_conf.txt"' EXIT

echo "== static =="
check "GET 200"          bash -c "curl -sf $BASE/ >/dev/null"
check "index is text/html" bash -c "curl -sI $BASE/ | grep -qi 'content-type: text/html'"
check "Content-Length set" bash -c "curl -sI $BASE/_conf.txt | grep -qi 'content-length: 36'"
check "HEAD has no body"  bash -c "[ -z \"\$(curl -s --head $BASE/_conf.txt -o /dev/null -w '%{size_download}' | tr -d 0)\" ]"
check "404 for missing"   bash -c "[ \$(curl -s -o /dev/null -w '%{http_code}' $BASE/nope) = 404 ]"
check "path traversal blocked" bash -c "[ \$(curl -s -o /dev/null -w '%{http_code}' --path-as-is $BASE/../../etc/passwd) != 200 ]"

echo "== range / conditional =="
check "206 Partial"       bash -c "[ \$(curl -s -o /dev/null -w '%{http_code}' -H 'Range: bytes=5-9' $BASE/_conf.txt) = 206 ]"
check "range body correct" bash -c "[ \"\$(curl -s -H 'Range: bytes=5-9' $BASE/_conf.txt)\" = FGHIJ ]"
check "suffix range"      bash -c "[ \"\$(curl -s -H 'Range: bytes=-4' $BASE/_conf.txt)\" = 6789 ]"
check "304 If-None-Match" bash -c 'E=$(curl -sI '"$BASE"'/_conf.txt | awk -F": " "tolower(\$1)==\"etag\"{print \$2}" | tr -d "\r"); [ $(curl -s -o /dev/null -w "%{http_code}" -H "If-None-Match: $E" '"$BASE"'/_conf.txt) = 304 ]'

echo "== framing / keep-alive =="
check "chunked resp on gzip" bash -c "curl -sD - -o /dev/null --compressed $BASE/ | grep -qi 'transfer-encoding: chunked'"
check "gzip encoding"     bash -c "curl -sD - -o /dev/null --compressed $BASE/ | grep -qi 'content-encoding: gzip'"
check "keep-alive reuse"  bash -c "[ \$(curl -s -o /dev/null -w '%{num_connects}\n' $BASE/_conf.txt $BASE/ | awk '{s+=\$1} END{print s}') -eq 1 ]"
check "chunked req + pipelined GET" bash -c "[ \$(printf 'POST /_conf.txt HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\nGET /_conf.txt HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n' | nc $HOST $PORT | grep -c 'HTTP/1.1') -eq 2 ]"

echo "== proxy / middleware (needs :$PXP proxy w/ basic_auth $AUTH) =="
check "401 without auth"  bash -c "[ \$(curl -s -o /dev/null -w '%{http_code}' $PROXY/) = 401 ]"
check "WWW-Authenticate"  bash -c "curl -sI $PROXY/ | grep -qi 'www-authenticate: basic'"
check "proxy 200 with auth" bash -c "[ \$(curl -s -o /dev/null -w '%{http_code}' -u $AUTH $PROXY/index.html) = 200 ]"

echo
echo "RESULT: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
