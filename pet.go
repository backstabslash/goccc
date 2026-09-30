package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PetConfig is the kaomoji shown by the "pet" segment. Hooks record the turn
// state; the statusline's refreshInterval drives blinking and the spinner.
type PetConfig struct {
	Idle         string   `json:"idle,omitempty"`
	Blink        string   `json:"blink,omitempty"`
	Happy        string   `json:"happy,omitempty"`
	Working      string   `json:"working,omitempty"`
	Spinner      []string `json:"spinner,omitempty"`
	BlinkEvery   *int     `json:"blink_every,omitempty"`   // pointer so 0 can turn blinking off
	HappySeconds *int     `json:"happy_seconds,omitempty"` // pointer so 0 can turn the happy face off
}

var defaultPet = PetConfig{
	Idle:         "( •ᴗ•)",
	Blink:        "( -ᴗ-)",
	Happy:        "( ^ᴗ^)",
	Working:      "( •̀ᴗ•́)",
	Spinner:      []string{"◐", "◓", "◑", "◒"},
	BlinkEvery:   new(4),
	HappySeconds: new(3),
}

type petActivity int

const (
	petIdle petActivity = iota
	petWorking
	petHappy
)

var petStates = map[string]bool{"working": true, "done": true, "idle": true}

const interruptMarker = "[Request interrupted by user"

func resolvePet(cfg *PetConfig) PetConfig {
	p := defaultPet
	if cfg == nil {
		return p
	}
	p.Idle = cmp.Or(cfg.Idle, p.Idle)
	p.Blink = cmp.Or(cfg.Blink, p.Blink)
	p.Happy = cmp.Or(cfg.Happy, p.Happy)
	p.Working = cmp.Or(cfg.Working, p.Working)
	if cfg.BlinkEvery != nil {
		p.BlinkEvery = cfg.BlinkEvery
	}
	if cfg.HappySeconds != nil {
		p.HappySeconds = cfg.HappySeconds
	}
	// An explicit [] turns the spinner off.
	if cfg.Spinner != nil {
		p.Spinner = cfg.Spinner
	}
	return p
}

// sessionStateDir lives under the user's cache dir, not the shared /tmp, where
// another user could pre-create it and plant symlinks.
var sessionStateDir = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "goccc", "sessions")
}

// sessionStatePath returns "" for ids that could escape the state dir.
func sessionStatePath(sessionID, ext string) string {
	dir := sessionStateDir()
	if dir == "" || sessionID == "" || sessionID != filepath.Base(sessionID) || strings.HasPrefix(sessionID, ".") {
		return ""
	}
	return filepath.Join(dir, sessionID+ext)
}

func writeSessionState(sessionID, ext string, data []byte) error {
	path := sessionStatePath(sessionID, ext)
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// Without the SessionEnd hook nothing removes a session's files, so any older
// than this are swept whenever a new session writes its first state.
const staleSessionState = 24 * time.Hour

func pruneSessionState(now time.Time) {
	dir := sessionStateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > staleSessionState {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func removeSessionState(sessionID string) {
	for _, ext := range []string{".pet", ".costs.json"} {
		if path := sessionStatePath(sessionID, ext); path != "" {
			_ = os.Remove(path)
		}
	}
}

// runPetState is the hook entry point. Errors are silent so a hook never blocks a turn.
func runPetState(state string) {
	if !petStates[state] {
		return
	}
	var input struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		return
	}
	_ = writeSessionState(input.SessionID, ".pet", []byte(state))
}

func readPetActivity(sessionID, transcriptPath string, now time.Time, happyFor time.Duration) petActivity {
	path := sessionStatePath(sessionID, ".pet")
	if path == "" {
		return petIdle
	}
	info, err := os.Stat(path)
	if err != nil {
		return petIdle
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return petIdle
	}
	switch strings.TrimSpace(string(data)) {
	case "working":
		// Esc interrupts fire no Stop hook, so the transcript is the only signal.
		if lastTurnInterrupted(transcriptPath) {
			return petIdle
		}
		return petWorking
	case "done":
		if now.Sub(info.ModTime()) < happyFor {
			return petHappy
		}
	}
	return petIdle
}

const transcriptTailBytes = 64 * 1024

func lastTurnInterrupted(transcriptPath string) bool {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	offset := max(info.Size()-transcriptTailBytes, 0)
	tail := make([]byte, info.Size()-offset)
	if _, err := f.ReadAt(tail, offset); err != nil && err != io.EOF {
		return false
	}

	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &entry) != nil {
			continue
		}
		switch entry.Type {
		case "assistant":
			return false
		case "user":
			return isInterruptMarker(entry.Message.Content)
		}
	}
	return false
}

// isInterruptMarker matches only Claude Code's own marker, a text block (or string
// content) starting with it, not a tool result that merely quotes it.
func isInterruptMarker(content json.RawMessage) bool {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return strings.HasPrefix(text, interruptMarker)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "text" && strings.HasPrefix(b.Text, interruptMarker) {
			return true
		}
	}
	return false
}

func petFace(p PetConfig, a petActivity, tick int64) string {
	switch a {
	case petWorking:
		if len(p.Spinner) == 0 {
			return p.Working
		}
		if frame := p.Spinner[tick%int64(len(p.Spinner))]; frame != "" {
			return p.Working + " " + frame
		}
		return p.Working
	case petHappy:
		return p.Happy
	}
	if n := intOrZero(p.BlinkEvery); n > 0 && tick%int64(n) == 0 {
		return p.Blink
	}
	return p.Idle
}

func renderPet(ctx *StatuslineContext) string {
	now := time.Now()
	happyFor := time.Duration(intOrZero(ctx.Pet.HappySeconds)) * time.Second
	a := readPetActivity(ctx.Input.SessionID, ctx.Input.TranscriptPath, now, happyFor)
	return petFace(ctx.Pet, a, now.Unix())
}

func intOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
