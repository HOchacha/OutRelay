// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// correlate groups JSONL audit / log events by stream_id across the
// agent, relay, and controller binaries — the minimum viable
// cross-component log correlator for ORP.
//
// Usage:
//
//	correlate <file...>            # group by stream_id, sort by ts
//	correlate -id <stream-id> file # filter to one stream
//
// Each input line must be a JSON object with a "stream_id" string
// and a "ts_unix_ms" integer; lines missing either are dropped.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
)

type record struct {
	StreamID string         `json:"stream_id"`
	TsUnixMs int64          `json:"ts_unix_ms"`
	Source   string         `json:"source,omitempty"`
	Raw      map[string]any `json:"-"`
}

func main() {
	wantID := flag.String("id", "", "filter to a single stream id")
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: correlate [-id ID] <file...>")
		os.Exit(2)
	}

	byID := map[string][]record{}
	for _, path := range files {
		// CLI tool: paths come straight from the operator's argv, so
		// "user-controlled file open" is by design, not a vulnerability.
		f, err := os.Open(path) // #nosec G304
		if err != nil {
			fmt.Fprintln(os.Stderr, "open:", err)
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var raw map[string]any
			if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
				continue
			}
			id, _ := raw["stream_id"].(string)
			ts, _ := numField(raw["ts_unix_ms"])
			if id == "" || ts == 0 {
				continue
			}
			if *wantID != "" && id != *wantID {
				continue
			}
			r := record{StreamID: id, TsUnixMs: ts, Source: path, Raw: raw}
			byID[id] = append(byID[id], r)
		}
		_ = f.Close()
	}

	// Stable iteration: sort group keys, then events within a group.
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	enc := json.NewEncoder(os.Stdout)
	for _, id := range ids {
		group := byID[id]
		sort.SliceStable(group, func(i, j int) bool { return group[i].TsUnixMs < group[j].TsUnixMs })
		fmt.Printf("# stream_id=%s events=%d\n", id, len(group))
		for _, r := range group {
			r.Raw["_source_file"] = r.Source
			_ = enc.Encode(r.Raw)
		}
	}
}

// numField accepts JSON numbers (float64 in encoding/json) or
// integer-typed values written by other encoders.
func numField(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	}
	return 0, false
}
