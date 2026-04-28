// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package orp

import "testing"

// TestStreamFSMHappyPath walks the full lifecycle through splice.
func TestStreamFSMHappyPath(t *testing.T) {
	t.Parallel()
	f := NewStreamFSM()
	steps := []struct {
		ev   StreamEvent
		want StreamState
	}{
		{EventOpen, StateOpening},
		{EventAccept, StateAccepted},
		{EventSplice, StateSpliced},
		{EventFin, StateClosing},
		{EventFin, StateClosed},
	}
	for i, s := range steps {
		if err := f.Apply(s.ev); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if got := f.State(); got != s.want {
			t.Fatalf("step %d: got %s, want %s", i, got, s.want)
		}
	}
}

// TestStreamFSMResetTerminates: any state moves to Closed on Reset.
func TestStreamFSMResetTerminates(t *testing.T) {
	t.Parallel()
	starts := []StreamState{StateInit, StateOpening, StateAccepted, StateSpliced, StateClosing}
	for _, s := range starts {
		t.Run(s.String(), func(t *testing.T) {
			t.Parallel()
			f := &StreamFSM{state: s}
			if err := f.Apply(EventReset); err != nil {
				t.Fatal(err)
			}
			if f.State() != StateClosed {
				t.Fatalf("got %s, want Closed", f.State())
			}
		})
	}
}

// TestStreamFSMRejectsInvalid: bad transitions error and leave state intact.
func TestStreamFSMRejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from StreamState
		ev   StreamEvent
	}{
		{StateInit, EventAccept},  // can't accept before open
		{StateInit, EventSplice},  // can't splice before open
		{StateInit, EventFin},     // can't FIN before open
		{StateOpening, EventOpen}, // double open
		{StateAccepted, EventOpen},
		{StateSpliced, EventOpen},
		{StateSpliced, EventAccept},
		{StateClosed, EventOpen},
		{StateClosed, EventAccept},
		{StateClosed, EventSplice},
		{StateClosed, EventFin},
	}
	for _, c := range cases {
		t.Run(c.from.String()+"_"+c.ev.String(), func(t *testing.T) {
			t.Parallel()
			f := &StreamFSM{state: c.from}
			if err := f.Apply(c.ev); err == nil {
				t.Fatalf("expected error, got state=%s", f.State())
			}
			if f.State() != c.from {
				t.Fatalf("state changed from %s to %s on invalid transition", c.from, f.State())
			}
		})
	}
}
