package app

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFiles embed.FS

// dashboardHTML 返回看板页面。
func dashboardHTML() []byte {
	data, err := fs.ReadFile(staticFiles, "static/dashboard.html")
	if err != nil {
		return []byte("看板页面缺失：" + err.Error())
	}
	return data
}

// echartsJS 返回内嵌的 ECharts 库（vendor 进二进制，离线可用）。
func echartsJS() []byte {
	data, err := fs.ReadFile(staticFiles, "static/echarts.min.js")
	if err != nil {
		return []byte("/* echarts.min.js 缺失：" + err.Error() + " */")
	}
	return data
}
