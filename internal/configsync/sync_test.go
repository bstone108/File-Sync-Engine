package configsync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConcurrentEditsToDifferentFieldsBothSurvive(t *testing.T) {
	a := NewReplica("group", "engine-a")
	b := NewReplica("group", "engine-b")
	if _, err := a.SetField(SharedPath("logging.level"), "debug", false, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SetField(SharedPath("transfer.sendBytesPerSecond"), 5000, false, 100); err != nil {
		t.Fatal(err)
	}
	merged, _ := Merge(a, b)
	if merged.Fields[SharedPath("logging.level")].Value != "debug" {
		t.Fatalf("logging field = %#v", merged.Fields[SharedPath("logging.level")])
	}
	if merged.Fields[SharedPath("transfer.sendBytesPerSecond")].Value != 5000 {
		t.Fatalf("transfer field = %#v", merged.Fields[SharedPath("transfer.sendBytesPerSecond")])
	}
}

func TestSameFieldKeepsTheLaterHybridClock(t *testing.T) {
	a := NewReplica("group", "engine-a")
	b := NewReplica("group", "engine-b")
	if _, err := a.SetField(SharedPath("logging.level"), "debug", false, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SetField(SharedPath("logging.level"), "info", false, 90); err != nil {
		t.Fatal(err)
	}
	merged, stats := Merge(a, b)
	if merged.Fields[SharedPath("logging.level")].Value != "debug" || stats.FieldsKept != 1 {
		t.Fatalf("older edit won: %#v stats=%+v", merged.Fields[SharedPath("logging.level")], stats)
	}
	if _, err := b.SetField(SharedPath("logging.level"), "warn", false, 100); err != nil {
		t.Fatal(err)
	}
	// engine-b's first tick at wall 100 is counter 1, matching engine-a.
	// A second tick at the same wall is later.
	if _, err := b.SetField(SharedPath("logging.level"), "warn", false, 100); err != nil {
		t.Fatal(err)
	}
	merged, _ = Merge(a, b)
	if merged.Fields[SharedPath("logging.level")].Value != "warn" {
		t.Fatalf("later counter lost: %#v", merged.Fields[SharedPath("logging.level")])
	}
}

func TestEqualClocksUseNodeIDAsTieBreak(t *testing.T) {
	left := Clock{Wall: 50, Counter: 1, NodeID: "engine-a"}
	right := Clock{Wall: 50, Counter: 1, NodeID: "engine-b"}
	if Compare(right, left) <= 0 {
		t.Fatal("expected engine-b to win an equal wall and counter")
	}
}

func TestInstanceNamespacesDoNotCollide(t *testing.T) {
	a := NewReplica("group", "engine-a")
	b := NewReplica("group", "engine-b")
	if _, err := a.SetField(InstancePath("engine-a", "folders.docs.path"), "/data/a", false, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SetField(InstancePath("engine-b", "folders.docs.path"), "/data/b", false, 10); err != nil {
		t.Fatal(err)
	}
	merged, _ := Merge(a, b)
	if merged.Fields[InstancePath("engine-a", "folders.docs.path")].Value != "/data/a" {
		t.Fatal("engine-a row missing")
	}
	if merged.Fields[InstancePath("engine-b", "folders.docs.path")].Value != "/data/b" {
		t.Fatal("engine-b row missing")
	}
}

func TestOfflineEditReachesTargetThroughAThirdPeer(t *testing.T) {
	origin := NewReplica("group", "engine-a")
	relay := NewReplica("group", "engine-b")
	target := NewReplica("group", "engine-c")
	if _, err := origin.QueueEdit("engine-c", "logging.level", "debug", false, 200); err != nil {
		t.Fatal(err)
	}
	carried, stats := Merge(relay, origin)
	if stats.EditsRelayed != 1 {
		t.Fatalf("relay did not carry the offline edit: %+v pending=%+v", stats, carried.Pending)
	}
	if _, ok := carried.Fields[InstancePath("engine-c", "logging.level")]; !ok {
		t.Fatal("relay replica dropped the field")
	}
	delivered, stats := Merge(target, carried)
	if stats.EditsApplied != 1 {
		t.Fatalf("target did not apply the relayed edit: %+v", stats)
	}
	field := delivered.Fields[InstancePath("engine-c", "logging.level")]
	if field.Value != "debug" {
		t.Fatalf("target field = %#v", field)
	}
	for _, edit := range delivered.Pending {
		if edit.TargetInstanceID == "engine-c" && edit.Status != StatusApplied {
			t.Fatalf("edit status = %s", edit.Status)
		}
	}
}

func TestOlderRelayedEditDoesNotRollBackANewerField(t *testing.T) {
	target := NewReplica("group", "engine-c")
	if _, err := target.SetField(InstancePath("engine-c", "logging.level"), "warn", false, 300); err != nil {
		t.Fatal(err)
	}
	origin := NewReplica("group", "engine-a")
	if _, err := origin.QueueEdit("engine-c", "logging.level", "debug", false, 100); err != nil {
		t.Fatal(err)
	}
	delivered, _ := Merge(target, origin)
	if delivered.Fields[InstancePath("engine-c", "logging.level")].Value != "warn" {
		t.Fatalf("older offer rolled the field back: %#v", delivered.Fields[InstancePath("engine-c", "logging.level")])
	}
}

func TestConnectionDetailsReplicateWithoutSecrets(t *testing.T) {
	a := NewReplica("group", "engine-a")
	if _, err := a.PublishConnection(Connection{InstanceID: "engine-a", Label: "Kiki", APIURL: "https://kiki:22420", Online: true}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PublishConnection(Connection{InstanceID: "engine-a", Label: "Kiki", APIURL: "https://kiki:22420/?apiKey=nope", Online: true}, 11); err == nil {
		t.Fatal("accepted an API key in a connection URL")
	}
	b := NewReplica("group", "engine-b")
	merged, _ := Merge(b, a)
	conn := merged.Connections["engine-a"]
	if conn.APIURL != "https://kiki:22420" || conn.Label != "Kiki" {
		t.Fatalf("connection = %+v", conn)
	}
	views := merged.Instances()
	if len(views) < 2 || !views[0].Local || views[0].InstanceID != "engine-b" {
		t.Fatalf("instances = %+v", views)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "config-sync.json")
	store, err := Open(path, "group", "engine-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(replica *Replica) error {
		_, err := replica.QueueEdit("engine-b", "logging.level", "debug", false, 5)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Fields[InstancePath("engine-b", "logging.level")].Value != "debug" {
		t.Fatalf("stored field = %#v", loaded.Fields)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config sync file mode = %o", info.Mode().Perm())
	}
}
