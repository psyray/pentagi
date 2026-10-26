package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The idle gate alone never armed on the productive prod flows: every tool call
// produced a new fingerprint, so 56 CLOSE verdicts on flow 128 never reached the
// completion critic. With the critic enabled, consecutive close verdicts must be
// verified even while the chain keeps producing new-but-redundant output.
func TestClosureDetector_CriticRunsOnCloseSignalsWithoutIdle(t *testing.T) {
	t.Parallel()

	cd := &closureDetector{enabled: true, closeThreshold: 2, minIdleCalls: 3, criticEnabled: true}

	cd.observeVerdict("## Verdict: CLOSE")
	assert.Equal(t, closureContinue, cd.evaluate(), "one close signal is below the threshold")

	cd.observeVerdict("## Verdict: CLOSE")
	cd.observeResult("fresh evidence that resets the idle counter")
	assert.Equal(t, closureVerify, cd.evaluate(), "the critic must run on the signal, not on idleness")
}

func TestClosureDetector_ConfirmOnSignalForcesTheStop(t *testing.T) {
	t.Parallel()

	cd := &closureDetector{enabled: true, closeThreshold: 2, minIdleCalls: 3, criticEnabled: true}

	cd.observeVerdict("## Verdict: CLOSE")
	cd.observeVerdict("## Verdict: CLOSE")
	assert.Equal(t, closureVerify, cd.evaluate())

	cd.confirm()
	assert.Equal(t, closureForce, cd.evaluate(), "a confirmed close forces the graceful shutdown")
}

// A critic that keeps rejecting must not be re-asked forever. The cap keeps the
// chain working and bounds the LLM cost of the adversarial check.
func TestClosureDetector_CriticCallsAreCapped(t *testing.T) {
	t.Parallel()

	cd := &closureDetector{enabled: true, closeThreshold: 2, minIdleCalls: 3, criticEnabled: true}

	cd.observeVerdict("## Verdict: CLOSE")
	cd.observeVerdict("## Verdict: CLOSE")
	assert.Equal(t, closureVerify, cd.evaluate())
	cd.reject()

	cd.observeVerdict("## Verdict: CLOSE")
	cd.observeVerdict("## Verdict: CLOSE")
	assert.Equal(t, closureVerify, cd.evaluate())
	cd.reject()

	cd.observeVerdict("## Verdict: CLOSE")
	cd.observeVerdict("## Verdict: CLOSE")
	assert.Equal(t, closureContinue, cd.evaluate(), "after the cap, the chain keeps working without a new critic call")
}

// When the critic is disabled, the deterministic idle gate remains the only
// trigger, so a productive chain is never cut.
func TestClosureDetector_IdleGateStillAppliesWithoutCritic(t *testing.T) {
	t.Parallel()

	cd := &closureDetector{enabled: true, closeThreshold: 2, minIdleCalls: 3}

	cd.observeVerdict("## Verdict: CLOSE")
	cd.observeVerdict("## Verdict: CLOSE")
	assert.Equal(t, closureContinue, cd.evaluate(), "without the critic, new output keeps the subtask running")

	cd.observeResult("steady")
	cd.observeResult("steady")
	cd.observeResult("steady")
	cd.observeResult("steady")
	assert.Equal(t, closureForce, cd.evaluate(), "idle evidence plus close verdicts forces the stop")
}
