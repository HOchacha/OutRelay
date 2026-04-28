// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package observe_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/boanlab/OutRelay/lib/observe"
)

func TestCounter(t *testing.T) {
	t.Parallel()
	c := &observe.Counter{}
	c.Inc()
	c.Inc()
	c.Add(5)
	if c.Value() != 7 {
		t.Fatalf("got %d", c.Value())
	}
}

func TestGauge(t *testing.T) {
	t.Parallel()
	g := &observe.Gauge{}
	g.Inc()
	g.Inc()
	g.Set(10)
	g.Dec()
	if g.Value() != 9 {
		t.Fatalf("got %d", g.Value())
	}
}

func TestHistogramQuantile(t *testing.T) {
	t.Parallel()
	h := observe.NewHistogram(
		time.Microsecond,
		10*time.Microsecond,
		100*time.Microsecond,
		time.Millisecond,
	)
	// 90 fast (≤1us) + 10 slow (≤1ms): p99 must cross into the slow
	// bucket because the top 10% of samples are slow.
	for range 90 {
		h.Observe(500 * time.Nanosecond)
	}
	for range 10 {
		h.Observe(900 * time.Microsecond)
	}

	snap := h.Snapshot()
	if snap.N != 100 {
		t.Fatalf("N=%d", snap.N)
	}
	if got := h.Quantile(0.5); got != time.Microsecond {
		t.Fatalf("p50=%v, want 1us", got)
	}
	if got := h.Quantile(0.99); got != time.Millisecond {
		t.Fatalf("p99=%v, want 1ms", got)
	}
}

func TestRegistryCreatesIdempotent(t *testing.T) {
	t.Parallel()
	r := observe.NewRegistry()
	c1 := r.Counter("foo")
	c2 := r.Counter("foo")
	if c1 != c2 {
		t.Fatal("Counter must be idempotent for the same name")
	}
	g1 := r.Gauge("g")
	g2 := r.Gauge("g")
	if g1 != g2 {
		t.Fatal("Gauge must be idempotent")
	}
	h1 := r.Histogram("h")
	h2 := r.Histogram("h")
	if h1 != h2 {
		t.Fatal("Histogram must be idempotent")
	}
}

func TestDumperWritesJSONL(t *testing.T) {
	t.Parallel()
	r := observe.NewRegistry()
	r.Counter("ops").Add(3)
	r.Gauge("inflight").Set(7)
	r.Histogram("latency").Observe(2 * time.Millisecond)

	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	d := observe.NewDumper(r, path, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	// Wait for at least two dumps.
	if !waitForLines(path, 2, 2*time.Second) {
		t.Fatal("dumper never wrote two lines")
	}

	// Verify each line parses as a Snapshot.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s observe.Snapshot
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatalf("parse line: %v", err)
		}
		if s.Counters["ops"] != 3 {
			t.Fatalf("ops=%d", s.Counters["ops"])
		}
	}
}

func TestDebugMux(t *testing.T) {
	t.Parallel()
	r := observe.NewRegistry()
	r.Counter("ops").Inc()

	srv := httptest.NewServer(observe.DebugMux(r))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var s observe.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s.Counters["ops"] != 1 {
		t.Fatalf("ops=%d", s.Counters["ops"])
	}
}

func waitForLines(path string, want int, total time.Duration) bool {
	deadline := time.Now().Add(total)
	for time.Now().Before(deadline) {
		f, err := os.Open(path)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		sc := bufio.NewScanner(f)
		got := 0
		for sc.Scan() {
			got++
		}
		f.Close()
		if got >= want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
