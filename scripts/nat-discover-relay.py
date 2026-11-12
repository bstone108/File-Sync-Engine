#!/usr/bin/env python3
"""Two cone NATs sync through a relay they were not configured with.

A DHT bootstrap container is not a relay. The relay container advertises
itself. The engines learn it from the DHT, reserve it, and copy a file.
Direct inbound between the engines is dropped.
"""

import atexit
import base64
import hashlib
import json
import os
import re
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ROOT / "e2e" / "nat-discover" / "compose.yaml"
PROJECT = "fse-nat-discover"
ENGINES = ["engine-a", "engine-b"]
PORTS = {"engine-a": 22811, "engine-b": 22812}
GROUP = "identity-a"
TOKEN = "identity-a-bootstrap-token-must-be-43-characters"
SSL_CONTEXT = ssl._create_unverified_context()


def compose(*args, input_text=None, check=True, timeout=180):
    result = subprocess.run(
        ["docker", "compose", "-p", PROJECT, "-f", str(COMPOSE), *args],
        input=input_text,
        text=True,
        capture_output=True,
        check=False,
        timeout=timeout,
    )
    if check and result.returncode != 0:
        sys.stderr.write(result.stdout or "")
        sys.stderr.write(result.stderr or "")
        fail(f"docker compose {' '.join(args)} exited {result.returncode}")
    return result


def cleanup():
    logs = compose("logs", "--tail", "40", check=False, timeout=60)
    sys.stdout.write(logs.stdout or "")
    sys.stderr.write(logs.stderr or "")
    compose("down", "-v", "--remove-orphans", check=False, timeout=120)


def fail(message):
    print(message, file=sys.stderr)
    sys.exit(1)


def parse_jsonc(raw):
    text = re.sub(r"/\*.*?\*/", "", raw, flags=re.S)
    text = re.sub(r"(^|\s)//.*", "", text)
    return json.loads(text)


def cert_pin(pem):
    body = pem.split("-----BEGIN CERTIFICATE-----", 1)[1].split("-----END CERTIFICATE-----", 1)[0]
    return hashlib.sha256(base64.b64decode("".join(body.split()))).hexdigest()


def peer_id(service):
    deadline = time.time() + 120
    while time.time() < deadline:
        logs = (compose("logs", service, check=False).stdout or "") + (compose("logs", service, check=False).stderr or "")
        stored = compose("exec", "-T", service, "sh", "-c", "cat /config/logs/fse.jsonl 2>/dev/null || true", check=False).stdout or ""
        match = re.search(r'"peerId":"(12D3Koo[^"]+)"', logs + "\n" + stored)
        if match:
            return match.group(1)
        time.sleep(2)
    fail(f"{service} did not announce a libp2p peer id")


def api(port, key, path):
    request = urllib.request.Request(f"https://127.0.0.1:{port}{path}")
    request.add_header("X-FSE-API-Key", key)
    try:
        with urllib.request.urlopen(request, context=SSL_CONTEXT, timeout=20) as response:
            return response.status
    except urllib.error.HTTPError as err:
        return err.code
    except (urllib.error.URLError, TimeoutError, ConnectionError):
        return 0


def wait_api(service, key):
    deadline = time.time() + 90
    while time.time() < deadline:
        if api(PORTS[service], key, "/v1/status") == 200:
            return
        time.sleep(2)
    fail(f"{service} API did not start")


def main():
    atexit.register(cleanup)
    os.environ["FSE_DHT_BOOTSTRAP_PEERS"] = "/ip4/172.30.72.11/tcp/22421/p2p/12D3KooWPlaceholderPlaceholderPlacehol"
    print("starting the DHT bootstrap", flush=True)
    compose("up", "-d", "router", "bootstrap")
    bootstrap = peer_id("bootstrap")
    os.environ["FSE_DHT_BOOTSTRAP_PEERS"] = f"/ip4/172.30.72.11/tcp/22421/p2p/{bootstrap}"
    print(f"bootstrap {bootstrap}", flush=True)
    compose("up", "-d", "--force-recreate", "relay")
    deadline = time.time() + 150
    stored = ""
    while time.time() < deadline:
        stored = compose("exec", "-T", "relay", "sh", "-c", "cat /config/logs/fse.jsonl 2>/dev/null || true", check=False).stdout or ""
        if "public relay is discoverable" in stored:
            break
        time.sleep(2)
    else:
        interesting = "\n".join(line[-400:] for line in stored.splitlines() if "libp2p" in line or "bootstrap" in line or "relay" in line)
        sys.stderr.write(interesting[-4000:] + "\n")
        fail("relay did not advertise itself for discovery")
    print("relay advertised", flush=True)
    compose("up", "-d", "--force-recreate", "engine-a", "engine-b")
    infos = {}
    for service in ENGINES:
        deadline = time.time() + 120
        while time.time() < deadline:
            ready = compose("exec", "-T", service, "sh", "-c", "test -s /config/config.jsonc && test -s /config/fse-api-auto.crt", check=False)
            if ready.returncode == 0:
                break
            time.sleep(2)
        else:
            fail(f"{service} did not write a config")
        cfg = parse_jsonc(compose("exec", "-T", service, "cat", "/config/config.jsonc").stdout)
        infos[service] = {
            "config": cfg,
            "key": cfg["api"]["key"],
            "public": cfg["identity"]["publicKey"],
            "pin": cert_pin(compose("exec", "-T", service, "cat", "/config/fse-api-auto.crt").stdout),
            "dht": peer_id(service),
            "url": "https://engine-a.nat.test:22420" if service == "engine-a" else "https://engine-b.nat.test:22420",
        }
        wait_api(service, infos[service]["key"])
    compose("stop", "engine-a", "engine-b")
    for service, other in (("engine-a", "engine-b"), ("engine-b", "engine-a")):
        cfg = infos[service]["config"]
        cfg["shareACLVersion"] = 1
        cfg["api"]["advertiseURL"] = infos[service]["url"]
        cfg["api"]["advertisePinned"] = True
        cfg.setdefault("discovery", {})["dht"] = True
        cfg["discovery"]["local"] = False
        cfg["discovery"]["dhtBootstrapPeers"] = [os.environ["FSE_DHT_BOOTSTRAP_PEERS"]]
        cfg.setdefault("identity", {})["groups"] = [{"id": GROUP, "token": TOKEN, "enabled": True}]
        cfg["peers"] = [{
            "id": other,
            "apiKey": infos[other]["key"],
            "apiCertificateSha256": infos[other]["pin"],
            "identityPublicKey": infos[other]["public"],
            "identityGroupId": GROUP,
            "dhtPeerId": infos[other]["dht"],
            "encryptionLevel": 4,
            "addresses": [],
            "endpoints": [],
            "relays": [],
        }]
        cfg["folders"] = [{
            "id": "shared",
            "path": "/data/shared",
            "enabled": True,
            "mode": "sendrecv",
            "sharedWith": [other],
            "ignore": [],
        }]
        payload = json.dumps(cfg, indent=2) + "\n"
        command = "cat > /config/config.jsonc && chown 99:100 /config/config.jsonc && mkdir -p /data/shared && chmod 0777 /data /data/shared"
        compose("run", "-q", "-i", "-T", "--rm", "--no-deps", "--user", "0", "--entrypoint", "sh", service, "-c", command, input_text=payload)
    compose("start", "engine-a", "engine-b")
    for service in ENGINES:
        wait_api(service, infos[service]["key"])
    compose("exec", "-T", "-u", "0", "engine-a", "sh", "-c", "mkdir -p /data/shared && printf nat-discovered > /data/shared/note.txt && chmod 666 /data/shared/note.txt")
    deadline = time.time() + 180
    while time.time() < deadline:
        got = compose("exec", "-T", "engine-b", "sh", "-c", "cat /data/shared/note.txt", check=False)
        if got.returncode == 0 and got.stdout.strip() == "nat-discovered":
            print("synced through a discovered public relay", flush=True)
            break
        time.sleep(3)
    else:
        fail("the file did not sync through a discovered relay")
    compose("restart", "engine-b", timeout=60)
    wait_api("engine-b", infos["engine-b"]["key"])
    compose("exec", "-T", "-u", "0", "engine-a", "sh", "-c", "printf after-restart > /data/shared/again.txt")
    deadline = time.time() + 60
    while time.time() < deadline:
        got = compose("exec", "-T", "engine-b", "sh", "-c", "cat /data/shared/again.txt", check=False)
        if got.returncode == 0 and got.stdout.strip() == "after-restart":
            print("reconnected within 60s after a restart", flush=True)
            return
        time.sleep(2)
    fail("the engines did not reconnect within 60s after a restart")


if __name__ == "__main__":
    main()
