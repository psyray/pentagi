package controller

import (
	"context"
	"testing"

	"pentagi/pkg/database"
	"pentagi/pkg/graph/subscriptions"
	"pentagi/pkg/tools"
)

// recordingMsgLogQuerier captures what the worker hands to storage.
type recordingMsgLogQuerier struct {
	database.Querier

	messages []database.CreateMsgLogParams
	results  []database.CreateResultMsgLogParams
}

func (q *recordingMsgLogQuerier) CreateMsgLog(
	_ context.Context, arg database.CreateMsgLogParams,
) (database.Msglog, error) {
	q.messages = append(q.messages, arg)

	return database.Msglog{ID: int64(len(q.messages))}, nil
}

func (q *recordingMsgLogQuerier) CreateResultMsgLog(
	_ context.Context, arg database.CreateResultMsgLogParams,
) (database.Msglog, error) {
	q.results = append(q.results, arg)

	return database.Msglog{ID: int64(len(q.results))}, nil
}

// recordingMsgLogPublisher ignores publication; only MessageLogAdded is ever called here.
type recordingMsgLogPublisher struct {
	subscriptions.FlowPublisher
}

func (recordingMsgLogPublisher) MessageLogAdded(context.Context, database.Msglog) {}

func TestMsgLog_PutMsg_AttributesTheMessageToTheAgentInTheContext(t *testing.T) {
	t.Parallel()

	q := &recordingMsgLogQuerier{}
	w := NewFlowMsgLogWorker(q, 7, recordingMsgLogPublisher{})

	ctx := tools.PutAgentContext(t.Context(), database.MsgchainTypePentester)

	if _, err := w.PutMsg(ctx, database.MsglogTypeTerminal, nil, nil, 0, "", "nmap -sV"); err != nil {
		t.Fatalf("PutMsg: %v", err)
	}

	if len(q.messages) != 1 {
		t.Fatalf("want one stored message, got %d", len(q.messages))
	}

	got := q.messages[0].AgentType
	if !got.Valid || got.MsgchainType != database.MsgchainTypePentester {
		t.Fatalf("agent = %+v, want a valid pentester", got)
	}
}

func TestMsgLog_PutFlowMsg_LeavesMessagesWrittenOutsideAnAgentUnattributed(t *testing.T) {
	t.Parallel()

	q := &recordingMsgLogQuerier{}
	w := NewFlowMsgLogWorker(q, 7, recordingMsgLogPublisher{})

	if _, err := w.PutFlowMsg(t.Context(), database.MsglogTypeReport, "", "the flow could not start"); err != nil {
		t.Fatalf("PutFlowMsg: %v", err)
	}

	if got := q.messages[0].AgentType; got.Valid {
		t.Fatalf("agent = %+v, want unattributed", got)
	}
}

func TestMsgLog_PutSubtaskMsgResult_AttributesTheMessageToTheAgentInTheContext(t *testing.T) {
	t.Parallel()

	q := &recordingMsgLogQuerier{}
	w := NewFlowMsgLogWorker(q, 7, recordingMsgLogPublisher{})

	ctx := tools.PutAgentContext(t.Context(), database.MsgchainTypePrimaryAgent)

	_, err := w.PutSubtaskMsgResult(
		ctx, database.MsglogTypeReport, 3, 9, "", "write-up", "the result",
		database.MsglogResultFormatMarkdown,
	)
	if err != nil {
		t.Fatalf("PutSubtaskMsgResult: %v", err)
	}

	if len(q.results) != 1 {
		t.Fatalf("want one stored result, got %d", len(q.results))
	}

	got := q.results[0].AgentType
	if !got.Valid || got.MsgchainType != database.MsgchainTypePrimaryAgent {
		t.Fatalf("agent = %+v, want a valid primary_agent", got)
	}
}
