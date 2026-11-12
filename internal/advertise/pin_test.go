package advertise

import (
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestPinnedAdvertiseURLIsNotReplaced(t *testing.T) {
	cfg := config.Config{
		NodeName: "kiki",
		API: config.APIConfig{
			Listen:          "0.0.0.0:22420",
			Key:             "secret",
			AdvertiseURL:    "https://192.168.86.154:22420",
			AdvertisePinned: true,
		},
	}
	got, err := ResolveWithHost(cfg, "", "192.168.1.101")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.168.86.154:22420" {
		t.Fatalf("advertise URL = %s", got)
	}
}

func TestObservedForeignLANIsNotAdopted(t *testing.T) {
	urls := observationURLs(config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420"}}, Observations{
		ObservedIPs: []string{"192.168.1.101"},
	})
	for _, raw := range urls {
		if strings.Contains(raw, "192.168.1.101") {
			t.Fatalf("foreign LAN address was adopted: %v", urls)
		}
	}
}
