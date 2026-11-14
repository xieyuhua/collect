package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"collect/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestStopRunMarksOrphanedRunStopped(t *testing.T) {
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
	if err := db.AutoMigrate(&model.Task{}, &model.RunLog{}, &model.StepLog{}); err != nil {
		t.Fatal(err)
	}

	task := model.Task{Name: "orphaned run", Mode: "api", LastStatus: "running"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	run := model.RunLog{
		TaskID: task.ID, TaskName: task.Name, Mode: task.Mode,
		Status: "running", StartedAt: time.Now().Add(-2 * time.Second),
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	step := model.StepLog{RunID: run.ID, Seq: 1, Name: "请求中", Kind: "http", Status: "running"}
	if err := db.Create(&step).Error; err != nil {
		t.Fatal(err)
	}

	server := &Server{DB: db}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%d/stop", run.ID), nil)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(run.ID)}}
	server.stopRun(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"code":0`) {
		t.Fatalf("response did not succeed: %s", recorder.Body.String())
	}

	var savedRun model.RunLog
	if err := db.First(&savedRun, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if savedRun.Status != "stopped" {
		t.Fatalf("run status = %q, want stopped", savedRun.Status)
	}
	if savedRun.FinishedAt == nil {
		t.Fatal("finished_at was not persisted")
	}
	if savedRun.DurationMs < 0 {
		t.Fatalf("duration_ms = %d, want non-negative", savedRun.DurationMs)
	}
	if savedRun.Error == "" {
		t.Fatal("run error was not persisted")
	}

	var savedTask model.Task
	if err := db.First(&savedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if savedTask.LastStatus != "stopped" {
		t.Fatalf("task last_status = %q, want stopped", savedTask.LastStatus)
	}

	var savedStep model.StepLog
	if err := db.First(&savedStep, step.ID).Error; err != nil {
		t.Fatal(err)
	}
	if savedStep.Status != "stopped" {
		t.Fatalf("step status = %q, want stopped", savedStep.Status)
	}

	changed, err := server.markRunInterrupted(&savedRun)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("already stopped run was changed again")
	}
}
