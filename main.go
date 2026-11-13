package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"collect/internal/api"
	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/collector"
	"collect/internal/config"
	"collect/internal/jobs"
	"collect/internal/notify"
	"collect/internal/scheduler"
	"collect/internal/store"
	"collect/internal/studio"
)

const version = "1.0.0"

func main() {
	addr := flag.String("addr", "", "监听地址，例如 127.0.0.1:8686")
	open := flag.Bool("open", true, "启动后自动打开浏览器")
	flag.Parse()

	cfg := config.Load()
	if *addr != "" {
		cfg.Addr = *addr
	}
	logger := applog.New(config.DataPath("logs"))

	db, err := store.Open()
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	bm := browser.NewManager(cfg, logger)
	nt := notify.New(db, logger)
	engine := collector.NewEngine(cfg, db, logger, bm, nt)
	sm := studio.NewManager(cfg, logger, bm)
	sc := scheduler.New(db, engine, logger)
	jrunner := jobs.NewRunner(cfg, db, logger, nt)
	sc.SetJobRunner(jrunner)

	srv := &api.Server{Cfg: cfg, DB: db, Log: logger, Engine: engine, Studio: sm, Sched: sc, Jobs: jrunner, BM: bm, NT: nt}
	router := srv.Router()

	sc.Start()
	defer sc.Stop()
	defer sm.CloseAll()

	httpSrv := &http.Server{Addr: cfg.Addr, Handler: router}
	go func() {
		logger.Info("main", "采集平台 v%s 已启动: http://%s", version, cfg.Addr)
		fmt.Printf("\n  采集平台 v%s 已启动\n  访问地址: http://%s\n  数据目录: %s\n  平台: %s/%s\n\n", version, cfg.Addr, config.DataPath(), runtime.GOOS, runtime.GOARCH)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("main", "HTTP 服务异常: %v", err)
			log.Fatalf("HTTP 服务启动失败: %v", err)
		}
	}()

	if st := bm.Status(); st["available"] == true {
		logger.Info("browser", "浏览器内核: %v (%v)", st["path"], st["source"])
	} else {
		logger.Warn("browser", "未检测到浏览器内核，可在「系统设置」中下载，或配置系统 Chrome/Edge 路径")
	}

	if *open {
		go func() {
			time.Sleep(600 * time.Millisecond)
			openBrowser("http://" + cfg.Addr)
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	logger.Info("main", "正在关闭平台…")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	logger.Info("main", "平台已退出")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("打开浏览器失败（可手动访问 %s）: %v", url, err)
	}
}
