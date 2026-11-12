package discovery

import (
	"os"
	"strings"
)

// Hole-punch coordination roles. Own engines are used before any public network.
const (
	// RoleOwnPublic is a publicly reachable engine this node can dial directly.
	RoleOwnPublic = "own"
	// RoleOwnMesh is a publicly reachable engine reached by bouncing through
	// other authenticated peers.
	RoleOwnMesh = "own-mesh"
	// RoleDirect means our own peers already connect the two sides, so no
	// public coordinator is used.
	RoleDirect = "direct"
	// RolePublicSTUN is the public STUN fallback.
	RolePublicSTUN = "public-stun"
	// RolePublicDHT is a DHT-found public coordinator, used only after STUN
	// when no own engine can coordinate.
	RolePublicDHT = "public-dht"
)

// MeshNode is one authenticated engine in this identity. Links are peers that
// node can already reach. Addresses are dial candidates, not saved endpoints.
type MeshNode struct {
	ID     string
	Addrs  []string
	Public bool
	Links  []string
}

// Coordinator is who arranges a hole punch between two peers.
type Coordinator struct {
	ID    string
	Addrs []string
	Role  string
	// Path is the chain of our peer IDs after self, ending at the other peer
	// when that peer is known. A mesh bounce includes the intermediate peers
	// and the public engine.
	Path []string
}

// SelectPunchCoordinator chooses who coordinates a hole punch from self to target.
// An empty target asks for a coordinator for this node. Own publicly reachable
// engines come first. A public engine that is only reachable through other own
// peers is a mesh bounce. Public STUN and DHT coordinators are used only when
// no own public engine is reachable. A path that already connects the peers
// through our mesh does not use public infrastructure.
func SelectPunchCoordinator(self, target string, nodes []MeshNode, public []Coordinator) (Coordinator, bool) {
	self = strings.TrimSpace(self)
	target = strings.TrimSpace(target)
	index := map[string]*MeshNode{}
	for _, node := range nodes {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			continue
		}
		node.ID = id
		node.Addrs = cleanAddrs(node.Addrs)
		node.Links = cleanAddrs(node.Links)
		if prev, ok := index[id]; ok {
			if node.Public {
				prev.Public = true
			}
			prev.Addrs = appendUnique(prev.Addrs, node.Addrs)
			prev.Links = appendUnique(prev.Links, node.Links)
			continue
		}
		copied := node
		index[id] = &copied
	}
	adj := map[string][]string{}
	for id, node := range index {
		for _, link := range node.Links {
			link = strings.TrimSpace(link)
			if link == "" || link == id {
				continue
			}
			adj[id] = appendUnique(adj[id], []string{link})
			adj[link] = appendUnique(adj[link], []string{id})
		}
	}
	if target != "" && target != self {
		if path := shortestPath(self, target, adj); len(path) > 0 {
			return coordinatorOnPath(path, index), true
		}
	}
	if publicNode, path := nearestPublic(self, adj, index); publicNode != "" {
		return coordinatorOnPath(path, index), true
	}
	for _, fallback := range public {
		if strings.TrimSpace(fallback.Role) == "" || strings.TrimSpace(fallback.ID) == "" {
			continue
		}
		fallback.Addrs = cleanAddrs(fallback.Addrs)
		fallback.Path = nil
		return fallback, true
	}
	return Coordinator{}, false
}

// PublicCoordination reports whether coord is public infrastructure.
// Missing coordinators are treated as public fallback so a node with no own
// engine can still use STUN or a DHT relay.
func PublicCoordination(coord Coordinator, ok bool) bool {
	if !ok {
		return true
	}
	switch coord.Role {
	case RolePublicSTUN, RolePublicDHT:
		return true
	default:
		return false
	}
}

// DataRelays returns own relays when any exist. Public relays are the data
// fallback only when no own relay is available.
func DataRelays(own, public []string) []string {
	own = cleanAddrs(own)
	if len(own) > 0 {
		return own
	}
	return cleanAddrs(public)
}

func coordinatorOnPath(path []string, index map[string]*MeshNode) Coordinator {
	// path[0] is self. Prefer the first publicly reachable engine after self.
	for i := 1; i < len(path); i++ {
		node := index[path[i]]
		if node == nil || !node.Public {
			continue
		}
		role := RoleOwnMesh
		if i == 1 {
			role = RoleOwnPublic
		}
		return Coordinator{ID: node.ID, Addrs: append([]string(nil), node.Addrs...), Role: role, Path: append([]string(nil), path[1:]...)}
	}
	return Coordinator{ID: path[len(path)-1], Role: RoleDirect, Path: append([]string(nil), path[1:]...)}
}

func nearestPublic(self string, adj map[string][]string, index map[string]*MeshNode) (string, []string) {
	if node := index[self]; node != nil && node.Public && len(adj[self]) == 0 {
		return "", nil
	}
	path := shortestMatch(self, adj, func(id string) bool {
		node := index[id]
		return id != self && node != nil && node.Public
	})
	if len(path) == 0 {
		return "", nil
	}
	return path[len(path)-1], path
}

func shortestPath(from, to string, adj map[string][]string) []string {
	if from == "" || to == "" {
		return nil
	}
	return shortestMatch(from, adj, func(id string) bool { return id == to })
}

func shortestMatch(from string, adj map[string][]string, match func(string) bool) []string {
	if from == "" {
		return nil
	}
	type step struct {
		id   string
		path []string
	}
	seen := map[string]struct{}{from: {}}
	queue := []step{{id: from, path: []string{from}}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.id != from && match(cur.id) {
			return cur.path
		}
		links := append([]string(nil), adj[cur.id]...)
		// Stable order so the same mesh always picks the same coordinator.
		sortStrings(links)
		for _, next := range links {
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = struct{}{}
			queue = append(queue, step{id: next, path: append(append([]string(nil), cur.path...), next)})
		}
	}
	return nil
}

func cleanAddrs(in []string) []string {
	return appendUnique(nil, in)
}

func appendUnique(base, extra []string) []string {
	seen := map[string]struct{}{}
	for _, item := range base {
		item = strings.TrimSpace(item)
		if item != "" {
			seen[item] = struct{}{}
		}
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, item := range base {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	for _, item := range extra {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// PublicRelayFallback reports that no own relay or public engine is known,
// so public STUN, DHT coordinators, and public relays may be used.
func PublicRelayFallback() bool {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	return len(ownRelayAddrsLocked()) == 0
}

// OwnRelayAddrs returns relays of our own publicly reachable engines.
func OwnRelayAddrs() []string {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	return ownRelayAddrsLocked()
}

func ownRelayAddrsLocked() []string {
	own := append([]string(nil), mesh.identityRelays...)
	for _, node := range mesh.punchNodes {
		if node.Public && node.ID != "" && node.ID != mesh.selfID {
			own = append(own, node.Addrs...)
		}
	}
	return cleanAddrs(own)
}

const maxReservedRelays = 8

// NoteReservedRelays remembers relay reservations in memory. They are not
// written to config. The newest addresses win and the set stays bounded.
func NoteReservedRelays(addrs []string) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	seen := map[string]struct{}{}
	var next []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.Contains(raw, "/p2p/") {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		next = append(next, raw)
	}
	for _, raw := range addrs {
		add(raw)
		if len(next) >= maxReservedRelays {
			break
		}
	}
	for _, raw := range mesh.reservedRelays {
		add(raw)
		if len(next) >= maxReservedRelays {
			break
		}
	}
	mesh.reservedRelays = next
}

// ReservedRelayCircuits builds circuits through relays this node has reserved.
// The addresses stay in memory and are the public fallback after own relays.
func ReservedRelayCircuits(target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	mesh.mu.Lock()
	addrs := append([]string(nil), mesh.discoveredRelays...)
	addrs = append(addrs, mesh.reservedRelays...)
	mesh.mu.Unlock()
	if raw := strings.TrimSpace(os.Getenv("FSE_STATIC_RELAYS")); raw != "" {
		raw = strings.ReplaceAll(raw, ",", " ")
		addrs = append(strings.Fields(raw), addrs...)
	}
	var out []string
	for _, raw := range addrs {
		if bootstrapRelay(raw) {
			continue
		}
		if circuit := circuitTo(raw, target); circuit != "" {
			out = append(out, circuit)
		}
	}
	return cleanAddrs(out)
}

// OwnRelayCircuits builds circuits through our own relays to target.
func OwnRelayCircuits(target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	var out []string
	for _, raw := range OwnRelayAddrs() {
		if circuit := circuitTo(raw, target); circuit != "" {
			out = append(out, circuit)
		}
	}
	return cleanAddrs(out)
}

func circuitTo(raw, target string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, "/p2p/") {
		return ""
	}
	if !strings.Contains(raw, "p2p-circuit") {
		return strings.TrimRight(raw, "/") + "/p2p-circuit/p2p/" + target
	}
	if strings.HasSuffix(raw, "/p2p/"+target) {
		return raw
	}
	parts := strings.SplitN(raw, "p2p-circuit", 2)
	if len(parts) == 2 && strings.Contains(parts[1], "/p2p/") {
		return ""
	}
	return strings.TrimRight(raw, "/") + "/p2p/" + target
}

// RememberMesh records an authenticated peer and the peers it can reach.
// Learned addresses stay in memory until those peers authenticate themselves.
func RememberMesh(self string, remote MeshNode, learned []MeshNode) {
	if strings.TrimSpace(self) == "" || strings.TrimSpace(remote.ID) == "" {
		return
	}
	NoteMeshHop(self, remote)
	for _, peer := range learned {
		NoteMeshHop(remote.ID, peer)
	}
}

// MergePunchMesh records authenticated identity connections. Public nodes are
// hole-punch coordinators. Existing mesh hops are kept.
func MergePunchMesh(self string, nodes []MeshNode) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	if strings.TrimSpace(self) != "" {
		mesh.selfID = strings.TrimSpace(self)
	}
	for _, node := range nodes {
		mergePunchNodeLocked(node)
	}
}

// NoteMeshHop records that from can reach to. A later public announcement
// upgrades the node. It does not clear a node that is already public.
func NoteMeshHop(from string, to MeshNode) {
	from = strings.TrimSpace(from)
	to.ID = strings.TrimSpace(to.ID)
	if from == "" || to.ID == "" || from == to.ID {
		return
	}
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	if mesh.selfID == "" {
		mesh.selfID = from
	}
	mergePunchNodeLocked(MeshNode{ID: from, Links: []string{to.ID}})
	mergePunchNodeLocked(to)
	mergePunchNodeLocked(MeshNode{ID: from, Links: []string{to.ID}})
}

func mergePunchNodeLocked(node MeshNode) {
	node.ID = strings.TrimSpace(node.ID)
	if node.ID == "" {
		return
	}
	node.Addrs = cleanAddrs(node.Addrs)
	node.Links = cleanAddrs(node.Links)
	for i := range mesh.punchNodes {
		if mesh.punchNodes[i].ID != node.ID {
			continue
		}
		if node.Public {
			mesh.punchNodes[i].Public = true
		}
		mesh.punchNodes[i].Addrs = appendUnique(mesh.punchNodes[i].Addrs, node.Addrs)
		mesh.punchNodes[i].Links = appendUnique(mesh.punchNodes[i].Links, node.Links)
		return
	}
	mesh.punchNodes = append(mesh.punchNodes, node)
}
