package providers

import "context"

// operatorSteerPrefix marks a message the operator sent while the agent was
// already working. The prefix keeps the model from mistaking it for a new task
// and makes the message easy to spot in the stored chain.
const operatorSteerPrefix = "[operator steer, not a new task] "

type steerContextKey struct{}

// WithSteerChannel attaches the operator steer queue of a running subtask to
// the context. performAgentChain drains the queue at the start of every
// iteration, so the message reaches the model on its next turn without
// cancelling the call that is already running.
func WithSteerChannel(ctx context.Context, ch <-chan string) context.Context {
	return context.WithValue(ctx, steerContextKey{}, ch)
}

// drainSteer returns every operator message waiting in the context queue, in
// arrival order. It never blocks: an empty or absent queue returns nil.
func drainSteer(ctx context.Context) []string {
	ch, ok := ctx.Value(steerContextKey{}).(<-chan string)
	if !ok || ch == nil {
		return nil
	}

	var messages []string
	for {
		select {
		case message, ok := <-ch:
			if !ok {
				return messages
			}
			if message != "" {
				messages = append(messages, message)
			}
		default:
			return messages
		}
	}
}
