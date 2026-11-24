// Package ai OpenAI 兼容端点客户端：把自然语言解析为结构化计划草稿。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Draft AI 解析出的计划草稿。
type Draft struct {
	Title    string `json:"title"`
	Date     string `json:"date,omitempty"` // YYYY-MM-DD；空=让用户确认
	Time     string `json:"time,omitempty"` // HH:MM
	Duration int    `json:"duration,omitempty"`
	Reminder int    `json:"reminder,omitempty"`
	Category string `json:"category,omitempty"`
	Priority string `json:"priority,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

const systemPrompt = `你是日程助手。把用户的自然语言转换为一条计划草稿，只输出 JSON 对象，不要任何其他文字。
字段：title(必填,≤60字)、date(YYYY-MM-DD)、time(HH:MM 24小时)、duration(分钟,默认60)、
reminder(提前分钟,-1/0/5/10/15/30/60/1440,默认15)、category(work/study/life,默认life)、
priority(normal/high)、notes。无法确定日期时 date 留空。`

// Client HTTP 客户端。
type Client struct {
	Endpoint string // base URL，如 https://api.deepseek.com/v1
	Model    string
	APIKey   string
	HTTP     *http.Client
}

// NewClient 创建客户端。
func NewClient(endpoint, model, key string) *Client {
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		Model:    model,
		APIKey:   key,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

type chatReq struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Parse 解析自然语言为草稿。
func (c *Client) Parse(ctx context.Context, text string) (*Draft, string, error) {
	body, err := json.Marshal(chatReq{
		Model: c.Model,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
	})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("请求 AI 端点失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := string(raw)
		var e chatResp
		if json.Unmarshal(raw, &e) == nil && e.Error != nil {
			msg = e.Error.Message
		}
		return nil, "", fmt.Errorf("AI 端点返回 %d: %s", resp.StatusCode, msg)
	}
	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, "", fmt.Errorf("解析 AI 响应失败: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, "", fmt.Errorf("AI 未返回内容")
	}
	content := strings.TrimSpace(cr.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return nil, content, fmt.Errorf("AI 输出不是 JSON")
	}
	var d Draft
	if err := json.Unmarshal([]byte(content[start:end+1]), &d); err != nil {
		return nil, content, fmt.Errorf("解析 AI 草稿失败: %w", err)
	}
	return &d, content, nil
}
