package configsync

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	idA = "11111111-1111-4111-8111-111111111111"
	idB = "22222222-2222-4222-8222-222222222222"
	idC = "33333333-3333-4333-8333-333333333333"
)

func TestNoCrossInstanceAdoption(t *testing.T) {
	instance1 := NewReplica("group", idA)
	instance2 := NewReplica("group", idB)
	if _, err := instance1.SetOwn("logging.level", "debug", false, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := instance2.SetOwn("logging.level", "warn", false, 100); err != nil {
		t.Fatal(err)
	}

	stored, _ := Merge(instance2, instance1)
	if stored.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("instance 2 adopted instance 1's logging level: %#v", stored.OwnRecord().Fields["logging.level"])
	}
	if stored.Records[idA].Fields["logging.level"].Value != "debug" {
		t.Fatalf("instance 1's record was not stored for management: %#v", stored.Records[idA])
	}
	if _, ok := stored.OwnRecord().Fields["logging.level"]; !ok || stored.Records[idB].Fields["logging.level"].Value == "debug" {
		t.Fatalf("own record changed: %#v", stored.Records[idB])
	}

	queued, err := stored.QueueEdit(idA, "logging.level", "error", false, 250)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != StatusPending || queued.TargetInstanceID != idA {
		t.Fatalf("edit = %+v", queued)
	}
	if stored.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("queuing an edit for instance 1 changed instance 2: %#v", stored.OwnRecord())
	}
	if stored.Records[idA].Fields["logging.level"].Value != "debug" {
		t.Fatalf("queued edit was written into instance 1's stored record before instance 1 applied it: %#v", stored.Records[idA])
	}

	applied, stats := Merge(instance1, stored)
	if stats.EditsApplied != 1 {
		t.Fatalf("instance 1 did not apply the edit targeted at it: %+v", stats)
	}
	if applied.OwnRecord().Fields["logging.level"].Value != "error" {
		t.Fatalf("instance 1 own record = %#v", applied.OwnRecord())
	}
	echoed, _ := Merge(stored, applied)
	if echoed.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("instance 1's applied setting came back as instance 2's setting: %#v", echoed.OwnRecord())
	}
	if echoed.Records[idA].Fields["logging.level"].Value != "error" {
		t.Fatalf("stored copy of instance 1 = %#v", echoed.Records[idA])
	}
}

func TestQueuedEditIsNotAppliedByARelay(t *testing.T) {
	origin := NewReplica("group", idA)
	relay := NewReplica("group", idB)
	target := NewReplica("group", idC)
	if _, err := relay.SetOwn("logging.level", "warn", false, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := target.SetOwn("logging.level", "info", false, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := origin.QueueEdit(idC, "logging.level", "debug", false, 200); err != nil {
		t.Fatal(err)
	}
	if _, ok := origin.Records[idC]; ok {
		t.Fatal("origin wrote the target record before the target applied the edit")
	}

	carried, stats := Merge(relay, origin)
	if stats.EditsRelayed != 1 {
		t.Fatalf("relay did not carry the offline edit: %+v pending=%+v", stats, carried.Pending)
	}
	if carried.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("relay adopted the queued setting: %#v", carried.OwnRecord())
	}
	if _, ok := carried.Records[idC]; ok {
		t.Fatalf("relay wrote the target record: %#v", carried.Records[idC])
	}

	delivered, stats := Merge(target, carried)
	if stats.EditsApplied != 1 {
		t.Fatalf("target did not apply the relayed edit: %+v", stats)
	}
	if delivered.OwnRecord().Fields["logging.level"].Value != "debug" {
		t.Fatalf("target own record = %#v", delivered.OwnRecord())
	}
	for _, edit := range delivered.Pending {
		if edit.TargetInstanceID == idC && edit.Status != StatusApplied {
			t.Fatalf("edit status = %s", edit.Status)
		}
	}

	back, _ := Merge(carried, delivered)
	if back.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("target's applied setting was adopted by the relay: %#v", back.OwnRecord())
	}
	if back.Records[idC].Fields["logging.level"].Value != "debug" {
		t.Fatalf("relay did not store the target record after the target published it: %#v", back.Records[idC])
	}
}

func TestConcurrentFieldsInsideOneInstanceRecordBothSurvive(t *testing.T) {
	logging := NewReplica("group", idA)
	transfer := NewReplica("group", idA)
	if _, err := logging.SetOwn("logging.level", "debug", false, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := transfer.SetOwn("transfer.sendBytesPerSecond", 5000, false, 100); err != nil {
		t.Fatal(err)
	}
	viewer := NewReplica("group", idB)
	if _, err := viewer.SetOwn("logging.level", "warn", false, 100); err != nil {
		t.Fatal(err)
	}
	step, _ := Merge(viewer, logging)
	merged, _ := Merge(step, transfer)
	record := merged.Records[idA]
	if record.Fields["logging.level"].Value != "debug" || record.Fields["transfer.sendBytesPerSecond"].Value != 5000 {
		t.Fatalf("instance record = %#v", record.Fields)
	}
	if merged.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("viewer adopted a field from the stored record: %#v", merged.OwnRecord())
	}
	if _, ok := merged.OwnRecord().Fields["transfer.sendBytesPerSecond"]; ok {
		t.Fatal("viewer adopted the other instance's transfer setting")
	}
}

func TestSameFieldKeepsTheLaterHybridClock(t *testing.T) {
	early := NewReplica("group", idA)
	later := NewReplica("group", idA)
	if _, err := early.SetOwn("logging.level", "debug", false, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := later.SetOwn("logging.level", "info", false, 90); err != nil {
		t.Fatal(err)
	}
	viewer := NewReplica("group", idB)
	merged, stats := Merge(viewer, early)
	merged, stats = Merge(merged, later)
	if merged.Records[idA].Fields["logging.level"].Value != "debug" || stats.FieldsKept != 1 {
		t.Fatalf("older edit won: %#v stats=%+v", merged.Records[idA].Fields["logging.level"], stats)
	}

	if _, err := later.SetOwn("logging.level", "warn", false, 100); err != nil {
		t.Fatal(err)
	}
	// The first tick at wall 100 matches early's counter. A second tick at
	// the same wall is later.
	if _, err := later.SetOwn("logging.level", "warn", false, 100); err != nil {
		t.Fatal(err)
	}
	merged, _ = Merge(merged, later)
	if merged.Records[idA].Fields["logging.level"].Value != "warn" {
		t.Fatalf("later counter lost: %#v", merged.Records[idA].Fields["logging.level"])
	}
	if _, ok := merged.OwnRecord().Fields["logging.level"]; ok {
		t.Fatal("storing instance A's field wrote it onto instance B")
	}
}

func TestEqualClocksUseNodeIDAsTieBreak(t *testing.T) {
	left := Clock{Wall: 50, Counter: 1, NodeID: idA}
	right := Clock{Wall: 50, Counter: 1, NodeID: idB}
	if Compare(right, left) <= 0 {
		t.Fatal("expected the higher instance id to win an equal wall and counter")
	}
}

func TestOlderRelayedEditDoesNotRollBackANewerField(t *testing.T) {
	target := NewReplica("group", idC)
	if _, err := target.SetOwn("logging.level", "warn", false, 300); err != nil {
		t.Fatal(err)
	}
	origin := NewReplica("group", idA)
	if _, err := origin.QueueEdit(idC, "logging.level", "debug", false, 100); err != nil {
		t.Fatal(err)
	}
	delivered, stats := Merge(target, origin)
	if stats.EditsApplied != 0 {
		t.Fatalf("older edit was applied: %+v", stats)
	}
	if delivered.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("older offer rolled the field back: %#v", delivered.OwnRecord().Fields["logging.level"])
	}
	for _, edit := range delivered.Pending {
		if edit.TargetInstanceID == idC && edit.Status != StatusSuperseded {
			t.Fatalf("edit status = %s", edit.Status)
		}
	}
}

func TestCrossInstancePathsAreRejected(t *testing.T) {
	replica := NewReplica("group", idA)
	for _, key := range []string{"shared/logging.level", "instance/" + idB + "/logging.level", "../logging.level", ""} {
		if _, err := replica.SetOwn(key, "debug", false, 10); err == nil {
			t.Fatalf("accepted key %q", key)
		}
		if _, err := replica.QueueEdit(idB, key, "debug", false, 10); err == nil {
			t.Fatalf("queued key %q", key)
		}
	}
	if _, err := replica.QueueEdit("engine-b", "logging.level", "debug", false, 10); err == nil {
		t.Fatal("accepted a non-UUID target")
	}
}

func TestConnectionDetailsReplicateWithoutSecrets(t *testing.T) {
	a := NewReplica("group", idA)
	if _, err := a.PublishConnection(Connection{InstanceID: idA, Label: "Kiki", APIURL: "https://kiki:22420", Online: true}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PublishConnection(Connection{InstanceID: idA, Label: "Kiki", APIURL: "https://kiki:22420/?apiKey=nope", Online: true}, 11); err == nil {
		t.Fatal("accepted an API key in a connection URL")
	}
	b := NewReplica("group", idB)
	merged, _ := Merge(b, a)
	conn := merged.Connections[idA]
	if conn.APIURL != "https://kiki:22420" || conn.Label != "Kiki" {
		t.Fatalf("connection = %+v", conn)
	}
	views := merged.Instances()
	if len(views) < 2 || !views[0].Local || views[0].InstanceID != idB {
		t.Fatalf("instances = %+v", views)
	}
}

func TestStoreRoundTripKeepsForeignEditsQueued(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "config-sync.json")
	store, err := Open(path, "group", idA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(replica *Replica) error {
		if _, err := replica.SetOwn("logging.level", "warn", false, 5); err != nil {
			return err
		}
		_, err := replica.QueueEdit(idB, "logging.level", "debug", false, 6)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OwnRecord().Fields["logging.level"].Value != "warn" {
		t.Fatalf("own record = %#v", loaded.OwnRecord())
	}
	if _, ok := loaded.Records[idB]; ok {
		t.Fatalf("foreign record was written by the queued edit: %#v", loaded.Records[idB])
	}
	if len(loaded.Pending) != 1 || loaded.Pending[0].TargetInstanceID != idB || loaded.Pending[0].Status != StatusPending || loaded.Pending[0].Value != "debug" {
		t.Fatalf("pending = %+v", loaded.Pending)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config sync file mode = %o", info.Mode().Perm())
	}
}

func TestNewInstanceIDIsAUUID(t *testing.T) {
	id, err := NewInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInstanceID(id); err != nil {
		t.Fatal(err)
	}
}
