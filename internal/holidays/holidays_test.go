package holidays

import "testing"

func newT(t *testing.T) *Holidays {
	t.Helper()
	h, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestLunar(t *testing.T) {
	h := newT(t)
	// 2026-02-17 是农历正月初一（春节）
	info, err := h.Lunar("2026-02-17")
	if err != nil {
		t.Fatal(err)
	}
	if info.Day != "初一" || info.Text != info.Month+"月" {
		t.Fatalf("lunar: %+v", info)
	}
	// 2026-06-19 端午（五月初五）
	info2, err := h.Lunar("2026-06-19")
	if err != nil {
		t.Fatal(err)
	}
	if info2.Day != "初五" {
		t.Fatalf("duanwu: %+v", info2)
	}
	if info2.Animal != "马" || info2.YearGanZhi != "丙午" {
		t.Fatalf("ganzhi: %+v", info2)
	}
}

func TestCNDays(t *testing.T) {
	h := newT(t)
	days := h.CN(2026)
	if len(days) == 0 {
		t.Fatal("no CN data")
	}
	// 春节假期中
	if cn := h.CNAt("2026-02-17"); cn == nil || !cn.Off {
		t.Fatalf("spring festival off: %+v", cn)
	}
	// 补班日 2026-02-14
	if w := h.CNAt("2026-02-14"); w == nil || !w.Work {
		t.Fatalf("makeup workday: %+v", w)
	}
}

func TestUSHolidays(t *testing.T) {
	h := newT(t)
	// 2026-07-04 是周六 → 7/3 周五观测
	if us := h.USAt("2026-07-03"); us == nil || us.Name != "Independence Day (Observed)" {
		t.Fatalf("july4: %+v", us)
	}
	// 2026 感恩节：11 月第 4 个周四 = 11-26
	if h.USAt("2026-11-26") == nil {
		t.Fatal("thanksgiving 2026-11-26 missing")
	}
	// 2027-01-01 是周五，无顺延
	if h.USAt("2027-01-01") == nil {
		t.Fatal("new year 2027 missing")
	}
}

func TestDayAggregation(t *testing.T) {
	h := newT(t)
	info, err := h.Day("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if info.CN == nil || !info.CN.Off {
		t.Fatalf("national day: %+v", info)
	}
	if info.Weekend {
		t.Fatal("2026-10-01 is Thursday; Weekend should be false")
	}
	if info.Lunar.Text == "" {
		t.Fatal("lunar text missing")
	}
}
