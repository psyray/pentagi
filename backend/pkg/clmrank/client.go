// Package clmrank provides a minimal HTTP client for a CLM (Contrastive-LM)
// System One server ("clm-serve", see https://github.com/Contrastive-LM/CLM).
//
// It implements the single primitive PentAGI uses: POST /v1/rank, which ranks
// a closed list of candidate next actions against a textual state and returns
// them best-first with calibrated probabilities. The server is a fast
// classifier/ranker (no text generation), so calls are expected to complete in
// milliseconds once its embedding cache is warm.
package clmrank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// maxBodySize guards against reading a runaway response body.
	maxBodySize = 1 << 20 // 1 MB is far more than any rank response
	// latencyHeader carries the server-side decision time in milliseconds.
	latencyHeader = "X-CLM-Latency-Ms"
)

// RankedCandidate is one entry of a /v1/rank response, best first.
type RankedCandidate struct {
	Rank      int     `json:"rank"`
	Candidate string  `json:"candidate"`
	Prob      float64 `json:"prob"`
}

// RankResult is the decoded outcome of one Rank call.
type RankResult struct {
	Model     string
	Ranked    []RankedCandidate
	LatencyMs float64 // server-side latency from X-CLM-Latency-Ms, negative if absent
}

// Client is an authenticated client of one clm-serve instance.
type Client struct {
	httpClient *http.Client
	serverURL  string // base URL without trailing slash, e.g. http://clm.local:8700
	apiKey     string // Bearer token (may be empty when the server has no auth)
	model      string // default CLM head name, e.g. "clm-latest"
}

// New returns a Client; it never fails, misconfiguration surfaces at Rank time.
// timeout covers the whole HTTP exchange including connect.
func New(serverURL, apiKey, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		serverURL:  strings.TrimRight(strings.TrimSpace(serverURL), "/"),
		apiKey:     apiKey,
		model:      model,
	}
}

// Model returns the default head name the client asks for.
func (c *Client) Model() string {
	return c.model
}

// ServerURL returns the configured base URL (used for health checks/logging).
func (c *Client) ServerURL() string {
	return c.serverURL
}

type rankRequest struct {
	Context     string   `json:"context"`
	Question    string   `json:"question,omitempty"`
	Answers     []string `json:"answers"`
	Model       string   `json:"model,omitempty"`
	Temperature float64  `json:"temperature,omitempty"`
}

type rankResponse struct {
	Model  string            `json:"model"`
	Ranked []RankedCandidate `json:"ranked"`
}

// Rank orders answers (at least 2) against ctxText, best candidate first.
// question is the optional instruction the state head sees after the context.
// Candidates are matched back by string identity, so callers should deduplicate
// identical answers beforehand.
func (c *Client) Rank(ctx context.Context, ctxText, question string, answers []string) (*RankResult, error) {
	if c == nil {
		return nil, errors.New("clmrank: nil client")
	}
	if c.serverURL == "" {
		return nil, errors.New("clmrank: empty server URL")
	}
	if len(answers) < 2 {
		return nil, fmt.Errorf("clmrank: need at least 2 answers to rank, got %d", len(answers))
	}

	body, err := json.Marshal(rankRequest{
		Context:  ctxText,
		Question: question,
		Answers:  answers,
		Model:    c.model,
	})
	if err != nil {
		return nil, fmt.Errorf("clmrank: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.serverURL+"/v1/rank", bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("clmrank: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clmrank: rank request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("clmrank: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		const maxErrBody = 500
		msg := strings.TrimSpace(string(raw))
		if len(msg) > maxErrBody {
			msg = msg[:maxErrBody] + "..."
		}
		return nil, fmt.Errorf("clmrank: server returned %s: %s", resp.Status, msg)
	}

	var decoded rankResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("clmrank: failed to decode response: %w", err)
	}
	if len(decoded.Ranked) == 0 {
		return nil, errors.New("clmrank: server returned an empty ranking")
	}

	latencyMs := -1.0
	if h := resp.Header.Get(latencyHeader); h != "" {
		if v, convErr := strconv.ParseFloat(h, 64); convErr == nil {
			latencyMs = v
		}
	}

	return &RankResult{
		Model:     decoded.Model,
		Ranked:    decoded.Ranked,
		LatencyMs: latencyMs,
	}, nil
}
