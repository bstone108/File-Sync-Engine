#!/usr/bin/env python3
"""Kill one engine with SIGKILL while it is writing a multi-block file.

The final path must not appear until the hash matches. After restart, verified
blocks are reused and only the missing blocks are fetched again. The file then
matches on all five peers.
"""

import atexit
import base64
import hashlib
import json
import re
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ROOT / "e2e" / "crash-block" / "compose.yaml"
PROJECT = "fse-crash-block"
SERVICES = ["mac", "windows", "unraid-a", "unraid-b", "unraid-share"]
PORTS = {"mac": 22701, "windows": 22702, "unraid-a": 22703, "unraid-b": 22704, "unraid-share": 22705}
VICTIM = "windows"
SOURCE = "unraid-a"
FILE = "/data/shared/crash.bin"
BLOCKS = 5
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
        if result.stdout:
            print(result.stdout)
        if result.stderr:
            print(result.stderr, file=sys.stderr)
        fail(f"docker compose {' '.join(args)} exited {result.returncode}")
    return result


def cleanup():
    logs = compose("logs", "--tail", "100", check=False, timeout=60)
    if logs.stdout:
        print(logs.stdout)
    if logs.stderr:
        print(logs.stderr, file=sys.stderr)
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
    der = base64.b64decode("".join(body.split()))
    return hashlib.sha256(der).hexdigest()


def api(port, key, path):
    request = urllib.request.Request(f"https://127.0.0.1:{port}{path}", method="GET")
    request.add_header("X-FSE-API-Key", key)
    try:
        with urllib.request.urlopen(request, context=SSL_CONTEXT, timeout=20) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode(errors="replace")
    except (urllib.error.URLError, TimeoutError, ConnectionError) as err:
        return 0, str(err)


def wait_api(service, key):
    deadline = time.time() + 90
    last = ""
    while time.time() < deadline:
        status, body = api(PORTS[service], key, "/v1/status")
        if status == 200:
            return
        last = f"{status} {body[:160]}"
        time.sleep(2)
    fail(f"{service} API did not start: {last}")


def running(service):
    result = compose("ps", "--status", "running", "-q", service, check=False)
    return bool(result.stdout.strip())


def read_volume(service, path):
    result = compose("run", "-q", "-i", "-T", "--rm", "--no-deps", "--entrypoint", "sh", service, "-c", f"cat {path}", check=False)
    if result.returncode != 0:
        return None
    return result.stdout


def file_exists(service, path):
    result = compose("exec", "-T", service, "sh", "-c", f"test -f {path}", check=False)
    return result.returncode == 0


def digest(service, path):
    result = compose("exec", "-T", service, "sha256sum", path, check=False)
    if result.returncode != 0:
        return ""
    return result.stdout.split()[0]


def install_configs(infos):
    for service in SERVICES:
        peers = []
        for other in SERVICES:
            if other == service:
                continue
            peers.append({
                "id": other,
                "apiKey": infos[other]["key"],
                "apiCertificateSha256": infos[other]["pin"],
                "identityPublicKey": infos[other]["public"],
                "encryptionLevel": 4,
                "addresses": [infos[other]["url"]],
                "endpoints": [{"kind": "manual", "address": infos[other]["url"], "authenticated": True}],
            })
        cfg = infos[service]["config"]
        cfg["shareACLVersion"] = 1
        cfg["peers"] = peers
        cfg["folders"] = [{
            "id": "shared",
            "path": "/data/shared",
            "enabled": True,
            "mode": "sendrecv",
            "sharedWith": SERVICES,
            "ignore": [],
        }]
        cfg.setdefault("api", {})["advertiseURL"] = infos[service]["url"]
        cfg.setdefault("discovery", {})["dht"] = False
        cfg["discovery"]["local"] = False
        payload = json.dumps(cfg, indent=2) + "\n"
        command = "cat > /config/config.jsonc && rm -f /config/config-sync.json && chown 99:100 /config/config.jsonc && mkdir -p /data/shared && chmod 0777 /data /data/shared"
        compose("run", "-q", "-i", "-T", "--rm", "--no-deps", "--user", "0", "--entrypoint", "sh", service, "-c", command, input_text=payload)


def kill_engine():
    compose(
        "exec",
        "-T",
        VICTIM,
        "sh",
        "-c",
        'for pid in /proc/[0-9]*; do if [ "$(cat "$pid/comm" 2>/dev/null)" = "fse" ]; then kill -9 "${pid##*/}"; fi; done',
        check=False,
    )
    deadline = time.time() + 30
    while time.time() < deadline:
        if not running(VICTIM):
            return
        time.sleep(0.5)
    fail("windows engine did not exit after SIGKILL")


def block_events(text):
    reused = []
    fetched = []
    for line in text.splitlines():
        if "folder.sync.block" not in line:
            continue
        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            continue
        if record.get("path") != "crash.bin":
            continue
        index = int(record.get("index"))
        if record.get("reused") is True:
            reused.append(index)
        else:
            fetched.append(index)
    return reused, fetched


def main():
    atexit.register(cleanup)
    print("starting five containers")
    compose("up", "-d")
    infos = {}
    for service in SERVICES:
        deadline = time.time() + 120
        ready = False
        while time.time() < deadline:
            result = compose("exec", "-T", service, "sh", "-c", "test -s /config/config.jsonc && test -s /config/fse-api-auto.crt", check=False)
            if result.returncode == 0:
                ready = True
                break
            time.sleep(2)
        if not ready:
            fail(f"{service} did not write a config")
        raw = compose("exec", "-T", service, "cat", "/config/config.jsonc").stdout
        cfg = parse_jsonc(raw)
        infos[service] = {
            "config": cfg,
            "key": cfg["api"]["key"],
            "public": cfg["identity"]["publicKey"],
            "pin": cert_pin(compose("exec", "-T", service, "cat", "/config/fse-api-auto.crt").stdout),
            "url": f"https://{service}:22420",
        }
    compose("stop")
    install_configs(infos)
    compose("start")
    for service in SERVICES:
        wait_api(service, infos[service]["key"])

    print("writing multi-block file")
    compose("exec", "-T", SOURCE, "sh", "-c", f"dd if=/dev/zero of={FILE} bs=1048576 count={BLOCKS * 4} status=none")
    expect = digest(SOURCE, FILE)
    if not expect:
        fail("source file was not written")

    have = []
    deadline = time.time() + 120
    while time.time() < deadline:
        if file_exists(VICTIM, FILE):
            fail("crash.bin was exposed on windows before the kill window")
        state = compose("exec", "-T", VICTIM, "sh", "-c", f"cat {FILE}.fse-sync-state", check=False)
        if state.returncode == 0 and state.stdout.strip():
            parsed = json.loads(state.stdout)
            have = [int(index) for index in parsed.get("have") or []]
            if have and len(have) < BLOCKS:
                break
        time.sleep(0.2)
    else:
        fail("windows did not stop in the middle of crash.bin")
    print(f"killing windows after verified blocks {have}")
    kill_engine()
    final = read_volume(VICTIM, FILE)
    if final is not None:
        fail("crash.bin was on disk after SIGKILL")
    saved = read_volume(VICTIM, FILE + ".fse-sync-state")
    if not saved:
        fail("partial block state disappeared with the process")
    have = [int(index) for index in json.loads(saved).get("have") or []]
    if not have or len(have) >= BLOCKS:
        fail(f"kill did not leave a partial verified set: {have}")
    log_before = read_volume(VICTIM, "/config/logs/fse.jsonl")
    if log_before is None:
        fail("windows log was missing after SIGKILL")

    compose("start", VICTIM)
    wait_api(VICTIM, infos[VICTIM]["key"])
    deadline = time.time() + 240
    while time.time() < deadline:
        current = digest(VICTIM, FILE)
        if current and current != expect:
            fail("windows exposed crash.bin before its hash matched")
        if all(digest(service, FILE) == expect for service in SERVICES):
            break
        time.sleep(2)
    else:
        fail("crash.bin did not converge on all five peers")

    fresh = read_volume(VICTIM, "/config/logs/fse.jsonl") or ""
    if fresh.startswith(log_before):
        fresh = fresh[len(log_before):]
    reused, fetched = block_events(fresh)
    if set(fetched) & set(have):
        fail(f"verified blocks were fetched again: fetched={fetched} have={have}")
    if not set(reused) & set(have):
        fail(f"no verified block was reused: reused={reused} have={have}")
    if any(index not in have for index in reused):
        fail(f"reused an unverified block: reused={reused} have={have}")
    if len(have) < BLOCKS and not fetched:
        fail("missing blocks were not requested after restart")
    print(f"resumed with reused={sorted(set(reused))} fetched={sorted(set(fetched))}")


if __name__ == "__main__":
    main()
