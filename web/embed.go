// Package web 内嵌前端资源（index.html / style.css / app.js）。
package web

import "embed"

//go:embed index.html style.css app.js
var FS embed.FS

// Files 资源文件名列表（http 服务用）。
var Files = []string{"index.html", "style.css", "app.js"}
