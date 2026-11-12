#!/usr/bin/env python3
"""Five Linux containers standing in for the live peer set.

Identity A is the Mac desktop client, the Windows desktop client, and two
Unraid engines. Identity B is the third Unraid engine. It syncs the shared
folder with a share-code secret and cannot manage the others. The containers
are all Linux. The desktop builds are covered by the Windows smoke and the
shared daemon bridge test.
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
COMPOSE = ROOT / "e2e" / "five-peer" / "compose.yaml"
PROJECT = "fse-five-peer"
IDENTITY_A = ["mac", "windows", "unraid-a", "unraid-b"]
SHARE = "unraid-share"
SERVICES = IDENTITY_A + [SHARE]
PORTS = {"mac": 22601, "windows": 22602, "unraid-a": 22603, "unraid-b": 22604, "unraid-share": 22605}
SECRET = "share-only-sync-secret-not-a-daemon-key"
GROUP_A = "identity-a"
TOKEN_A = "identity-a-bootstrap-token-must-be-43-characters"
GROUP_B = "identity-b"
TOKEN_B = "identity-b-bootstrap-token-must-be-43-characters"
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
    logs = compose("logs", "--tail", "80", check=False, timeout=60)
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


def read_file(service, path):
    result = compose("exec", "-T", service, "cat", path)
    return result.stdout


def wait_until(label, probe, timeout):
    deadline = time.time() + timeout
    last = ""
    while time.time() < deadline:
        ok, last = probe()
        if ok:
            return last
        time.sleep(2)
    fail(f"timed out waiting for {label}: {last}")


def api(port, key, method, path, body=None):
    data = None if body is None else body.encode()
    request = urllib.request.Request(f"https://127.0.0.1:{port}{path}", data=data, method=method)
    request.add_header("X-FSE-API-Key", key)
    request.add_header("Accept", "application/json")
    if data is not None:
        request.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(request, context=SSL_CONTEXT, timeout=30) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode(errors="replace")
    except (urllib.error.URLError, TimeoutError, ConnectionError) as err:
        return 0, str(err)


def wait_api(service, key):
    def probe():
        status, body = api(PORTS[service], key, "GET", "/v1/status")
        return status == 200, f"{status} {body[:180]}"

    wait_until(f"{service} API", probe, 90)


def shell(service, script):
    result = compose("exec", "-T", service, "sh", "-c", script)
    return result.stdout.strip()


def write_configs(infos):
    for service in SERVICES:
        group = GROUP_B if service == SHARE else GROUP_A
        token = TOKEN_B if service == SHARE else TOKEN_A
        peers = []
        for other in SERVICES:
            if other == service:
                continue
            share_link = service == SHARE or other == SHARE
            key = SECRET if share_link else infos[other]["key"]
            record = {
                "id": other,
                "apiKey": key,
                "apiCertificateSha256": infos[other]["pin"],
                "identityPublicKey": infos[other]["public"],
                "encryptionLevel": 4,
                "addresses": [infos[other]["url"]],
                "endpoints": [{"kind": "manual", "address": infos[other]["url"], "authenticated": True}],
            }
            if share_link:
                record["peerSecret"] = SECRET
                record["identityGroupId"] = GROUP_B if other == SHARE else ""
            else:
                record["identityGroupId"] = GROUP_A
            peers.append(record)
        folders = [{
            "id": "shared",
            "path": "/data/shared",
            "enabled": True,
            "mode": "sendrecv",
            "sharedWith": SERVICES,
            "ignore": [],
        }]
        if service != SHARE:
            folders.append({
                "id": "private",
                "path": "/data/private",
                "enabled": True,
                "mode": "sendrecv",
                "sharedWith": [],
                "ignore": [],
            })
        cfg = infos[service]["config"]
        cfg["peers"] = peers
        cfg["folders"] = folders
        cfg.setdefault("identity", {})["groups"] = [{"id": group, "token": token, "enabled": True}]
        cfg.setdefault("api", {})["advertiseURL"] = infos[service]["url"]
        cfg.setdefault("discovery", {})["dht"] = False
        cfg["discovery"]["local"] = False
        payload = json.dumps(cfg, indent=2) + "\n"
        command = "cat > /config/config.jsonc && rm -f /config/config-sync.json && chown 99:100 /config/config.jsonc && mkdir -p /data/shared /data/private && chmod -R 0777 /data"
        compose("run", "-i", "-T", "--rm", "--no-deps", "--user", "0", "--entrypoint", "sh", service, "-c", command, input_text=payload)


def file_text(service, path):
    result = compose("exec", "-T", service, "sh", "-c", f"cat {path}", check=False)
    if result.returncode != 0:
        return None
    return result.stdout


def wait_text(service, path, expect):
    def probe():
        got = file_text(service, path)
        return got == expect, repr(got)

    wait_until(f"{service}:{path}", probe, 120)


def wait_absent(service, path):
    def probe():
        result = compose("exec", "-T", service, "sh", "-c", f"test ! -e {path}", check=False)
        return result.returncode == 0, "still present" if result.returncode != 0 else ""

    wait_until(f"{service}:{path} removed", probe, 120)


def digest(service, path):
    result = compose("exec", "-T", service, "sha256sum", path, check=False)
    if result.returncode != 0:
        return ""
    return result.stdout.split()[0]


def wait_digest(service, path, expect):
    def probe():
        got = digest(service, path)
        return got == expect and got != "", got

    wait_until(f"{service}:{path} digest", probe, 150)


def main():
    atexit.register(cleanup)
    print("starting five containers")
    up = compose("up", "-d", timeout=180)
    if up.stdout:
        print(up.stdout)
    infos = {}
    for service in SERVICES:
        def ready(service=service):
            result = compose("exec", "-T", service, "sh", "-c", "test -s /config/config.jsonc && test -s /config/fse-api-auto.crt", check=False)
            return result.returncode == 0, result.stderr.strip()

        wait_until(f"{service} config", ready, 90)
        cfg = parse_jsonc(read_file(service, "/config/config.jsonc"))
        if cfg.get("nodeName") != service:
            fail(f"{service} nodeName = {cfg.get('nodeName')}")
        pin = cert_pin(read_file(service, "/config/fse-api-auto.crt"))
        infos[service] = {
            "config": cfg,
            "key": cfg["api"]["key"],
            "public": cfg["identity"]["publicKey"],
            "pin": pin,
            "url": f"https://{service}:22420",
        }
        if not infos[service]["key"] or not infos[service]["public"] or not pin:
            fail(f"{service} is missing an API key, identity, or certificate")
    print("writing identity and share config", flush=True)
    compose("stop", check=False, timeout=120)
    write_configs(infos)
    compose("start", timeout=120)
    for service in SERVICES:
        wait_api(service, infos[service]["key"])

    share_status, share_body = api(PORTS["unraid-a"], SECRET, "GET", "/v1/sync-journal?folder=private")
    if share_status != 403:
        fail(f"share-only private journal = {share_status} {share_body}")
    config_status, config_body = api(PORTS["unraid-a"], SECRET, "GET", "/v1/config")
    if config_status != 401:
        fail(f"share-only config = {config_status} {config_body}")
    shared_status, shared_body = api(PORTS["unraid-a"], SECRET, "GET", "/v1/sync-journal?folder=shared")
    if shared_status != 200:
        fail(f"share-only shared journal = {shared_status} {shared_body}")
    print("share-only authorization holds")

    views = {}
    for service in SERVICES:
        status, body = api(PORTS[service], infos[service]["key"], "GET", "/v1/config-sync")
        if status != 200:
            fail(f"{service} config sync = {status} {body}")
        views[service] = json.loads(body)
    edit = json.dumps({"targetInstanceId": views["windows"]["localInstanceId"], "path": "logging.level", "value": "debug"})
    status, body = api(PORTS["unraid-b"], infos["unraid-b"]["key"], "POST", "/v1/config-sync/edit", edit)
    if status != 200:
        fail(f"identity edit = {status} {body}")
    replica = json.loads(read_file("unraid-b", "/config/config-sync.json"))
    def windows_debug():
        deadline = time.time() + 20
        current = {}
        while time.time() < deadline:
            current = parse_jsonc(read_file("windows", "/config/config.jsonc"))
            if current.get("logging", {}).get("level") == "debug":
                return current
            time.sleep(1)
        return current

    status, body = api(PORTS["windows"], infos["windows"]["key"], "POST", "/v1/config-sync/merge", json.dumps({"replica": replica}))
    if status != 200:
        fail(f"identity merge = {status} {body}")
    windows_cfg = windows_debug()
    if windows_cfg.get("logging", {}).get("level") != "debug":
        replica = json.loads(read_file("unraid-b", "/config/config-sync.json"))
        status, body = api(PORTS["windows"], infos["windows"]["key"], "POST", "/v1/config-sync/merge", json.dumps({"replica": replica}))
        if status != 200:
            fail(f"identity merge retry = {status} {body}")
        windows_cfg = windows_debug()
    origin_cfg = parse_jsonc(read_file("unraid-b", "/config/config.jsonc"))
    if windows_cfg.get("logging", {}).get("level") != "debug":
        fail(f"windows logging = {windows_cfg.get('logging')}")
    if origin_cfg.get("logging", {}).get("level") == "debug":
        fail("unraid-b adopted the Windows setting")
    status, body = api(PORTS[SHARE], infos[SHARE]["key"], "POST", "/v1/config-sync/merge", json.dumps({"replica": replica}))
    if status == 200 or "group mismatch" not in body:
        fail(f"share-only merge = {status} {body}")
    print("identity management stays inside identity A")

    shell("unraid-a", "printf from-a > /data/shared/note.txt")
    for service in ["mac", "windows", "unraid-b", SHARE]:
        wait_text(service, "/data/shared/note.txt", "from-a")
    shell("windows", "mv /data/shared/note.txt /data/shared/renamed.txt")
    for service in ["mac", "unraid-a", "unraid-b", SHARE]:
        wait_absent(service, "/data/shared/note.txt")
        wait_text(service, "/data/shared/renamed.txt", "from-a")
    shell("unraid-b", "rm /data/shared/renamed.txt")
    for service in ["mac", "windows", "unraid-a", SHARE]:
        wait_absent(service, "/data/shared/renamed.txt")
    shell("mac", "dd if=/dev/zero of=/data/shared/large.bin bs=1048576 count=9 status=none")
    large = digest("mac", "/data/shared/large.bin")
    if not large:
        fail("mac large file was not written")
    for service in ["windows", "unraid-a", "unraid-b", SHARE]:
        wait_digest(service, "/data/shared/large.bin", large)
    shell(SHARE, "printf edited-on-share-only > /data/shared/from-share.txt")
    for service in IDENTITY_A:
        wait_text(service, "/data/shared/from-share.txt", "edited-on-share-only")
    print("folder changes converged")

    shell("unraid-a", "dd if=/dev/zero of=/data/shared/resume.bin bs=1048576 count=9 status=none")
    compose("restart", "windows", timeout=120)
    wait_api("windows", infos["windows"]["key"])
    resume = digest("unraid-a", "/data/shared/resume.bin")
    wait_digest("windows", "/data/shared/resume.bin", resume)
    print("windows caught up after restart")


if __name__ == "__main__":
    main()
