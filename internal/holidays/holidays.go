// Package holidays 农历与中国法定节假日（含调休补班）+ 美国联邦假日。
// 中国节假日为按年打包的数据文件（docs/module-api.md §holidays），可替换热更。
package holidays

import (
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/6tail/lunar-go/calendar"

	"myschedule/internal/dates"
)

//go:embed data/cn-*.json
var cnFS embed.FS

// CNDay 中国节假日数据行。
type CNDay struct {
	Date string `json:"date"`
	Name string `json:"name"`
	Off  bool   `json:"off"`  // 放假
	Work bool   `json:"work"` // 补班
}

type cnYear struct {
	Year int     `json:"year"`
	Days []CNDay `json:"days"`
}

// USDay 美国联邦假日（已按周末顺延规则观测）。
type USDay struct {
	Date string `json:"date"`
	Name string `json:"name"`
}

// LunarInfo 农历信息。
type LunarInfo struct {
	Text       string `json:"text"`       // 如「十一月廿三」
	Month      string `json:"month"`      // 十一月
	Day        string `json:"day"`        // 廿三
	YearGanZhi string `json:"yearGanZhi"` // 丙午
	Animal     string `json:"animal"`     // 马
	Term       string `json:"term,omitempty"` // 当日节气
}

// DayInfo holidays.day 的聚合返回。
type DayInfo struct {
	Date    string    `json:"date"`
	Lunar   LunarInfo `json:"lunar"`
	CN      *CNDay    `json:"cn,omitempty"`
	US      *USDay    `json:"us,omitempty"`
	Weekend bool      `json:"weekend"`
}

// Holidays 节假日服务。
type Holidays struct {
	cn map[int]map[string]CNDay
	us map[int]map[string]USDay
}

// New 加载内置数据。
func New() (*Holidays, error) {
	h := &Holidays{cn: map[int]map[string]CNDay{}, us: map[int]map[string]USDay{}}
	entries, err := cnFS.ReadDir("data")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		raw, err := cnFS.ReadFile("data/" + e.Name())
		if err != nil {
			return nil, err
		}
		var y cnYear
		if err := json.Unmarshal(raw, &y); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		m := map[string]CNDay{}
		for _, d := range y.Days {
			m[d.Date] = d
		}
		h.cn[y.Year] = m
	}
	return h, nil
}

// Years 内置中国节假日数据覆盖年份。
func (h *Holidays) Years() []int {
	out := make([]int, 0, len(h.cn))
	for y := range h.cn {
		out = append(out, y)
	}
	for i := 0; i < len(out); i++ { // 小顶排序
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Lunar 取某日农历。
func (h *Holidays) Lunar(key string) (LunarInfo, error) {
	t, err := dates.Parse(key)
	if err != nil {
		return LunarInfo{}, err
	}
	lunar := calendar.NewSolarFromYmd(t.Year(), int(t.Month()), t.Day()).GetLunar()
	info := LunarInfo{
		Month:      lunar.GetMonthInChinese(),
		Day:        lunar.GetDayInChinese(),
		YearGanZhi: lunar.GetYearInGanZhi(),
	}
	info.Animal = animalOfGanZhi(info.YearGanZhi)
	if info.Day == "初一" { // 每月初一显示月名
		info.Text = info.Month + "月"
	} else {
		info.Text = info.Month + "月" + info.Day
	}
	info.Term = lunar.GetJieQi()
	return info, nil
}

// branchAnimals 十二地支对应生肖（lunar-go GetAnimal 不可靠，自行映射）。
var branchAnimals = map[string]string{
	"子": "鼠", "丑": "牛", "寅": "虎", "卯": "兔", "辰": "龙", "巳": "蛇",
	"午": "马", "未": "羊", "申": "猴", "酉": "鸡", "戌": "狗", "亥": "猪",
}

// animalOfGanZhi 由干支年（如 丙午）取生肖。
func animalOfGanZhi(gz string) string {
	if len([]rune(gz)) < 2 {
		return ""
	}
	return branchAnimals[string([]rune(gz)[1])]
}

// CN 某年中国节假日全表。
func (h *Holidays) CN(year int) []CNDay {
	m, ok := h.cn[year]
	if !ok {
		return nil
	}
	out := make([]CNDay, 0, len(m))
	// 按日期排序输出
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

// CNAt 某日的中国节假日信息（无则 nil）。
func (h *Holidays) CNAt(key string) *CNDay {
	if len(key) < 4 {
		return nil
	}
	y, err := strconv.Atoi(key[:4])
	if err != nil {
		return nil
	}
	if d, ok := h.cn[y][key]; ok {
		return &d
	}
	return nil
}

// US 某年美国联邦假日（观测日）。
func (h *Holidays) US(year int) []USDay {
	if m, ok := h.us[year]; ok {
		out := make([]USDay, 0, len(m))
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for _, k := range keys {
			out = append(out, m[k])
		}
		return out
	}
	h.us[year] = usCompute(year)
	return h.US(year)
}

// USAt 某日的美国联邦假日（无则 nil）。
func (h *Holidays) USAt(key string) *USDay {
	if len(key) < 4 {
		return nil
	}
	y, err := strconv.Atoi(key[:4])
	if err != nil {
		return nil
	}
	for _, d := range h.US(y) {
		if d.Date == key {
			return &d
		}
	}
	return nil
}

// Day 聚合某日全部信息。
func (h *Holidays) Day(key string) (DayInfo, error) {
	t, err := dates.Parse(key)
	if err != nil {
		return DayInfo{}, err
	}
	info := DayInfo{Date: key, Weekend: t.Weekday() == time.Saturday || t.Weekday() == time.Sunday}
	if info.Lunar, err = h.Lunar(key); err != nil {
		return info, err
	}
	info.CN = h.CNAt(key)
	info.US = h.USAt(key)
	return info, nil
}

// usCompute 计算某年美国联邦假日（5 U.S.C. § 6103 周末顺延观测规则）。
func usCompute(year int) map[string]USDay {
	out := map[string]USDay{}
	key := func(m time.Month, d int) string {
		return time.Date(year, m, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}
	add := func(k, name string) { out[k] = USDay{Date: k, Name: name} }
	observed := func(name string, t time.Time) {
		switch t.Weekday() {
		case time.Saturday:
			out[key(t.Month(), t.Day()-1)] = USDay{key(t.Month(), t.Day()-1), name + " (Observed)"}
		case time.Sunday:
			out[key(t.Month(), t.Day()+1)] = USDay{key(t.Month(), t.Day()+1), name + " (Observed)"}
		default:
			out[key(t.Month(), t.Day())] = USDay{key(t.Month(), t.Day()), name}
		}
	}
	nth := func(m time.Month, wd time.Weekday, n int) time.Time {
		t := time.Date(year, m, 1, 0, 0, 0, 0, time.UTC)
		for int(t.Weekday()) != int(wd) {
			t = t.AddDate(0, 0, 1)
		}
		return t.AddDate(0, 0, 7*(n-1))
	}
	last := func(m time.Month, wd time.Weekday) time.Time {
		t := time.Date(year, m+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		for t.Weekday() != wd {
			t = t.AddDate(0, 0, -1)
		}
		return t
	}
	observed("New Year's Day", time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC))
	add(dkey(nth(time.January, time.Monday, 3)), "Martin Luther King Jr. Day")
	add(dkey(nth(time.February, time.Monday, 3)), "Washington's Birthday")
	add(dkey(last(time.May, time.Monday)), "Memorial Day")
	observed("Juneteenth", time.Date(year, 6, 19, 0, 0, 0, 0, time.UTC))
	observed("Independence Day", time.Date(year, 7, 4, 0, 0, 0, 0, time.UTC))
	add(dkey(nth(time.September, time.Monday, 1)), "Labor Day")
	add(dkey(nth(time.October, time.Monday, 2)), "Columbus Day")
	observed("Veterans Day", time.Date(year, 11, 11, 0, 0, 0, 0, time.UTC))
	add(dkey(nth(time.November, time.Thursday, 4)), "Thanksgiving Day")
	observed("Christmas Day", time.Date(year, 12, 25, 0, 0, 0, 0, time.UTC))
	return out
}

func dkey(t time.Time) string { return t.Format("2006-01-02") }
