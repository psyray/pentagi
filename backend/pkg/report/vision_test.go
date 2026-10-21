package report

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateVisionAnswer_RequiresTheExactReadingOrder(t *testing.T) {
	order := []string{"blue", "green", "red", "yellow"}

	assert.True(t, evaluateVisionAnswer("blue, green, red, yellow", order), "an exact match passes")
	assert.True(t, evaluateVisionAnswer("the quadrants are blue, green, red and yellow.", order))
	assert.False(t, evaluateVisionAnswer("red, green, blue, yellow", order), "the wrong order fails")
	assert.False(t, evaluateVisionAnswer("blue, green, red", order), "a missing colour fails")
	assert.False(t, evaluateVisionAnswer("", order), "an empty answer fails")
	assert.False(t, evaluateVisionAnswer("I cannot see the image, it is probably red.", order))
}
