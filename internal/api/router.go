package api

import (
	"io/fs"
	"net/http"
	"strings"

	"collect/web"

	"github.com/gin-gonic/gin"
)

func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		s.Log.Error("api", "接口异常: %v", err)
		failMsg(c, "服务内部错误")
	}))
	r.Use(func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/logs") {
			s.Log.Debug("http", "%s %s %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())
		}
		c.Next()
	})

	api := r.Group("/api")
	api.GET("/health", s.health)
	api.GET("/dashboard", s.dashboard)
	api.GET("/logs", s.logs)

	tasks := api.Group("/tasks")
	{
		tasks.GET("", s.listTasks)
		tasks.POST("", s.createTask)
		tasks.GET("/options", s.taskOptions)
		tasks.POST("/import", s.importTasks)
		tasks.GET("/export", s.exportAllTasks)
		tasks.GET("/:id", s.getTask)
		tasks.PUT("/:id", s.updateTask)
		tasks.DELETE("/:id", s.deleteTask)
		tasks.POST("/:id/run", s.runTask)
		tasks.POST("/:id/clone", s.cloneTask)
		tasks.GET("/:id/runs", s.taskRuns)
		tasks.GET("/:id/export", s.exportTask)
	}

	runs := api.Group("/runs")
	{
		runs.GET("", s.listRuns)
		runs.POST("/batch-delete", s.deleteRunsBatch)
		runs.GET("/:id", s.getRun)
		runs.POST("/:id/stop", s.stopRun)
		runs.DELETE("/:id", s.deleteRun)
		runs.GET("/:id/steps", s.runSteps)
		runs.GET("/:id/sources", s.runSources)
		runs.GET("/:id/data", s.runData)
		runs.GET("/:id/export", s.exportRun)
	}

	st := api.Group("/studio")
	{
		st.POST("/sessions", s.studioStart)
		st.GET("/sessions", s.studioList)
		st.GET("/sessions/:id/capture", s.studioCapture)
		st.GET("/sessions/:id/requests/:reqId", s.studioRequest)
		st.POST("/sessions/:id/stop", s.studioStop)
		st.DELETE("/sessions/:id", s.studioRemove)
		st.POST("/sessions/:id/draft", s.studioDraft)
	}

	ds := api.Group("/datasources")
	{
		ds.GET("", s.listDataSources)
		ds.POST("", s.saveDataSource)
		ds.PUT("/:id", s.saveDataSource)
		ds.DELETE("/:id", s.deleteDataSource)
		ds.POST("/test", s.testDataSourceBody)
		ds.POST("/:id/test", s.testDataSource)
		ds.POST("/:id/exec", s.execDataSource)
	}

	jobsGroup := api.Group("/jobs")
	{
		jobsGroup.GET("", s.listJobs)
		jobsGroup.POST("", s.createJob)
		jobsGroup.GET("/:id", s.getJob)
		jobsGroup.PUT("/:id", s.updateJob)
		jobsGroup.DELETE("/:id", s.deleteJob)
		jobsGroup.POST("/:id/run", s.runJob)
		jobsGroup.POST("/:id/clone", s.cloneJob)
		jobsGroup.GET("/:id/runs", s.jobRuns)
	}

	jobRuns := api.Group("/job-runs")
	{
		jobRuns.GET("", s.listJobRuns)
		jobRuns.GET("/:id", s.getJobRun)
		jobRuns.DELETE("/:id", s.deleteJobRun)
	}

	api.GET("/settings", s.getSettings)
	api.PUT("/settings", s.updateSettings)
	api.POST("/settings/test-wecom", s.testWecom)
	api.PUT("/config/general", s.generalConfig)
	api.GET("/browser/status", s.browserStatus)
	api.POST("/browser/download", s.browserDownload)
	api.PUT("/browser/config", s.browserConfig)
	api.POST("/browser/session/clear", s.browserSessionClear)
	api.POST("/browser/probe-ua", s.browserProbeUA)

	api.GET("/notifications", s.listNotifications)
	api.POST("/notifications/read-all", s.readAllNotifications)
	api.DELETE("/notifications", s.clearNotifications)
	api.POST("/notifications/:id/read", s.readNotification)
	api.DELETE("/notifications/:id", s.deleteNotification)

	// 静态资源
	sub, err := fs.Sub(web.FS, ".")
	if err == nil {
		fileServer := http.FileServer(http.FS(sub))
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			if strings.HasPrefix(p, "/api/") {
				c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": "接口不存在"})
				return
			}
			if p == "/" || p == "" || p == "/index.html" {
				// 直接输出 index.html：交给 http.FileServer 会因为 /index.html -> ./ 重定向造成死循环
				data, err := fs.ReadFile(sub, "index.html")
				if err != nil {
					c.String(http.StatusInternalServerError, "index.html 缺失")
					return
				}
				c.Header("Cache-Control", "no-cache, must-revalidate")
				c.Data(http.StatusOK, "text/html; charset=utf-8", data)
				return
			}
			// 内嵌资源没有修改时间，浏览器可能一直用旧缓存（升级后看不到新页面）。
			// 页面与自己的 js/css 每次校验；第三方库（vendor）可以长期缓存。
			if strings.HasPrefix(p, "/vendor/") {
				c.Header("Cache-Control", "public, max-age=604800")
			} else {
				c.Header("Cache-Control", "no-cache, must-revalidate")
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}
	return r
}
