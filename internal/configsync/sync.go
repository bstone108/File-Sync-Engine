// Package configsync stores a replica of every identity-linked engine's
// settings so a GUI on any of them can view and manage the others.
//
// Each instance owns its settings. Records are keyed by instance UUID. An
// engine applies only its own record, plus queued edits whose target is that
// same UUID. Another instance's record is stored for display and remote
// management and is never copied into the local record. An edit made for
// instance X stays a pending X-targeted edit until X itself applies it.
//
// Inside one instance's record, each field has a hybrid logical clock. Merge
// keeps the later clock, so concurrent edits to different fields of that
// record both survive.
package configsync

import (
	"crypto/rand"
	"fmt"
	"regexp"
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

// Field is one setting inside a single instance's record.
type Field struct {
	Key     string `json:"key"`
	Value   any    `json:"value,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
	Clock   Clock  `json:"clock"`
	Writer  string `json:"writer"`
}

// InstanceRecord is the settings owned by one engine. Other engines store a
// copy so their GUIs can show it. They do not apply it locally.
type InstanceRecord struct {
	InstanceID string           `json:"instanceId"`
	Fields     map[string]Field `json:"fields,omitempty"`
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

// Edit is a change queued for one instance UUID. Any replica may carry it.
// Only the targeted instance applies it to its own record.
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
// Records holds every known instance, keyed by that instance's UUID.
// InstanceID is the UUID of the engine that owns this copy.
type Replica struct {
	GroupID     string                    `json:"groupId"`
	InstanceID  string                    `json:"instanceId"`
	Records     map[string]InstanceRecord `json:"records,omitempty"`
	Connections map[string]Connection     `json:"connections,omitempty"`
	Pending     []Edit                    `json:"pending,omitempty"`
	LocalClock  Clock                     `json:"localClock"`
}

// InstanceView is the GUI's row for one linked engine, including the stored
// settings for that engine. Those settings belong to that engine.
type InstanceView struct {
	InstanceID            string         `json:"instanceId"`
	Label                 string         `json:"label"`
	APIURL                string         `json:"apiURL,omitempty"`
	Online                bool           `json:"online"`
	LastSeen              string         `json:"lastSeen,omitempty"`
	PendingChanges        int            `json:"pendingChanges"`
	Local                 bool           `json:"local"`
	AverageBytesPerSecond int64          `json:"averageBytesPerSecond"`
	PendingBytes          int64          `json:"pendingBytes"`
	Settings              map[string]any `json:"settings,omitempty"`
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

var instanceIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// NewInstanceID returns a random UUID version 4 for a new engine record.
func NewInstanceID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// ValidateInstanceID requires the UUID that keys an instance record.
func ValidateInstanceID(id string) error {
	if !instanceIDPattern.MatchString(id) {
		return fmt.Errorf("instance id must be a UUID")
	}
	return nil
}

// ValidateKey accepts a bare setting name. Shared paths and instance-prefixed
// paths are rejected so a value cannot be addressed at more than one engine.
func ValidateKey(key string) error {
	if key == "" || strings.Contains(key, "..") || strings.ContainsAny(key, "/\\") {
		return fmt.Errorf("config key %q belongs to one instance and cannot be a shared or cross-instance path", key)
	}
	return nil
}

// NewReplica returns an empty replica for one engine in an identity group.
func NewReplica(groupID, instanceID string) Replica {
	if err := ValidateInstanceID(instanceID); err != nil {
		panic(err)
	}
	return Replica{
		GroupID:     groupID,
		InstanceID:  instanceID,
		Records:     map[string]InstanceRecord{},
		Connections: map[string]Connection{},
		LocalClock:  Clock{NodeID: instanceID},
	}
}

// OwnRecord returns the settings this engine applies to itself.
func (r Replica) OwnRecord() InstanceRecord {
	rec := r.Records[r.InstanceID]
	rec.InstanceID = r.InstanceID
	if rec.Fields == nil {
		rec.Fields = map[string]Field{}
	}
	return rec
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

func (r *Replica) putOwn(key string, value any, deleted bool, clock Clock) {
	if r.Records == nil {
		r.Records = map[string]InstanceRecord{}
	}
	rec := r.Records[r.InstanceID]
	rec.InstanceID = r.InstanceID
	if rec.Fields == nil {
		rec.Fields = map[string]Field{}
	}
	rec.Fields[key] = Field{Key: key, Value: value, Deleted: deleted, Clock: clock, Writer: r.InstanceID}
	r.Records[r.InstanceID] = rec
}

// SetOwn writes a setting into this engine's record only.
func (r *Replica) SetOwn(key string, value any, deleted bool, wall int64) (Field, error) {
	if err := ValidateKey(key); err != nil {
		return Field{}, err
	}
	if err := RejectSecrets(key, value); err != nil {
		return Field{}, err
	}
	clock := r.Tick(wall)
	r.putOwn(key, value, deleted, clock)
	return r.Records[r.InstanceID].Fields[key], nil
}

// QueueEdit records a change for target. The target's stored record is left
// untouched unless target is this engine, in which case the edit is applied
// to this engine's own record immediately.
func (r *Replica) QueueEdit(target, key string, value any, deleted bool, wall int64) (Edit, error) {
	if err := ValidateInstanceID(target); err != nil {
		return Edit{}, err
	}
	if err := ValidateKey(key); err != nil {
		return Edit{}, err
	}
	if err := RejectSecrets(key, value); err != nil {
		return Edit{}, err
	}
	clock := r.Tick(wall)
	edit := Edit{
		ID:               fmt.Sprintf("%s|%s|%d|%d|%s", target, key, clock.Wall, clock.Counter, clock.NodeID),
		TargetInstanceID: target,
		Path:             key,
		Value:            value,
		Deleted:          deleted,
		Clock:            clock,
		OriginInstanceID: r.InstanceID,
		Status:           StatusPending,
	}
	if target == r.InstanceID {
		r.putOwn(key, value, deleted, clock)
		edit.Status = StatusApplied
	}
	r.upsertEdit(edit)
	return edit, nil
}

// PublishConnection records how to reach instanceID. API keys are not accepted.
func (r *Replica) PublishConnection(conn Connection, wall int64) (Connection, error) {
	if err := ValidateInstanceID(conn.InstanceID); err != nil {
		return Connection{}, err
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
// id. Foreign records are stored field-by-field and never copied into the
// local record. Pending edits for other instances stay queued so this engine
// can relay them. Pending edits for this engine are applied to its own record
// when their clock wins.
func Merge(local, remote Replica) (Replica, MergeStats) {
	out := cloneReplica(local)
	var stats MergeStats
	if out.GroupID == "" {
		out.GroupID = remote.GroupID
	}
	if out.Records == nil {
		out.Records = map[string]InstanceRecord{}
	}
	if out.Connections == nil {
		out.Connections = map[string]Connection{}
	}
	for id, remoteRec := range remote.Records {
		if ValidateInstanceID(id) != nil || id == out.InstanceID {
			continue
		}
		localRec := out.Records[id]
		localRec.InstanceID = id
		if localRec.Fields == nil {
			localRec.Fields = map[string]Field{}
		}
		for key, field := range remoteRec.Fields {
			if ValidateKey(key) != nil || RejectSecrets(key, field.Value) != nil {
				continue
			}
			out.witness(field.Clock)
			current, ok := localRec.Fields[key]
			if !ok || Compare(field.Clock, current.Clock) > 0 {
				field.Key = key
				localRec.Fields[key] = field
				stats.FieldsAccepted++
				continue
			}
			stats.FieldsKept++
		}
		out.Records[id] = localRec
	}
	for id, conn := range remote.Connections {
		if ValidateInstanceID(id) != nil {
			continue
		}
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
			if ValidateKey(merged.Path) == nil && RejectSecrets(merged.Path, merged.Value) == nil {
				current, ok := out.OwnRecord().Fields[merged.Path]
				if !ok || Compare(merged.Clock, current.Clock) > 0 {
					out.putOwn(merged.Path, merged.Value, merged.Deleted, merged.Clock)
					merged.Status = StatusApplied
					stats.EditsApplied++
				} else {
					merged.Status = StatusSuperseded
				}
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
	for id := range r.Records {
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
		settings := map[string]any{}
		if rec, ok := r.Records[id]; ok {
			for key, field := range rec.Fields {
				if !field.Deleted {
					settings[key] = field.Value
				}
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
			Settings:       settings,
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
	out.Records = map[string]InstanceRecord{}
	for id, rec := range in.Records {
		cloned := InstanceRecord{InstanceID: rec.InstanceID, Fields: map[string]Field{}}
		for key, field := range rec.Fields {
			cloned.Fields[key] = field
		}
		out.Records[id] = cloned
	}
	out.Connections = map[string]Connection{}
	for id, conn := range in.Connections {
		conn.Addresses = append([]string(nil), conn.Addresses...)
		out.Connections[id] = conn
	}
	out.Pending = append([]Edit(nil), in.Pending...)
	return out
}
