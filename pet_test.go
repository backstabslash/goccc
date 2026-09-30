package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolvePet(t *testing.T) {
	p := resolvePet(&PetConfig{Idle: "(o_o)", Spinner: []string{"-"}})
	if p.Idle != "(o_o)" || len(p.Spinner) != 1 {
		t.Errorf("overrides not applied: %+v", p)
	}
	if p.Blink != defaultPet.Blink || *p.HappySeconds != *defaultPet.HappySeconds {
		t.Errorf("defaults not kept: %+v", p)
	}
}

func TestResolvePet_ZeroAndEmptyTurnThingsOff(t *testing.T) {
	p := resolvePet(&PetConfig{BlinkEvery: new(0), HappySeconds: new(0), Spinner: []string{}})
	if *p.BlinkEvery != 0 || *p.HappySeconds != 0 || len(p.Spinner) != 0 {
		t.Errorf("explicit zero/empty should override defaults: %+v", p)
	}
	for tick := range int64(8) {
		if got := petFace(p, petIdle, tick); got != p.Idle {
			t.Errorf("blink_every 0: tick %d shows %q, want idle", tick, got)
		}
	}
	if got := petFace(p, petWorking, 0); got != p.Working {
		t.Errorf("empty spinner: got %q, want bare working face", got)
	}
}

func TestPetFace(t *testing.T) {
	p := PetConfig{Idle: "i", Blink: "b", Happy: "h", Working: "w", Spinner: []string{"1", "2", ""}, BlinkEvery: new(3)}
	tests := []struct {
		a    petActivity
		tick int64
		want string
	}{
		{petWorking, 0, "w 1"},
		{petWorking, 4, "w 2"},
		{petWorking, 5, "w"},
		{petHappy, 0, "h"},
		{petIdle, 3, "b"},
		{petIdle, 4, "i"},
	}
	for _, tt := range tests {
		if got := petFace(p, tt.a, tt.tick); got != tt.want {
			t.Errorf("petFace(%d, %d) = %q, want %q", tt.a, tt.tick, got, tt.want)
		}
	}
}

func TestSessionStatePath_RejectsTraversal(t *testing.T) {
	for _, id := range []string{"", "../x", "a/b", ".."} {
		if got := sessionStatePath(id, ".pet"); got != "" {
			t.Errorf("sessionStatePath(%q) = %q, want empty", id, got)
		}
	}
}

// useStateDir points session state at a temp dir for the test.
func useStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := sessionStateDir
	sessionStateDir = func() string { return dir }
	t.Cleanup(func() { sessionStateDir = orig })
	return dir
}

func TestPruneSessionState(t *testing.T) {
	dir := useStateDir(t)
	for _, id := range []string{"old", "fresh"} {
		if err := writeSessionState(id, ".pet", []byte("idle")); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * staleSessionState)
	if err := os.Chtimes(filepath.Join(dir, "old.pet"), old, old); err != nil {
		t.Fatal(err)
	}
	pruneSessionState(time.Now())
	if _, err := os.Stat(filepath.Join(dir, "old.pet")); !os.IsNotExist(err) {
		t.Error("stale state should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "fresh.pet")); err != nil {
		t.Errorf("fresh state should survive: %v", err)
	}
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLastTurnInterrupted(t *testing.T) {
	interrupted := writeTranscript(t,
		`{"type":"assistant","message":{"content":[]}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}`,
		`{"type":"system","subtype":"x"}`,
	)
	if !lastTurnInterrupted(interrupted) {
		t.Error("expected interrupted")
	}
	working := writeTranscript(t,
		`{"type":"user","message":{"content":"[Request interrupted by user]"}}`,
		`{"type":"user","message":{"content":"next prompt"}}`,
	)
	if lastTurnInterrupted(working) {
		t.Error("new prompt after interrupt should count as working")
	}
	quoted := writeTranscript(t,
		`{"type":"assistant","message":{"content":[]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"pet.go:12: [Request interrupted by user"}]}}`,
	)
	if lastTurnInterrupted(quoted) {
		t.Error("a tool result quoting the marker is not an interrupt")
	}
	prompt := writeTranscript(t, `{"type":"user","message":{"content":"why does it say [Request interrupted by user]?"}}`)
	if lastTurnInterrupted(prompt) {
		t.Error("a prompt mentioning the marker is not an interrupt")
	}
	if lastTurnInterrupted(filepath.Join(t.TempDir(), "missing.jsonl")) {
		t.Error("missing transcript should not count as interrupted")
	}
}

func TestReadPetActivity(t *testing.T) {
	useStateDir(t)
	now := time.Now()
	transcript := writeTranscript(t, `{"type":"user","message":{"content":"go"}}`)

	if got := readPetActivity("s1", transcript, now, 3*time.Second); got != petIdle {
		t.Errorf("no state file: got %d, want idle", got)
	}
	if err := writeSessionState("s1", ".pet", []byte("working")); err != nil {
		t.Fatal(err)
	}
	if got := readPetActivity("s1", transcript, now, 3*time.Second); got != petWorking {
		t.Errorf("working: got %d", got)
	}
	if err := writeSessionState("s1", ".pet", []byte("done")); err != nil {
		t.Fatal(err)
	}
	if got := readPetActivity("s1", transcript, time.Now(), 3*time.Second); got != petHappy {
		t.Errorf("just done: got %d, want happy", got)
	}
	if got := readPetActivity("s1", transcript, time.Now().Add(5*time.Second), 3*time.Second); got != petIdle {
		t.Errorf("done long ago: got %d, want idle", got)
	}
}
