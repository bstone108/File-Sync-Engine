package nat

import (
	"os"
	"strings"
	"sync"
)

// Class is how much unsolicited inbound this node can accept.
// public can be dialed directly. cone can be punched. symmetric and
// no-inbound cannot accept a new inbound flow.
type Class string

const (
	ClassPublic    Class = "public"
	ClassCone      Class = "cone"
	ClassSymmetric Class = "symmetric"
	ClassNone      Class = "no-inbound"
	ClassUnknown   Class = "unknown"
)

// Evidence is what classification may use. AutoNAT v2, STUN, and a VPN or
// tunnel default route are the three inputs. Forced overrides them for tests.
type Evidence struct {
	AutoNATPublic bool
	STUN          string
	VPN           bool
	Forced        string
}

// ClassifyInbound reports the overall class and the TCP and UDP classes.
// The overall class is the most reachable transport, because one open
// transport is enough to accept.
func ClassifyInbound(e Evidence) (overall, tcp, udp Class) {
	if forced := ParseClass(e.Forced); forced != ClassUnknown {
		return forced, forced, forced
	}
	udp = classifyTransport(e, true)
	tcp = classifyTransport(e, false)
	return better(tcp, udp), tcp, udp
}

func classifyTransport(e Evidence, udp bool) Class {
	if e.AutoNATPublic {
		return ClassPublic
	}
	if e.VPN {
		return ClassNone
	}
	switch e.STUN {
	case "symmetric":
		return ClassSymmetric
	case "cone":
		if udp {
			return ClassCone
		}
		// STUN observes UDP. A cone UDP mapping is the same NAT for TCP
		// unless AutoNAT already proved TCP is public.
		return ClassCone
	default:
		return ClassUnknown
	}
}

// ClassRank orders classes from least reachable to most.
func ClassRank(c Class) int {
	switch ParseClass(string(c)) {
	case ClassNone:
		return 0
	case ClassSymmetric:
		return 1
	case ClassUnknown:
		return 2
	case ClassCone:
		return 3
	case ClassPublic:
		return 4
	default:
		return 2
	}
}

func better(a, b Class) Class {
	if ClassRank(a) >= ClassRank(b) {
		return ParseClass(string(a))
	}
	return ParseClass(string(b))
}

// ParseClass accepts the status strings. Anything else is unknown.
func ParseClass(raw string) Class {
	switch Class(strings.ToLower(strings.TrimSpace(raw))) {
	case ClassPublic:
		return ClassPublic
	case ClassCone:
		return ClassCone
	case ClassSymmetric:
		return ClassSymmetric
	case ClassNone, "none", "vpn", "cgnat":
		return ClassNone
	default:
		return ClassUnknown
	}
}

// ShouldDialNow is the dial order.
// With no live session both sides dial together so a cone NAT can be punched.
// Once a session is up, the more reachable side accepts and the other side
// keeps the outbound connection. Equal classes both keep the session.
func ShouldDialNow(local, remote Class, live bool) bool {
	local = ParseClass(string(local))
	remote = ParseClass(string(remote))
	if !live || local == ClassUnknown || remote == ClassUnknown {
		return true
	}
	return ClassRank(local) <= ClassRank(remote)
}

// DialMode names the attempt for logs and the GUI.
// two-way is the simultaneous dial. one-way means this side holds an outbound
// session. accept means the peer is holding it. poll is a short session
// opened because a persistent one could not be kept.
func DialMode(local, remote Class, live bool) string {
	local = ParseClass(string(local))
	remote = ParseClass(string(remote))
	if live && ClassRank(local) < ClassRank(remote) && remote != ClassUnknown && local != ClassUnknown {
		return "one-way"
	}
	if live && ClassRank(local) > ClassRank(remote) && remote != ClassUnknown && local != ClassUnknown {
		return "accept"
	}
	if !live && (local == ClassNone || local == ClassSymmetric) {
		return "poll"
	}
	return "two-way"
}

// SkipHolePunch reports that DCUtR cannot succeed for this pair.
func SkipHolePunch(local, remote Class) bool {
	local = ParseClass(string(local))
	remote = ParseClass(string(remote))
	if local == ClassNone || local == ClassSymmetric {
		return true
	}
	if remote == ClassNone || remote == ClassSymmetric {
		return true
	}
	return false
}

var (
	classMu    sync.Mutex
	localClass = ClassUnknown
	localByTr  = map[string]Class{}
	peerClass  = map[string]Class{}
	peerInit   = map[string]string{}
)

// SetLocal stores this node's class. It reports whether the overall class changed.
func SetLocal(overall Class, transports map[string]Class) bool {
	overall = ParseClass(string(overall))
	classMu.Lock()
	defer classMu.Unlock()
	changed := localClass != overall
	localClass = overall
	localByTr = map[string]Class{}
	for transport, class := range transports {
		localByTr[transport] = ParseClass(string(class))
	}
	return changed
}

// LocalClass is the forced class, when set, or the last classification.
func LocalClass() Class {
	if forced := ParseClass(os.Getenv("FSE_INBOUND_CLASS")); forced != ClassUnknown || strings.TrimSpace(os.Getenv("FSE_INBOUND_CLASS")) != "" {
		if forced != ClassUnknown {
			return forced
		}
	}
	classMu.Lock()
	defer classMu.Unlock()
	return localClass
}

// LocalTransports is a copy of the per-transport classes.
func LocalTransports() map[string]string {
	classMu.Lock()
	defer classMu.Unlock()
	out := make(map[string]string, len(localByTr))
	for transport, class := range localByTr {
		out[transport] = string(class)
	}
	return out
}

// SetPeerClass records a class learned from an authenticated peer.
func SetPeerClass(id string, class Class) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	class = ParseClass(string(class))
	if class == ClassUnknown {
		return
	}
	classMu.Lock()
	peerClass[id] = class
	classMu.Unlock()
}

// PeerClass is the class shared by an identity peer. Unknown means both sides dial.
func PeerClass(id string) Class {
	id = strings.TrimSpace(id)
	classMu.Lock()
	defer classMu.Unlock()
	if class, ok := peerClass[id]; ok {
		return class
	}
	return ClassUnknown
}

// SetInitiator records which side opened the live session: local or remote.
func SetInitiator(id, who string) {
	id = strings.TrimSpace(id)
	who = strings.TrimSpace(who)
	if id == "" || (who != "local" && who != "remote") {
		return
	}
	classMu.Lock()
	peerInit[id] = who
	classMu.Unlock()
}

// Initiator is local, remote, or empty when no session has been authenticated.
func Initiator(id string) string {
	classMu.Lock()
	defer classMu.Unlock()
	return peerInit[strings.TrimSpace(id)]
}

// ForgetPeer clears the initiator when the session is gone.
func ForgetPeer(id string) {
	classMu.Lock()
	delete(peerInit, strings.TrimSpace(id))
	classMu.Unlock()
}
