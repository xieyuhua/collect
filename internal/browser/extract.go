package browser

import (
	"encoding/json"
	"errors"
	"time"

	"collect/internal/model"

	"github.com/go-rod/rod"
)

// ExtractDOM 从页面表格中按选择器提取数据（纯 UI 兜底方案）。
// cfg.Frame 不为空时，到对应 iframe 内部提取表格。
func ExtractDOM(page *rod.Page, cfg model.CaptureConfig, mapping []model.FieldMapping) ([]map[string]any, error) {
	if cfg.RowSelector == "" {
		return nil, errors.New("DOM 模式需要填写列表行选择器，例如 table tbody tr")
	}
	// 与录制器轮询/回放共用页面求值锁，避免并发求值写坏 rod 内部 helpers 表
	lock := EvalLock(page)
	lock.Lock()
	defer lock.Unlock()
	p := page
	if len(cfg.Frame) > 0 {
		fp, err := PageForFrame(page, cfg.Frame, 20*time.Second)
		if err != nil {
			return nil, err
		}
		p = fp
	} else {
		// 没有填「表格所在 iframe」时：主页面找不到表格行选择器，就自动到 iframe 里找。
		// 这样最早那批录制（没有 frame 字段）的任务也能直接回放，不用先手工查 iframe。
		if _, err := page.Timeout(900 * time.Millisecond).Element(cfg.RowSelector); err != nil {
			if fsel, e := DetectFrameForSelector(page, cfg.RowSelector, 1200*time.Millisecond); e == nil {
				if fp, e2 := PageForFrame(page, fsel, 5*time.Second); e2 == nil {
					p = fp
				}
			}
		}
	}
	type field struct {
		Target string `json:"target"`
		Source string `json:"source"`
	}
	var fields []field
	for _, m := range mapping {
		if m.Source == "" {
			continue
		}
		fields = append(fields, field{Target: m.Target, Source: m.Source})
	}
	res, err := p.Timeout(20 * time.Second).Eval(`(rowSel, fields) => {
		const rows = Array.from(document.querySelectorAll(rowSel));
		const out = rows.map(row => {
			const o = {};
			if (!fields || fields.length === 0) {
				Array.from(row.children).forEach((c, i) => { o['col' + (i + 1)] = (c.innerText || '').trim(); });
				return o;
			}
			for (const f of fields) {
				const raw = f.source || '';
				let sel = raw;
				let attr = null;
				const at = sel.lastIndexOf('@');
				if (at > 0) { attr = sel.slice(at + 1); sel = sel.slice(0, at); }
				const el = sel ? row.querySelector(sel) : row;
				let val = '';
				if (el) {
					if (attr) val = el.getAttribute(attr) || '';
					else if ('value' in el && (el.tagName === 'INPUT' || el.tagName === 'SELECT')) val = el.value;
					else val = (el.innerText || '').trim();
				}
				// 同时保留 selector 原始键与目标字段键：字段映射按 source(选择器) 取值，
				// 数据预览/导出按 target 展示，两边都要能拿到值。
				o[f.target || sel] = val;
				if (raw && raw !== f.target) o[raw] = val;
			}
			return o;
		});
		return JSON.stringify(out);
	}`, cfg.RowSelector, fields)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(res.Value.Str()), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
