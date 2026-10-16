package link

import (
	"context"
	"testing"
)

type scriptedDirectory struct {
	name     string
	order    *[]string
	payloads [][]byte
}

func (d scriptedDirectory) Publish(context.Context, string, []byte) error { return nil }
func (d scriptedDirectory) Unpublish(context.Context, string) error       { return nil }
func (d scriptedDirectory) Lookup(context.Context, string) ([][]byte, error) {
	*d.order = append(*d.order, d.name)
	return d.payloads, nil
}

func TestLookupTriesLANAndPeersBeforeDHT(t *testing.T) {
	var order []string
	dir := NewMultiDirectory(
		scriptedDirectory{name: "lan", order: &order},
		scriptedDirectory{name: "peers", order: &order, payloads: [][]byte{[]byte("sealed")}},
		scriptedDirectory{name: "dht", order: &order, payloads: [][]byte{[]byte("later")}},
	)
	payloads, err := dir.Lookup(context.Background(), "namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || string(payloads[0]) != "sealed" {
		t.Fatalf("payloads = %q", payloads)
	}
	if len(order) != 2 || order[0] != "lan" || order[1] != "peers" {
		t.Fatalf("order = %#v", order)
	}
}
