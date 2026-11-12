package discovery

import "testing"

func TestReservedRelaysStayBoundedAndBuildACircuit(t *testing.T) {
	mesh.mu.Lock()
	prev := append([]string(nil), mesh.reservedRelays...)
	mesh.reservedRelays = nil
	mesh.mu.Unlock()
	t.Cleanup(func() {
		mesh.mu.Lock()
		mesh.reservedRelays = prev
		mesh.mu.Unlock()
	})
	var addrs []string
	for i := 0; i < 20; i++ {
		addrs = append(addrs, "/ip4/198.51.100."+itoa(i)+"/tcp/4001/p2p/12D3KooWRelay"+itoa(i))
	}
	NoteReservedRelays(addrs)
	mesh.mu.Lock()
	got := len(mesh.reservedRelays)
	mesh.mu.Unlock()
	if got != maxReservedRelays {
		t.Fatalf("reserved = %d, want %d", got, maxReservedRelays)
	}
	circuits := ReservedRelayCircuits("12D3KooWPeer")
	if len(circuits) == 0 || !containsAll(circuits, "/p2p-circuit/p2p/12D3KooWPeer") {
		t.Fatalf("circuits = %v", circuits)
	}
}

func containsAll(addrs []string, part string) bool {
	for _, addr := range addrs {
		if len(addr) >= len(part) && addr[len(addr)-len(part):] == part {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [4]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
