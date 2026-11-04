package skills

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPExtractor_SendsTheDocumentAndDecodesTheMarkdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if token := r.Header.Get("X-Ingest-Token"); token != "secret" {
			t.Errorf("token header = %q", token)
		}
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if name := r.FormValue("filename"); name != "guide.pdf" {
			t.Errorf("filename field = %q", name)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("file part: %v", err)
		}
		defer file.Close()
		content, _ := io.ReadAll(file)
		if header.Filename != "guide.pdf" || string(content) != "%PDF-1.7 fake" {
			t.Errorf("file part = %q %q", header.Filename, string(content))
		}

		_ = json.NewEncoder(w).Encode(Extraction{
			Markdown: "# Guide\n\nBody.",
			Title:    "Guide",
			Source:   "guide.pdf",
			Format:   "pdf",
			Warnings: []string{"one page was empty"},
		})
	}))
	defer server.Close()

	extractor := NewHTTPExtractor(server.URL+"/", "secret", 5*time.Second)

	extraction, err := extractor.Extract(t.Context(), "guide.pdf", []byte("%PDF-1.7 fake"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if extraction.Title != "Guide" || extraction.Format != "pdf" || extraction.Chars != len(extraction.Markdown) {
		t.Errorf("extraction = %+v", extraction)
	}
	if len(extraction.Warnings) != 1 {
		t.Errorf("warnings = %v", extraction.Warnings)
	}
}

func TestHTTPExtractor_ReportsTheSidecarRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		_, _ = w.Write([]byte("unsupported extension .bin"))
	}))
	defer server.Close()

	extractor := NewHTTPExtractor(server.URL, "", 5*time.Second)

	_, err := extractor.Extract(t.Context(), "x.bin", []byte("data"))
	if err == nil || !strings.Contains(err.Error(), "415") || !strings.Contains(err.Error(), "unsupported extension") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPExtractor_RefusesAnEmptyExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Extraction{Markdown: "   "})
	}))
	defer server.Close()

	extractor := NewHTTPExtractor(server.URL, "", 5*time.Second)

	_, err := extractor.Extract(t.Context(), "scan.pdf", []byte("%PDF"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestHTTPExtractor_IsUnavailableWithoutAURL(t *testing.T) {
	extractor := NewHTTPExtractor("", "", time.Second)
	if extractor.IsAvailable() {
		t.Fatal("an extractor without a URL must be unavailable")
	}

	if _, err := extractor.Extract(t.Context(), "a.md", []byte("x")); !errors.Is(err, ErrExtractorUnavailable) {
		t.Fatalf("error = %v, want ErrExtractorUnavailable", err)
	}
}

func TestHTTPExtractor_RefusesAnEmptyFile(t *testing.T) {
	extractor := NewHTTPExtractor("http://127.0.0.1:1", "", time.Second)

	if _, err := extractor.Extract(t.Context(), "a.md", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
