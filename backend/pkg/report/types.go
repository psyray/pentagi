// Package report implements the AI document generation of the fork
// extended-version (plan § 6): one report row walks created -> planning ->
// generating -> completed/failed/canceled, instantiating its template outline
// (planner node), then writing the outline's sections (content nodes, soft
// agent-time budgets). Reports live for the life of their flow and are edited,
// exported and re-watched from the Reports surfaces.
package report

// Report statuses for the reports row.
const (
	StatusCreated    = "created"
	StatusPlanning   = "planning"
	StatusGenerating = "generating"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCanceled   = "canceled"
)

// Section statuses for the report_sections rows.
const (
	SectionQueued     = "queued"
	SectionWriting    = "writing"
	SectionDone       = "done"
	SectionFailed     = "failed"
	SectionSkipped    = "skipped"
	SectionNotWritten = "not_written"
)

// Section kinds.
const (
	KindLLM       = "llm"       // one writer call produces the body
	KindAggregate = "aggregate" // heading over planner-instantiated children
	KindComposed  = "composed"  // body computed at read/export time
)

// Log kinds for the report_logs rows (generation console).
const (
	LogControl = "control" // planner / engine decisions
	LogSection = "section" // writer lifecycle events
	LogNote    = "note"    // operator notes passed to sections still to come
	LogError   = "error"   // failures worth surfacing
	LogStatus  = "status"  // run status transitions
)

var reportStatuses = map[string]struct{}{
	StatusCreated:    {},
	StatusPlanning:   {},
	StatusGenerating: {},
	StatusCompleted:  {},
	StatusFailed:     {},
	StatusCanceled:   {},
}

var sectionStatuses = map[string]struct{}{
	SectionQueued:     {},
	SectionWriting:    {},
	SectionDone:       {},
	SectionFailed:     {},
	SectionSkipped:    {},
	SectionNotWritten: {},
}

var sectionKinds = map[string]struct{}{
	KindLLM:       {},
	KindAggregate: {},
	KindComposed:  {},
}

var logKinds = map[string]struct{}{
	LogControl: {},
	LogSection: {},
	LogNote:    {},
	LogError:   {},
	LogStatus:  {},
}

// KnownReportStatus reports whether the value is part of the reference
// vocabulary; unknown values are kept verbatim (fork convention).
func KnownReportStatus(v string) bool {
	_, ok := reportStatuses[v]
	return ok
}

// KnownSectionStatus reports whether the value is part of the reference
// vocabulary; unknown values are kept verbatim.
func KnownSectionStatus(v string) bool {
	_, ok := sectionStatuses[v]
	return ok
}

// KnownSectionKind reports whether the value is part of the reference
// vocabulary; unknown values are kept verbatim.
func KnownSectionKind(v string) bool {
	_, ok := sectionKinds[v]
	return ok
}

// KnownLogKind reports whether the value is part of the reference vocabulary.
func KnownLogKind(v string) bool {
	_, ok := logKinds[v]
	return ok
}
