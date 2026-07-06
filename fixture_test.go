package main

import (
	"math"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	initPricing()
	os.Exit(m.Run())
}

// TestFixture_RealisticConversation runs parseLogs against the static testdata/
// fixture — a multi-turn JWT refactor conversation with streaming duplicates,
// subagent files, thinking blocks, mixed cache tiers, a <synthetic> error entry,
// and two different models (Opus main + Haiku subagent).
//
// All expected values are hand-calculated from the fixture JSONL files.
func TestFixture_RealisticConversation(t *testing.T) {
	data, err := parseLogs("testdata", 0, "")
	if err != nil {
		t.Fatalf("parseLogs: %v", err)
	}

	// --- File and record counts ---

	assertInt(t, "TotalFiles", data.TotalFiles, 2)

	// 7 unique requestIds after dedup:
	//   main:     req_main_001, req_main_002, req_main_003, req_main_004
	//   subagent: req_sub_001,  req_sub_002,  req_sub_003
	// <synthetic> req_error_001 is filtered out.
	assertInt(t, "TotalRecords", data.TotalRecords, 7)

	assertInt(t, "ParseErrors", data.ParseErrors, 0)

	// --- Opus 4.6 model bucket ---
	// Final values after dedup (last-write-wins per requestId):
	//   req_main_001: in=15000  out=2500  cr=50000  cw5m=8000  cw1h=3000
	//   req_main_002: in=22000  out=4800  cr=62000  cw5m=0     cw1h=5000
	//   req_main_003: in=30000  out=6200  cr=70000  cw5m=2000  cw1h=0
	//   req_main_004: in=35000  out=3500  cr=80000  cw5m=0     cw1h=4000  (flat fallback → 1h)

	opus := data.ModelUsage["claude-opus-4-6"]
	if opus == nil {
		t.Fatal("missing Opus 4.6 model bucket")
		return
	}
	assertInt(t, "opus.Requests", opus.Requests, 4)
	assertInt(t, "opus.InputTokens", opus.InputTokens, 102000)
	assertInt(t, "opus.OutputTokens", opus.OutputTokens, 17000)
	assertInt(t, "opus.CacheRead", opus.CacheRead, 262000)
	assertInt(t, "opus.CacheWrite5m", opus.CacheWrite5m, 10000)
	assertInt(t, "opus.CacheWrite1h", opus.CacheWrite1h, 12000)

	// Opus cost per request (pricing: Input=$5/M, Output=$25/M,
	//   CacheWrite5m=$6.25/M, CacheWrite1h=$10/M, CacheRead=$0.50/M):
	//   req_main_001: 0.075  + 0.0625 + 0.05   + 0.03  + 0.025  = 0.2425
	//   req_main_002: 0.11   + 0.12   + 0      + 0.05  + 0.031  = 0.311
	//   req_main_003: 0.15   + 0.155  + 0.0125 + 0     + 0.035  = 0.3525
	//   req_main_004: 0.175  + 0.0875 + 0      + 0.04  + 0.04   = 0.3425 (flat fallback → 1h)
	//   Total: 1.2485
	assertCost(t, "opus.Cost", opus.Cost, 1.2485)

	// --- Haiku 4.5 model bucket ---
	// Final values after dedup:
	//   req_sub_001: in=5000  out=1200  cr=12000  cw5m=3000  cw1h=0
	//   req_sub_002: in=8000  out=2800  cr=15000  cw5m=0     cw1h=2000
	//   req_sub_003: in=6000  out=900   cr=18000  cw5m=1500  cw1h=0

	haiku := data.ModelUsage["claude-haiku-4-5-20251001"]
	if haiku == nil {
		t.Fatal("missing Haiku 4.5 model bucket")
		return
	}
	assertInt(t, "haiku.Requests", haiku.Requests, 3)
	assertInt(t, "haiku.InputTokens", haiku.InputTokens, 19000)
	assertInt(t, "haiku.OutputTokens", haiku.OutputTokens, 4900)
	assertInt(t, "haiku.CacheRead", haiku.CacheRead, 45000)
	assertInt(t, "haiku.CacheWrite5m", haiku.CacheWrite5m, 4500)
	assertInt(t, "haiku.CacheWrite1h", haiku.CacheWrite1h, 2000)

	// Haiku cost per request (pricing: Input=$1/M, Output=$5/M,
	//   CacheWrite5m=$1.25/M, CacheWrite1h=$2/M, CacheRead=$0.10/M):
	//   req_sub_001: 0.005 + 0.006 + 0.00375 + 0      + 0.0012 = 0.01595
	//   req_sub_002: 0.008 + 0.014 + 0       + 0.004  + 0.0015 = 0.0275
	//   req_sub_003: 0.006 + 0.0045+ 0.001875+ 0      + 0.0018 = 0.014175
	//   Total: 0.057625
	assertCost(t, "haiku.Cost", haiku.Cost, 0.057625)

	// --- No other models should exist ---
	assertInt(t, "len(ModelUsage)", len(data.ModelUsage), 2)

	// --- Daily aggregation ---

	// 2026-02-18: req_main_001 + req_main_002 (both Opus)
	day18 := data.DailyUsage["2026-02-18"]
	if day18 == nil {
		t.Fatal("missing daily bucket for 2026-02-18")
	}
	if day18opus := day18["claude-opus-4-6"]; day18opus != nil {
		assertInt(t, "day18.opus.Requests", day18opus.Requests, 2)
		assertInt(t, "day18.opus.InputTokens", day18opus.InputTokens, 37000)
		// req_main_001 (0.2425) + req_main_002 (0.311)
		assertCost(t, "day18.opus.Cost", day18opus.Cost, 0.5535)
	} else {
		t.Error("missing opus bucket for 2026-02-18")
	}

	// 2026-02-19: req_main_003 + req_main_004 (Opus) + all 3 subagent reqs (Haiku)
	day19 := data.DailyUsage["2026-02-19"]
	if day19 == nil {
		t.Fatal("missing daily bucket for 2026-02-19")
	}
	if day19opus := day19["claude-opus-4-6"]; day19opus != nil {
		assertInt(t, "day19.opus.Requests", day19opus.Requests, 2)
		// req_main_003 (0.3525) + req_main_004 (0.3425)
		assertCost(t, "day19.opus.Cost", day19opus.Cost, 0.695)
	} else {
		t.Error("missing opus bucket for 2026-02-19")
	}
	if day19haiku := day19["claude-haiku-4-5-20251001"]; day19haiku != nil {
		assertInt(t, "day19.haiku.Requests", day19haiku.Requests, 3)
		assertCost(t, "day19.haiku.Cost", day19haiku.Cost, 0.057625)
	} else {
		t.Error("missing haiku bucket for 2026-02-19")
	}

	// --- Project aggregation ---

	proj := data.ProjectUsage["C--Users-alice-git-webapp"]
	if proj == nil {
		t.Fatal("missing project bucket")
	}
	assertInt(t, "len(project models)", len(proj), 2)
	assertCost(t, "project.opus.Cost", proj["claude-opus-4-6"].Cost, 1.2485)
	assertCost(t, "project.haiku.Cost", proj["claude-haiku-4-5-20251001"].Cost, 0.057625)

	// --- Branch aggregation ---
	// All fixture entries have gitBranch:"main"
	branchData := data.BranchUsage["C--Users-alice-git-webapp"]
	if branchData == nil {
		t.Fatal("missing branch data for project")
	}
	mainBranch := branchData["main"]
	if mainBranch == nil {
		t.Fatal("missing 'main' branch bucket")
	}
	assertInt(t, "len(main branch models)", len(mainBranch), 2)
	assertCost(t, "branch.main.opus.Cost", mainBranch["claude-opus-4-6"].Cost, 1.2485)
	assertCost(t, "branch.main.haiku.Cost", mainBranch["claude-haiku-4-5-20251001"].Cost, 0.057625)
}

func TestFixture_SyntheticEntriesSkipped(t *testing.T) {
	data, err := parseLogs("testdata", 0, "")
	if err != nil {
		t.Fatalf("parseLogs: %v", err)
	}
	for model := range data.ModelUsage {
		if model == "<synthetic>" {
			t.Error("<synthetic> model should be filtered out")
		}
	}
}

func TestFixture_ProjectFilter(t *testing.T) {
	data, err := parseLogs("testdata", 0, "alice")
	if err != nil {
		t.Fatalf("parseLogs: %v", err)
	}
	if data.TotalRecords != 7 {
		t.Errorf("filter 'alice': TotalRecords = %d, want 7", data.TotalRecords)
	}

	data, err = parseLogs("testdata", 0, "nonexistent")
	if err != nil {
		t.Fatalf("parseLogs: %v", err)
	}
	if data.TotalRecords != 0 {
		t.Errorf("filter 'nonexistent': TotalRecords = %d, want 0", data.TotalRecords)
	}
}

func assertInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}

func assertCost(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000001 {
		t.Errorf("%s = %.6f, want %.6f", name, got, want)
	}
}
