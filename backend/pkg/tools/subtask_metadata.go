package tools

// Subtask plan vocabulary (see to-implement/PLAN_extended-version.md § 3.9).
//
// The generator and the refiner may attach a plan step (`type`) and a list of
// expected proof classes (`evidence`) to each subtask, plus a planned
// wall-clock allowance (`budget_seconds`). All three are optional and the
// vocabulary is open: a value outside these lists is kept verbatim rather than
// rejected, so a model that invents one never fails the plan; the lists below
// are the reference for logging and for the UI colours.
//
// Keep the kebab-case values in sync with the `type`/`evidence` description in
// the generator and refiner prompts.

// KnownSubtaskTypes are the pentest steps a subtask can be assigned.
var KnownSubtaskTypes = []string{
	"recon",
	"enumeration",
	"vulnerability-analysis",
	"web-testing",
	"exploitation",
	"credential-access",
	"privilege-escalation",
	"lateral-movement",
	"collection",
	"exfiltration",
	"persistence",
	"research",
	"analysis",
	"verification",
	"setup",
	"coding",
	"reporting",
}

// KnownEvidenceClasses are the proof classes a subtask can be expected to
// produce. They are grouped by phase for readability only.
var KnownEvidenceClasses = []string{
	// Discovery / reconnaissance.
	"osint-evidence",
	"http-discovery-evidence",
	"network-exposure-evidence",
	"service-banner-evidence",
	"dns-evidence",
	"vhost-evidence",
	"tls-certificate-evidence",
	"technology-fingerprint-evidence",
	"cloud-metadata-evidence",
	// Vulnerability identification.
	"vulnerable-component-evidence",
	"misconfiguration-evidence",
	"information-disclosure-evidence",
	"default-credentials-evidence",
	"weak-crypto-evidence",
	// Exploitation / weakness classes.
	"injection-evidence",
	"rce-evidence",
	"xss-evidence",
	"xxe-evidence",
	"ssti-evidence",
	"ssrf-evidence",
	"deserialization-evidence",
	"file-upload-evidence",
	"path-traversal-evidence",
	"csrf-evidence",
	"open-redirect-evidence",
	"auth-bypass-evidence",
	"access-control-evidence",
	"waf-bypass-evidence",
	"race-condition-evidence",
	"business-logic-evidence",
	// Credentials / access.
	"credential-evidence",
	"session-evidence",
	"hash-cracking-evidence",
	"valid-access-evidence",
	// Post-exploitation.
	"privilege-escalation-evidence",
	"lateral-movement-evidence",
	"credential-dump-evidence",
	"persistence-evidence",
	"data-exfiltration-evidence",
	"objective-achieved-evidence",
	// Verification / reporting.
	"proof-of-exploitation-evidence",
	"reproducibility-evidence",
	"negative-control-evidence",
	"screenshot-evidence",
	"log-evidence",
	"finding-report-evidence",
}

var knownSubtaskTypes = func() map[string]bool {
	set := make(map[string]bool, len(KnownSubtaskTypes))
	for _, t := range KnownSubtaskTypes {
		set[t] = true
	}
	return set
}()

var knownEvidenceClasses = func() map[string]bool {
	set := make(map[string]bool, len(KnownEvidenceClasses))
	for _, e := range KnownEvidenceClasses {
		set[e] = true
	}
	return set
}()

// SubtaskMetadataNone is the sentinel a modify operation sends to clear a
// metadata field, since an omitted field means "keep the existing value".
const SubtaskMetadataNone = "none"

// IsKnownSubtaskType reports whether a subtask type is part of the reference
// vocabulary. An unknown value is not an error; it is only worth a log line.
func IsKnownSubtaskType(t string) bool {
	return knownSubtaskTypes[t]
}

// IsKnownEvidenceClass reports whether an evidence class is part of the
// reference vocabulary. An unknown value is not an error.
func IsKnownEvidenceClass(e string) bool {
	return knownEvidenceClasses[e]
}

// UnknownSubtaskMetadata returns the subtask type and evidence classes that are
// not part of the reference vocabulary, for a single log line at plan time.
func UnknownSubtaskMetadata(subtaskType string, evidence []string) (unknownType string, unknownEvidence []string) {
	if subtaskType != "" && !IsKnownSubtaskType(subtaskType) {
		unknownType = subtaskType
	}
	for _, e := range evidence {
		if e != "" && !IsKnownEvidenceClass(e) {
			unknownEvidence = append(unknownEvidence, e)
		}
	}
	return unknownType, unknownEvidence
}
