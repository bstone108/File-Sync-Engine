package syncplan

import (
	"fmt"
	pathpkg "path"
	"strings"
)

type FolderMode string

const (
	ModeSendReceive FolderMode = "sendrecv"
	ModeSendOnly    FolderMode = "sendonly"
	ModeReceiveOnly FolderMode = "recvonly"
)

type Policy struct {
	AdvertiseLocalChanges bool
	ApplyRemoteChanges    bool
	ReportLocalDrift      bool
}

func PolicyForMode(mode FolderMode) Policy {
	switch mode {
	case ModeSendOnly:
		return Policy{AdvertiseLocalChanges: true}
	case ModeReceiveOnly:
		return Policy{ApplyRemoteChanges: true, ReportLocalDrift: true}
	default:
		return Policy{AdvertiseLocalChanges: true, ApplyRemoteChanges: true}
	}
}

type FileVersion struct {
	Path     string
	Version  uint64
	DeviceID string
	Hash     string
}

type Action string

const (
	ActionNoop         Action = "noop"
	ActionSendLocal    Action = "send_local"
	ActionApplyRemote  Action = "apply_remote"
	ActionConflictCopy Action = "conflict_copy"
	ActionReportDrift  Action = "report_drift"
)

type Decision struct {
	Action          Action
	Reason          string
	KeepLocalPath   string
	ApplyRemotePath string
}

func Decide(local FileVersion, remote FileVersion, mode FolderMode) Decision {
	if local.Hash == remote.Hash {
		return Decision{Action: ActionNoop, Reason: "hashes match"}
	}
	policy := PolicyForMode(mode)
	if policy.ReportLocalDrift && local.Hash != remote.Hash {
		return Decision{Action: ActionReportDrift, Reason: "receive-only folder has local drift"}
	}
	if !policy.ApplyRemoteChanges {
		return Decision{Action: ActionSendLocal, Reason: "send-only folder advertises local version"}
	}
	if !policy.AdvertiseLocalChanges {
		return Decision{Action: ActionApplyRemote, Reason: "receive-only folder applies remote version"}
	}
	if local.Version == remote.Version && local.DeviceID != remote.DeviceID {
		return Decision{
			Action:          ActionConflictCopy,
			Reason:          "concurrent divergent edits",
			KeepLocalPath:   local.Path,
			ApplyRemotePath: conflictPath(remote.Path, remote.DeviceID),
		}
	}
	if remote.Version > local.Version {
		return Decision{Action: ActionApplyRemote, Reason: "remote version is newer"}
	}
	return Decision{Action: ActionSendLocal, Reason: "local version is newer"}
}

func conflictPath(path string, deviceID string) string {
	ext := pathpkg.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return fmt.Sprintf("%s.sync-conflict-%s%s", base, deviceID, ext)
}
