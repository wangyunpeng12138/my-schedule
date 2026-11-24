package app

import (
	"context"
	"encoding/json"
)

// Eval 调用一个注册命令（CLI --eval 与未来 MCP 的公共入口）。
func (a *App) Eval(method string, params json.RawMessage) (any, error) {
	return a.reg.Invoke(method, params)
}

// ctx 供 AI 等外呼请求使用的上下文。
func (a *App) ctx() context.Context {
	return context.Background()
}
