package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"pentagi/pkg/chains"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/observability/langfuse"

	"github.com/sirupsen/logrus"
)

// The exploit-chain library is embedded and static, so it is parsed once and
// shared by every flow. A load failure is a build-time mistake (a baked chain
// failed validation), surfaced here rather than silently.
var (
	exploitChainsOnce sync.Once
	exploitChainsLib  *chains.Library
	exploitChainsErr  error
)

func loadExploitChains() (*chains.Library, error) {
	exploitChainsOnce.Do(func() {
		exploitChainsLib, exploitChainsErr = chains.Load()
	})
	return exploitChainsLib, exploitChainsErr
}

// exploitChain is the search_exploit_chain tool: a deterministic lookup over a
// curated, safe-verification exploit-chain library. It has no external
// dependency, so it is always available.
type exploitChain struct{}

// NewExploitChainTool builds the exploit-chain lookup tool.
func NewExploitChainTool() Tool {
	return &exploitChain{}
}

func (t *exploitChain) Handle(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if !t.IsAvailable() {
		return "", fmt.Errorf("exploit chain library is not available")
	}

	ctx, observation := obs.Observer.NewObservation(ctx)
	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{
		"tool": name,
		"args": string(args),
	})

	switch name {
	case SearchExploitChainToolName:
		var action SearchExploitChainAction
		if err := json.Unmarshal(args, &action); err != nil {
			logger.WithError(err).Error("failed to unmarshal search exploit chain action")
			return "", fmt.Errorf("failed to unmarshal %s arguments: %w", name, err)
		}

		lib, err := loadExploitChains()
		if err != nil {
			logger.WithError(err).Error("failed to load exploit chains")
			return "", fmt.Errorf("failed to load exploit chains: %w", err)
		}

		matches, err := selectExploitChains(lib, action)
		if err != nil {
			logger.WithError(err).Warn("exploit chain lookup rejected")
			return "", err
		}

		observation.Event(
			langfuse.WithEventName("search exploit chain"),
			langfuse.WithEventInput(action),
			langfuse.WithEventOutput(map[string]any{
				"matches": len(matches),
				"ids":     chainIDs(matches),
			}),
			langfuse.WithEventLevel(langfuse.ObservationLevelDebug),
		)

		logger.WithField("matches", len(matches)).Debug("exploit chain lookup done")

		return chains.RenderMatches(matches), nil

	default:
		logger.Error("unknown tool")
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (t *exploitChain) IsAvailable() bool {
	_, err := loadExploitChains()
	return err == nil
}

// selectExploitChains resolves the action into a deterministic match set. At
// least one selector is required so the tool never dumps the whole library.
// An id selects a single chain; otherwise cve, product, tag and query are
// combined with AND semantics.
func selectExploitChains(lib *chains.Library, a SearchExploitChainAction) ([]chains.Chain, error) {
	if id := strings.TrimSpace(a.ID); id != "" {
		if c, ok := lib.ByID(id); ok {
			return []chains.Chain{c}, nil
		}
		return nil, nil
	}

	out := lib.All()
	applied := false

	if cve := strings.TrimSpace(a.CVE); cve != "" {
		out = intersectChains(out, lib.ByCVE(cve))
		applied = true
	}
	if product := strings.TrimSpace(a.Product); product != "" {
		out = intersectChains(out, lib.ByProduct(product))
		applied = true
	}
	if tag := strings.TrimSpace(a.Tag); tag != "" {
		out = intersectChains(out, lib.ByTag(tag))
		applied = true
	}
	if query := strings.TrimSpace(a.Query); query != "" {
		out = intersectChains(out, lib.Search(query))
		applied = true
	}

	if !applied {
		return nil, fmt.Errorf("provide at least one of id, cve, product, tag or query")
	}

	return out, nil
}

func intersectChains(a, b []chains.Chain) []chains.Chain {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(b))
	for _, c := range b {
		set[c.ID] = struct{}{}
	}

	out := make([]chains.Chain, 0, len(a))
	for _, c := range a {
		if _, ok := set[c.ID]; ok {
			out = append(out, c)
		}
	}
	return out
}

func chainIDs(cs []chains.Chain) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}
