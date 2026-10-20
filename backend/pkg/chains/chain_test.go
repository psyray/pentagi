package chains

import "testing"

func TestChain_Validate_AcceptsAWellFormedChain(t *testing.T) {
	t.Parallel()

	c := Chain{
		ID: "x", Name: "n", Description: "d", CVSS: 5, Severity: SeverityMedium,
		Links: []Link{{Name: "step", Verify: "read a header"}},
	}

	if err := c.Validate(); err != nil {
		t.Fatalf("a well-formed chain was rejected: %v", err)
	}
}

func TestChain_Validate_RejectsTheMissingPiece(t *testing.T) {
	t.Parallel()

	base := Chain{
		ID: "x", Name: "n", Description: "d", CVSS: 5, Severity: SeverityHigh,
		Links: []Link{{Name: "step", Verify: "read a header"}},
	}

	for name, mutate := range map[string]func(*Chain){
		"no id":          func(c *Chain) { c.ID = "  " },
		"no name":        func(c *Chain) { c.Name = "" },
		"no description": func(c *Chain) { c.Description = "" },
		"cvss above ten": func(c *Chain) { c.CVSS = 10.5 },
		"cvss below zero": func(c *Chain) {
			c.CVSS = -1
		},
		"unknown severity": func(c *Chain) { c.Severity = "spicy" },
		"no links":         func(c *Chain) { c.Links = nil },
		"link without name": func(c *Chain) {
			c.Links = []Link{{Verify: "read a header"}}
		},
		"link without verify": func(c *Chain) { c.Links = []Link{{Name: "step"}} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := base
			c.Links = append([]Link(nil), base.Links...)
			mutate(&c)

			if err := c.Validate(); err == nil {
				t.Fatalf("expected an error for %q", name)
			}
		})
	}
}

func TestChain_CVEsNormalized_UpperCasesAndTrims(t *testing.T) {
	t.Parallel()

	c := Chain{CVEs: []string{" cve-2021-44228 ", "CVE-2022-0001"}}
	got := c.CVEsNormalized()

	if len(got) != 2 || got[0] != "CVE-2021-44228" || got[1] != "CVE-2022-0001" {
		t.Fatalf("unexpected normalization: %#v", got)
	}
}

func TestParse_RejectsMalformedYAML(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]byte("id: [unclosed")); err == nil {
		t.Fatal("expected a parse error")
	}
}
