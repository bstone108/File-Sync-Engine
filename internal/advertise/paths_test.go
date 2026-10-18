package advertise

import (
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestExcludeTailscaleDropsCGNATFromDetectionAndProfiles(t *testing.T) {
	t.Setenv(EnvExcludeTailscale, "1")
	if got := SelectDetected([]string{"100.64.0.8", "203.0.113.8"}); got != "203.0.113.8" {
		t.Fatalf("detected = %q", got)
	}
	t.Setenv(EnvVarList, "https://203.0.113.8:22420,https://100.70.0.2:22420")
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420", AdvertiseURL: "https://100.64.1.8:22420"}}
	urls := ProfileURLs(cfg)
	joined := strings.Join(urls, " ")
	if strings.Contains(joined, "100.64.") || strings.Contains(joined, "100.70.") {
		t.Fatalf("tailscale URLs advertised: %s", joined)
	}
	if !strings.Contains(joined, "203.0.113.8:22420") {
		t.Fatalf("public URL missing: %s", joined)
	}
}

func TestProfileURLsKeepPublicBesideTailscale(t *testing.T) {
	t.Setenv(EnvExcludeTailscale, "")
	t.Setenv(EnvVarList, "https://203.0.113.8:22420")
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420", AdvertiseURL: "https://100.64.1.8:22420"}}
	urls := ProfileURLs(cfg)
	joined := strings.Join(urls, " ")
	if !strings.Contains(joined, "100.64.1.8:22420") || !strings.Contains(joined, "203.0.113.8:22420") {
		t.Fatalf("profile = %s", joined)
	}
}
