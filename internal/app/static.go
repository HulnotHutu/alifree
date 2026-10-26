package app

import (
	"embed"
	"io/fs"
)

//go:embed static/dashboard.html
var staticFiles embed.FS

// dashboardHTML 返回看板页面（编译期内嵌，单二进制即可分发）。
func dashboardHTML() []byte {
	data, err := fs.ReadFile(staticFiles, "static/dashboard.html")
	if err != nil {
		return []byte("看板页面缺失：" + err.Error())
	}
	return data
}
