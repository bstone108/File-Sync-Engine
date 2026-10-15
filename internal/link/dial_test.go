package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestDialFailsOverWhenFirstAddressDies(t *testing.T) {
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sync-journal" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[{"path":"notes.txt"}]}`))
	}))
	defer live.Close()
	peer := config.PeerConfig{
		ID:     "peer-a",
		APIKey: "secret",
		Endpoints: []config.EndpointConfig{
			{Kind: "manual", Address: "http://127.0.0.1:1", NetworkHint: "local"},
			{Kind: "manual", Address: live.URL, NetworkHint: "wan"},
		},
	}
	var used string
	body := ""
	got, err := Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/sync-journal", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		buf := make([]byte, 128)
		n, _ := resp.Body.Read(buf)
		body = string(buf[:n])
		used = base
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != live.URL || used != live.URL {
		t.Fatalf("used %q, dial returned %q", used, got)
	}
	if !strings.Contains(body, "notes.txt") {
		t.Fatalf("body = %s", body)
	}
}
