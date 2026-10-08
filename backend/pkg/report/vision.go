package report

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"pentagi/pkg/providers/pconfig"
	"pentagi/pkg/providers/provider"

	"github.com/vxcontrol/langchaingo/llms"
)

// screenshot limits bound what one writer call carries as images.
const (
	maxScreenshotsPerSection = 4
	maxScreenshotBytes       = 4 << 20 // 4 MiB before base64
)

// visionColor is one quadrant colour of the probe grid; names must be
// unambiguous so a vision model reports them back predictably.
type visionColor struct {
	name string
	rgba color.RGBA
}

var visionColors = []visionColor{
	{"red", color.RGBA{R: 230, G: 30, B: 30, A: 255}},
	{"green", color.RGBA{R: 30, G: 180, B: 30, A: 255}},
	{"blue", color.RGBA{R: 30, G: 30, B: 230, A: 255}},
	{"yellow", color.RGBA{R: 230, G: 220, B: 30, A: 255}},
}

// visionProbeImage draws a 2x2 grid whose quadrants are the palette in a random
// order, and returns it as a data URL plus the expected reading order. A blind
// model names the four colours in the wrong order; only a model that sees the
// image matches the sequence (1 in 24 by chance).
func visionProbeImage() (string, []string) {
	perm := rand.Perm(len(visionColors))
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	quadrant := func(x0, y0 int, c color.RGBA) {
		for y := y0; y < y0+32; y++ {
			for x := x0; x < x0+32; x++ {
				img.Set(x, y, c)
			}
		}
	}
	quadrant(0, 0, visionColors[perm[0]].rgba)
	quadrant(32, 0, visionColors[perm[1]].rgba)
	quadrant(0, 32, visionColors[perm[2]].rgba)
	quadrant(32, 32, visionColors[perm[3]].rgba)

	order := make([]string, 0, 4)
	for _, i := range perm {
		order = append(order, visionColors[i].name)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", order
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), order
}

// probeVision asks the provider's simple model to name the four quadrants of a
// 2x2 grid in reading order. It returns true only when the model saw them.
func probeVision(ctx context.Context, prv provider.Provider) (bool, error) {
	dataURL, order := visionProbeImage()
	if dataURL == "" {
		return false, fmt.Errorf("vision probe image unavailable")
	}

	chain := []llms.MessageContent{{
		Role: llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "The image is a 2x2 grid of solid-colour squares. Name the colour of each " +
				"quadrant in reading order (top-left, top-right, bottom-left, bottom-right), comma-separated, " +
				"one word each, nothing else."},
			llms.ImageURLContent{URL: dataURL},
		},
	}}

	resp, err := prv.CallEx(ctx, pconfig.OptionsTypeSimple, chain, nil)
	if err != nil {
		return false, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return false, fmt.Errorf("empty vision probe response")
	}

	answer := strings.ToLower(resp.Choices[0].Content)

	if !evaluateVisionAnswer(answer, order) {
		return false, nil
	}

	return true, nil
}

// evaluateVisionAnswer reports whether the model named the grid's four colours in
// the expected reading order. A blind model names them in some other order, which
// fails the sequence check.
func evaluateVisionAnswer(answer string, order []string) bool {
	cursor := 0
	for _, name := range order {
		index := strings.Index(answer[cursor:], name)
		if index < 0 {
			return false
		}
		cursor += index + len(name)
	}

	return true
}

// CheckVision probes whether the provider a report would use can read images, so
// the creation dialog can warn the operator and let them switch model
// (plan § 6.10, P7.4). An empty provider name means the flow's own provider.
func (c *Controller) CheckVision(ctx context.Context, userID, flowID int64, providerName string) (bool, error) {
	flow, err := c.deps.DB.GetFlow(ctx, flowID)
	if err != nil {
		return false, fmt.Errorf("failed to load flow %d: %w", flowID, err)
	}

	name := strings.TrimSpace(providerName)
	if name == "" {
		name = flow.ModelProviderName
	}
	if name == "" {
		return false, fmt.Errorf("no provider to probe")
	}

	prv, err := c.deps.Provs.GetProvider(ctx, provider.ProviderName(name), userID)
	if err != nil {
		return false, fmt.Errorf("failed to resolve provider %q: %w", name, err)
	}

	return probeVision(ctx, prv)
}

// screenshotDataURL reads a flow screenshot and returns it as a data URL, or ""
// when it cannot be read. The writer call carries these as image parts.
func screenshotDataURL(dataDir string, flowID int64, name string) string {
	if dataDir == "" || name == "" {
		return ""
	}

	blob, err := os.ReadFile(filepath.Join(dataDir, "screenshots", fmt.Sprintf("flow-%d", flowID), name))
	if err != nil || len(blob) > maxScreenshotBytes {
		return ""
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(blob)
}
