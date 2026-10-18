#!/bin/sh
# Issue a pairing code, restart the publishing container, and redeem it.
# The code file must stay on /config (appdata), the local record and DHT
# advertisement must come back, and docker stop must exit 0.
set -eu

cd "$(dirname "$0")/.."
compose="e2e/pairing-restart/compose.yaml"

cleanup() {
  docker compose -f "$compose" logs --tail 120 || true
  docker compose -f "$compose" down -v || true
}
trap cleanup EXIT

docker compose -f "$compose" up -d

echo "waiting for engines"
i=0
while [ "$i" -lt 45 ]; do
  if docker compose -f "$compose" exec -T publisher sh -c 'test -s /config/config.jsonc' \
    && docker compose -f "$compose" exec -T redeemer sh -c 'test -s /config/config.jsonc'; then
    break
  fi
  i=$((i + 1))
  sleep 2
done

api_key() {
  docker compose -f "$compose" exec -T "$1" cat /config/config.jsonc | python3 -c '
import json, re, sys
text = re.sub(r"(^|\s)//.*", "", sys.stdin.read())
print(json.loads(text)["api"]["key"])
'
}

wait_api() {
  port="$1"
  key="$2"
  i=0
  while [ "$i" -lt 45 ]; do
    if curl -kfsS -H "X-FSE-API-Key: $key" "https://127.0.0.1:${port}/v1/status" >/dev/null; then
      return 0
    fi
    i=$((i + 1))
    sleep 2
  done
  echo "API on port $port did not start" >&2
  return 1
}

KEY_A="$(api_key publisher)"
KEY_B="$(api_key redeemer)"
wait_api 22451 "$KEY_A"
wait_api 22452 "$KEY_B"

post() {
  port="$1"
  key="$2"
  path="$3"
  body="$4"
  curl -kfsS -H "X-FSE-API-Key: $key" -H "Content-Type: application/json" -d "$body" "https://127.0.0.1:${port}${path}"
}

echo "issuing pairing code"
code=""
i=0
while [ "$i" -lt 8 ] && [ -z "$code" ]; do
  code="$(post 22451 "$KEY_A" /v1/pair-code '{}' | python3 -c 'import json,sys; print(json.load(sys.stdin).get("code",""))' || true)"
  i=$((i + 1))
  sleep 2
done
if [ -z "$code" ]; then
  echo "pairing code was not issued" >&2
  exit 1
fi
echo "issued code"

namespace="$(python3 -c '
import hashlib, sys, urllib.parse
code = sys.argv[1].replace("-", "").replace(" ", "").upper()
locator = code[:8]
digest = hashlib.sha256(("fse-pair-code-v2:locator:" + locator).encode()).hexdigest()
print(urllib.parse.quote("filesyncengine/pair/v1/" + digest, safe=""))
' "$code")"

echo "checking the code file is on the appdata volume"
docker compose -f "$compose" exec -T publisher sh -c 'test -s /config/pairing-codes.json && test ! -e /tmp/pairing-codes.json'

# Structured pairing logs go to the appdata log file, not the container's stdout.
publisher_log() {
  docker compose -f "$compose" exec -T publisher cat /config/logs/fse.jsonl 2>/dev/null || true
}

log_count() {
  publisher_log | grep -c "$1" || true
}

advertised_before="$(log_count 'pairing code advertised on the DHT')"

echo "restarting publisher"
docker compose -f "$compose" restart publisher
wait_api 22451 "$KEY_A"

echo "checking the code survived the restart"
docker compose -f "$compose" exec -T publisher sh -c 'test -s /config/pairing-codes.json && test ! -e /tmp/pairing-codes.json'

echo "waiting for the restored record"
i=0
while [ "$i" -lt 30 ]; do
  if curl -kfsS "https://127.0.0.1:22451/v1/pair-code/record?namespace=${namespace}" >/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 2
done
curl -kfsS "https://127.0.0.1:22451/v1/pair-code/record?namespace=${namespace}" >/dev/null

echo "redeeming after restart"
paired=""
i=0
while [ "$i" -lt 15 ]; do
  paired="$(curl -ksS -H "X-FSE-API-Key: $KEY_B" -H "Content-Type: application/json" -d "{\"code\":\"$code\"}" "https://127.0.0.1:22452/v1/pair-code/redeem" || true)"
  echo "$paired"
  if echo "$paired" | grep -qi 'not found'; then
    i=$((i + 1))
    sleep 3
    continue
  fi
  echo "$paired" | grep -qi 'paired by code' && break
  i=$((i + 1))
  sleep 3
done
if ! echo "$paired" | grep -qi 'paired by code'; then
  echo "redeem after restart did not find the code" >&2
  exit 1
fi

echo "waiting for republish and the DHT advertisement"
i=0
republished=0
dht_ok=0
while [ "$i" -lt 45 ]; do
  text="$(publisher_log)"
  if printf '%s\n' "$text" | grep -q 'pairing code republished after restart'; then
    republished=1
  fi
  advertised_after="$(printf '%s\n' "$text" | grep -c 'pairing code advertised on the DHT' || true)"
  if [ "$advertised_after" -gt "$advertised_before" ]; then
    dht_ok=1
  fi
  if [ "$republished" = "1" ] && [ "$dht_ok" = "1" ]; then
    break
  fi
  i=$((i + 1))
  sleep 2
done
if [ "$republished" != "1" ]; then
  echo "restart did not republish the stored pairing code" >&2
  exit 1
fi
if [ "$dht_ok" != "1" ]; then
  echo "DHT record was not republished after restart (before=$advertised_before)" >&2
  exit 1
fi

echo "stopping publisher"
cid="$(docker compose -f "$compose" ps -aq publisher)"
docker stop "$cid" >/dev/null
status="$(docker inspect -f '{{.State.ExitCode}}' "$cid")"
echo "publisher exit $status"
if [ "$status" != "0" ]; then
  echo "docker stop exited $status" >&2
  exit 1
fi
echo "pairing restart ok"
