package providers

import (
	"context"
	"strings"

	"pentagi/pkg/skills"

	"github.com/sirupsen/logrus"
)

// catalogueInputLimit keeps the flow request short enough to stay a focused
// similarity query instead of a whole brief.
const catalogueInputLimit = 800

// skillCatalogueText renders the skill catalogue once per flow and then serves
// it from memory. Caching keeps every prompt of the flow identical (prompt
// cache, deterministic replay) and costs a single database round trip.
func (fp *flowProvider) skillCatalogueText(ctx context.Context) string {
	if fp == nil || fp.cfg == nil || !fp.cfg.SkillCatalogueEnabled {
		return ""
	}

	fp.skillCatalogueOnce.Do(func() {
		if fp.cfg.PgxPool == nil || fp.embedder == nil || !fp.embedder.IsAvailable() {
			return
		}

		store := skills.NewStore(fp.cfg.PgxPool, fp.embedder, fp.cfg.EmbeddingMaxTextBytes)
		catalogue, err := store.BuildCatalogue(ctx, fp.userID, skills.CatalogueOptions{
			Query:         fp.catalogueQuery(),
			Limit:         fp.cfg.SkillCatalogueLimit,
			IncludeDrafts: fp.cfg.SkillCatalogueIncludeDrafts,
		})
		if err != nil {
			logrus.WithContext(ctx).WithError(err).WithField("flow_id", fp.flowID).
				Warn("failed to build the skill catalogue, the prompts run without it")
			return
		}

		if catalogue.Text == "" {
			logrus.WithContext(ctx).WithField("flow_id", fp.flowID).
				Debug("no skill matches this flow, the prompts run without a catalogue")
			return
		}

		fp.skillCatalogue = catalogue.Text
		fp.skillCatalogueHash = catalogue.Hash
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"flow_id":   fp.flowID,
			"skills":    len(catalogue.Entries),
			"truncated": catalogue.Truncated,
			"hash":      catalogue.Hash,
			"chars":     len(catalogue.Text),
		}).Info("skill catalogue frozen for the flow")
	})

	return fp.skillCatalogue
}

// catalogueQuery combines the flow title with the head of the initial request,
// which is what the selection is ranked against.
func (fp *flowProvider) catalogueQuery() string {
	parts := make([]string, 0, 2)
	if title := strings.TrimSpace(fp.title); title != "" {
		parts = append(parts, title)
	}

	input := strings.TrimSpace(fp.flowInput)
	if input != "" {
		runes := []rune(input)
		if len(runes) > catalogueInputLimit {
			input = string(runes[:catalogueInputLimit])
		}
		parts = append(parts, input)
	}

	return strings.Join(parts, "\n")
}
