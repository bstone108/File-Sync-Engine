package applyplan

import "testing"

func TestPlanAppliesWritesBeforeDeletesWhenSpaceAvailable(t *testing.T) {
	changes := Changes{
		Writes:  []Write{{Path: "new.bin", BytesNeeded: 20}},
		Deletes: []Delete{{Path: "old.bin", BytesFreed: 100}},
	}
	plan := Order(changes, Space{Available: 50})
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	if plan.Steps[0].Kind != StepWrite || plan.Steps[0].Path != "new.bin" {
		t.Fatalf("first step = %+v, want write", plan.Steps[0])
	}
	if plan.Steps[1].Kind != StepDelete {
		t.Fatalf("second step = %+v, want delete", plan.Steps[1])
	}
}

func TestPlanFreesSpaceBeforeNextWriteWhenInsufficient(t *testing.T) {
	changes := Changes{
		Writes: []Write{
			{Path: "a.bin", BytesNeeded: 40},
			{Path: "b.bin", BytesNeeded: 80},
		},
		Deletes: []Delete{
			{Path: "small.old", BytesFreed: 20},
			{Path: "big.old", BytesFreed: 100},
		},
	}
	plan := Order(changes, Space{Available: 50})
	want := []StepKind{StepWrite, StepDelete, StepWrite, StepDelete}
	if len(plan.Steps) != len(want) {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	for i, kind := range want {
		if plan.Steps[i].Kind != kind {
			t.Fatalf("step %d = %+v, want kind %s", i, plan.Steps[i], kind)
		}
	}
	if plan.Steps[1].Path != "big.old" {
		t.Fatalf("expected largest useful delete before second write: %+v", plan.Steps)
	}
}
