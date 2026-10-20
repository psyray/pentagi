package chains

import (
	"strings"
	"testing"
)

func TestDetectorRulesReferenceExistingChains(t *testing.T) {
	rules, lib, err := loadDetection()
	if err != nil {
		t.Fatalf("loadDetection: %v", err)
	}
	if lib == nil {
		t.Fatal("library not loaded")
	}
	if len(rules) == 0 {
		t.Fatal("no detector rules loaded")
	}
	for _, r := range rules {
		if _, ok := lib.ByID(r.Chain); !ok {
			t.Errorf("rule references unknown chain id %q", r.Chain)
		}
		if len(r.AllOf) == 0 && len(r.AnyOf) == 0 {
			t.Errorf("rule for %q has no all_of/any_of", r.Chain)
		}
	}
}

func TestDetectSMBSigningNotRequired(t *testing.T) {
	// Verbatim nmap smb2-security-mode output on a host with signing optional.
	out := "# cmd: nmap --script smb2-security-mode 10.0.0.5\n" +
		"| smb2-security-mode:\n" +
		"|   3.1.1:\n" +
		"|_    Message signing enabled but not required\n"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "smb-signing-disabled" {
		t.Fatalf("expected smb-signing-disabled, got %+v", dets)
	}
}

func TestDetectSMBSigningRequiredIsNotMatched(t *testing.T) {
	out := "| smb2-security-mode:\n|   3.1.1:\n|_    Message signing enabled and required\n"
	for _, d := range Detect(out) {
		if d.ChainID == "smb-signing-disabled" {
			t.Fatalf("signing required must not match smb-signing-disabled: %+v", d)
		}
	}
}

func TestDetectNetexecSigningFalse(t *testing.T) {
	// netexec prints "signing:False" in its SMB info line.
	out := "SMB         10.0.0.5   445    HOST   [*] Windows Server ... (name:HOST) (domain:LAB) (signing:False) (SMBv1:False)"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "smb-signing-disabled" {
		t.Fatalf("expected smb-signing-disabled, got %+v", dets)
	}
}

func TestDetectADCSWebEnrollment(t *testing.T) {
	out := "Disk            IPC$   Disk\nCertEnroll      Disk   AD CS\n"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "adcs-esc8-http-enrollment" {
		t.Fatalf("expected adcs-esc8-http-enrollment, got %+v", dets)
	}
}

func TestDetectKerberoastableSPN(t *testing.T) {
	out := "dn: CN=svc,CN=Users,DC=lab,DC=local\nservicePrincipalName: MSSQLSvc/host.lab.local:1433\n"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "kerberoastable-service-account" {
		t.Fatalf("expected kerberoastable-service-account, got %+v", dets)
	}
}

func TestDetectConstrainedDelegation(t *testing.T) {
	out := "dn: CN=svc,CN=Users,DC=lab,DC=local\nmsDS-AllowedToDelegateTo: CIFS/host.lab.local\n"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "constrained-delegation-allowed-to-delegate" {
		t.Fatalf("expected constrained-delegation-allowed-to-delegate, got %+v", dets)
	}
}

func TestDetectConstrainedDelegationFilterIsNotMatched(t *testing.T) {
	// Le filtre LDAP lui-meme n'est pas une preuve d'attribut renseigne.
	out := "# filter: (msDS-AllowedToDelegateTo=*)\n"
	for _, d := range Detect(out) {
		if d.ChainID == "constrained-delegation-allowed-to-delegate" {
			t.Fatalf("a bare LDAP filter must not match: %+v", d)
		}
	}
}

func TestDetectUnconstrainedDelegation(t *testing.T) {
	out := "dn: CN=dc,OU=Domain Controllers,DC=lab,DC=local\nuserAccountControl: 524288\n"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "unconstrained-delegation-trusted-for-delegation" {
		t.Fatalf("expected unconstrained-delegation-trusted-for-delegation, got %+v", dets)
	}
}

func TestDetectASREPRoastable(t *testing.T) {
	out := "dn: CN=svc,CN=Users,DC=lab,DC=local\nuserAccountControl: 4194304\n"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "asrep-roastable-account" {
		t.Fatalf("expected asrep-roastable-account, got %+v", dets)
	}
}

func TestDetectAnonymousUserEnumerationNullAuth(t *testing.T) {
	// netexec prints "(Null Auth:True)" in its SMB info line.
	out := "SMB  192.168.56.11  445  WINTERFELL  [*] Windows Server ... (domain:north.lab) (signing:True) (SMBv1:None) (Null Auth:True)\n"
	dets := Detect(out)
	if len(dets) == 0 || dets[0].ChainID != "smb-anonymous-user-enumeration" {
		t.Fatalf("expected smb-anonymous-user-enumeration, got %+v", dets)
	}
}

func TestDetectNullAuthFalseIsNotMatched(t *testing.T) {
	out := "SMB  192.168.56.11  445  WINTERFELL  [*] Windows Server ... (Null Auth:False)\n"
	for _, d := range Detect(out) {
		if d.ChainID == "smb-anonymous-user-enumeration" {
			t.Fatalf("Null Auth:False must not match smb-anonymous-user-enumeration: %+v", d)
		}
	}
}

func TestDetectBenignOutputHasNoMatch(t *testing.T) {
	out := "total 0\n-rw-r--r-- 1 root root 0 Oct 13 10:00 file.txt\n"
	if dets := Detect(out); len(dets) != 0 {
		t.Fatalf("benign output must not match, got %+v", dets)
	}
	if Detect("") != nil {
		t.Fatal("empty output must return nil")
	}
}

func TestDetectIsDeduplicatedAndCapped(t *testing.T) {
	// One output carrying several signatures: each chain surfaces at most once.
	out := "Message signing enabled but not required\nCertEnroll\nservicePrincipalName: x\n"
	dets := Detect(out)
	seen := map[string]int{}
	for _, d := range dets {
		seen[d.ChainID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("chain %q surfaced %d times", id, n)
		}
	}
	if len(dets) > maxDetections {
		t.Errorf("expected at most %d detections, got %d", maxDetections, len(dets))
	}
}

func TestRenderDetectionsShape(t *testing.T) {
	dets := Detect("Message signing enabled but not required\n")
	block := RenderDetections(dets)
	for _, want := range []string{"<detected_techniques", "smb-signing-disabled", "SAFE verify:", "search_exploit_chain", "</detected_techniques>"} {
		if !strings.Contains(block, want) {
			t.Errorf("rendered block missing %q:\n%s", want, block)
		}
	}
	if got := RenderDetections(nil); got != "" {
		t.Errorf("empty detections must render empty, got %q", got)
	}
}
