package skills

import (
	"testing"
)

func missionDocument(slug string, services []string, os, platform string, tags []string) Document {
	metadata := map[string]any{}
	if len(services) > 0 {
		metadata["services"] = services
	}
	if os != "" {
		metadata["os"] = os
	}
	metadata["outcome"] = "user"

	doc := Document{
		Slug:     slug,
		Kind:     KindEngagement,
		Title:    slug,
		Tags:     tags,
		Metadata: metadata,
	}
	if platform != "" {
		doc.Metadata["platform"] = []string{platform}
	}

	return doc
}

func TestNormalizeService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"lowercases and orders port first", "HTTPS/443", "443/https"},
		{"keeps a canonical value", "443/https", "443/https"},
		{"strips the surrounding spaces", "  22 / SSH ", "22/ssh"},
		{"reorders a name first fingerprint", "jetdirect/9100", "9100/jetdirect"},
		{"accepts a bare service", "SSH", "ssh"},
		{"accepts a bare port", "445", "445"},
		{"keeps a name and name pair", "tcp/smb", "tcp/smb"},
		{"drops an empty value", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeService(tt.value); got != tt.want {
				t.Fatalf("NormalizeService(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestNormalizeServices_DropsTheDuplicates(t *testing.T) {
	t.Parallel()

	got := NormalizeServices([]string{"80/HTTP", "80/http", " 22/ssh ", ""})
	want := []string{"80/http", "22/ssh"}

	if len(got) != len(want) {
		t.Fatalf("NormalizeServices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeServices = %v, want %v", got, want)
		}
	}
}

func TestScoreEngagement_Weights(t *testing.T) {
	t.Parallel()

	doc := missionDocument("lpd", []string{"22/ssh", "80/http", "9100/jetdirect"}, "linux", "HTB", []string{"lpd", "privesc"})

	tests := []struct {
		name       string
		query      EngagementQuery
		wantScore  float64
		wantMatchd []string
	}{
		{
			name:       "an exact service is the strongest signal",
			query:      EngagementQuery{Services: []string{"9100/jetdirect"}},
			wantScore:  engagementServiceExactWeight,
			wantMatchd: []string{"9100/jetdirect"},
		},
		{
			name:       "a shared port only still counts",
			query:      EngagementQuery{Services: []string{"9100/ipp"}},
			wantScore:  engagementServicePortWeight,
			wantMatchd: []string{"9100/jetdirect"},
		},
		{
			name:       "a shared name only still counts",
			query:      EngagementQuery{Services: []string{"8080/http"}},
			wantScore:  engagementServiceNameWeight,
			wantMatchd: []string{"80/http"},
		},
		{
			name:       "a repeated query service is scored once",
			query:      EngagementQuery{Services: []string{"80/http", "80/http"}},
			wantScore:  engagementServiceExactWeight,
			wantMatchd: []string{"80/http"},
		},
		{
			name:      "the os adds its weight",
			query:     EngagementQuery{OS: "Linux"},
			wantScore: engagementOSWeight,
		},
		{
			name:      "the platform adds its weight",
			query:     EngagementQuery{Platform: "htb"},
			wantScore: engagementPlatformWeight,
		},
		{
			name:      "a tag is case insensitive",
			query:     EngagementQuery{Tags: []string{"LPD"}},
			wantScore: engagementTagWeight,
		},
		{
			name: "every criterion adds up",
			query: EngagementQuery{
				Services: []string{"80/http"},
				OS:       "linux",
				Platform: "HTB",
				Tags:     []string{"privesc"},
			},
			wantScore:  engagementServiceExactWeight + engagementOSWeight + engagementPlatformWeight + engagementTagWeight,
			wantMatchd: []string{"80/http"},
		},
		{
			name:      "an unrelated target scores nothing",
			query:     EngagementQuery{Services: []string{"3389/rdp"}, OS: "windows"},
			wantScore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			score, matched := ScoreEngagement(doc, normalizeEngagementQuery(tt.query))
			if score != tt.wantScore {
				t.Fatalf("score = %v, want %v", score, tt.wantScore)
			}
			if len(matched) != len(tt.wantMatchd) {
				t.Fatalf("matched = %v, want %v", matched, tt.wantMatchd)
			}
			for i := range tt.wantMatchd {
				if matched[i] != tt.wantMatchd[i] {
					t.Fatalf("matched = %v, want %v", matched, tt.wantMatchd)
				}
			}
		})
	}
}

func TestRankEngagements_OrdersAndFilters(t *testing.T) {
	t.Parallel()

	documents := []Document{
		missionDocument("alpha", []string{"22/ssh", "80/http", "9100/jetdirect"}, "linux", "HTB", []string{"lpd"}),
		missionDocument("beta", []string{"443/https"}, "windows", "HTB", []string{"kerberos"}),
		missionDocument("gamma", []string{"80/http"}, "linux", "OSCP", []string{"web"}),
		{Slug: "a-skill", Kind: KindSkill, Title: "not a mission"},
	}

	slugs := func(matches []EngagementMatch) []string {
		out := make([]string, len(matches))
		for i, match := range matches {
			out[i] = match.Document.Slug
		}
		return out
	}

	equal := func(t *testing.T, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}

	t.Run("the best service overlap comes first", func(t *testing.T) {
		t.Parallel()
		got := slugs(RankEngagements(documents, EngagementQuery{Services: []string{"9100/jetdirect", "80/http"}}))
		equal(t, got, []string{"alpha", "gamma"})
	})

	t.Run("a tie is broken by slug", func(t *testing.T) {
		t.Parallel()
		got := slugs(RankEngagements(documents, EngagementQuery{Services: []string{"80/http"}}))
		equal(t, got, []string{"alpha", "gamma"})
	})

	t.Run("an unmatched target drops every document", func(t *testing.T) {
		t.Parallel()
		got := slugs(RankEngagements(documents, EngagementQuery{Services: []string{"3389/rdp"}}))
		equal(t, got, nil)
	})

	t.Run("an empty query keeps the recency order", func(t *testing.T) {
		t.Parallel()
		got := slugs(RankEngagements(documents, EngagementQuery{}))
		equal(t, got, []string{"alpha", "beta", "gamma"})
	})

	t.Run("the limit truncates the ranked list", func(t *testing.T) {
		t.Parallel()
		got := slugs(RankEngagements(documents, EngagementQuery{OS: "linux", Limit: 1}))
		equal(t, got, []string{"alpha"})
	})

	t.Run("a skill is never a mission", func(t *testing.T) {
		t.Parallel()
		for _, match := range RankEngagements(documents, EngagementQuery{}) {
			if match.Document.Kind != KindEngagement {
				t.Fatalf("ranked a %s document", match.Document.Kind)
			}
		}
	})
}
