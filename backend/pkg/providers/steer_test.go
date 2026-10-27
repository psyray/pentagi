package providers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDrainSteer_ReturnsMessagesInOrderAndEmptiesTheQueue(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 3)
	ch <- "first"
	ch <- "second"
	ctx := WithSteerChannel(context.Background(), ch)

	assert.Equal(t, []string{"first", "second"}, drainSteer(ctx))
	assert.Nil(t, drainSteer(ctx))
}

func TestDrainSteer_WithoutAChannelReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, drainSteer(context.Background()))
}

func TestDrainSteer_ClosedChannelDoesNotLoop(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 1)
	ch <- "last"
	close(ch)
	ctx := WithSteerChannel(context.Background(), ch)

	assert.Equal(t, []string{"last"}, drainSteer(ctx))
	assert.Nil(t, drainSteer(ctx))
}
