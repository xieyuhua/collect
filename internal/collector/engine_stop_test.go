package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/config"
	"collect/internal/model"
	"collect/internal/notify"
	"collect/internal/store"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestStopPersistsStoppedRunAndStep(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Task{}, &model.RunLog{}, &model.StepLog{}, &model.Notification{}); err != nil {
		t.Fatal(err)
	}
	store.DB = db

	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	steps, err := json.Marshal([]model.HTTPStep{{
		Name: "slow request", Method: http.MethodGet, URL: server.URL,
	}})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		Name: "stop test", Mode: "api", Enabled: true,
		TimeoutSec: 30, HTTPSteps: string(steps),
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{MaxBodyKB: 256, TimeoutSec: 30}
	log := applog.New(os.DevNull)
	engine := NewEngine(cfg, db, log, browser.NewManager(cfg, log), notify.New(db, log))
	run, err := engine.Start(task.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request did not start")
	}
	if !engine.Stop(run.ID) {
		t.Fatal("Stop did not find the active run")
	}

	var saved model.RunLog
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.First(&saved, run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if saved.Status != "stopped" {
		t.Fatalf("run status = %q, want stopped", saved.Status)
	}
	if saved.FinishedAt == nil {
		t.Fatal("finished_at was not persisted")
	}
	if engine.Stop(run.ID) {
		t.Fatal("Stop reported an already completed run as active")
	}

	var step model.StepLog
	if err := db.Where("run_id = ?", run.ID).First(&step).Error; err != nil {
		t.Fatal(err)
	}
	if step.Status != "stopped" {
		t.Fatalf("step status = %q, want stopped", step.Status)
	}
}
