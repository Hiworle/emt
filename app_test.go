package main

import "testing"

func TestNormalizeRunningSessionsMarksRunningIdle(t *testing.T) {
	sessions := []Session{
		{ID: "emt-1", Status: SessionStatusRunning},
		{ID: "emt-2", Status: SessionStatusIdle},
	}

	changed := normalizeRunningSessions(sessions)

	if !changed {
		t.Fatal("expected sessions to change")
	}
	if sessions[0].Status != SessionStatusIdle {
		t.Fatalf("expected running session to become idle, got %q", sessions[0].Status)
	}
	if sessions[1].Status != SessionStatusIdle {
		t.Fatalf("expected idle session to stay idle, got %q", sessions[1].Status)
	}
}

func TestNextSessionName(t *testing.T) {
	sessions := []Session{
		{Name: "Session 1"},
		{Name: "Session 2"},
	}

	if got := nextSessionName(sessions); got != "Session 3" {
		t.Fatalf("got %q, want %q", got, "Session 3")
	}
}
