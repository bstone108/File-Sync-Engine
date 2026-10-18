# NAT traversal

Traversal is on by default. `discovery.nat.disabled` or `FSE_NAT_TRAVERSAL=0` turns the whole stack off. `discovery.nat.meshHelperDisabled` or `FSE_MESH_HELPER=0` leaves traversal on and stops this instance from helping the rest of its identity group.

## What runs

- Public DHT discovery stays enabled, and it is only a backup. The lookup order is documented in `docs/CONFIG.md`: saved connection details, peer exchange and same-identity connection details, LAN discovery, then DHT.
- The node stays private, and AutoRelay reserves a relay, until a TCP dial-back of the libp2p port succeeds. A published container port or a LAN address is not that proof. After the dial-back, the node answers DHT queries.
- DCUtR hole punching runs over a relayed connection and replaces it with a direct path when the punch succeeds. Success and failure are logged as `libp2p.dcutr.ok` and `libp2p.dcutr.failed`.
- Circuit relay v2 and autorelay. Same-identity helpers are tried before public relays. Public IPFS/kubo relays are relay v2 limited circuits (about 128 KiB and 2 minutes). Those circuits are used for DCUtR, the pairing stream, `/v1/pair-code/claim`, and `/v1/sync-journal` only. File bytes wait for a direct or hole-punched path, then for an unlimited relay: a same-identity helper or a relay configured with `discovery.nat.relayPeers` / `FSE_RELAY_PEERS`. The default bootstrap list is not that fallback.
- UPnP and NAT-PMP map the published API port and the libp2p port on the local gateway when the gateway allows it. Each attempt is logged (`nat.pmp.ok` / `nat.pmp.failed`, `nat.upnp.ok` / `nat.upnp.failed`, and `nat.mapping.ok` / `nat.mapping.failed`).
- STUN binding requests go out at startup and every two minutes from the libp2p QUIC UDP socket, so the mapped port is that socket's external mapping. The default servers are `stun.l.google.com:19302` and `stun.cloudflare.com:3478`. Each attempt is logged (`nat.stun.ok`, `nat.stun.failed`, `nat.stun.result`) with the server, the mapped address, and the NAT type. The same mapping from both servers is `cone` and is announced as a QUIC candidate for DCUtR. Different mappings are `symmetric` and are logged, not announced as a stable dial address. One successful server is `unknown` and that mapping is still announced. `FSE_STUN_SERVERS=off` or the traversal switch off is `disabled`. WebRTC does not listen on that UDP socket, so it does not consume the binding response. The API port still comes from the published port or from UPnP/NAT-PMP. `GET /v1/status` reports `natType` and `stunMappedAddress`.

Direct, LAN, and Tailscale addresses are still tried. A reconnect profile keeps every usable path: LAN, public addresses learned from STUN, AutoNAT, or a NAT mapping, Tailscale, and same-identity relay addresses. Dialing uses the saved LAN and Tailscale profile first, then exchanged and LAN addresses, then a libp2p stream (relay, hole punch, or direct), and only then an authenticated public API URL. A literal public IP that the paired peer did not authenticate is removed on startup. `FSE_ADVERTISE_EXCLUDE_TAILSCALE=1` leaves Tailscale and CGNAT `100.64.0.0/10` (and the Tailscale IPv6 prefix) out of advertisement, libp2p announcements, and new reconnect profiles when those addresses are not actually reachable. If the libp2p stream fails, pairing falls back to the HTTPS API URL in the provider record.

Dialing races the candidates. A relay success is kept only until a LAN, hole-punched, or direct path connects. `/v1/peers` reports `connectionPath` as `lan`, `hole-punched`, `direct`, `tailscale`, `relay`, or `limited-relay`. `limited-relay` means the only path is a limited public circuit, so file transfer is not using it. The peers page shows that path and a `networkDiagnostics` entry with code `limited-relay`.

## Public infrastructure

| Use | Default | Override |
| --- | --- | --- |
| DHT bootstrap and relay candidates | `/dnsaddr/bootstrap.libp2p.io` | `discovery.dhtBootstrapPeers`, `discovery.nat.relayPeers`, or `FSE_RELAY_PEERS` |
| STUN | `stun.l.google.com:19302`, `stun.cloudflare.com:3478`, queried from the libp2p UDP socket at startup and every 2 minutes | `discovery.nat.stunServers` or `FSE_STUN_SERVERS` (`off` disables STUN) |

These servers see dial attempts and binding requests. They do not see file contents. Pairing records stay sealed. A bootstrap node is a relay candidate only when it speaks circuit relay v2. The public list is not a configured relay, and a node that has not passed a libp2p-port dial-back does not advertise that port.

The CI namespace test puts two engines behind separate NATs. The only shared host is a lab DHT node that also speaks circuit relay v2. Its address is public to libp2p and is not written into `peer.relays`. That test does not dial `bootstrap.libp2p.io`. A two-home run is still what proves those public nodes will reserve a slot and carry a stream.

## Same-identity helper

An instance that AutoNAT marks public, or that has a UPnP/NAT-PMP mapping, helps other instances in the same identity group:

- It serves circuit relay v2 with no per-circuit data limit, so a punched path that fails can still move file bytes through a publicly reachable same-identity engine. Reservations and circuits are limited to libp2p peer IDs in an enabled identity group. Code-paired peers outside the group are refused. The default cap is 32 reservations and 4 circuits. `discovery.nat.maxRelayReservations` sets the reservation cap, up to 128.
- Hole punching is coordinated on that relay. A successful punch is reported as `hole-punched` and new streams leave the relay.
- `/fse/observe/1.0.0` answers same-identity peers with the address the helper observed and with the helper's own relay address.

The helper publishes its peer ID, relay addresses, and reachability on the replicated connection record. Other instances prefer those relays over the public list. Public relays and public STUN remain the fallback when no helper is reachable.
