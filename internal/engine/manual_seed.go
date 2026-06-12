package engine

import (
	"filesyncengine/internal/config"
	"filesyncengine/internal/scanner"
)

const HashStateAssumedValidUnverified = "assumed-valid-unverified"

type ManualSeedAdoptionResult struct {
	Adopted int
	Skipped int
}

func (e Engine) AdoptManualSeed(folder config.FolderConfig, authoritative map[string]scanner.File) (ManualSeedAdoptionResult, error) {
	result := ManualSeedAdoptionResult{}
	for rel, remote := range authoritative {
		local, ok, err := e.store.LoadManifest(folder.ID, rel)
		if err != nil {
			return ManualSeedAdoptionResult{}, err
		}
		if !ok || local.HashState == "complete" || local.Size != remote.Manifest.Size {
			result.Skipped++
			continue
		}
		adopted := remote.Manifest
		adopted.HashState = HashStateAssumedValidUnverified
		adopted.SeedBaselineModTimeUnixNano = local.ModTimeUnixNano
		adopted.SeedBaselineChangeTimeUnixNano = local.ChangeTimeUnixNano
		if err := e.store.SaveManifest(folder.ID, rel, adopted); err != nil {
			return ManualSeedAdoptionResult{}, err
		}
		result.Adopted++
	}
	return result, nil
}
