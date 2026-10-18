package link

import (
	"net/http"
	"testing"

	"filesyncengine/internal/discovery"
)

func TestTransferClientHasNoTotalTimeout(t *testing.T) {
	client, err := verifyPinnedPeerCertificateClient("http://127.0.0.1:9", "")
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 0 {
		t.Fatalf("total timeout = %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.ResponseHeaderTimeout <= 0 || transport.DialContext == nil {
		t.Fatalf("transport = %#v", client.Transport)
	}
}

func TestDialableMultiaddrBecomesHTTP(t *testing.T) {
	got, ok := discovery.DialableHTTPAddress("/ip4/203.0.113.9/tcp/22420/https")
	if !ok || got != "https://203.0.113.9:22420" {
		t.Fatalf("address = %q ok=%v", got, ok)
	}
	if _, ok := discovery.DialableHTTPAddress("/ip4/203.0.113.9/tcp/22421"); ok {
		t.Fatal("plain libp2p address was treated as an API URL")
	}
}
