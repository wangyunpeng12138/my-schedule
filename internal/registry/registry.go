// Package registry 命令注册表：module.method → handler，MCP tools/call 直接对齐。
package registry

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Handler 命令处理器。
type Handler func(params json.RawMessage) (any, error)

// Tool 命令的元信息（导出 MCP tools 用）。
type Tool struct {
	Module      string
	Method      string
	Description string
	Params      string // 参数示例 JSON
}

// Registry 注册表。
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	tools    map[string]Tool
	order    []string
}

// New 创建注册表。
func New() *Registry {
	return &Registry{handlers: map[string]Handler{}, tools: map[string]Tool{}}
}

// Register 注册一个命令（重复注册覆盖）。
func (r *Registry) Register(module, method, description, paramsExample string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := module + "." + method
	r.handlers[key] = h
	if _, ok := r.tools[key]; !ok {
		r.order = append(r.order, key)
	}
	r.tools[key] = Tool{Module: module, Method: method, Description: description, Params: paramsExample}
}

// Invoke 调用一个命令。
func (r *Registry) Invoke(method string, params json.RawMessage) (any, error) {
	r.mu.RLock()
	h, ok := r.handlers[method]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown_method: %s", method)
	}
	return h(params)
}

// Tools 全部命令元信息（注册序）。
func (r *Registry) Tools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.tools[k])
	}
	return out
}

// ExportMCPTools 导出为 MCP tools/list 的 tools 数组结构。
func (r *Registry) ExportMCPTools() []map[string]any {
	out := []map[string]any{}
	for _, t := range r.Tools() {
		schema := map[string]any{"type": "object", "additionalProperties": true}
		if t.Params != "" {
			var p map[string]any
			if err := json.Unmarshal([]byte(t.Params), &p); err == nil {
				props := map[string]any{}
				for k := range p {
					props[k] = map[string]any{"type": "string"}
				}
				schema["properties"] = props
			}
		}
		out = append(out, map[string]any{
			"name":        t.Module + "." + t.Method,
			"description": t.Description,
			"inputSchema": schema,
		})
	}
	return out
}
