package advertise

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/config"
)

func TestResolveUsesVerifiedGUIHostWhenBridgeIsTheOnlyLocalAddress(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420", AdvertiseURL: "https://172.17.0.4:22420"}}
	obs := Observations{
		GUIHost: "192.0.2.40:8385",
		Probe: func(context.Context, string, int) error {
			return nil
		},
	}
	got, err := ResolveWithObservations(cfg, "", "172.17.0.4", obs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.0.2.40:22420" {
		t.Fatalf("gui host = %q", got)
	}
}

func TestResolveUsesPublishedPortForGUIHost(t *testing.T) {
	t.Setenv(EnvPublishedAPIPort, "32420")
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420"}}
	obs := Observations{GUIHost: "files.example", Probe: func(context.Context, string, int) error { return nil }}
	got, err := ResolveWithObservations(cfg, "", "", obs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://files.example:32420" {
		t.Fatalf("published port = %q", got)
	}
}

func TestResolveSkipsUnverifiedGUIHost(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420"}}
	for _, probeErr := range []error{errors.New("i/o timeout"), errors.New("connect: connection refused")} {
		obs := Observations{GUIHost: "192.0.2.40", Probe: func(context.Context, string, int) error { return probeErr }}
		got, err := ResolveWithObservations(cfg, "", "10.1.1.5", obs)
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://10.1.1.5:22420" {
			t.Fatalf("probe %v selected %q", probeErr, got)
		}
	}
}

func TestResolveUsesObservedAddressAndNATMapping(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420"}}
	obs := Observations{ObservedIPs: []string{"172.17.0.4", "203.0.113.9"}}
	got, err := ResolveWithObservations(cfg, "", "172.17.0.4", obs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://203.0.113.9:22420" {
		t.Fatalf("observed = %q", got)
	}
	obs = Observations{ExternalIP: "198.51.100.8", ExternalPort: 22420}
	got, err = ResolveWithObservations(cfg, "", "", obs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://198.51.100.8:22420" {
		t.Fatalf("nat = %q", got)
	}
}

func TestResolveOverrideWinsWithoutDialing(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420"}}
	obs := Observations{
		GUIHost:      "192.0.2.40",
		ObservedIPs:  []string{"203.0.113.9"},
		ExternalIP:   "198.51.100.8",
		ExternalPort: 22420,
		Probe: func(context.Context, string, int) error {
			t.Fatal("explicit override must not be probed")
			return nil
		},
	}
	got, err := ResolveWithObservations(cfg, "https://files.example:22420", "172.17.0.4", obs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://files.example:22420" {
		t.Fatalf("override = %q", got)
	}
}

func TestRefreshPersistsGUIHostOverBridgeAndKeepsSavedURL(t *testing.T) {
	t.Cleanup(resetObservations)
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{"nodeName":"node-a","api":{"listen":"0.0.0.0:22420","key":"secret","advertiseURL":"https://172.17.0.4:22420"},"peers":[],"folders":[]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, "")
	t.Setenv(EnvVarList, "")
	SetReachabilityProbe(func(context.Context, string, int) error { return nil })
	NoteGUIHost("192.0.2.40:8385")
	NoteGUIRequest("example.com", "198.51.100.9:8385")
	if err := RefreshNow(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.AdvertiseURL != "https://192.0.2.40:22420" || loaded.API.Key != "secret" {
		t.Fatalf("persisted = %+v", loaded.API)
	}
	NoteNAT("198.51.100.8", 22420)
	if err := RefreshNow(path); err != nil {
		t.Fatal(err)
	}
	loaded, err = config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.AdvertiseURL != "https://192.0.2.40:22420" {
		t.Fatalf("saved URL replaced = %q", loaded.API.AdvertiseURL)
	}
}

func TestNoteGUIRequestIgnoresDirectClientsAndLoopback(t *testing.T) {
	t.Cleanup(resetObservations)
	NoteGUIRequest("192.0.2.40:22420", "198.51.100.9:8385")
	NoteGUIRequest("fse-native", "127.0.0.1:8385")
	NoteGUIRequest("fse-native", "fse-native")
	NoteGUIRequest("fse-native", "172.18.0.4:8385")
	obs := CurrentObservations()
	if obs.GUIHost != "" {
		t.Fatalf("gui host = %q", obs.GUIHost)
	}
	NoteGUIRequest("fse-native", "192.0.2.40:8385")
	if CurrentObservations().GUIHost != "192.0.2.40" {
		t.Fatalf("gui host = %q", CurrentObservations().GUIHost)
	}
}

func TestNoteObservedIPDropsBridge(t *testing.T) {
	t.Cleanup(resetObservations)
	NoteObservedIP("172.17.0.8")
	NoteObservedIP("203.0.113.9")
	obs := CurrentObservations()
	if len(obs.ObservedIPs) != 1 || obs.ObservedIPs[0] != "203.0.113.9" {
		t.Fatalf("observed = %v", obs.ObservedIPs)
	}
}

func TestGatewayFromProcNetRoute(t *testing.T) {
	table := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\neth0\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n"
	got := gatewayFromRouteTable(table)
	if got == nil || !got.Equal(net.ParseIP("192.168.1.1")) {
		t.Fatalf("gateway = %v", got)
	}
}

func TestStartAutoDetectStoresMappedAddress(t *testing.T) {
	t.Cleanup(resetObservations)
	previous := mapExternalPort
	mapExternalPort = func(context.Context, int, int) (string, int, error) {
		return "198.51.100.20", 22420, nil
	}
	t.Cleanup(func() { mapExternalPort = previous })
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{"nodeName":"node-a","api":{"listen":"0.0.0.0:22420","key":"secret"},"peers":[],"folders":[]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, "")
	BindConfigPath(path)
	changed := make(chan string, 1)
	SetOnChange(func(url string) {
		select {
		case changed <- url:
		default:
		}
	})
	StartAutoDetect(context.Background(), config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420"}})
	select {
	case url := <-changed:
		if url != "https://198.51.100.20:22420" {
			t.Fatalf("onChange = %q", url)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mapped URL was not stored")
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.AdvertiseURL != "https://198.51.100.20:22420" {
		t.Fatalf("persisted = %q", loaded.API.AdvertiseURL)
	}
}

func TestRefreshNowDoesNotCallProbeForExplicitOverride(t *testing.T) {
	t.Cleanup(resetObservations)
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{"nodeName":"node-a","api":{"listen":"0.0.0.0:22420","key":"secret"},"peers":[],"folders":[]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, "https://files.example:22420")
	SetReachabilityProbe(func(context.Context, string, int) error {
		t.Fatal("override refresh probed a GUI host")
		return nil
	})
	NoteGUIHost("192.0.2.40")
	if err := RefreshNow(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.AdvertiseURL != "https://files.example:22420" {
		t.Fatalf("persisted = %q", loaded.API.AdvertiseURL)
	}
	if strings.Contains(string(mustRead(t, path)), "192.0.2.40") {
		t.Fatal("GUI host replaced the override")
	}
}
