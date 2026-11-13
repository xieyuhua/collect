package api

import (
	"net/http"
	"strconv"

	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/collector"
	"collect/internal/config"
	"collect/internal/jobs"
	"collect/internal/notify"
	"collect/internal/scheduler"
	"collect/internal/studio"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Server struct {
	Cfg    *config.Config
	DB     *gorm.DB
	Log    *applog.Logger
	Engine *collector.Engine
	Studio *studio.Manager
	Sched  *scheduler.Scheduler
	Jobs   *jobs.Runner
	BM     *browser.Manager
	NT     *notify.Notifier
}

func New(s *Server) *Server { return s }

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data})
}

func okMsg(c *gin.Context, msg string, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msg, "data": data})
}

func fail(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": err.Error(), "data": nil})
}

func failMsg(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": msg, "data": nil})
}

func idParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		failMsg(c, "参数 id 不合法")
		return 0, false
	}
	return uint(id), true
}

func intQuery(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func pageQuery(c *gin.Context) (page, size int) {
	page = intQuery(c, "page", 1)
	size = intQuery(c, "page_size", 20)
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 500 {
		size = 500
	}
	return
}
