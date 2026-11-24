/*
 * 任务编辑器「读入 → 保存」往返一致性检查（无需浏览器 / 后端）。
 *
 * 背景：录制生成的任务里，点击动作会带录制时的 x / y 坐标；回放时选择器
 * 失效会退回坐标点击。编辑器保存若把坐标丢掉，就会出现「导入能跑、改名
 * 保存后点不动」。这里直接从 web/js/page-editor.js 抽取 normalizeTask /
 * serialize，用真实任务 JSON 做一次往返并断言关键字段不丢。
 *
 * 用法: node tools/editor_roundtrip_test.js [task.json]
 *   不给文件时使用内置样例。
 */
'use strict';
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const editorPath = path.join(__dirname, '..', 'web', 'js', 'page-editor.js');

// 从编辑器源码里截出纯函数部分（normalizeTask / serialize 之前的辅助函数 + serialize）
function loadEditor() {
  const src = fs.readFileSync(editorPath, 'utf8');
  const headStart = src.indexOf('  let seq = 1;');
  const headEnd = src.indexOf('  const TABS = [');
  const serStart = src.indexOf('      function serialize() {');
  const serEnd = src.indexOf('      /* ---------- 保存 ---------- */');
  if (headStart < 0 || headEnd < 0 || serStart < 0 || serEnd < 0) {
    throw new Error('无法从 page-editor.js 定位 normalizeTask / serialize，请检查文件结构是否变化');
  }
  const head = src.slice(headStart, headEnd).replace('  let seq = 1;', '');
  const ser = src.slice(serStart, serEnd).replace(/function serialize\(\) \{/, 'function serialize(task) {');
  const code = 'let seq = 1;\n' + head + '\n' + ser +
    '\nmodule.exports = { normalizeTask: normalizeTask, serialize: serialize };';
  const sandbox = { module: { exports: {} }, console: console };
  vm.runInNewContext(code, sandbox, { filename: 'page-editor.extract.js' });
  return sandbox.module.exports;
}

function sampleTask() {
  return {
    id: 1, name: '样例任务', mode: 'ui', remark: '', enabled: true, notify: true,
    notify_on: 'failure', schedule_type: 'manual', cron: '', interval_sec: 0, timeout_sec: 60,
    session_mode: '', vars: [],
    http_steps: [],
    ui_steps: [
      { action: 'open', selector: '', value: 'https://demo.test/list', desc: '打开首页' },
      {
        action: 'click', selector: '#pagination > a:nth-of-type(7)', text: '>',
        x: 412, y: 806, desc: '点击 >',
      },
      {
        action: 'select', selector: '.el-select-dropdown__item:nth-of-type(3)',
        trigger: '.el-select', trigger_text: '20 条/页', value: '50', text: '50 条/页',
      },
    ],
    login_steps: [{ action: 'click', selector: '#login', text: '登录', x: 33, y: 44 }],
    capture: { mode: 'network', url_contains: '', method: '', records_path: '', wait_ms: 3000, row_selector: '', rules: [] },
    pagination: {
      enabled: false, type: 'page', page_param: 'page', size_param: 'pageSize', page_size: 20,
      start_page: 1, max_pages: 100, offset_param: 'offset', cursor_param: 'cursor', cursor_path: '',
      records_path: '', total_path: '', stop_when_empty: true, delay_ms: 0, page_in_body: false,
    },
    mapping: [{ source: 'id', target: 'id', type: 'int' }],
    sink: {
      type: 'table', data_source_id: 0, table: '', write_mode: 'insert', key_columns: [],
      batch_size: 200, auto_create: true, export_format: 'xlsx',
    },
    dedupe: { enabled: false, keys: [] },
  };
}

const { normalizeTask, serialize } = loadEditor();
const input = process.argv[2]
  ? JSON.parse(fs.readFileSync(process.argv[2], 'utf8'))
  : sampleTask();
if (Array.isArray(input)) throw new Error('传入的 JSON 应是单个任务对象，不是数组');

const out = serialize(normalizeTask(JSON.parse(JSON.stringify(input))));
let failed = 0;
function check(label, cond, detail) {
  if (cond) {
    console.log('  OK   ' + label);
  } else {
    failed++;
    console.log('  FAIL ' + label + (detail ? '  ' + detail : ''));
  }
}

console.log('编辑器保存往返检查');
check('任务名称保留', out.name === input.name);
check('采集模式保留', out.mode === input.mode);
check('UI 步骤数量不变', out.ui_steps.length === (input.ui_steps || []).length);

const before = (input.ui_steps || []);
const after = out.ui_steps || [];
for (let i = 0; i < before.length; i++) {
  if (typeof before[i].x !== 'number' && typeof before[i].y !== 'number') continue;
  const bx = Number(before[i].x || 0), by = Number(before[i].y || 0);
  const ax = Number(after[i] && after[i].x || 0), ay = Number(after[i] && after[i].y || 0);
  check('UI 步骤 ' + (i + 1) + ' 点击坐标保留', ax === bx && ay === by,
    'before=' + bx + '/' + by + ' after=' + ax + '/' + ay);
}
const selBefore = before.find(function (s) { return s.action === 'select' && s.trigger; });
if (selBefore) {
  const selAfter = after.find(function (s) { return s.action === 'select'; });
  check('下拉触发器保留', !!selAfter && selAfter.trigger === selBefore.trigger);
  check('下拉触发器文字保留', !!selAfter && selAfter.trigger_text === selBefore.trigger_text);
}
const loginBefore = (input.login_steps || [])[0];
if (loginBefore) {
  const loginAfter = (out.login_steps || [])[0];
  check('登录步骤保留', !!loginAfter);
  check('登录步骤点击坐标保留',
    Number(loginAfter.x || 0) === Number(loginBefore.x || 0) &&
    Number(loginAfter.y || 0) === Number(loginBefore.y || 0));
}

if (failed > 0) {
  console.log('\n' + failed + ' 项检查未通过');
  process.exit(1);
}
console.log('\n全部通过');
