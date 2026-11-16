// Package webui 嵌入 React 前端的构建产物（web-ui/dist），
// 供 quota serve 以单个二进制直接托管 SPA。
//
// 构建顺序：先在 web-ui/ 运行 npm run build 生成 dist，再编译 Go
// （dist 不入库，见 .gitignore；缺失时 go build 会报 embed 错误提醒）。
// 开发期无需本包参与：Vite dev server 把 /api 代理到 quota serve。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:web-ui/dist
var embedded embed.FS

// Dist 返回以 web-ui/dist 为根的静态文件系统（index.html + assets/*）。
// 更新嵌入内容：先 make web 重建前端，再触发任意 .go 改动让 air 重编译（或直接 make build）。
func Dist() (fs.FS, error) {
	return fs.Sub(embedded, "web-ui/dist")
}
