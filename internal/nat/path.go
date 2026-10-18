// Package nat classifies connection paths and races them.
// A relay success is kept only until a direct, hole-punched, or LAN path wins.
package nat

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// Path is the active way this engine is reaching a peer.
type Path string

const (
	PathLAN         Path = "lan"
	PathHolePunched Path = "hole-punched"
	PathDirect      Path = "direct"
	PathTailscale   Path = "tailscale"
	PathRelay       Path = "relay"
	// PathLimitedRelay is a relay v2 circuit with a data limit. Pairing and
	// small control requests may use it. File bytes may not.
	PathLimitedRelay Path = "limited-relay"
)

// ErrAnswered marks a dial whose peer sent an application response.
// The address is alive, so a better path is not required.
var ErrAnswered = errors.New("address answered")

// Candidate is one address that can be dialed.
type Candidate struct {
	Address string
	Path    Path
}

// Classify reports how an address would be reached.
// Hole-punched is recorded by the DCUtR tracer, not by the address text.
func Classify(addr string) Path {
	text := strings.ToLower(strings.TrimSpace(addr))
	if text == "" {
		return PathDirect
	}
	if strings.Contains(text, "p2p-circuit") || strings.Contains(text, "relay://") || strings.Contains(text, "/p2p-circuit") {
		return PathRelay
	}
	host := hostOf(addr)
	if host == "" {
		return PathDirect
	}
	if strings.HasSuffix(strings.ToLower(host), ".local") || strings.EqualFold(host, "localhost") {
		return PathLAN
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return PathDirect
	}
	if isTailscale(ip) {
		return PathTailscale
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || isDockerBridge(ip) {
		return PathLAN
	}
	return PathDirect
}

// Rank prefers LAN, then a punched or direct path, then Tailscale, then an
// unlimited relay. A limited relay is worse than an unlimited one.
func Rank(path Path) int {
	switch path {
	case PathLAN:
		return 0
	case PathHolePunched:
		return 1
	case PathDirect:
		return 2
	case PathTailscale:
		return 3
	case PathRelay:
		return 4
	case PathLimitedRelay:
		return 5
	default:
		return 6
	}
}

// Better reports whether next should replace current.
func Better(current, next Path) bool {
	if next == "" {
		return false
	}
	if current == "" {
		return true
	}
	return Rank(next) < Rank(current)
}

// Race dials every candidate at once. A relay win waits upgradeWait for a
// better path. An answered application error is returned when nothing better
// connects in that window.
func Race(ctx context.Context, candidates []Candidate, dial func(context.Context, string) error, upgradeWait time.Duration) (string, Path, error) {
	if len(candidates) == 0 {
		return "", "", errors.New("no connection candidates")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if upgradeWait <= 0 {
		upgradeWait = 250 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		address string
		path    Path
		err     error
	}
	ch := make(chan result, len(candidates))
	for _, candidate := range candidates {
		candidate := candidate
		if candidate.Path == "" {
			candidate.Path = Classify(candidate.Address)
		}
		go func() {
			err := dial(ctx, candidate.Address)
			ch <- result{address: candidate.Address, path: candidate.Path, err: err}
		}()
	}
	var best result
	var answered error
	var last error
	pending := len(candidates)
	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer != nil {
			return
		}
		timer = time.NewTimer(upgradeWait)
		timerC = timer.C
	}
	for pending > 0 {
		select {
		case <-ctx.Done():
			if best.address != "" && best.err == nil {
				return best.address, best.path, nil
			}
			if answered != nil {
				return "", "", answered
			}
			if last != nil {
				return "", "", last
			}
			return "", "", ctx.Err()
		case <-timerC:
			if best.err == nil && best.address != "" {
				return best.address, best.path, nil
			}
			if answered != nil {
				return "", "", answered
			}
		case res := <-ch:
			pending--
			if res.err == nil {
				if best.address == "" || Better(best.path, res.path) {
					best = res
				}
				if Rank(best.path) == 0 || pending == 0 {
					return best.address, best.path, nil
				}
				arm()
				continue
			}
			if errors.Is(res.err, ErrAnswered) && answered == nil {
				answered = res.err
				arm()
			} else {
				last = res.err
			}
			if pending == 0 {
				if best.err == nil && best.address != "" {
					return best.address, best.path, nil
				}
				if answered != nil {
					return "", "", answered
				}
				if last == nil {
					last = errors.New("every connection path failed")
				}
				return "", "", last
			}
		}
	}
	return "", "", errors.New("every connection path failed")
}

func hostOf(addr string) string {
	text := strings.TrimSpace(addr)
	if strings.Contains(text, "://") {
		if i := strings.Index(text, "://"); i >= 0 {
			text = text[i+3:]
		}
	}
	text = strings.TrimPrefix(text, "/")
	if strings.HasPrefix(text, "ip4/") || strings.HasPrefix(text, "ip6/") || strings.HasPrefix(text, "dns4/") || strings.HasPrefix(text, "dns6/") || strings.HasPrefix(text, "dns/") {
		parts := strings.Split(text, "/")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	if host, _, err := net.SplitHostPort(text); err == nil {
		return host
	}
	if i := strings.Index(text, "/"); i >= 0 {
		text = text[:i]
	}
	return strings.Trim(text, "[]")
}

func isTailscale(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 != nil {
		return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return false
	}
	return ip16[0] == 0xfd && ip16[1] == 0x7a && ip16[2] == 0x11 && ip16[3] == 0x5c && ip16[4] == 0xa1 && ip16[5] == 0xe0
}

func isDockerBridge(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 172 && ip4[1] >= 17 && ip4[1] <= 31
}

// PreferRelays lists same-identity helpers before public relays.
func PreferRelays(sameIdentity, public []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(sameIdentity)+len(public))
	for _, group := range [][]string{sameIdentity, public} {
		for _, addr := range group {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			if _, ok := seen[addr]; ok {
				continue
			}
			seen[addr] = struct{}{}
			out = append(out, addr)
		}
	}
	return out
}
