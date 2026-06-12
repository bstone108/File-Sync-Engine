package metadatacontrol

import (
	"fmt"

	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func Compact(cfg config.Config, store state.JSONStore, folderID string) ([]state.MetadataCompactionResult, error) {
	peerIDs := make([]string, 0, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		peerIDs = append(peerIDs, peer.ID)
	}
	results := []state.MetadataCompactionResult{}
	matched := 0
	for _, folder := range cfg.Folders {
		if folderID != "" && folder.ID != folderID {
			continue
		}
		matched++
		result, err := store.CompactFolderMetadata(folder.ID, state.MetadataCompactionPolicy{PeerIDs: peerIDs})
		if err != nil {
			return nil, fmt.Errorf("compact %s: %w", folder.ID, err)
		}
		results = append(results, result)
	}
	if folderID != "" && matched == 0 {
		return nil, fmt.Errorf("folder %q not found", folderID)
	}
	return results, nil
}
