package tools

import "testing"

func i64(v int64) *int64 { return &v }

func TestSubtaskPatchNormalizeInfersMissingOp(t *testing.T) {
	p := SubtaskPatch{Message: "m", Operations: SubtaskOperations{
		{Title: "t", Description: "d"},    // no op, no id  -> add
		{ID: i64(7), Title: "t2"},         // no op, known id -> modify
		{Op: SubtaskOpRemove, ID: i64(9)}, // explicit op    -> untouched
	}}
	p.Normalize()

	if string(p.Operations[0].Op) != string(SubtaskOpAdd) {
		t.Fatalf("op0 = %q, want add", p.Operations[0].Op)
	}
	if string(p.Operations[1].Op) != string(SubtaskOpModify) {
		t.Fatalf("op1 = %q, want modify", p.Operations[1].Op)
	}
	if string(p.Operations[2].Op) != string(SubtaskOpRemove) {
		t.Fatalf("op2 = %q, want remove (explicit op must not be rewritten)", p.Operations[2].Op)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("validate after normalize must pass, got %v", err)
	}
}

func TestSubtaskPatchWithoutNormalizeStillRejectsEmptyOp(t *testing.T) {
	p := SubtaskPatch{Message: "m", Operations: SubtaskOperations{{Title: "t", Description: "d"}}}
	if err := p.Validate(); err == nil {
		t.Fatal("an empty op without Normalize must still be rejected")
	}
}
