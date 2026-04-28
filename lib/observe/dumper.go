// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Dumper periodically writes a Snapshot of the Registry as one JSON
// line to a file. Each line is self-contained, so downstream tooling
// (e.g. `tools/correlate`, a notebook) can stream the file without
// holding state across lines.
type Dumper struct {
	reg      *Registry
	path     string
	interval time.Duration

	mu sync.Mutex
	w  io.WriteCloser
}

// NewDumper writes JSONL lines to path every interval. path is created
// if missing; its parent directory must already exist (or be ".").
func NewDumper(reg *Registry, path string, interval time.Duration) *Dumper {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Dumper{reg: reg, path: path, interval: interval}
}

// Run blocks until ctx cancels, dumping snapshots at the configured
// interval. The first dump happens immediately after Run starts so
// short-lived test processes still produce output.
func (d *Dumper) Run(ctx context.Context) error {
	if err := d.openIfNeeded(); err != nil {
		return err
	}
	defer d.close()

	if err := d.writeOne(); err != nil {
		return err
	}

	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := d.writeOne(); err != nil {
				return err
			}
		}
	}
}

func (d *Dumper) openIfNeeded() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.w != nil {
		return nil
	}
	if dir := filepath.Dir(d.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("observe: mkdir %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(d.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("observe: open %s: %w", d.path, err)
	}
	d.w = f
	return nil
}

func (d *Dumper) writeOne() error {
	snap := d.reg.Snapshot()
	d.mu.Lock()
	defer d.mu.Unlock()
	enc := json.NewEncoder(d.w)
	if err := enc.Encode(snap); err != nil {
		return fmt.Errorf("observe: encode: %w", err)
	}
	return nil
}

func (d *Dumper) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.w != nil {
		_ = d.w.Close()
		d.w = nil
	}
}
