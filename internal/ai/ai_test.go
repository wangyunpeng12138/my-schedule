package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestParseInjectsCurrentDate 验证相对日期换算所需的当前日期/星期已注入用户消息。
func TestParseInjectsCurrentDate(t *testing.T) {
	var gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) > 1 {
			gotUser = req.Messages[1].Content
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"title\":\"团队周会\",\"date\":\"2026-11-24\",\"time\":\"15:00\"}"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model", "k")
	now := time.Date(2026, 11, 24, 10, 30, 0, 0, time.Local)
	d, _, err := c.Parse(context.Background(), now, "今天下午三点开团队周会")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.Title != "团队周会" || d.Date != "2026-11-24" || d.Time != "15:00" {
		t.Fatalf("草稿解析异常: %+v", d)
	}
	for _, want := range []string{"2026-11-24", "周二", "10:30", "今天下午三点开团队周会"} {
		if !strings.Contains(gotUser, want) {
			t.Fatalf("用户消息缺少 %q: %q", want, gotUser)
		}
	}
}
