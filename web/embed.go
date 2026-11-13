package web

import "embed"

// FS 内嵌前端静态资源，无需单独部署
//
//go:embed index.html style.css js vendor
var FS embed.FS
