// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package observe is the in-process metrics + JSONL dumping layer
// shared by the controller, relay, and agent. The surface is
// deliberately minimal — Counter, Gauge, Histogram, plus a Registry
// that owns named instances. No external observability stack
// (Prometheus, OpenTelemetry) is pulled in: the project ships a
// JSONL dump (Dumper) and a localhost-only debug endpoint
// (DebugMux) so operators can collect data without running a TSDB.
package observe

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Counter is a monotonically increasing int64.
type Counter struct {
	v atomic.Int64
}

func (c *Counter) Inc()         { c.v.Add(1) }
func (c *Counter) Add(n int64)  { c.v.Add(n) }
func (c *Counter) Value() int64 { return c.v.Load() }

// Gauge is a freely-mutable int64 (concurrent stream count, RSS, etc).
type Gauge struct {
	v atomic.Int64
}

func (g *Gauge) Set(n int64)  { g.v.Store(n) }
func (g *Gauge) Inc()         { g.v.Add(1) }
func (g *Gauge) Dec()         { g.v.Add(-1) }
func (g *Gauge) Value() int64 { return g.v.Load() }

// DefaultDurationBounds covers ~10ns through 10s in roughly decade
// steps; it's the default for histograms that observe a duration.
var DefaultDurationBounds = []time.Duration{
	time.Microsecond,
	10 * time.Microsecond,
	100 * time.Microsecond,
	time.Millisecond,
	10 * time.Millisecond,
	100 * time.Millisecond,
	time.Second,
	10 * time.Second,
}

// Histogram is a bucketed observation distribution. Buckets are
// open-bottom, closed-top: counts[i] holds observations <= bounds[i],
// counts[len(bounds)] is the overflow ("> bounds[last]") bucket.
type Histogram struct {
	bounds []time.Duration
	counts []atomic.Int64
	sumNs  atomic.Int64
	n      atomic.Int64
}

// NewHistogram constructs a histogram. If bounds is empty,
// DefaultDurationBounds is used.
func NewHistogram(bounds ...time.Duration) *Histogram {
	if len(bounds) == 0 {
		bounds = DefaultDurationBounds
	}
	return &Histogram{
		bounds: bounds,
		counts: make([]atomic.Int64, len(bounds)+1),
	}
}

// Observe records one duration sample.
func (h *Histogram) Observe(d time.Duration) {
	h.n.Add(1)
	h.sumNs.Add(int64(d))
	for i, b := range h.bounds {
		if d <= b {
			h.counts[i].Add(1)
			return
		}
	}
	h.counts[len(h.counts)-1].Add(1)
}

// HistogramSnapshot is a point-in-time view used by dumpers / HTTP.
type HistogramSnapshot struct {
	Bounds []time.Duration `json:"bounds_ns"`
	Counts []int64         `json:"counts"`
	SumNs  int64           `json:"sum_ns"`
	N      int64           `json:"n"`
}

// Snapshot copies counter values; cheap.
func (h *Histogram) Snapshot() HistogramSnapshot {
	out := HistogramSnapshot{
		Bounds: append([]time.Duration(nil), h.bounds...),
		Counts: make([]int64, len(h.counts)),
		SumNs:  h.sumNs.Load(),
		N:      h.n.Load(),
	}
	for i := range h.counts {
		out.Counts[i] = h.counts[i].Load()
	}
	return out
}

// Quantile estimates the value at q (0 < q < 1) via linear bucket
// interpolation. Returns 0 if there are no observations.
func (h *Histogram) Quantile(q float64) time.Duration {
	if q <= 0 || q >= 1 {
		return 0
	}
	snap := h.Snapshot()
	if snap.N == 0 {
		return 0
	}
	target := int64(float64(snap.N) * q)
	var cum int64
	for i, c := range snap.Counts {
		cum += c
		if cum >= target {
			if i < len(snap.Bounds) {
				return snap.Bounds[i]
			}
			// Overflow bucket: report mean of overflow as estimate.
			if c > 0 {
				return time.Duration(snap.SumNs / snap.N)
			}
			return snap.Bounds[len(snap.Bounds)-1]
		}
	}
	return 0
}

// Registry owns named metrics. It is goroutine-safe and the same
// instance is shared across server constructors.
type Registry struct {
	mu sync.RWMutex
	c  map[string]*Counter
	g  map[string]*Gauge
	h  map[string]*Histogram
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		c: map[string]*Counter{},
		g: map[string]*Gauge{},
		h: map[string]*Histogram{},
	}
}

// Counter returns (or creates) the named counter.
func (r *Registry) Counter(name string) *Counter {
	r.mu.RLock()
	if c, ok := r.c[name]; ok {
		r.mu.RUnlock()
		return c
	}
	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.c[name]; ok {
		return c
	}
	c := &Counter{}
	r.c[name] = c
	return c
}

// Gauge returns (or creates) the named gauge.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.RLock()
	if g, ok := r.g[name]; ok {
		r.mu.RUnlock()
		return g
	}
	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.g[name]; ok {
		return g
	}
	g := &Gauge{}
	r.g[name] = g
	return g
}

// Histogram returns (or creates) the named histogram with the given
// bounds. If a histogram is already registered with the same name, the
// existing one is returned regardless of bounds — first registration
// wins.
func (r *Registry) Histogram(name string, bounds ...time.Duration) *Histogram {
	r.mu.RLock()
	if h, ok := r.h[name]; ok {
		r.mu.RUnlock()
		return h
	}
	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.h[name]; ok {
		return h
	}
	h := NewHistogram(bounds...)
	r.h[name] = h
	return h
}

// Snapshot is a flat point-in-time view of every registered metric.
type Snapshot struct {
	TsUnixMs   int64                        `json:"ts_unix_ms"`
	Counters   map[string]int64             `json:"counters"`
	Gauges     map[string]int64             `json:"gauges"`
	Histograms map[string]HistogramSnapshot `json:"histograms"`
}

// Snapshot collects every metric. Names within each kind are sorted
// for stable JSONL output.
func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := Snapshot{
		TsUnixMs:   time.Now().UnixMilli(),
		Counters:   make(map[string]int64, len(r.c)),
		Gauges:     make(map[string]int64, len(r.g)),
		Histograms: make(map[string]HistogramSnapshot, len(r.h)),
	}
	for _, name := range sortedKeys(r.c) {
		out.Counters[name] = r.c[name].Value()
	}
	for _, name := range sortedKeys(r.g) {
		out.Gauges[name] = r.g[name].Value()
	}
	for _, name := range sortedKeys(r.h) {
		out.Histograms[name] = r.h[name].Snapshot()
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
