package report

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
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

// visionProbePNG is a solid red square, generated once. A vision model names the
// colour; a text-only one rejects the image or answers without seeing it. This
// lets the app probe capability instead of hardcoding a model list that would
// age out.
var visionProbePNG = func() string {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 220, G: 20, B: 20, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}()

// probeVision asks the provider's simple model to name the colour of a red
// square. It returns true only when the model accepted the image and saw it.
func probeVision(ctx context.Context, prv provider.Provider) (bool, error) {
	if visionProbePNG == "" {
		return false, fmt.Errorf("vision probe image unavailable")
	}

	chain := []llms.MessageContent{{
		Role: llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "What colour is the square in this image? Answer with a single word."},
			llms.ImageURLContent{URL: visionProbePNG},
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

	return strings.Contains(answer, "red") || strings.Contains(answer, "rouge"), nil
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
