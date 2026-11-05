package skills

import (
	"strings"
	"testing"
)

func TestParsePackIDs_ValidatesAndDeduplicates(t *testing.T) {
	if ids, err := ParsePackIDs(nil); err != nil || ids != nil {
		t.Fatalf("empty selection = %v, %v", ids, err)
	}
	if ids, err := ParsePackIDs([]string{"  ", ""}); err != nil || ids != nil {
		t.Fatalf("blank selection = %v, %v", ids, err)
	}

	const pack = "a3f1c2d4-0000-4000-8000-000000000000"
	ids, err := ParsePackIDs([]string{pack, " " + strings.ToUpper(pack) + " "})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ids) != 1 || ids[0].String() != pack {
		t.Fatalf("ids = %v", ids)
	}

	if _, err := ParsePackIDs([]string{"not-a-uuid"}); err == nil {
		t.Fatal("an invalid pack id was accepted")
	}
}

func TestNormalisePackIDs_DropsBlanksAndDuplicates(t *testing.T) {
	got := normalisePackIDs([]string{" a ", "", "a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("normalised = %v", got)
	}
	if normalisePackIDs([]string{"", " "}) != nil {
		t.Fatal("a blank selection must normalise to nil so the filter stays unset")
	}
}

func TestScopedStore_Allows(t *testing.T) {
	packA := "a3f1c2d4-0000-4000-8000-00000000000a"
	packB := "a3f1c2d4-0000-4000-8000-00000000000b"

	scoped := &ScopedStore{packs: []string{packA}}
	if !scoped.allows(&Document{PackID: &packA}) {
		t.Error("a document of the selected pack was refused")
	}
	if scoped.allows(&Document{PackID: &packB}) {
		t.Error("a document of another pack was allowed")
	}
	if scoped.allows(&Document{}) {
		t.Error("a document without a pack was allowed by a scoped store")
	}

	unscoped := &ScopedStore{}
	if !unscoped.allows(&Document{}) || !unscoped.allows(&Document{PackID: &packB}) {
		t.Error("an empty selection must allow every document")
	}
}

func TestStore_ScopeKeepsTheSelection(t *testing.T) {
	store := &Store{}
	scoped := store.Scope([]string{" a ", "a", "b"})
	if packs := scoped.Packs(); len(packs) != 2 || packs[0] != "a" || packs[1] != "b" {
		t.Fatalf("packs = %v", packs)
	}
	if scoped.Unscoped() != store {
		t.Fatal("the scope lost its store")
	}
}
