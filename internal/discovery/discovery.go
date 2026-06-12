package discovery

type Peer struct {
	ID        string
	Addresses []string
}

type Source interface {
	Peers() ([]Peer, error)
}

type StaticSource struct {
	peers []Peer
}

func NewStaticSource(peers []Peer) StaticSource {
	copyPeers := make([]Peer, len(peers))
	copy(copyPeers, peers)
	return StaticSource{peers: copyPeers}
}

func (s StaticSource) Peers() ([]Peer, error) {
	copyPeers := make([]Peer, len(s.peers))
	copy(copyPeers, s.peers)
	return copyPeers, nil
}

type Plan struct {
	DisableAll  bool
	EnableDHT   bool
	EnableLocal bool
	ManualPeers []Peer
}

func (p Plan) RequiresDHT() bool {
	return !p.DisableAll && p.EnableDHT
}

func (p Plan) RequiresLocal() bool {
	return !p.DisableAll && p.EnableLocal
}

func (p Plan) HasManualPeers() bool {
	return len(p.ManualPeers) > 0
}
