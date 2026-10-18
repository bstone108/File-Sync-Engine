#!/bin/sh
# Two isolated engines on a throttled link. Large files must take longer than
# the old 8s total HTTP timeout, and thousands of small files must still arrive.
set -eu

cd "$(dirname "$0")"
BIG_COUNT="${SOAK_BIG_FILES:-5}"
BIG_MB="${SOAK_BIG_MB:-40}"
SMALL_COUNT="${SOAK_SMALL_FILES:-2500}"
TIMEOUT="${SOAK_TIMEOUT:-720}"

cleanup() {
  docker compose -f compose.yaml logs --tail 80 || true
  docker compose -f compose.yaml down -v || true
}
trap cleanup EXIT

docker compose -f compose.yaml up -d

echo "waiting for engines"
i=0
while [ "$i" -lt 60 ]; do
  if docker compose -f compose.yaml exec -T soak-a sh -c 'test -s /config/config.jsonc' \
    && docker compose -f compose.yaml exec -T soak-b sh -c 'test -s /config/config.jsonc'; then
    break
  fi
  i=$((i + 1))
  sleep 2
done

api_key() {
  docker compose -f compose.yaml exec -T "$1" cat /config/config.jsonc | python3 -c '
import json, re, sys
text = re.sub(r"(^|\s)//.*", "", sys.stdin.read())
print(json.loads(text)["api"]["key"])
'
}

KEY_A="$(api_key soak-a)"
KEY_B="$(api_key soak-b)"

wait_api() {
  name="$1"
  port="$2"
  key="$3"
  i=0
  while [ "$i" -lt 40 ]; do
    if curl -kfsS -H "X-FSE-API-Key: $key" "https://127.0.0.1:${port}/v1/status" >/dev/null; then
      return 0
    fi
    i=$((i + 1))
    sleep 2
  done
  echo "API $name did not start" >&2
  return 1
}
wait_api soak-a 22440 "$KEY_A"
wait_api soak-b 22441 "$KEY_B"

for side in soak-a soak-b; do
  if ! docker compose -f compose.yaml exec -T "$side" tc qdisc show | grep -q tbf; then
    echo "$side has no throttled qdisc" >&2
    docker compose -f compose.yaml exec -T "$side" tc qdisc show >&2 || true
    exit 1
  fi
done

post() {
  port="$1"
  key="$2"
  path="$3"
  body="$4"
  curl -kfsS -H "X-FSE-API-Key: $key" -H "Content-Type: application/json" -d "$body" "https://127.0.0.1:${port}${path}"
}

echo "pairing"
code=""
i=0
while [ "$i" -lt 5 ] && [ -z "$code" ]; do
  code="$(post 22440 "$KEY_A" /v1/pair-code '{}' | python3 -c 'import json,sys; print(json.load(sys.stdin).get("code",""))')"
  i=$((i + 1))
  sleep 1
done
if [ -z "$code" ]; then
  echo "pairing code was not issued" >&2
  exit 1
fi

paired=""
i=0
while [ "$i" -lt 8 ]; do
  paired="$(post 22441 "$KEY_B" /v1/pair-code/redeem "{\"code\":\"$code\"}" || true)"
  echo "$paired"
  echo "$paired" | grep -qi 'paired by code' && break
  i=$((i + 1))
  sleep 3
done
echo "$paired" | grep -qi 'paired by code'

echo "adding shared folder"
post 22440 "$KEY_A" /v1/folder-command '{"action":"add","id":"shared","path":"/data/shared","mode":"sendrecv"}' >/dev/null
post 22441 "$KEY_B" /v1/folder-command '{"action":"add","id":"shared","path":"/data/shared","mode":"sendrecv"}' >/dev/null
sleep 2

echo "seeding ${BIG_COUNT}x${BIG_MB}MB and ${SMALL_COUNT} small files"
docker compose -f compose.yaml exec -T soak-a sh -c 'mkdir -p /data/shared/small && chown -R 99:100 /data'
docker compose -f compose.yaml exec -T soak-b sh -c 'mkdir -p /data/shared/small && chown -R 99:100 /data'
docker compose -f compose.yaml exec -i -T -u 99:100 soak-a sh -s <<EOF
set -eu
n=1
while [ "\$n" -le ${BIG_COUNT} ]; do
  dd if=/dev/zero of=/data/shared/big-\$n.bin bs=1M count=${BIG_MB}
  n=\$((n + 1))
done
i=0
while [ "\$i" -lt ${SMALL_COUNT} ]; do
  printf '%s\n' "\$i" > /data/shared/small/a-\$i.txt
  i=\$((i + 1))
done
EOF
docker compose -f compose.yaml exec -i -T -u 99:100 soak-b sh -s <<EOF
set -eu
dd if=/dev/zero of=/data/shared/from-b.bin bs=1M count=16
i=0
while [ "\$i" -lt 500 ]; do
  printf '%s\n' "\$i" > /data/shared/small/b-\$i.txt
  i=\$((i + 1))
done
EOF

manifest() {
  docker compose -f compose.yaml exec -T "$1" sh -c 'cd /data/shared && find . -type f | sort | xargs sha256sum'
}

echo "waiting for checksum convergence"
start=$(date +%s)
deadline=$((start + TIMEOUT))
ok=0
while [ "$(date +%s)" -lt "$deadline" ]; do
  left="$(manifest soak-a || true)"
  right="$(manifest soak-b || true)"
  if [ -n "$left" ] && [ "$left" = "$right" ] && echo "$left" | grep -q 'big-1.bin' && echo "$left" | grep -q 'from-b.bin' && echo "$left" | grep -q "a-$((SMALL_COUNT - 1)).txt"; then
    ok=1
    break
  fi
  sleep 5
done
elapsed=$(( $(date +%s) - start ))
if [ "$ok" -ne 1 ]; then
  echo "checksums did not converge after ${elapsed}s" >&2
  echo "---- soak-a ----" >&2
  manifest soak-a | tail -n 20 >&2 || true
  echo "---- soak-b ----" >&2
  manifest soak-b | tail -n 20 >&2 || true
  exit 1
fi
if [ "$elapsed" -le 8 ]; then
  echo "sync finished in ${elapsed}s; the link was not slow enough to pass the old 8s timeout" >&2
  exit 1
fi
for side in soak-a soak-b; do
  # A public bootstrap dial gives up after its own short timeout. That is not
  # a stalled file transfer, and it is expected when the lab cannot reach
  # bootstrap.libp2p.io.
  if docker compose -f compose.yaml exec -T "$side" sh -c 'grep "context deadline exceeded" /config/logs/fse.jsonl | grep -v libp2p.bootstrap.dial | grep -q .'; then
    echo "$side log contains context deadline exceeded" >&2
    exit 1
  fi
done
echo "soak converged in ${elapsed}s"
trap - EXIT
docker compose -f compose.yaml down -v
