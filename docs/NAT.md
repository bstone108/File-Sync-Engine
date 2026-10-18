# NAT traversal

Traversal is on by default. `discovery.nat.disabled` or `FSE_NAT_TRAVERSAL=0` turns the whole stack off. `discovery.nat.meshHelperDisabled` or `FSE_MESH_HELPER=0` leaves traversal on and stops this instance from helping the rest of its identity group.

## What runs

- Public DHT discovery stays enabled, and it is only a backup. The lookup order is documented in `docs/CONFIG.md`: saved connection details, peer exchange and same-identity connection details, LAN discovery, then DHT.
- AutoNAT decides whether this instance is publicly reachable. A published container port does not by itself force that decision.
- DCUtR hole punching runs over a relayed connection and replaces it with a direct path when the punch succeeds.
- Circuit relay v2 and autorelay. Same-identity helpers are tried before public relays.
- UPnP and NAT-PMP map the published API port on the local gateway when the gateway allows it.
- STUN learns the external IP. The default servers are `stun.l.google.com:19302` and `stun.cloudflare.com:3478`. The mapped STUN port belongs to the query socket, so the API port still comes from the published port or from UPnP/NAT-PMP.

Direct, LAN, and Tailscale addresses are still tried. A reconnect profile keeps every usable path: LAN, public addresses learned from STUN, AutoNAT, or a NAT mapping, Tailscale, and same-identity relay addresses. Dialing uses the saved profile first, then exchanged and LAN addresses, then a DHT address. `FSE_ADVERTISE_EXCLUDE_TAILSCALE=1` leaves Tailscale and CGNAT `100.64.0.0/10` (and the Tailscale IPv6 prefix) out of advertisement, libp2p announcements, and new reconnect profiles when those addresses are not actually reachable. If the libp2p stream fails, pairing falls back to the HTTPS API URL in the provider record.

Dialing races the candidates. A relay success is kept only until a LAN, hole-punched, or direct path connects. `/v1/peers` reports `connectionPath` as `lan`, `hole-punched`, `direct`, `tailscale`, or `relay`. The peers page shows that path.

## Public infrastructure

| Use | Default | Override |
| --- | --- | --- |
| DHT bootstrap and relay candidates | `/dnsaddr/bootstrap.libp2p.io` | `discovery.dhtBootstrapPeers`, `discovery.nat.relayPeers`, or `FSE_RELAY_PEERS` |
| STUN | `stun.l.google.com:19302`, `stun.cloudflare.com:3478` | `discovery.nat.stunServers` or `FSE_STUN_SERVERS` (`off` disables STUN) |

These servers see dial attempts and binding requests. They do not see file contents. Pairing records stay sealed.

## Same-identity helper

An instance that AutoNAT marks public, or that has a UPnP/NAT-PMP mapping, helps other instances in the same identity group:

- It serves circuit relay v2. Reservations and circuits are limited to libp2p peer IDs in an enabled identity group. Code-paired peers outside the group are refused. The default cap is 32 reservations, 4 circuits, and 1 MiB in each direction before the relay resets the circuit. `discovery.nat.maxRelayReservations` sets the reservation cap, up to 128.
- Hole punching is coordinated on that relay. A successful punch is reported as `hole-punched` and new streams leave the relay.
- `/fse/observe/1.0.0` answers same-identity peers with the address the helper observed and with the helper's own relay address.

The helper publishes its peer ID, relay addresses, and reachability on the replicated connection record. Other instances prefer those relays over the public list. Public relays and public STUN remain the fallback when no helper is reachable.
