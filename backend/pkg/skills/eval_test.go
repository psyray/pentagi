package skills

import (
	"testing"
)

func hitsFromSlugs(slugs ...string) []SearchHit {
	hits := make([]SearchHit, 0, len(slugs))
	for i, slug := range slugs {
		hits = append(hits, SearchHit{Document: Document{Slug: slug}, ChunkIndex: i})
	}
	return hits
}

func TestLoadEvalDataset_ValidatesTheCases(t *testing.T) {
	t.Parallel()

	valid := []byte(`
corpus: test
statuses: [reviewed]
cases:
  - id: one
    query: relay NTLM to the enrollment endpoint
    category: ad
    expected: [adcs-esc8]
`)
	dataset, err := LoadEvalDataset(valid)
	if err != nil {
		t.Fatalf("valid dataset: %v", err)
	}
	if len(dataset.Cases) != 1 || dataset.Cases[0].ID != "one" {
		t.Fatalf("dataset = %+v", dataset)
	}

	invalid := map[string]string{
		"no case":           "corpus: test\ncases: []\n",
		"no id":             "cases:\n  - query: q\n    expected: [a]\n",
		"no query":          "cases:\n  - id: a\n    expected: [a]\n",
		"no expected":       "cases:\n  - id: a\n    query: q\n",
		"empty slug":        "cases:\n  - id: a\n    query: q\n    expected: [' ']\n",
		"duplicate case id": "cases:\n  - id: a\n    query: q\n    expected: [a]\n  - id: a\n    query: q2\n    expected: [b]\n",
	}
	for name, content := range invalid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadEvalDataset([]byte(content)); err == nil {
				t.Fatal("an invalid dataset was accepted")
			}
		})
	}
}

func TestEvalRank_FindsTheFirstExpectedDocument(t *testing.T) {
	t.Parallel()

	hits := hitsFromSlugs("other", "adcs-esc8", "adcs-esc8-alt")

	tests := []struct {
		name     string
		expected []string
		want     int
	}{
		{"the second hit matches", []string{"adcs-esc8"}, 2},
		{"the alternate slug matches", []string{"adcs-esc8-alt"}, 3},
		{"the first expected in rank order wins", []string{"adcs-esc8-alt", "adcs-esc8"}, 2},
		{"a case difference is tolerated", []string{"ADCS-ESC8"}, 2},
		{"a missing document has rank zero", []string{"absent"}, 0},
		{"an empty expectation has rank zero", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := evalRank(tt.expected, hits); got != tt.want {
				t.Fatalf("rank = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSummariseEval_ComputesRecallAndMRR(t *testing.T) {
	t.Parallel()

	results := []EvalCaseResult{
		{ID: "a", Rank: 1},
		{ID: "b", Rank: 3},
		{ID: "c", Rank: 6},
		{ID: "d", Rank: 0, Empty: true},
	}

	recall, mrr, empty := summariseEval(results, []int{3, 5})

	if recall[3] != 0.5 {
		t.Fatalf("recall@3 = %v, want 0.5", recall[3])
	}
	if recall[5] != 0.5 {
		t.Fatalf("recall@5 = %v, want 0.5", recall[5])
	}
	wantMRR := (1 + 1.0/3 + 1.0/6) / 4
	if mrr != wantMRR {
		t.Fatalf("mrr = %v, want %v", mrr, wantMRR)
	}
	if empty != 0.25 {
		t.Fatalf("empty rate = %v, want 0.25", empty)
	}
}

func TestSummariseEvalCategories_GroupsAndSorts(t *testing.T) {
	t.Parallel()

	results := []EvalCaseResult{
		{ID: "a", Category: "web", Rank: 1},
		{ID: "b", Category: "ad", Rank: 2},
		{ID: "c", Category: "ad", Rank: 0, Empty: true},
	}

	reports := summariseEvalCategories(results, []int{3})

	if len(reports) != 2 || reports[0].Category != "ad" || reports[1].Category != "web" {
		t.Fatalf("categories = %+v", reports)
	}
	if reports[0].Cases != 2 || reports[0].Recall[3] != 0.5 {
		t.Fatalf("ad report = %+v", reports[0])
	}
	if reports[1].Cases != 1 || reports[1].Recall[3] != 1 {
		t.Fatalf("web report = %+v", reports[1])
	}
}

func TestNormaliseEvalOptions_DefaultsAndLimit(t *testing.T) {
	t.Parallel()

	opts := normaliseEvalOptions(EvalOptions{})
	if len(opts.Ks) != 2 || opts.Ks[0] != 3 || opts.Ks[1] != 5 {
		t.Fatalf("default ks = %v", opts.Ks)
	}
	if opts.Limit != 10 {
		t.Fatalf("default limit = %d", opts.Limit)
	}

	opts = normaliseEvalOptions(EvalOptions{Ks: []int{10, 2, 0}, Limit: 3})
	if opts.Ks[0] != 2 || opts.Ks[1] != 10 {
		t.Fatalf("sorted ks = %v", opts.Ks)
	}
	if opts.Limit != 10 {
		t.Fatalf("limit below the largest k = %d, want 10", opts.Limit)
	}
}

func TestEvalReport_Pass(t *testing.T) {
	t.Parallel()

	report := &EvalReport{Ks: []int{3, 5}, Recall: map[int]float64{3: 0.8, 5: 0.9}}
	if !report.Pass(0.9) {
		t.Fatal("a report at the target must pass")
	}
	if report.Pass(0.95) {
		t.Fatal("a report below the target must fail")
	}
}
