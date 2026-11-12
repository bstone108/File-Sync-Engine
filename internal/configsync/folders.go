package configsync

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AddedFolder is a folder an identity engine has created. Path stays on that
// engine. Other engines list it as available until they add a local path.
type AddedFolder struct {
	ID   string
	Mode string
}

// RemoteAddedFolders reads the local config-sync replica and reports which
// folder ids each other identity engine has added. Keys are DHT peer ids and
// identity public keys.
func RemoteAddedFolders(configPath string) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	replica, ok := readReplica(configPath)
	if !ok {
		return out
	}
	for id, conn := range replica.Connections {
		if id == replica.InstanceID || len(conn.AddedFolders) == 0 {
			continue
		}
		addRemoteFolders(out, conn.DHTPeerID, conn.AddedFolders)
		addRemoteFolders(out, conn.DiscoveryID, conn.AddedFolders)
	}
	return out
}

// AvailableFolders are folder ids another identity engine has added that this
// engine has not. Mode is a hint from the publisher.
func AvailableFolders(configPath string, localIDs map[string]struct{}) []AddedFolder {
	replica, ok := readReplica(configPath)
	if !ok {
		return nil
	}
	seen := map[string]AddedFolder{}
	for id, conn := range replica.Connections {
		if id == replica.InstanceID {
			continue
		}
		for _, folderID := range conn.AddedFolders {
			if folderID == "" {
				continue
			}
			if _, have := localIDs[folderID]; have {
				continue
			}
			if _, ok := seen[folderID]; ok {
				continue
			}
			seen[folderID] = AddedFolder{ID: folderID, Mode: "sendrecv"}
		}
	}
	out := make([]AddedFolder, 0, len(seen))
	for _, folder := range seen {
		out = append(out, folder)
	}
	return out
}

func readReplica(configPath string) (Replica, bool) {
	path := filepath.Join(filepath.Dir(configPath), "config-sync.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return Replica{}, false
	}
	var replica Replica
	if err := json.Unmarshal(raw, &replica); err != nil {
		return Replica{}, false
	}
	return replica, true
}

func addRemoteFolders(out map[string]map[string]struct{}, key string, folders []string) {
	if key == "" {
		return
	}
	set := out[key]
	if set == nil {
		set = map[string]struct{}{}
		out[key] = set
	}
	for _, id := range folders {
		if id != "" {
			set[id] = struct{}{}
		}
	}
}
