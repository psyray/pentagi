package providers

import (
	"strings"
	"testing"

	"pentagi/pkg/config"

	"github.com/stretchr/testify/assert"
)

func TestCatalogueQuery_CombinesTitleAndInputHead(t *testing.T) {
	fp := &flowProvider{title: "Internal AD assessment", flowInput: "Enumerate the domain and escalate."}

	assert.Equal(t, "Internal AD assessment\nEnumerate the domain and escalate.", fp.catalogueQuery())
}

func TestCatalogueQuery_TruncatesTheInput(t *testing.T) {
	fp := &flowProvider{title: "Title", flowInput: strings.Repeat("a", catalogueInputLimit*2)}

	query := fp.catalogueQuery()
	assert.Equal(t, "Title\n"+strings.Repeat("a", catalogueInputLimit), query)
}

func TestCatalogueQuery_FallsBackToWhicheverPartExists(t *testing.T) {
	assert.Equal(t, "Title", (&flowProvider{title: "Title"}).catalogueQuery())
	assert.Equal(t, "Input only", (&flowProvider{flowInput: "Input only"}).catalogueQuery())
	assert.Empty(t, (&flowProvider{}).catalogueQuery())
}

func TestSkillCatalogueText_IsEmptyWhenDisabled(t *testing.T) {
	fp := &flowProvider{cfg: &config.Config{SkillCatalogueEnabled: false}}

	assert.Empty(t, fp.skillCatalogueText(t.Context()))
}

func TestSkillCatalogueText_IsEmptyWithoutAPool(t *testing.T) {
	fp := &flowProvider{cfg: &config.Config{SkillCatalogueEnabled: true}}

	assert.Empty(t, fp.skillCatalogueText(t.Context()))
	assert.Empty(t, fp.skillCatalogueHash)
}
