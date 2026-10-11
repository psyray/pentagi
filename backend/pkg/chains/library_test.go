package chains

import (
	"testing"
	"testing/fstest"
)

func TestLoad_BakesAValidatedSet(t *testing.T) {
	t.Parallel()

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(lib.All()) == 0 {
		t.Fatal("no chains were baked into the binary")
	}
	for _, c := range lib.All() {
		if err := c.Validate(); err != nil {
			t.Errorf("baked chain %q is invalid: %v", c.ID, err)
		}
	}
}

func TestLibrary_ByID_IsCaseInsensitive(t *testing.T) {
	t.Parallel()

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, ok := lib.ByID(" LOG4SHELL-JNDI-CALLBACK "); !ok {
		t.Fatal("ByID should match an id regardless of case and surrounding spaces")
	}
	if _, ok := lib.ByID("does-not-exist"); ok {
		t.Fatal("ByID should miss an unknown id")
	}
}

func TestLibrary_ByCVE_MatchesRegardlessOfCase(t *testing.T) {
	t.Parallel()

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := lib.ByCVE("cve-2021-44228")
	if len(got) != 1 || got[0].ID != "log4shell-jndi-callback" {
		t.Fatalf("ByCVE returned %#v", got)
	}
	if len(lib.ByCVE("CVE-0000-0000")) != 0 {
		t.Fatal("ByCVE should miss an unknown CVE")
	}
}

func TestLibrary_ByTag_MatchesRegardlessOfCase(t *testing.T) {
	t.Parallel()

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(lib.ByTag("JWT")) == 0 {
		t.Fatal("ByTag should find the jwt chain")
	}
	if len(lib.ByTag("")) != 0 {
		t.Fatal("ByTag should return nothing for an empty tag")
	}
}

func TestLibrary_ByProduct_MatchesNameAndAffected(t *testing.T) {
	t.Parallel()

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(lib.ByProduct("log4j")) == 0 {
		t.Fatal("ByProduct should find the log4j chain")
	}
	if len(lib.ByProduct("")) != 0 {
		t.Fatal("ByProduct should return nothing for an empty needle")
	}
}

func TestLibrary_Search_RequiresEveryToken(t *testing.T) {
	t.Parallel()

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := lib.Search("jwt none"); len(got) != 1 || got[0].ID != "jwt-alg-none-accepted" {
		t.Fatalf("Search(jwt none) returned %#v", got)
	}
	if got := lib.Search("jwt log4j"); len(got) != 0 {
		t.Fatalf("Search over unrelated tokens should return nothing, got %#v", got)
	}
	if got := lib.Search("   "); len(got) != 0 {
		t.Fatalf("an empty query should return nothing, got %#v", got)
	}
}

func TestLoadFS_FailsTheWholeLoadOnOneBadChain(t *testing.T) {
	t.Parallel()

	good := `id: ok
name: ok
description: ok
cvss: 5
severity: high
links:
  - name: step
    verify: read a header
`

	t.Run("a malformed chain fails", func(t *testing.T) {
		t.Parallel()

		fsys := fstest.MapFS{
			"data/good.yaml": {Data: []byte(good)},
			"data/bad.yaml":  {Data: []byte("id: [unclosed")},
		}
		if _, err := LoadFS(fsys, "data"); err == nil {
			t.Fatal("expected LoadFS to fail on a malformed chain")
		}
	})

	t.Run("a chain without a verify step fails", func(t *testing.T) {
		t.Parallel()

		noVerify := "id: nv\nname: nv\ndescription: nv\ncvss: 5\nseverity: high\nlinks:\n  - name: step\n"
		fsys := fstest.MapFS{
			"data/good.yaml": {Data: []byte(good)},
			"data/nv.yaml":   {Data: []byte(noVerify)},
		}
		if _, err := LoadFS(fsys, "data"); err == nil {
			t.Fatal("expected LoadFS to fail when a link carries no verify step")
		}
	})

	t.Run("duplicate ids fail", func(t *testing.T) {
		t.Parallel()

		fsys := fstest.MapFS{
			"data/a.yaml": {Data: []byte(good)},
			"data/b.yaml": {Data: []byte(good)},
		}
		if _, err := LoadFS(fsys, "data"); err == nil {
			t.Fatal("expected LoadFS to fail on a duplicate id")
		}
	})

	t.Run("an empty directory fails", func(t *testing.T) {
		t.Parallel()

		if _, err := LoadFS(fstest.MapFS{}, "data"); err == nil {
			t.Fatal("expected LoadFS to fail when no chain is found")
		}
	})
}
