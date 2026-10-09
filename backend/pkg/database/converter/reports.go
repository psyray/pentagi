package converter

import (
	"database/sql"
	"encoding/json"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/graph/model"

	"github.com/sqlc-dev/pqtype"
)

// ConvertReport maps a report row to the GraphQL model.
func ConvertReport(report database.Report) *model.Report {
	return &model.Report{
		ID:                     report.ID,
		FlowID:                 report.FlowID,
		UserID:                 report.UserID,
		Status:                 model.ReportStatusType(report.Status),
		Title:                  report.Title,
		TemplateKey:            report.TemplateKey,
		TemplateLabel:          report.TemplateLabel,
		ProviderName:           report.ProviderName,
		ProviderType:           model.ProviderType(report.ProviderType),
		Language:               report.Language,
		AdditionalInstructions: nullStringPtr(report.AdditionalInstructions),
		ControlBudgetSeconds:   int(report.ControlBudgetSeconds),
		ContentBudgetSeconds:   int(report.ContentBudgetSeconds),
		ControlUsedSeconds:     report.ControlUsedSeconds,
		ContentUsedSeconds:     report.ContentUsedSeconds,
		SectionsEstimate:       int(report.SectionsEstimate),
		SectionsTotal:          int(report.SectionsTotal),
		SectionsDone:           int(report.SectionsDone),
		TokensIn:               int(report.TokensIn),
		TokensOut:              int(report.TokensOut),
		Error:                  nullStringPtr(report.Error),
		CreatedAt:              report.CreatedAt.Time,
		UpdatedAt:              report.UpdatedAt.Time,
		FinishedAt:             nullTimePtr(report.FinishedAt),
		StartedAt:              nullTimePtr(report.StartedAt),
	}
}

// ConvertReportSection maps one section row; the variables JSON array is
// re-typed to the name/value model (values stay raw JSON text: "443" is a
// string, 443 is a number).
func ConvertReportSection(row database.ReportSection) *model.ReportSection {
	return &model.ReportSection{
		ID:              row.ID,
		ReportID:        row.ReportID,
		ParentID:        nullInt64Ptr(row.ParentID),
		Position:        int(row.Position),
		Key:             row.Key,
		Kind:            model.ReportSectionKind(row.Kind),
		Title:           row.Title,
		Status:          model.ReportSectionStatusType(row.Status),
		Content:         row.Content,
		Instructions:    nullStringPtr(row.Instructions),
		Variables:       convertReportVariables(row.Variables),
		Warnings:        nullTextArray(row.Warnings),
		Hidden:          row.Hidden,
		TokensIn:        int(row.TokensIn),
		TokensOut:       int(row.TokensOut),
		DurationSeconds: row.DurationSeconds,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}
}

func ConvertReportSections(sections []database.ReportSection) []*model.ReportSection {
	out := make([]*model.ReportSection, 0, len(sections))
	for i := range sections {
		out = append(out, ConvertReportSection(sections[i]))
	}
	return out
}

// ConvertReportAttachment maps one operator attachment row (backlog v2, L6).
func ConvertReportAttachment(row database.ReportSectionAttachment) *model.ReportAttachment {
	return &model.ReportAttachment{
		ID:        row.ID,
		Label:     row.Label,
		Path:      row.Path,
		Bytes:     int(row.Bytes),
		CreatedAt: row.CreatedAt,
	}
}

func ConvertReportAttachments(rows []database.ReportSectionAttachment) []*model.ReportAttachment {
	out := make([]*model.ReportAttachment, 0, len(rows))
	for i := range rows {
		out = append(out, ConvertReportAttachment(rows[i]))
	}
	return out
}

// ConvertReportLog maps one generation-log row; the payload JSON is decoded
// into the model's map value when present.
func ConvertReportLog(row database.ReportLog) (*model.ReportLog, error) {
	payload := map[string]any(nil)
	if row.Payload.Valid && len(row.Payload.RawMessage) > 0 {
		payload = make(map[string]any, 8)
		if err := json.Unmarshal(row.Payload.RawMessage, &payload); err != nil {
			// A malformed payload must not drop the event: surface it as a string.
			payload = map[string]any{"raw": rawOf(row.Payload)}
		}
	}

	log := &model.ReportLog{
		ID:        row.ID,
		ReportID:  row.ReportID,
		SectionID: nullInt64Ptr(row.SectionID),
		Kind:      model.ReportLogKind(row.Kind),
		Message:   row.Message,
		Payload:   payload,
		CreatedAt: row.CreatedAt.Time,
	}
	return log, nil
}

func convertReportVariables(value json.RawMessage) []*model.ReportVariable {
	if len(value) == 0 {
		return []*model.ReportVariable{}
	}

	var pairs []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(value, &pairs); err != nil {
		return []*model.ReportVariable{}
	}

	out := make([]*model.ReportVariable, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, &model.ReportVariable{
			Name:  pair.Name,
			Value: string(pair.Value),
		})
	}
	return out
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copied := value.String
	return &copied
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	copied := value.Int64
	return &copied
}

// nullTextArray copies a TEXT[] column (nil stays nil).
func nullTextArray(value []string) []string {
	out := make([]string, 0, len(value))
	out = append(out, value...)
	return out
}

func rawOf(value pqtype.NullRawMessage) string {
	if !value.Valid || len(value.RawMessage) == 0 {
		return ""
	}
	return string(value.RawMessage)
}
