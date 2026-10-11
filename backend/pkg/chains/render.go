package chains

import (
	"fmt"
	"strings"
)

// Render returns a prompt-ready, safe-verification document for a chain. The
// framing is deliberately prescriptive: prove reachability with the listed
// non-destructive checks, never with a destructive payload.
func (c Chain) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Exploit chain: %s (%s)\n\n", c.Name, c.ID)

	if c.Severity != "" || c.CVSS > 0 {
		fmt.Fprintf(&b, "Severity: %s (CVSS %.1f)", c.Severity, c.CVSS)
	}
	if cves := c.CVEsNormalized(); len(cves) > 0 {
		fmt.Fprintf(&b, " - CVEs: %s", strings.Join(cves, ", "))
	}
	b.WriteString("\n")

	if c.Product != "" {
		fmt.Fprintf(&b, "Product: %s\n", c.Product)
	}
	if c.Affected != "" {
		fmt.Fprintf(&b, "Affected: %s\n", c.Affected)
	}
	if len(c.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(c.Tags, ", "))
	}
	b.WriteString("\n")

	if strings.TrimSpace(c.Description) != "" {
		b.WriteString(strings.TrimSpace(c.Description))
		b.WriteString("\n\n")
	}

	b.WriteString("## Fingerprint (non-intrusive)\n")
	if len(c.Fingerprint.Paths) > 0 {
		fmt.Fprintf(&b, "- Paths: %s\n", strings.Join(c.Fingerprint.Paths, ", "))
	}
	for _, s := range c.Fingerprint.Signals {
		fmt.Fprintf(&b, "- Signal: %s\n", strings.TrimSpace(s))
	}
	if strings.TrimSpace(c.Fingerprint.Notes) != "" {
		fmt.Fprintf(&b, "- Notes: %s\n", strings.TrimSpace(c.Fingerprint.Notes))
	}
	b.WriteString("\n")

	b.WriteString("## Safe verification steps (detection only, no destructive payload)\n")
	for i, l := range c.Links {
		fmt.Fprintf(&b, "%d. %s", i+1, l.Name)
		if l.CVE != "" {
			fmt.Fprintf(&b, " [%s]", strings.ToUpper(strings.TrimSpace(l.CVE)))
		}
		b.WriteString("\n")
		if strings.TrimSpace(l.Enables) != "" {
			fmt.Fprintf(&b, "   Enables: %s\n", strings.TrimSpace(l.Enables))
		}
		fmt.Fprintf(&b, "   Verify (SAFE): %s\n", strings.TrimSpace(l.Verify))
	}
	b.WriteString("\n")

	if len(c.References) > 0 {
		b.WriteString("## References (external sources, read them; do not copy)")
		b.WriteString("\n")
		for _, ref := range c.References {
			if strings.TrimSpace(ref) != "" {
				fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(ref))
			}
		}
		b.WriteString("\n")
	}

	if strings.TrimSpace(c.Remediation) != "" {
		b.WriteString("## Remediation\n")
		b.WriteString(strings.TrimSpace(c.Remediation))
		b.WriteString("\n")
	}

	return b.String()
}

// RenderMatches renders a list of chains, or a short notice when none match.
func RenderMatches(chains []Chain) string {
	if len(chains) == 0 {
		return "No exploit chain matches this query."
	}
	parts := make([]string, 0, len(chains))
	for _, c := range chains {
		parts = append(parts, c.Render())
	}
	return strings.Join(parts, "\n---\n\n")
}
