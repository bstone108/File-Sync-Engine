package discovery

import (
	"context"
	"reflect"
	"testing"
)

func TestDHTDoesNotRefreshWhileIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	field := reflect.ValueOf(router.dht).Elem().FieldByName("autoRefresh")
	if !field.IsValid() || field.Kind() != reflect.Bool || field.Bool() {
		t.Fatalf("DHT auto-refresh is still enabled")
	}
}
