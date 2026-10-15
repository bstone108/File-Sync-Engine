// Package configsync replicates identity-linked engine configuration without
// collisions. Each field carries a hybrid logical clock. Merge keeps the later
// clock, so concurrent edits to different fields both survive and a later edit
// to the same field wins. Instance-local rows live under instance/<id>/ so two
// engines never write the same key. Shared rows live under shared/. Connection
// records travel with the replica, and pending edits for an offline target stay
// in the replica until any online peer delivers them.
package configsync

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Clock is a hybrid logical clock. Comparison is wall time, then counter, then
// the writer node id so equal physical times still have one winner.
type Clock struct {
	Wall    int64  `json:"wall"`
	Counter uint32 `json:"counter"`
	NodeID  string `json:"nodeId"`
}

// Compare returns 1 when a is later than b, -1 when a is earlier, and 0 when
// the clocks are identical.
func Compare(a, b Clock) int {
	if a.Wall != b.Wall {
		if a.Wall > b.Wall {
			return 1
		}
		return -1
	}
	if a.Counter != b.Counter {
		if a.Counter > b.Counter {
			return 1
		}
		return -1
	}
	return strings.Compare(a.NodeID, b.NodeID)
}

// Field is one replicated config value.
type Field struct {
	Path    string `json:"path"`
	Value   any    `json:"value,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
	Clock   Clock  `json:"clock"`
	Writer  string `json:"writer"`
}

// Connection is how identity-linked engines reach one instance.
type Connection struct {
	InstanceID string   `json:"instanceId"`
	Label      string   `json:"label"`
	APIURL     string   `json:"apiURL,omitempty"`
	Addresses  []string `json:"addresses,omitempty"`
	Online     bool     `json:"online"`
	LastSeen   string   `json:"lastSeen,omitempty"`
	Clock      Clock    `json:"clock"`
}

// Edit is a change queued for a specific instance. Any replica may carry it
// until that instance merges and applies it.
type Edit struct {
	ID               string `json:"id"`
	TargetInstanceID string `json:"targetInstanceId"`
	Path             string `json:"path"`
	Value            any    `json:"value,omitempty"`
	Deleted          bool   `json:"deleted,omitempty"`
	Clock            Clock  `json:"clock"`
	OriginInstanceID string `json:"originInstanceId"`
	Status           string `json:"status"`
}

// Replica is one engine's copy of the identity-linked config database.
type Replica struct {
	GroupID     string                `json:"groupId"`
	InstanceID  string                `json:"instanceId"`
	Fields      map[string]Field      `json:"fields,omitempty"`
	Connections map[string]Connection `json:"connections,omitempty"`
	Pending     []Edit                `json:"pending,omitempty"`
	LocalClock  Clock                 `json:"localClock"`
}

// InstanceView is the GUI's row for one linked engine.
type InstanceView struct {
	InstanceID     string `json:"instanceId"`
	Label          string `json:"label"`
	APIURL         string `json:"apiURL,omitempty"`
	Online         bool   `json:"online"`
	LastSeen       string `json:"lastSeen,omitempty"`
	PendingChanges int    `json:"pendingChanges"`
	Local          bool   `json:"local"`
}

// MergeStats reports what a merge changed.
type MergeStats struct {
	FieldsAccepted int
	FieldsKept     int
	EditsApplied   int
	EditsRelayed   int
}

const (
	StatusPending    = "pending"
	StatusApplied    = "applied"
	StatusSuperseded = "superseded"
)

// SharedPath is a row every identity-linked engine agrees on.
func SharedPath(key string) string {
	return "shared/" + strings.TrimPrefix(key, "shared/")
}

// InstancePath is a row owned by one engine. The instance id is part of the
// key, so two engines can store the same setting without colliding.
func InstancePath(instanceID, key string) string {
	prefix := "instance/" + instanceID + "/"
	return prefix + strings.TrimPrefix(key, prefix)
}

// NewReplica returns an empty replica for one engine in an identity group.
func NewReplica(groupID, instanceID string) Replica {
	return Replica{
		GroupID:     groupID,
		InstanceID:  instanceID,
		Fields:      map[string]Field{},
		Connections: map[string]Connection{},
		LocalClock:  Clock{NodeID: instanceID},
	}
}

// Tick moves the local clock forward and returns the clock for a new write.
func (r *Replica) Tick(wall int64) Clock {
	if wall < r.LocalClock.Wall {
		wall = r.LocalClock.Wall
	}
	counter := uint32(0)
	if wall == r.LocalClock.Wall {
		counter = r.LocalClock.Counter + 1
	}
	r.LocalClock = Clock{Wall: wall, Counter: counter, NodeID: r.InstanceID}
	return r.LocalClock
}

func (r *Replica) witness(remote Clock) {
	if Compare(remote, r.LocalClock) > 0 {
		r.LocalClock = Clock{Wall: remote.Wall, Counter: remote.Counter, NodeID: r.InstanceID}
	}
}

// SetField writes a namespaced field at wall time. The path must already use
// shared/ or instance/<id>/.
func (r *Replica) SetField(path string, value any, deleted bool, wall int64) (Field, error) {
	if err := ValidatePath(path); err != nil {
		return Field{}, err
	}
	if err := RejectSecrets(path, value); err != nil {
		return Field{}, err
	}
	field := Field{Path: path, Value: value, Deleted: deleted, Clock: r.Tick(wall), Writer: r.InstanceID}
	if r.Fields == nil {
		r.Fields = map[string]Field{}
	}
	r.Fields[path] = field
	return field, nil
}

// QueueEdit records a change for target. The field is stored immediately so
// other replicas can carry it, and the edit stays pending until target applies it.
func (r *Replica) QueueEdit(target, path string, value any, deleted bool, wall int64) (Edit, error) {
	if target == "" {
		return Edit{}, fmt.Errorf("target instance is required")
	}
	if !strings.HasPrefix(path, "shared/") && !strings.HasPrefix(path, "instance/") {
		path = InstancePath(target, path)
	}
	field, err := r.SetField(path, value, deleted, wall)
	if err != nil {
		return Edit{}, err
	}
	edit := Edit{
		ID:               fmt.Sprintf("%s|%s|%d|%d|%s", target, path, field.Clock.Wall, field.Clock.Counter, field.Clock.NodeID),
		TargetInstanceID: target,
		Path:             path,
		Value:            value,
		Deleted:          deleted,
		Clock:            field.Clock,
		OriginInstanceID: r.InstanceID,
		Status:           StatusPending,
	}
	if target == r.InstanceID {
		edit.Status = StatusApplied
	}
	r.upsertEdit(edit)
	return edit, nil
}

// PublishConnection records how to reach instanceID. API keys are not accepted.
func (r *Replica) PublishConnection(conn Connection, wall int64) (Connection, error) {
	if conn.InstanceID == "" {
		return Connection{}, fmt.Errorf("connection instance id is required")
	}
	if err := RejectSecrets(conn.APIURL, conn.Label); err != nil {
		return Connection{}, err
	}
	conn.Clock = r.Tick(wall)
	if conn.LastSeen == "" && conn.Online {
		conn.LastSeen = time.UnixMilli(conn.Clock.Wall).UTC().Format(time.RFC3339)
	}
	if r.Connections == nil {
		r.Connections = map[string]Connection{}
	}
	r.Connections[conn.InstanceID] = conn
	return conn, nil
}

func (r *Replica) upsertEdit(edit Edit) {
	for i := range r.Pending {
		if r.Pending[i].ID == edit.ID {
			r.Pending[i] = edit
			return
		}
	}
	r.Pending = append(r.Pending, edit)
}

// Merge folds remote into a copy of local. The result keeps local's instance
// id. Pending edits for other instances stay queued so this engine can relay
// them. Pending edits for this engine are applied when their clock wins.
func Merge(local, remote Replica) (Replica, MergeStats) {
	out := cloneReplica(local)
	var stats MergeStats
	if out.GroupID == "" {
		out.GroupID = remote.GroupID
	}
	if out.Fields == nil {
		out.Fields = map[string]Field{}
	}
	if out.Connections == nil {
		out.Connections = map[string]Connection{}
	}
	for path, field := range remote.Fields {
		out.witness(field.Clock)
		current, ok := out.Fields[path]
		if !ok || Compare(field.Clock, current.Clock) > 0 {
			out.Fields[path] = field
			stats.FieldsAccepted++
			continue
		}
		stats.FieldsKept++
	}
	for id, conn := range remote.Connections {
		out.witness(conn.Clock)
		current, ok := out.Connections[id]
		if !ok || Compare(conn.Clock, current.Clock) > 0 {
			out.Connections[id] = conn
		}
	}
	for _, edit := range remote.Pending {
		out.witness(edit.Clock)
		merged := edit
		if merged.TargetInstanceID == out.InstanceID && merged.Status == StatusPending {
			current, ok := out.Fields[merged.Path]
			if !ok || Compare(merged.Clock, current.Clock) >= 0 {
				if err := RejectSecrets(merged.Path, merged.Value); err == nil && ValidatePath(merged.Path) == nil {
					out.Fields[merged.Path] = Field{Path: merged.Path, Value: merged.Value, Deleted: merged.Deleted, Clock: merged.Clock, Writer: merged.OriginInstanceID}
					merged.Status = StatusApplied
					stats.EditsApplied++
				}
			} else {
				merged.Status = StatusSuperseded
			}
		} else if merged.Status == StatusPending {
			stats.EditsRelayed++
		}
		out.upsertEdit(merged)
	}
	sort.Slice(out.Pending, func(i, j int) bool { return out.Pending[i].ID < out.Pending[j].ID })
	return out, stats
}

// Instances lists the local engine first, then every known linked engine.
func (r Replica) Instances() []InstanceView {
	ids := map[string]struct{}{r.InstanceID: {}}
	for id := range r.Connections {
		ids[id] = struct{}{}
	}
	for _, edit := range r.Pending {
		if edit.TargetInstanceID != "" {
			ids[edit.TargetInstanceID] = struct{}{}
		}
	}
	views := make([]InstanceView, 0, len(ids))
	for id := range ids {
		conn := r.Connections[id]
		label := conn.Label
		if label == "" {
			label = id
		}
		pending := 0
		for _, edit := range r.Pending {
			if edit.TargetInstanceID == id && edit.Status == StatusPending {
				pending++
			}
		}
		online := conn.Online
		if id == r.InstanceID {
			online = true
			if label == id {
				label = "This engine"
			}
		}
		views = append(views, InstanceView{
			InstanceID:     id,
			Label:          label,
			APIURL:         conn.APIURL,
			Online:         online,
			LastSeen:       conn.LastSeen,
			PendingChanges: pending,
			Local:          id == r.InstanceID,
		})
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Local != views[j].Local {
			return views[i].Local
		}
		return views[i].Label < views[j].Label
	})
	return views
}

// ValidatePath requires the shared or per-instance namespace.
func ValidatePath(path string) error {
	if path == "" || strings.Contains(path, "..") {
		return fmt.Errorf("config path is required")
	}
	if strings.HasPrefix(path, "shared/") && len(path) > len("shared/") {
		return nil
	}
	if strings.HasPrefix(path, "instance/") {
		rest := strings.TrimPrefix(path, "instance/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return nil
		}
	}
	return fmt.Errorf("config path %q must start with shared/ or instance/<id>/", path)
}

// RejectSecrets refuses paths and string values that would copy credentials
// into the replicated database or the browser.
func RejectSecrets(values ...any) error {
	for _, value := range values {
		if secretString(fmt.Sprint(value)) {
			return fmt.Errorf("config sync refuses secret-bearing fields")
		}
		if mapped, ok := value.(map[string]any); ok {
			for key, nested := range mapped {
				if err := RejectSecrets(key, nested); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func secretString(value string) bool {
	folded := strings.ToLower(value)
	for _, marker := range []string{"apikey", "api_key", "privatekey", "private_key", "password", "secret", "credential"} {
		if strings.Contains(folded, marker) {
			return true
		}
	}
	return false
}

func cloneReplica(in Replica) Replica {
	out := in
	out.Fields = map[string]Field{}
	for path, field := range in.Fields {
		out.Fields[path] = field
	}
	out.Connections = map[string]Connection{}
	for id, conn := range in.Connections {
		conn.Addresses = append([]string(nil), conn.Addresses...)
		out.Connections[id] = conn
	}
	out.Pending = append([]Edit(nil), in.Pending...)
	return out
}
