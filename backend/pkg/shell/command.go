// Package shell manages interactive shells attached to a flow's primary
// container. A session owns a docker exec process with a TTY and outlives any
// single websocket client, so a browser can detach and reattach (tab switch,
// reload, mobile background) and replay the output it missed.
package shell

import (
	"fmt"
	"strings"
)

// SupportedShells lists the shells a client can request; the first entry is the
// default. Each request is resolved against the container at start time with a
// fallback chain, because not every sandbox image ships every shell.
var SupportedShells = []string{"zsh", "bash", "sh"}

// NormalizeShell returns the canonical shell name for a request, defaulting to
// the first supported shell for an empty or unknown value.
func NormalizeShell(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))

	for _, s := range SupportedShells {
		if name == s {
			return s
		}
	}

	return SupportedShells[0]
}

// shellScript builds the POSIX command that execs the requested interactive
// login shell, falling back to lighter shells when it is missing. `exec`
// replaces the wrapper process so the TTY, job control and signals stay with
// the chosen shell.
func shellScript(name string) string {
	switch NormalizeShell(name) {
	case "bash":
		return fallbackChain("bash")
	case "sh":
		return "exec sh -l"
	default:
		return fallbackChain("zsh", "bash")
	}
}

// fallbackChain renders `if command -v X; then exec X -l; elif ...; else exec sh -l; fi`
// so a missing first choice degrades to the next available shell instead of
// leaving the client with a dead terminal.
func fallbackChain(candidates ...string) string {
	var b strings.Builder

	for i, c := range candidates {
		if i == 0 {
			fmt.Fprintf(&b, "if command -v %s >/dev/null 2>&1; then exec %s -l;", c, c)
		} else {
			fmt.Fprintf(&b, " elif command -v %s >/dev/null 2>&1; then exec %s -l;", c, c)
		}
	}

	b.WriteString(" else exec sh -l; fi")

	return b.String()
}
