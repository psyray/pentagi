package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// ErrExtractorUnavailable is returned when no ingestion sidecar is configured.
var ErrExtractorUnavailable = errors.New("document extraction is not configured")

const (
	// maxExtractionBytes mirrors the sidecar default so a file the backend
	// accepts is not rejected later with a less useful error.
	maxExtractionBytes = 64 << 20
	// MaxImportFileBytes is the largest document the file import route accepts.
	MaxImportFileBytes = maxExtractionBytes
	// maxExtractionErrorBytes bounds the sidecar error body we copy into ours.
	maxExtractionErrorBytes = 512
)

// Extraction is the markdown produced by the ingestion sidecar, with the
// provenance the import needs to record where the content came from.
type Extraction struct {
	Markdown string   `json:"markdown"`
	Title    string   `json:"title"`
	Source   string   `json:"source"`
	Format   string   `json:"format"`
	Chars    int      `json:"chars"`
	Warnings []string `json:"warnings"`
}

// Extractor converts an uploaded document to markdown. Keeping it an interface
// lets the service be tested without an HTTP server.
type Extractor interface {
	Extract(ctx context.Context, filename string, content []byte) (*Extraction, error)
	IsAvailable() bool
}

// HTTPExtractor talks to the Python ingestion sidecar. An empty URL leaves it
// unavailable, which disables the file import without breaking the markdown one.
type HTTPExtractor struct {
	url    string
	token  string
	client *http.Client
}

func NewHTTPExtractor(url, token string, timeout time.Duration) *HTTPExtractor {
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}

	return &HTTPExtractor{
		url:    strings.TrimRight(strings.TrimSpace(url), "/"),
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: timeout},
	}
}

func (e *HTTPExtractor) IsAvailable() bool {
	return e != nil && e.url != ""
}

func (e *HTTPExtractor) Extract(ctx context.Context, filename string, content []byte) (*Extraction, error) {
	if !e.IsAvailable() {
		return nil, ErrExtractorUnavailable
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("%w: the file is empty", ErrInvalidInput)
	}
	if len(content) > maxExtractionBytes {
		return nil, fmt.Errorf("%w: the file exceeds %d bytes", ErrInvalidInput, maxExtractionBytes)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("skills: build the extraction request: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return nil, fmt.Errorf("skills: build the extraction request: %w", err)
	}
	if err := writer.WriteField("filename", filename); err != nil {
		return nil, fmt.Errorf("skills: build the extraction request: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("skills: build the extraction request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/extract", &body)
	if err != nil {
		return nil, fmt.Errorf("skills: build the extraction request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	if e.token != "" {
		request.Header.Set("X-Ingest-Token", e.token)
	}

	response, err := e.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("skills: call the ingestion service: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, maxExtractionErrorBytes))
		return nil, fmt.Errorf("ingestion service answered %s: %s",
			response.Status, strings.TrimSpace(string(detail)))
	}

	var extraction Extraction
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&extraction); err != nil {
		return nil, fmt.Errorf("skills: decode the extraction response: %w", err)
	}
	if strings.TrimSpace(extraction.Markdown) == "" {
		return nil, fmt.Errorf("%w: the document has no extractable text", ErrInvalidInput)
	}
	if extraction.Chars == 0 {
		extraction.Chars = len(extraction.Markdown)
	}

	return &extraction, nil
}
