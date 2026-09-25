package clmrank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientRankHappyPath(t *testing.T) {
	var gotAuth, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rank" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")

		var req rankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.Context == "" || len(req.Answers) != 2 || req.Model != "clm-latest" {
			t.Errorf("bad payload: %+v", req)
		}
		raw, _ := json.Marshal(req)
		gotBody = string(raw)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(latencyHeader, "1.7")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "clm-latest",
			"ranked": []map[string]any{
				{"rank": 1, "candidate": "good", "prob": 0.9},
				{"rank": 2, "candidate": "bad", "prob": 0.1},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "secret-key", "clm-latest", time.Second)

	res, err := c.Rank(context.Background(), "state", "which?", []string{"bad", "good"})
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization header = %q, want Bearer secret-key", gotAuth)
	}
	if gotBody == "" {
		t.Error("empty request body captured")
	}
	if res.Model != "clm-latest" {
		t.Errorf("Model = %q, want clm-latest", res.Model)
	}
	if res.LatencyMs != 1.7 {
		t.Errorf("LatencyMs = %v, want 1.7", res.LatencyMs)
	}
	if len(res.Ranked) != 2 || res.Ranked[0].Candidate != "good" || res.Ranked[0].Rank != 1 {
		t.Errorf("Ranked = %+v, want 'good' first at rank 1", res.Ranked)
	}
	if res.Ranked[0].Prob != 0.9 {
		t.Errorf("top prob = %v, want 0.9", res.Ranked[0].Prob)
	}
}

func TestClientRankErrors(t *testing.T) {
	t.Run("too few answers", func(t *testing.T) {
		c := New("http://unused", "", "m", time.Second)
		if _, err := c.Rank(context.Background(), "s", "", []string{"one"}); err == nil {
			t.Fatal("expected error for a single answer")
		}
	})

	t.Run("empty server URL", func(t *testing.T) {
		c := New("", "", "m", time.Second)
		if _, err := c.Rank(context.Background(), "s", "", []string{"a", "b"}); err == nil {
			t.Fatal("expected error for empty server URL")
		}
	})

	t.Run("unauthorized includes server message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"detail":"invalid api key"}`))
		}))
		defer srv.Close()

		c := New(srv.URL, "wrong", "m", time.Second)
		_, err := c.Rank(context.Background(), "s", "", []string{"a", "b"})
		if err == nil {
			t.Fatal("expected 401 error")
		}
		if want := "401"; !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		c := New(srv.URL, "", "m", time.Second)
		if _, err := c.Rank(context.Background(), "s", "", []string{"a", "b"}); err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("empty ranking rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "m", "ranked": []any{}})
		}))
		defer srv.Close()

		c := New(srv.URL, "", "m", time.Second)
		if _, err := c.Rank(context.Background(), "s", "", []string{"a", "b"}); err == nil {
			t.Fatal("expected error for empty ranking")
		}
	})

	t.Run("dead server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		addr := srv.URL
		srv.Close() // closed before the call

		c := New(addr, "", "m", 500*time.Millisecond)
		if _, err := c.Rank(context.Background(), "s", "", []string{"a", "b"}); err == nil {
			t.Fatal("expected connection error")
		}
	})
}
