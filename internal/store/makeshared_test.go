package store

import "testing"

// TestErrandTemplateProvenanceSame: two errand templates with the same fields but a command or a sandbox
// setting written by someone else are not identical.
func TestErrandTemplateProvenanceSame(t *testing.T) {
	me := int64(3)
	a := &ErrandTemplate{Command: "echo", TimeoutS: 60, CommandSetBy: 3, Unsandboxed: true, UnsandboxedBy: &me}
	b := *a
	other := int64(3)
	b.UnsandboxedBy = &other
	if !errandTemplateSame(a, &b) {
		t.Fatal("equal templates differ")
	}
	b.CommandSetBy = 7
	if errandTemplateSame(a, &b) {
		t.Fatal("a command written by someone else is merged")
	}
	b = *a
	other = 7
	b.UnsandboxedBy = &other
	if errandTemplateSame(a, &b) {
		t.Fatal("a sandbox setting by someone else is merged")
	}
	b = *a
	b.UnsandboxedBy = nil
	if errandTemplateSame(a, &b) {
		t.Fatal("a set sandbox setting and an unset one are merged")
	}
}
