package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"collect/internal/collector"
	"collect/internal/config"
	"collect/internal/model"

	"github.com/gin-gonic/gin"
)

func (s *Server) listRuns(c *gin.Context) {
	page, size := pageQuery(c)
	q := s.DB.Model(&model.RunLog{})
	if v := c.Query("task_id"); v != "" {
		q = q.Where("task_id = ?", v)
	}
	if v := c.Query("status"); v != "" {
		q = q.Where("status = ?", v)
	}
	if v := c.Query("trigger"); v != "" {
		q = q.Where("trigger = ?", v)
	}
	var total int64
	q.Count(&total)
	var list []model.RunLog
	// 最近的运行记录排在最前（按开始时间倒序，同一时间再按 id 倒序）
	if err := q.Order("started_at desc, id desc").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"total": total, "page": page, "page_size": size, "list": list})
}

func (s *Server) getRun(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var run model.RunLog
	if err := s.DB.First(&run, id).Error; err != nil {
		failMsg(c, "运行记录不存在")
		return
	}
	var task model.Task
	_ = s.DB.First(&task, run.TaskID).Error
	ok(c, gin.H{"run": run, "task": task.ToMap()})
}

func (s *Server) runSteps(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var steps []model.StepLog
	if err := s.DB.Where("run_id = ?", id).Order("seq asc, id asc").Find(&steps).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, steps)
}

func (s *Server) runData(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	limit := intQuery(c, "limit", 50)
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	offset := intQuery(c, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	source := strings.TrimSpace(c.Query("source"))
	q := s.DB.Model(&model.DatasetRow{}).Where("run_id = ?", id)
	switch source {
	case "":
	case "__unlabeled":
		q = q.Where("source = '' OR source IS NULL")
	default:
		q = q.Where("source = ?", source)
	}
	var total int64
	q.Count(&total)
	var rows []model.DatasetRow
	if err := q.Order("seq asc").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}
	records := make([]map[string]any, 0, len(rows))
	headers := []string{}
	seen := map[string]bool{}
	for _, r := range rows {
		var m map[string]any
		if json.Unmarshal([]byte(r.Data), &m) != nil {
			continue
		}
		records = append(records, m)
		for k := range m {
			if !seen[k] {
				seen[k] = true
				headers = append(headers, k)
			}
		}
	}
	ok(c, gin.H{
		"source": source, "total": total, "offset": offset, "limit": limit,
		"headers": orderHeaders(s.runHeaders(id, source), seen), "rows": records,
	})
}

// orderHeaders 先按字段映射顺序输出实际存在的列，其余列按字母序补齐
func orderHeaders(preferred []string, seen map[string]bool) []string {
	out := make([]string, 0, len(seen))
	used := map[string]bool{}
	for _, h := range preferred {
		if seen[h] && !used[h] {
			out = append(out, h)
			used[h] = true
		}
	}
	var rest []string
	for k := range seen {
		if !used[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// runSources 本次运行的数据来源分组（不同接口 / DOM 规则分开预览）
func (s *Server) runSources(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var rows []struct {
		Source string
		Label  string
		Count  int64
		MinSeq int64
	}
	if err := s.DB.Model(&model.DatasetRow{}).
		Select("source AS source, MAX(source_label) AS label, COUNT(*) AS count, MIN(seq) AS min_seq").
		Where("run_id = ?", id).Group("source").Order("min_seq asc").Scan(&rows).Error; err != nil {
		fail(c, err)
		return
	}
	list := make([]gin.H, 0, len(rows))
	var total int64
	for _, r := range rows {
		total += r.Count
		src, label := r.Source, strings.TrimSpace(r.Label)
		if src == "" {
			src = "__unlabeled"
			if label == "" {
				label = "默认数据"
			}
		} else if label == "" {
			label = src
		}
		list = append(list, gin.H{"source": src, "label": label, "count": r.Count})
	}
	ok(c, gin.H{"total": total, "list": list})
}

// runHeaders 字段顺序：优先该接口自己的映射，其次任务映射
func (s *Server) runHeaders(runID uint, source string) []string {
	var run model.RunLog
	if err := s.DB.First(&run, runID).Error; err != nil {
		return nil
	}
	var task model.Task
	if err := s.DB.First(&task, run.TaskID).Error; err != nil {
		return nil
	}
	mapping := task.MappingList()
	if strings.HasPrefix(source, "step:") {
		if idx, err := strconv.Atoi(strings.TrimPrefix(source, "step:")); err == nil {
			steps := task.HTTPStepList()
			if idx >= 1 && idx <= len(steps) && len(steps[idx-1].Mapping) > 0 {
				mapping = steps[idx-1].Mapping
			}
		}
	}
	var headers []string
	for _, m := range mapping {
		if strings.TrimSpace(m.Target) != "" {
			headers = append(headers, m.Target)
		}
	}
	return headers
}

func (s *Server) exportRun(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var run model.RunLog
	if err := s.DB.First(&run, id).Error; err != nil {
		failMsg(c, "运行记录不存在")
		return
	}
	format := strings.ToLower(c.DefaultQuery("format", "xlsx"))
	source := strings.TrimSpace(c.Query("source"))
	headers := s.runHeaders(id, source)
	dir := config.DataPath("exports", fmt.Sprintf("%d", run.TaskID))
	base := collector.SanitizeFileName(fmt.Sprintf("%s_run%d", run.TaskName, run.ID))
	if base == "" {
		base = fmt.Sprintf("run%d", run.ID)
	}
	if source != "" {
		base += "_" + collector.SanitizeFileName(strings.ReplaceAll(source, ":", "-"))
	}
	var path string
	switch format {
	case "csv":
		path = filepath.Join(dir, base+".csv")
		if err := collector.ExportRunCSV(s.DB, id, source, headers, path); err != nil {
			fail(c, err)
			return
		}
	case "json":
		rows, heads, err := collector.RunRowsSource(s.DB, id, source, headers)
		if err != nil {
			fail(c, err)
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"; filename*=UTF-8''%s.json`, base, base))
		c.JSON(200, gin.H{"run": run, "headers": heads, "rows": rows})
		return
	default:
		path = filepath.Join(dir, base+".xlsx")
		if err := collector.ExportRunXLSX(s.DB, id, source, headers, path); err != nil {
			fail(c, err)
			return
		}
	}
	s.DB.Model(&run).Update("export_path", path)
	c.File(path)
}

func (s *Server) deleteRun(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var run model.RunLog
	if err := s.DB.First(&run, id).Error; err != nil {
		failMsg(c, "运行记录不存在")
		return
	}
	if run.Status == "running" {
		failMsg(c, "运行中的记录不能删除")
		return
	}
	s.purgeRuns([]model.RunLog{run})
	okMsg(c, "已删除", nil)
}

// purgeRuns 删除运行记录及其步骤日志、数据集行、导出文件。
// 分批 IN 查询，避免 SQLite 变量上限；单条失败不影响其它记录。
func (s *Server) purgeRuns(runs []model.RunLog) int {
	if len(runs) == 0 {
		return 0
	}
	const chunk = 200
	ids := make([]uint, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	for i := 0; i < len(ids); i += chunk {
		end := i + chunk
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		s.DB.Where("run_id IN ?", batch).Delete(&model.StepLog{})
		s.DB.Where("run_id IN ?", batch).Delete(&model.DatasetRow{})
		s.DB.Where("id IN ?", batch).Delete(&model.RunLog{})
	}
	// 导出文件一并清理，避免磁盘残留
	for _, r := range runs {
		if p := strings.TrimSpace(r.ExportPath); p != "" {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				_ = os.Remove(p)
			}
		}
	}
	return len(runs)
}

// deleteRunsBatch 批量删除运行记录。
//
// 三种用法（按优先级）：
//  1. ids 非空：精确删除这些运行记录
//  2. all=true：删除当前筛选条件（task_id/status/trigger）下的全部记录
//  3. keep=N：在当前筛选条件下只保留最近 N 条
//
// 运行中（running）的记录一律跳过。
func (s *Server) deleteRunsBatch(c *gin.Context) {
	var req struct {
		IDs     []uint `json:"ids"`
		TaskID  uint   `json:"task_id"`
		Status  string `json:"status"`
		Trigger string `json:"trigger"`
		All     bool   `json:"all"`
		Keep    int    `json:"keep"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数格式错误")
		return
	}
	if len(req.IDs) == 0 && !req.All && req.Keep <= 0 {
		failMsg(c, "请选择要删除的运行记录")
		return
	}
	if req.Keep < 0 {
		failMsg(c, "保留条数不能为负数")
		return
	}
	var targets []model.RunLog
	if len(req.IDs) > 0 {
		if len(req.IDs) > 5000 {
			failMsg(c, "一次最多删除 5000 条记录")
			return
		}
		if err := s.DB.Where("id IN ?", req.IDs).Find(&targets).Error; err != nil {
			fail(c, err)
			return
		}
	} else {
		q := s.DB.Model(&model.RunLog{})
		if req.TaskID > 0 {
			q = q.Where("task_id = ?", req.TaskID)
		}
		if v := strings.TrimSpace(req.Status); v != "" {
			q = q.Where("status = ?", v)
		}
		if v := strings.TrimSpace(req.Trigger); v != "" {
			q = q.Where("trigger = ?", v)
		}
		if req.Keep > 0 {
			// 保留最近 N 条：先取出要保留的 id，再删除其余
			var keepIDs []uint
			if err := q.Order("started_at desc, id desc").Limit(req.Keep).
				Pluck("id", &keepIDs).Error; err != nil {
				fail(c, err)
				return
			}
			dq := s.DB.Model(&model.RunLog{})
			if req.TaskID > 0 {
				dq = dq.Where("task_id = ?", req.TaskID)
			}
			if v := strings.TrimSpace(req.Status); v != "" {
				dq = dq.Where("status = ?", v)
			}
			if v := strings.TrimSpace(req.Trigger); v != "" {
				dq = dq.Where("trigger = ?", v)
			}
			if len(keepIDs) > 0 {
				dq = dq.Where("id NOT IN ?", keepIDs)
			}
			if err := dq.Where("status <> ?", "running").Find(&targets).Error; err != nil {
				fail(c, err)
				return
			}
		} else {
			if err := q.Where("status <> ?", "running").Find(&targets).Error; err != nil {
				fail(c, err)
				return
			}
		}
	}
	kept := make([]model.RunLog, 0, len(targets))
	running := 0
	for _, r := range targets {
		if r.Status == "running" {
			running++
			continue
		}
		kept = append(kept, r)
	}
	deleted := s.purgeRuns(kept)
	msg := fmt.Sprintf("已删除 %d 条运行记录", deleted)
	if running > 0 {
		msg += fmt.Sprintf("，跳过 %d 条运行中的记录", running)
	}
	okMsg(c, msg, gin.H{"deleted": deleted, "skipped": running})
}

func (s *Server) runDataExportPreview(c *gin.Context) {
	// 保留占位：前端通过 /api/runs/:id/export 导出
	failMsg(c, "请使用 /api/runs/:id/export")
}
