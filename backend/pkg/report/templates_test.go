package report

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTemplateQuerier answers only the report_templates surface the controller
// and the resolver touch; anything else panics (a test wants to see that).
type fakeTemplateQuerier struct {
	database.Querier

	byKey    map[string]database.ReportTemplate
	byKeyErr map[string]error
	listed   []database.ReportTemplate
	created  []database.CreateReportTemplateParams
	deleted  []int64
}

func (q *fakeTemplateQuerier) GetReportTemplateByKey(
	_ context.Context, arg database.GetReportTemplateByKeyParams,
) (database.ReportTemplate, error) {
	if err, ok := q.byKeyErr[arg.Key]; ok {
		return database.ReportTemplate{}, err
	}
	if row, ok := q.byKey[arg.Key]; ok {
		return row, nil
	}
	return database.ReportTemplate{}, sql.ErrNoRows
}

func (q *fakeTemplateQuerier) ListReportTemplates(
	_ context.Context, _ sql.NullInt64,
) ([]database.ReportTemplate, error) {
	return q.listed, nil
}

func (q *fakeTemplateQuerier) CreateReportTemplate(
	_ context.Context, arg database.CreateReportTemplateParams,
) (database.ReportTemplate, error) {
	q.created = append(q.created, arg)
	return database.ReportTemplate{
		ID:      int64(len(q.created)),
		UserID:  arg.UserID,
		Key:     arg.Key,
		Label:   arg.Label,
		Version: arg.Version,
		Yaml:    arg.Yaml,
		Enabled: arg.Enabled,
	}, nil
}

func (q *fakeTemplateQuerier) DeleteReportTemplate(_ context.Context, arg database.DeleteReportTemplateParams) error {
	q.deleted = append(q.deleted, arg.ID)
	return nil
}

func storedTemplate(id int64, userID sql.NullInt64, key, label, yaml string) database.ReportTemplate {
	return database.ReportTemplate{
		ID:        id,
		UserID:    userID,
		Key:       key,
		Label:     label,
		Version:   3,
		Yaml:      yaml,
		Enabled:   true,
		CreatedAt: time.Unix(0, 0).UTC(),
		UpdatedAt: time.Unix(0, 0).UTC(),
	}
}

const minimalTemplateYAML = `
key: custom
label: Custom Template
version: 2
sections:
  - key: cover
    kind: composed
    title: Cover
`

func TestResolveTemplate_PrefersTheUsersOwnRowOverBundled(t *testing.T) {
	q := &fakeTemplateQuerier{byKey: map[string]database.ReportTemplate{
		"custom": storedTemplate(7, sql.NullInt64{Int64: 42, Valid: true}, "custom", "Custom Template", minimalTemplateYAML),
	}}

	tpl, err := ResolveTemplate(context.Background(), q, 42, "custom")
	require.NoError(t, err)
	assert.Equal(t, "custom", tpl.Key)
	assert.Equal(t, "Custom Template", tpl.Label)
	assert.Equal(t, 2, tpl.Version)
}

func TestResolveTemplate_DegradesToBundledWhenTheRowIsMissingOrUnparsable(t *testing.T) {
	brokenYAML := "key: pentagi-default-pentest-report\nlabel: Broken\nsections: [ this is not valid"

	for _, tc := range []struct {
		name string
		q    *fakeTemplateQuerier
	}{
		{"no row", &fakeTemplateQuerier{}},
		{"a row that no longer parses", &fakeTemplateQuerier{byKey: map[string]database.ReportTemplate{
			"pentagi-default-pentest-report": storedTemplate(1, sql.NullInt64{Int64: 42, Valid: true},
				"pentagi-default-pentest-report", "Broken", brokenYAML),
		}}},
		{"a query error", &fakeTemplateQuerier{byKeyErr: map[string]error{
			"pentagi-default-pentest-report": sql.ErrConnDone,
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := ResolveTemplate(context.Background(), tc.q, 42, "pentagi-default-pentest-report")
			require.NoError(t, err, "a broken row must never stop a report")
			assert.Equal(t, "PentAGI Penetration Testing Report", tpl.Label)
		})
	}
}

func TestResolveTemplate_WithoutAUserSkipsTheDatabase(t *testing.T) {
	// A guest report (user 0) has no templates of its own: the lookup must not run.
	var q nilQuerier
	tpl, err := ResolveTemplate(context.Background(), q, 0, "pentagi-default-pentest-report")
	require.NoError(t, err)
	assert.Equal(t, "PentAGI Penetration Testing Report", tpl.Label)
}

// nilQuerier panics if any query reaches it, proving the DB is skipped.
type nilQuerier struct{ database.Querier }

func TestListTemplates_MergesBundledAndDBWithTheDBWinningOnAKeyClash(t *testing.T) {
	q := &fakeTemplateQuerier{listed: []database.ReportTemplate{
		storedTemplate(7, sql.NullInt64{Int64: 42, Valid: true}, "custom", "Custom Template", minimalTemplateYAML),
		// Shadows the bundled default: the DB row wins.
		storedTemplate(8, sql.NullInt64{}, "pentagi-default-pentest-report", "My Override", minimalTemplateYAML),
	}}
	ctrl := &Controller{deps: Deps{DB: q}}

	infos, err := ctrl.ListTemplates(context.Background(), 42)
	require.NoError(t, err)

	byKey := make(map[string]TemplateInfo, len(infos))
	for _, info := range infos {
		byKey[info.Key] = info
	}

	assert.Equal(t, TemplateSourceUser, byKey["custom"].Source)
	assert.Equal(t, int64(7), byKey["custom"].ID)
	assert.Equal(t, TemplateSourceGlobal, byKey["pentagi-default-pentest-report"].Source)
	assert.Equal(t, "My Override", byKey["pentagi-default-pentest-report"].Label,
		"the DB row must shadow the bundled one")
}

func TestCreateReportTemplate_RefusesAGlobalTemplateWithoutAdmin(t *testing.T) {
	q := &fakeTemplateQuerier{}
	ctrl := &Controller{deps: Deps{DB: q}}

	_, err := ctrl.CreateReportTemplate(context.Background(), 42, false, TemplateParams{
		Yaml: minimalTemplateYAML, Enabled: true,
	}, true)
	require.ErrorIs(t, err, ErrForbidden)
	assert.Empty(t, q.created, "a refused template reached the database")
}

func TestCreateReportTemplate_RejectsInvalidYAML(t *testing.T) {
	q := &fakeTemplateQuerier{}
	ctrl := &Controller{deps: Deps{DB: q}}

	_, err := ctrl.CreateReportTemplate(context.Background(), 42, false, TemplateParams{
		Yaml: "key: x\nlabel: ''\nsections: []", Enabled: true,
	}, false)
	require.Error(t, err)
	assert.Empty(t, q.created)
}

func TestCreateReportTemplate_StoresAPersonalTemplateForTheCaller(t *testing.T) {
	q := &fakeTemplateQuerier{}
	ctrl := &Controller{deps: Deps{DB: q}}

	row, err := ctrl.CreateReportTemplate(context.Background(), 42, false, TemplateParams{
		Yaml: minimalTemplateYAML, Enabled: true,
	}, false)
	require.NoError(t, err)
	require.Len(t, q.created, 1)
	assert.Equal(t, sql.NullInt64{Int64: 42, Valid: true}, q.created[0].UserID)
	assert.Equal(t, "custom", row.Key)
	assert.Equal(t, "Custom Template", row.Label)
}

func TestAuthorizeTemplateRow_GuardsOwnershipAndGlobalScope(t *testing.T) {
	ctrl := &Controller{}
	own := storedTemplate(1, sql.NullInt64{Int64: 42, Valid: true}, "k", "L", "")
	global := storedTemplate(2, sql.NullInt64{}, "k", "L", "")

	require.NoError(t, ctrl.authorizeTemplateRow(own, 42, false))
	require.ErrorIs(t, ctrl.authorizeTemplateRow(own, 43, true), ErrForbidden, "another user's row stays off")
	require.NoError(t, ctrl.authorizeTemplateRow(global, 42, true))
	require.ErrorIs(t, ctrl.authorizeTemplateRow(global, 42, false), ErrForbidden, "a global row needs admin")
}

func TestTemplateBrandingFromRow_ReadsTheStoredValues(t *testing.T) {
	row := database.ReportTemplate{
		BrandingTitle:    sql.NullString{String: "Acme", Valid: true},
		BrandingSubtitle: sql.NullString{},
		BrandingHeader:   sql.NullString{String: "Confidential", Valid: true},
	}
	branding := TemplateBrandingFromRow(row)
	assert.Equal(t, "Acme", branding.Title)
	assert.Equal(t, "", branding.Subtitle)
	assert.Equal(t, "Confidential", branding.Header)
}
