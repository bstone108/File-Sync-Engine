package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
)

func TestLabPublicRelayRegistersTheHopProtocol(t *testing.T) {
	t.Setenv("FSE_LAB_PUBLIC_RELAY", "1")
	port := freeUDPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{ListenPort: port, DHTMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	hop := protocol.ID("/libp2p/circuit/relay/0.2.0/hop")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range router.host.Mux().Protocols() {
			if id == hop {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hop protocol missing: %v", router.host.Mux().Protocols())
}
