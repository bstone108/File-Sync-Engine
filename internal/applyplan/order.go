package applyplan

import "sort"

type Write struct {
	Path        string
	BytesNeeded int64
}

type Delete struct {
	Path       string
	BytesFreed int64
}

type Changes struct {
	Writes  []Write
	Deletes []Delete
}

type Space struct {
	Available int64
}

type StepKind string

const (
	StepWrite  StepKind = "write"
	StepDelete StepKind = "delete"
)

type Step struct {
	Kind  StepKind
	Path  string
	Bytes int64
}

type Plan struct {
	Steps []Step
}

func Order(changes Changes, space Space) Plan {
	available := space.Available
	deletes := append([]Delete(nil), changes.Deletes...)
	plan := Plan{}

	for _, write := range changes.Writes {
		for available < write.BytesNeeded && len(deletes) > 0 {
			idx := bestDeleteIndex(deletes)
			delete := deletes[idx]
			plan.Steps = append(plan.Steps, Step{Kind: StepDelete, Path: delete.Path, Bytes: delete.BytesFreed})
			available += delete.BytesFreed
			deletes = append(deletes[:idx], deletes[idx+1:]...)
		}
		plan.Steps = append(plan.Steps, Step{Kind: StepWrite, Path: write.Path, Bytes: write.BytesNeeded})
		available -= write.BytesNeeded
	}

	sort.SliceStable(deletes, func(i, j int) bool { return deletes[i].Path < deletes[j].Path })
	for _, delete := range deletes {
		plan.Steps = append(plan.Steps, Step{Kind: StepDelete, Path: delete.Path, Bytes: delete.BytesFreed})
	}
	return plan
}

func bestDeleteIndex(deletes []Delete) int {
	best := 0
	for i := 1; i < len(deletes); i++ {
		if deletes[i].BytesFreed > deletes[best].BytesFreed {
			best = i
		}
	}
	return best
}
