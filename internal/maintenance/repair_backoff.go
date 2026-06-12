package maintenance

import (
	"time"

	"filesyncengine/internal/block"
)

const (
	RepairDecisionBackoff                = "backoff"
	RepairDecisionMaxAttempts            = "max-attempts"
	RepairDecisionTrustedManifestChanged = "trusted-manifest-changed"
)

type RepairAttemptState struct {
	FolderID             string `json:"folderId"`
	Path                 string `json:"path"`
	Attempts             int    `json:"attempts"`
	LastAttemptUnixNano  int64  `json:"lastAttemptUnixNano,omitempty"`
	BackoffUntilUnixNano int64  `json:"backoffUntilUnixNano,omitempty"`
	TrustedFingerprint   string `json:"trustedFingerprint,omitempty"`
	LastError            string `json:"lastError,omitempty"`
}

type RepairBackoffStore interface {
	RepairAttemptState(folderID string, path string) (RepairAttemptState, bool, error)
	SaveRepairAttemptState(state RepairAttemptState) error
	ClearRepairAttemptState(folderID string, path string) error
}

type RepairBackoffPolicy struct {
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	MaxAttempts int
	Now         func() time.Time
}

type RepairDecision struct {
	Allowed      bool
	Reason       string
	Attempts     int
	BackoffUntil time.Time
}

func ShouldAttemptRepair(store RepairBackoffStore, folderID string, path string, trusted block.Manifest, policy RepairBackoffPolicy) (RepairDecision, error) {
	state, ok, err := store.RepairAttemptState(folderID, path)
	if err != nil {
		return RepairDecision{}, err
	}
	if !ok {
		return RepairDecision{Allowed: true}, nil
	}
	fingerprint := manifestFingerprint(trusted)
	if state.TrustedFingerprint != "" && state.TrustedFingerprint != fingerprint {
		return RepairDecision{Allowed: true, Reason: RepairDecisionTrustedManifestChanged}, nil
	}
	if policy.MaxAttempts > 0 && state.Attempts >= policy.MaxAttempts {
		return RepairDecision{Allowed: false, Reason: RepairDecisionMaxAttempts, Attempts: state.Attempts, BackoffUntil: unixNanoTime(state.BackoffUntilUnixNano)}, nil
	}
	backoffUntil := unixNanoTime(state.BackoffUntilUnixNano)
	if !backoffUntil.IsZero() && now(policy).Before(backoffUntil) {
		return RepairDecision{Allowed: false, Reason: RepairDecisionBackoff, Attempts: state.Attempts, BackoffUntil: backoffUntil}, nil
	}
	return RepairDecision{Allowed: true, Attempts: state.Attempts}, nil
}

func RecordRepairFailure(store RepairBackoffStore, folderID string, path string, trusted block.Manifest, policy RepairBackoffPolicy, message string) error {
	state, ok, err := store.RepairAttemptState(folderID, path)
	if err != nil {
		return err
	}
	fingerprint := manifestFingerprint(trusted)
	if !ok || (state.TrustedFingerprint != "" && state.TrustedFingerprint != fingerprint) {
		state = RepairAttemptState{FolderID: folderID, Path: path, TrustedFingerprint: fingerprint}
	}
	state.FolderID = folderID
	state.Path = path
	state.TrustedFingerprint = fingerprint
	state.Attempts++
	attemptedAt := now(policy)
	state.LastAttemptUnixNano = attemptedAt.UnixNano()
	state.BackoffUntilUnixNano = attemptedAt.Add(backoffDelay(state.Attempts, policy)).UnixNano()
	state.LastError = message
	return store.SaveRepairAttemptState(state)
}

func RecordRepairSuccess(store RepairBackoffStore, folderID string, path string) error {
	return store.ClearRepairAttemptState(folderID, path)
}

func backoffDelay(attempts int, policy RepairBackoffPolicy) time.Duration {
	base := policy.BaseDelay
	if base <= 0 {
		base = time.Minute
	}
	max := policy.MaxDelay
	if max <= 0 {
		max = base
	}
	if attempts < 1 {
		attempts = 1
	}
	delay := base
	for i := 1; i < attempts; i++ {
		if delay >= max/2 {
			delay = max
			break
		}
		delay *= 2
	}
	if delay > max {
		delay = max
	}
	return delay
}

func now(policy RepairBackoffPolicy) time.Time {
	if policy.Now != nil {
		return policy.Now().UTC()
	}
	return time.Now().UTC()
}

func unixNanoTime(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}
