package dates

import (
	"testing"

	"myschedule/internal/model"
)

func TestKeyParseRoundTrip(t *testing.T) {
	k := "2026-11-24"
	tm, err := Parse(k)
	if err != nil {
		t.Fatal(err)
	}
	if Key(tm) != k {
		t.Fatalf("round trip: %s", Key(tm))
	}
	if _, err := Parse("2026-2-30"); err == nil {
		t.Fatal("should reject non-strict date")
	}
	if _, err := Parse("2026-11-31"); err == nil {
		t.Fatal("should reject invalid day")
	}
}

func TestAddDays(t *testing.T) {
	if k, _ := AddDays("2026-02-28", 1); k != "2026-03-01" {
		t.Fatalf("AddDays: %s", k)
	}
	if k, _ := AddDays("2024-02-28", 1); k != "2024-02-29" {
		t.Fatalf("leap year: %s", k)
	}
	if _, err := AddDays("bad", 1); err == nil {
		t.Fatal("should reject bad key")
	}
}

func TestClampTime(t *testing.T) {
	if ClampTime("25:99") != "09:00" || ClampTime("07:05") != "07:05" {
		t.Fatal("ClampTime")
	}
}

func TestMonday(t *testing.T) {
	sun, _ := Parse("2026-11-22") // 周日
	if got := Monday(sun, 1); Key(got) != "2026-11-16" {
		t.Fatalf("monday start: %s", Key(got))
	}
	if got := Monday(sun, 0); Key(got) != "2026-11-22" {
		t.Fatalf("sunday start: %s", Key(got))
	}
}

func TestShiftStartPreservesSpan(t *testing.T) {
	task := &model.Task{Date: "2026-11-24", EndDate: "2026-11-26"}
	nd, ne, err := ShiftStart(task, "2026-12-01")
	if err != nil {
		t.Fatal(err)
	}
	if nd != "2026-12-01" || ne != "2026-12-03" {
		t.Fatalf("shift: %s ~ %s", nd, ne)
	}
}

func TestTaskEndDateAndOccursOn(t *testing.T) {
	task := &model.Task{Date: "2026-11-24", EndDate: "2026-11-26"}
	if TaskEndDate(task) != "2026-11-26" {
		t.Fatal("TaskEndDate")
	}
	if !OccursOn(task, "2026-11-25") || OccursOn(task, "2026-11-27") {
		t.Fatal("OccursOn")
	}
	if !OverlapsRange(task, "2026-11-26", "2026-11-30") {
		t.Fatal("OverlapsRange should hit")
	}
	if OverlapsRange(task, "2026-12-01", "2026-12-02") {
		t.Fatal("OverlapsRange should miss")
	}
}
