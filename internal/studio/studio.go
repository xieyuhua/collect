package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"collect/internal/applog"
	"collect/internal/browser"
	"collect/internal/config"
	"collect/internal/jsonx"
	"collect/internal/model"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
)

type Session struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	StartURL  string    `json:"start_url"`
	CreatedAt time.Time `json:"created_at"`
	StoppedAt time.Time `json:"stopped_at"`
	Status    string    `json:"status"`
	Error     string    `json:"error"`
	Page      *rod.Page `json:"-"`

	browser *rod.Browser
	rec     *browser.Recorder
	cleanup func()
}

func (s *Session) Info() map[string]any {
	reqs := 0
	jsonReqs := 0
	acts := 0
	if s.rec != nil {
		for _, r := range s.rec.Requests() {
			reqs++
			if r.IsJSON {
				jsonReqs++
			}
		}
		acts = len(s.rec.Actions())
	}
	end := time.Now()
	if !s.StoppedAt.IsZero() {
		end = s.StoppedAt
	}
	var durationMs int64
	if !s.CreatedAt.IsZero() {
		durationMs = end.Sub(s.CreatedAt).Milliseconds()
		if durationMs < 0 {
			durationMs = 0
		}
	}
	var stoppedAt any
	if !s.StoppedAt.IsZero() {
		stoppedAt = s.StoppedAt
	}
	return map[string]any{
		"id": s.ID, "mode": s.Mode, "start_url": s.StartURL, "created_at": s.CreatedAt,
		"started_at": s.CreatedAt, "stopped_at": stoppedAt, "duration_ms": durationMs,
		"status": s.Status, "error": s.Error, "requests": reqs, "json_requests": jsonReqs,
		"actions": acts,
	}
}

type Manager struct {
	cfg *config.Config
	log *applog.Logger
	bm  *browser.Manager

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager(cfg *config.Config, log *applog.Logger, bm *browser.Manager) *Manager {
	return &Manager{cfg: cfg, log: log, bm: bm, sessions: map[string]*Session{}}
}

func (m *Manager) Start(rawURL, mode string) (*Session, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("请填写要打开的网址")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	if mode != "ui" && mode != "hybrid" && mode != "api" {
		mode = "api"
	}
	b, cleanup, err := m.bm.Launch(false, false)
	if err != nil {
		return nil, err
	}
	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建页面失败: %w", err)
	}
	m.bm.ApplyFingerprint(page)
	rec := browser.NewRecorder(page, m.cfg.MaxBodyKB)
	if err := rec.Start(); err != nil {
		cleanup()
		return nil, err
	}
	s := &Session{
		ID: uuid.NewString(), Mode: mode, StartURL: rawURL, CreatedAt: time.Now(),
		Status: "recording", Page: page, browser: b, rec: rec, cleanup: cleanup,
	}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	go func() {
		if err := page.Timeout(90 * time.Second).Navigate(rawURL); err != nil {
			s.Error = err.Error()
			m.log.Warn("studio", "打开页面失败: %v", err)
			return
		}
		_ = page.Timeout(30 * time.Second).WaitLoad()
	}()
	m.log.Info("studio", "开始录制会话 %s，模式 %s，地址 %s", shortID(s.ID), mode, rawURL)
	return s, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) List() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	return out
}

// Stop 结束录制并关闭浏览器，录制数据保留用于生成任务
func (m *Manager) Stop(id string) error {
	s := m.Get(id)
	if s == nil {
		return errors.New("会话不存在")
	}
	if s.StoppedAt.IsZero() {
		s.StoppedAt = time.Now()
	}
	if s.rec != nil {
		s.rec.Stop()
	}
	if s.cleanup != nil {
		s.cleanup()
		s.cleanup = nil
	}
	s.Status = "stopped"
	m.log.Info("studio", "录制会话 %s 已停止", shortID(id))
	return nil
}

func (m *Manager) Remove(id string) {
	s := m.Get(id)
	if s == nil {
		return
	}
	if s.Status == "recording" {
		_ = m.Stop(id)
	}
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		_ = m.Stop(s.ID)
	}
}

// Capture 返回会话捕获的请求摘要与页面动作
func (m *Manager) Capture(id string) (map[string]any, error) {
	s := m.Get(id)
	if s == nil {
		return nil, errors.New("会话不存在")
	}
	reqs := s.rec.Requests()
	list := make([]map[string]any, 0, len(reqs))
	for _, r := range reqs {
		list = append(list, map[string]any{
			"id": r.ID, "seq": r.Seq, "method": r.Method, "url": r.URL,
			"resource_type": r.ResourceType, "status": r.Status, "size": r.Size,
			"duration_ms": r.DurationMs, "is_json": r.IsJSON, "mime_type": r.MimeType,
			"failed": r.Failed, "error": r.Error, "started_at": r.StartedAt,
		})
	}
	return map[string]any{
		"session":     s.Info(),
		"requests":    list,
		"actions":     s.rec.Actions(),
		"diagnostics": s.rec.Diagnostics(),
		"candidates":  dataCandidates(reqs),
	}, nil
}

// dataCandidates 返回"看起来是数据列表"的接口，供草稿多选
func dataCandidates(reqs []browser.CapRequest) []map[string]any {
	out := make([]map[string]any, 0, 4)
	for _, r := range reqs {
		if !r.IsJSON || r.Status < 200 || r.Status >= 400 {
			continue
		}
		path, _, first := detectRecords(r.RespBody)
		if path == "" || first == nil {
			continue
		}
		rows := 0
		var v any
		if json.Unmarshal([]byte(r.RespBody), &v) == nil {
			if arr, ok := jsonx.Lookup(v, path); ok {
				if list, ok2 := arr.([]any); ok2 {
					rows = len(list)
				}
			}
		}
		out = append(out, map[string]any{
			"id": r.ID, "seq": r.Seq, "method": r.Method, "url": r.URL,
			"records_path": path, "rows": rows, "fields": preferredKeys(first),
			"name": ruleName(r.URL),
		})
	}
	return out
}

func (m *Manager) RequestDetail(id, reqID string) (*browser.CapRequest, error) {
	s := m.Get(id)
	if s == nil {
		return nil, errors.New("会话不存在")
	}
	for _, r := range s.rec.Requests() {
		if r.ID == reqID {
			return &r, nil
		}
	}
	return nil, errors.New("请求不存在")
}

// DraftRequest 生成任务草稿的入参
type DraftRequest struct {
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	LoginReqID string `json:"login_req_id"`
	ListReqID  string `json:"list_req_id"`
	// ListReqIDs 多接口：一次录制里选择多个数据接口，生成多个采集步骤/抓取规则
	ListReqIDs []string `json:"list_req_ids"`
	StartURL   string   `json:"start_url"`
}

// BuildDraft 根据录制结果生成任务草稿
func (m *Manager) BuildDraft(id string, req DraftRequest) (map[string]any, error) {
	s := m.Get(id)
	if s == nil {
		return nil, errors.New("会话不存在")
	}
	return buildDraftFrom(s, req, s.rec.Requests(), s.rec.Actions())
}

// buildDraftFrom 根据录制到的请求与动作生成任务草稿（拆分出来便于测试与复用）
func buildDraftFrom(s *Session, req DraftRequest, requests []browser.CapRequest, actions []browser.Action) (map[string]any, error) {
	if req.Mode == "" {
		req.Mode = s.Mode
	}
	if len(requests) == 0 && req.Mode != "ui" {
		return nil, errors.New("没有捕获到任何请求，请先在浏览器中完成操作后再生成")
	}
	var loginReq *browser.CapRequest
	byID := make(map[string]*browser.CapRequest, len(requests))
	for i := range requests {
		r := &requests[i]
		byID[r.ID] = r
		if r.ID == req.LoginReqID {
			loginReq = r
		}
	}
	// 支持一次生成多个数据接口：list_req_ids 优先，兼容旧的 list_req_id，
	// 都没有时自动挑选最像数据列表的一个
	var listReqs []*browser.CapRequest
	seenIDs := map[string]bool{}
	addList := func(r *browser.CapRequest) {
		if r == nil || seenIDs[r.ID] {
			return
		}
		seenIDs[r.ID] = true
		listReqs = append(listReqs, r)
	}
	for _, id := range req.ListReqIDs {
		addList(byID[id])
	}
	addList(byID[req.ListReqID])
	if len(listReqs) == 0 {
		addList(bestDataRequest(requests))
	}
	if len(listReqs) == 0 && req.Mode != "ui" {
		return nil, errors.New("请选择包含数据列表的接口请求")
	}
	var notes []string
	var listReq *browser.CapRequest
	if len(listReqs) > 0 {
		listReq = listReqs[0]
	}
	if req.Mode == "api" && loginReq == nil {
		loginReq = guessLoginRequest(requests, listReq)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultName(s.StartURL, listReq)
	}
	task := map[string]any{
		"name":          name,
		"mode":          req.Mode,
		"remark":        "由录制会话生成",
		"enabled":       true,
		"notify":        true,
		"notify_on":     "failure",
		"schedule_type": "manual",
		"cron":          "",
		"interval_sec":  0,
		"timeout_sec":   60,
		"vars":          []model.KV{},
		"http_steps":    []model.HTTPStep{},
		"ui_steps":      []model.UIStep{},
		"login_steps":   []model.UIStep{},
		"capture":       model.CaptureConfig{Mode: "network"},
		"pagination":    model.Pagination{},
		"mapping":       []model.FieldMapping{},
		"sink":          model.SinkConfig{Type: "table", ExportFormat: "xlsx", BatchSize: 200, WriteMode: "insert", AutoCreate: true},
		"dedupe":        model.DedupeConfig{},
	}

	var httpSteps []model.HTTPStep
	var tokenRule *model.ExtractRule
	var tokenValue string
	if loginReq != nil {
		tokenRule, tokenValue = detectToken(loginReq)
	}

	if req.Mode == "api" || req.Mode == "hybrid" {
		if req.Mode == "api" && loginReq != nil {
			st := stepFromRequest(loginReq, tokenRule, tokenValue)
			st.Name = "登录"
			httpSteps = append(httpSteps, st)
		}
		multi := len(listReqs) > 1
		for idx, lr := range listReqs {
			st := stepFromRequest(lr, nil, tokenValue)
			if multi {
				st.Name = fmt.Sprintf("数据列表%d · %s", idx+1, ruleName(lr.URL))
			} else {
				st.Name = "数据列表"
			}
			st.Capture = true
			recPath, totalPath, first := detectRecords(lr.RespBody)
			st.RecordsPath = recPath
			st.TotalPath = totalPath
			var mapping []model.FieldMapping
			if first != nil {
				for _, k := range preferredKeys(first) {
					mapping = append(mapping, model.FieldMapping{Source: k, Target: k, Type: guessType(first[k])})
				}
			}
			pg := detectPagination(lr)
			pg.RecordsPath = recPath
			pg.TotalPath = totalPath
			if multi {
				// 多接口时每个接口独立分页与字段映射，互不覆盖；
				// 未识别到分页的接口也显式关闭，避免误用其它接口的分页配置
				st.Paginate = pg.Enabled
				st.Pagination = &pg
				if len(mapping) > 0 {
					st.Mapping = mapping
				}
			}
			if idx == 0 {
				if len(mapping) > 0 {
					task["mapping"] = mapping
					if key := primaryKey(mapping); key != "" {
						task["dedupe"] = model.DedupeConfig{Enabled: true, Keys: []string{key}}
					}
				}
				if pg.Enabled {
					task["pagination"] = pg
				}
			}
			httpSteps = append(httpSteps, st)
		}
		if len(listReqs) > 0 {
			firstRecPath, _, _ := detectRecords(listReqs[0].RespBody)
			task["capture"] = model.CaptureConfig{
				Mode: "network", URLContains: stripQuery(listReqs[0].URL), Method: listReqs[0].Method,
				RecordsPath: firstRecPath, WaitMs: 3000,
			}
		}
		task["http_steps"] = httpSteps
	}

	if req.Mode == "ui" || req.Mode == "hybrid" {
		startURL := s.StartURL
		if req.StartURL != "" {
			startURL = req.StartURL
		}
		base := []model.UIStep{{Action: "open", Value: startURL, Desc: "打开首页"}}
		lastOpen := normURL(startURL)
		var limit int64
		if req.Mode == "hybrid" && listReq != nil {
			limit = listReq.StartedAt.Add(-2 * time.Second).UnixMilli()
		}
		for _, a := range actions {
			if limit > 0 && a.At > limit {
				continue
			}
			st, ok := actionToStep(a)
			if !ok {
				continue
			}
			if st.Action == "open" {
				// 录制过程中录到的页面跳转（例如点菜单进到内嵌列表页）必须保留，
				// 否则回放时会在错误的页面上找元素。与上一步地址相同的跳转直接跳过，
				// 避免生成「打开首页 → 打开首页」这种重复步骤。
				u := normURL(st.Value)
				if u == "" || u == lastOpen {
					continue
				}
				lastOpen = u
				st.Desc = "打开 " + truncate(st.Value, 50)
			}
			// 自定义下拉：录制会同时产出「click 触发器」和「select(trigger=触发器)」，
			// 回放时 select 会自己点开触发器，保留相邻 click 会把弹层 toggle 回去。
			if st.Action == "select" && strings.TrimSpace(st.Trigger) != "" {
				for j := len(base) - 1; j >= 0 && len(base)-j <= 2; j-- {
					prev := base[j]
					if prev.Action == "click" &&
						strings.EqualFold(strings.TrimSpace(prev.Selector), strings.TrimSpace(st.Trigger)) {
						base = append(base[:j], base[j+1:]...)
						break
					}
				}
			}
			base = append(base, st)
		}
		if req.Mode == "ui" {
			task["ui_steps"] = base
		} else {
			task["login_steps"] = base
		}
		if len(base) <= 1 && len(actions) > 0 {
			notes = append(notes, fmt.Sprintf(
				"录到了 %d 个页面动作，但都发生在首页上且无法转成步骤，请检查录制时是否真的操作了页面元素", len(actions)))
		}
	} else if len(actions) > 0 {
		notes = append(notes, fmt.Sprintf(
			"当前是「纯接口」模式，不会生成 UI 步骤，已忽略 %d 个页面动作；如需回放页面操作请把模式改成「纯 UI」或「UI + 接口」",
			len(actions)))
	}
	// 纯 UI 模式：把选中的数据接口一次性生成为多条抓取规则，
	// 一次回放即可同时采集多个接口
	if req.Mode == "ui" && len(listReqs) > 0 {
		rules := make([]model.CaptureConfig, 0, len(listReqs))
		for i, lr := range listReqs {
			recPath, _, first := detectRecords(lr.RespBody)
			rule := model.CaptureConfig{
				Name:        fmt.Sprintf("接口%d · %s", i+1, ruleName(lr.URL)),
				Mode:        "network",
				URLContains: stripQuery(lr.URL),
				Method:      lr.Method,
				RecordsPath: recPath,
				WaitMs:      3000,
			}
			// 识别到分页参数时，生成「按接口分页抓取」的默认配置，
			// 回放结束后会带登录会话逐页请求该接口并入库
			pg := detectPagination(lr)
			pg.RecordsPath = recPath
			if pg.Enabled {
				if pg.StartPage <= 0 {
					pg.StartPage = 1
				}
				rule.Pagination = &pg
			}
			if first != nil {
				for _, k := range preferredKeys(first) {
					rule.Mapping = append(rule.Mapping, model.FieldMapping{Source: k, Target: k, Type: guessType(first[k])})
				}
			}
			rules = append(rules, rule)
		}
		cfg := model.CaptureConfig{Mode: "network", WaitMs: 3000, Rules: rules}
		cfg.URLContains = rules[0].URLContains
		cfg.Method = rules[0].Method
		cfg.RecordsPath = rules[0].RecordsPath
		task["capture"] = cfg
		if len(rules[0].Mapping) > 0 {
			task["mapping"] = rules[0].Mapping
			if key := primaryKey(rules[0].Mapping); key != "" {
				task["dedupe"] = model.DedupeConfig{Enabled: true, Keys: []string{key}}
			}
		}
	}
	// 生成过程中的提示（前端只用来提示，不会写进任务表）
	if len(notes) > 0 {
		task["_notes"] = notes
	}
	return task, nil
}

func bestDataRequest(reqs []browser.CapRequest) *browser.CapRequest {
	var best *browser.CapRequest
	bestScore := 0
	for i := range reqs {
		r := &reqs[i]
		if !r.IsJSON || r.Status < 200 || r.Status >= 400 || len(r.RespBody) < 20 {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(r.RespBody), &v); err != nil {
			continue
		}
		for _, hit := range jsonx.Arrays(v) {
			score := hit.Count
			low := strings.ToLower(hit.Path)
			if strings.Contains(low, "data") || strings.Contains(low, "list") || strings.Contains(low, "rows") || strings.Contains(low, "items") || strings.Contains(low, "records") {
				score *= 3
			}
			u := strings.ToLower(r.URL)
			if strings.Contains(u, "list") || strings.Contains(u, "page") || strings.Contains(u, "query") {
				score += 20
			}
			if score > bestScore {
				bestScore = score
				best = r
			}
		}
	}
	return best
}

func guessLoginRequest(reqs []browser.CapRequest, list *browser.CapRequest) *browser.CapRequest {
	var best *browser.CapRequest
	for i := range reqs {
		r := &reqs[i]
		if list != nil && r.ID == list.ID {
			continue
		}
		if strings.EqualFold(r.Method, "GET") {
			continue
		}
		if r.Status < 200 || r.Status >= 400 {
			continue
		}
		low := strings.ToLower(r.URL)
		if strings.Contains(low, "login") || strings.Contains(low, "signin") || strings.Contains(low, "sign-in") ||
			strings.Contains(low, "auth") || strings.Contains(low, "token") || strings.Contains(low, "session") || strings.Contains(low, "passport") {
			return r
		}
		if best == nil && list != nil && r.StartedAt.Before(list.StartedAt) {
			best = r
		}
	}
	return best
}

func stepFromRequest(r *browser.CapRequest, extract *model.ExtractRule, tokenValue string) model.HTTPStep {
	headers := []model.KV{}
	contentType := ""
	for k, v := range r.ReqHeaders {
		lk := strings.ToLower(k)
		switch lk {
		case "host", "content-length", "connection", "accept-encoding", "cookie",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform":
			continue
		case "content-type":
			contentType = v
		}
		if tokenValue != "" {
			v = strings.ReplaceAll(v, tokenValue, "{{token}}")
		}
		headers = append(headers, model.KV{Key: k, Value: v})
	}
	u := r.URL
	body := r.ReqBody
	if tokenValue != "" {
		u = strings.ReplaceAll(u, tokenValue, "{{token}}")
		body = strings.ReplaceAll(body, tokenValue, "{{token}}")
	}
	st := model.HTTPStep{
		Method:      r.Method,
		URL:         u,
		Headers:     headers,
		Body:        body,
		ContentType: contentType,
	}
	if extract != nil {
		st.Extract = []model.ExtractRule{*extract}
	}
	return st
}

// detectToken 在登录响应中查找 token，并生成提取规则
func detectToken(r *browser.CapRequest) (*model.ExtractRule, string) {
	if !r.IsJSON {
		return nil, ""
	}
	var v any
	if json.Unmarshal([]byte(r.RespBody), &v) != nil {
		return nil, ""
	}
	type cand struct {
		path, value string
		score       int
	}
	var cands []cand
	var walk func(v any, path string, depth int)
	walk = func(v any, path string, depth int) {
		if depth > 6 {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				p := k
				if path != "" {
					p = path + "." + k
				}
				if s, ok := val.(string); ok {
					lk := strings.ToLower(k)
					if len(s) >= 10 && (strings.Contains(lk, "token") || strings.Contains(lk, "jwt") || strings.Contains(lk, "session") ||
						strings.Contains(lk, "ticket") || lk == "sid" || lk == "authorization") {
						score := 10
						if strings.Contains(lk, "access") {
							score += 5
						}
						if strings.Contains(lk, "token") {
							score += 5
						}
						if path == "" {
							score += 2
						}
						cands = append(cands, cand{path: p, value: s, score: score})
					}
				}
				walk(val, p, depth+1)
			}
		case []any:
			for i, val := range x {
				walk(val, fmt.Sprintf("%s[%d]", path, i), depth+1)
			}
		}
	}
	walk(v, "", 0)
	if len(cands) == 0 {
		return nil, ""
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return &model.ExtractRule{Var: "token", Path: best.path}, best.value
}

func detectRecords(body string) (string, string, map[string]any) {
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return "", "", nil
	}
	hits := jsonx.Arrays(v)
	if len(hits) == 0 {
		return "", "", nil
	}
	best := hits[0]
	bestScore := -1
	for _, h := range hits {
		score := h.Count
		low := strings.ToLower(h.Path)
		if strings.Contains(low, "data") || strings.Contains(low, "list") || strings.Contains(low, "rows") ||
			strings.Contains(low, "items") || strings.Contains(low, "records") || strings.Contains(low, "result") {
			score *= 3
		}
		if score > bestScore {
			bestScore = score
			best = h
		}
	}
	arr, ok := jsonx.Lookup(v, best.Path)
	var first map[string]any
	if ok {
		if a, ok2 := arr.([]any); ok2 && len(a) > 0 {
			if m, ok3 := a[0].(map[string]any); ok3 {
				first = m
			}
		}
	}
	totalPath := ""
	if m, ok := v.(map[string]any); ok {
		totalPath = findTotalPath(m, "")
	}
	return best.Path, totalPath, first
}

func findTotalPath(m map[string]any, prefix string) string {
	candidates := map[string]bool{"total": true, "totalcount": true, "total_count": true, "count": true, "totalnum": true, "totalelements": true, "totalrows": true}
	best := ""
	for k, v := range m {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			if r := findTotalPath(sub, p); r != "" && best == "" {
				best = r
			}
			continue
		}
		if candidates[strings.ToLower(k)] {
			switch v.(type) {
			case float64, int, json.Number:
				if best == "" || strings.Count(p, ".") < strings.Count(best, ".") {
					best = p
				}
			}
		}
	}
	return best
}

func detectPagination(r *browser.CapRequest) model.Pagination {
	pg := model.Pagination{Enabled: false, Type: "page", PageSize: 20, StartPage: 1, MaxPages: 100, StopWhenEmpty: true, DelayMs: 300}
	pageKeys := []string{"page", "pageNo", "pageNum", "pageNumber", "current", "currentPage", "pn"}
	sizeKeys := []string{"pageSize", "size", "limit", "page_size", "rows", "perPage"}
	if u, err := urlParse(r.URL); err == nil {
		q := u.Query()
		for _, key := range pageKeys {
			if v := q.Get(key); v != "" {
				pg.Enabled = true
				pg.PageParam = key
				pg.PageInBody = false
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					pg.StartPage = n
				}
				for _, sk := range sizeKeys {
					if sv := q.Get(sk); sv != "" {
						pg.SizeParam = sk
						if n, err := strconv.Atoi(sv); err == nil && n > 0 {
							pg.PageSize = n
						}
						break
					}
				}
				return pg
			}
		}
	}
	if r.ReqBody != "" {
		var m map[string]any
		if json.Unmarshal([]byte(r.ReqBody), &m) == nil {
			for _, key := range pageKeys {
				if v, ok := m[key]; ok {
					pg.Enabled = true
					pg.PageParam = key
					pg.PageInBody = true
					pg.Type = "page"
					if n, ok := v.(float64); ok && n > 0 {
						pg.StartPage = int(n)
					}
					for _, sk := range sizeKeys {
						if sv, ok := m[sk]; ok {
							pg.SizeParam = sk
							if n, ok := sv.(float64); ok && n > 0 {
								pg.PageSize = int(n)
							}
							break
						}
					}
					return pg
				}
			}
			if _, ok := m["offset"]; ok {
				pg.Enabled = true
				pg.Type = "offset"
				pg.OffsetParam = "offset"
				pg.PageInBody = true
				for _, sk := range sizeKeys {
					if sv, ok := m[sk]; ok {
						pg.SizeParam = sk
						if n, ok := sv.(float64); ok && n > 0 {
							pg.PageSize = int(n)
						}
						break
					}
				}
				return pg
			}
		}
	}
	return pg
}

func preferredKeys(m map[string]any) []string {
	order := []string{"id", "_id", "uuid", "code", "name", "title", "storeName", "shopName", "phone", "mobile", "address", "status", "createdAt", "updatedAt"}
	seen := map[string]bool{}
	var out []string
	for _, k := range order {
		if _, ok := m[k]; ok {
			out = append(out, k)
			seen[k] = true
		}
	}
	// 其余字段按字母序输出，保证每次生成的映射顺序稳定（map 遍历本身是随机的）
	rest := make([]string, 0, len(m))
	for k, v := range m {
		if seen[k] {
			continue
		}
		switch v.(type) {
		case map[string]any, []any:
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		if len(out) >= 40 {
			break
		}
		out = append(out, k)
	}
	return out
}

func primaryKey(mapping []model.FieldMapping) string {
	cands := []string{"id", "_id", "uuid", "code", "storeId", "shopId"}
	for _, c := range cands {
		for _, m := range mapping {
			if strings.EqualFold(m.Source, c) {
				return m.Source
			}
		}
	}
	return ""
}

func guessType(v any) string {
	switch x := v.(type) {
	case float64:
		// JSON 数字统一是 float64，整数（如 id）映射成 int 更贴合目标表
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return "int"
		}
		return "float"
	case bool:
		return "bool"
	case map[string]any, []any:
		return "json"
	}
	return "string"
}

func actionToStep(a browser.Action) (model.UIStep, bool) {
	var st model.UIStep
	switch a.Type {
	case "navigate":
		// 录制期间发生的页面跳转（顶层页面）也要变成「打开页面」步骤，
		// 否则回放时页面还停在上一页，后面的点击 / 输入全都找不到元素。
		u := strings.TrimSpace(a.URL)
		if u == "" || strings.HasPrefix(u, "about:") || !strings.HasPrefix(u, "http") {
			return model.UIStep{}, false
		}
		st = model.UIStep{Action: "open", Value: u, Desc: "打开 " + truncate(u, 50)}
	case "click":
		// 选择器以 html/body 开头 = cpath 没找到任何 id/testid/name 锚点，
		// 退化为纯标签路径。组件库弹层选项（挂在 body 下、无 id）都是这种，
		// 不能直接丢弃：回放时有按钮文字兜底（ElementR）、有坐标兜底（X/Y），
		// 只有「无锚点选择器 && 无文字 && 无坐标」的点击才真的是废动作。
		weakSel := a.Selector == "" || strings.HasPrefix(a.Selector, "html") || a.Selector == "body"
		if weakSel && strings.TrimSpace(a.Text) == "" && a.X <= 0 && a.Y <= 0 {
			return model.UIStep{}, false
		}
		// 无锚点也无文字但录到了坐标：保留坐标点击（回放 Mouse.MoveTo 兜底）；
		// 有文字：保留（回放按文字找）。desc 里提示选择器质量差，方便排查。
		st = model.UIStep{Action: "click", Selector: a.Selector, Text: a.Text, X: a.X, Y: a.Y, Desc: "点击 " + truncate(a.Text, 20)}
		if weakSel {
			if strings.TrimSpace(a.Text) != "" {
				st.Desc = "点击 " + truncate(a.Text, 20) + "（无锚点选择器，按文字回放）"
			} else {
				st.Desc = fmt.Sprintf("点击 (%.0f,%.0f)（无锚点选择器，按坐标回放）", a.X, a.Y)
			}
		}
	case "input":
		if a.Selector == "" {
			return model.UIStep{}, false
		}
		st = model.UIStep{Action: "input", Selector: a.Selector, Value: a.Value, Desc: "输入 " + truncate(a.Selector, 30)}
	case "select":
		if a.Selector == "" {
			return model.UIStep{}, false
		}
		// 自定义下拉（组件库）：选项元素回放时还不存在，必须带上触发器，
		// 回放时先点开触发器再按 value / 文字选选项
		st = model.UIStep{
			Action: "select", Selector: a.Selector, Value: a.Value, Text: a.Text,
			Trigger: a.Trigger, TriggerText: a.TriggerText,
			Desc: "选择 " + truncate(a.Selector, 30),
		}
	case "press":
		if a.Key != "Enter" {
			return model.UIStep{}, false
		}
		st = model.UIStep{Action: "press", Selector: a.Selector, Key: "Enter", Desc: "回车"}
	default:
		return model.UIStep{}, false
	}
	// 录制时如果动作发生在 iframe 内，这里把 iframe 选择器一并带上，
	// 回放时才能进入同一个 iframe 操作（否则会在主页面找不到元素）。
	st.Frame = strings.TrimSpace(a.Frame)
	return st, true
}

// truncate 按 rune 截断（desc 用于日志与界面展示，中文按字节截会乱码）
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// normURL 归一化 URL 用于步骤去重：统一 scheme/host 大小写、去掉末尾斜杠
func normURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := urlParse(s)
	if err != nil || u.Host == "" {
		return s
	}
	out := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.Fragment
	}
	return out
}

func stripQuery(raw string) string {
	if i := strings.Index(raw, "?"); i >= 0 {
		return raw[:i]
	}
	return raw
}

func defaultName(startURL string, list *browser.CapRequest) string {
	host := startURL
	if u, err := urlParse(startURL); err == nil {
		host = u.Hostname()
	}
	if list != nil {
		p := list.URL
		if u, err := urlParse(list.URL); err == nil {
			p = u.Path
		}
		if i := strings.LastIndex(p, "/"); i >= 0 && i < len(p)-1 {
			p = p[i+1:]
		}
		if len(p) > 24 {
			p = p[:24]
		}
		return host + "-" + p
	}
	return host + "-采集"
}

// ruleName 用 URL 末段生成可读的接口/规则名称
func ruleName(rawURL string) string {
	p := rawURL
	if u, err := urlParse(rawURL); err == nil {
		p = u.Path
	}
	p = strings.Trim(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		return "数据接口"
	}
	if r := []rune(p); len(r) > 24 {
		p = string(r[:24])
	}
	return p
}
