/* 我的日程 前端渲染层 —— 全部与 Go 内核经 native('module.method', params) 通信 */
'use strict';

const CATS = { work: { name: '工作', color: '#417c6a' }, study: { name: '学习', color: '#7c6ca8' }, life: { name: '生活', color: '#a4783f' } };
const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fmtD = d => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
const parseD = k => new Date(k + 'T00:00:00');
const addD = (d, n) => { const t = new Date(d); t.setDate(t.getDate() + n); return t; };
const today = () => fmtD(new Date());

const state = {
  tasks: [], settings: null, holidays: {}, // date -> DayInfo 缓存
  view: 'calendar', mode: 'month', anchor: new Date(), selected: today(),
  filter: null, query: '', editing: null, dataDir: '',
};

/* ---------- 工具 ---------- */
let toastTimer;
function toast(msg) {
  const el = $('#toast');
  el.textContent = msg;
  el.classList.add('visible');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove('visible'), 3200);
}
async function run(fn, okMsg) {
  try {
    const r = await fn();
    if (okMsg) toast(okMsg);
    return r;
  } catch (e) {
    toast(String(e.message || e).replace(/^Error:\s*/, ''));
    return null;
  }
}
const taskEnd = t => t.endDate && t.endDate >= t.date ? t.endDate : t.date;
const spanDays = t => Math.round((parseD(taskEnd(t)) - parseD(t.date)) / 86400000) + 1;
const occursOn = (t, k) => t.date <= k && k <= taskEnd(t);
function sortTasks(a, b) { return a.date === b.date ? a.time.localeCompare(b.time) : a.date.localeCompare(b.date); }
function catColor(t) { return CATS[t.category]?.color || '#8a8f98'; }
function timeLabel(t) { return `${t.time}${t.duration < 1440 ? ` · ${t.duration}分钟` : ' · 全天'}`; }

/* ---------- 数据加载 ---------- */
async function reload() {
  const [tasks, settings, info] = await Promise.all([
    native('plans.list', {}), native('settings.get', {}), native('app.info', {}),
  ]);
  if (tasks) state.tasks = tasks.tasks || [];
  if (settings) state.settings = settings;
  if (info) state.dataDir = info.dataDir || '';
  render();
}
async function holidayOf(k) {
  if (!(k in state.holidays)) {
    const r = await run(() => native('holidays.day', { date: k }));
    state.holidays[k] = r || null;
  }
  return state.holidays[k];
}

/* ---------- 渲染 ---------- */
function render() {
  if (!state.settings) return;
  $$('.nav-item[data-view]').forEach(b => b.classList.toggle('active', b.dataset.view === state.view));
  const undone = state.tasks.filter(t => !t.done);
  $('#today-count').textContent = state.tasks.filter(t => !t.done && t.date <= today()).length;
  $('#today-count').style.display = state.view === 'today' ? 'none' : '';
  $('#categories').innerHTML = Object.entries(CATS).map(([id, c]) =>
    `<button class="category-button ${state.filter === id ? 'active' : ''}" data-category="${id}">
      <i class="category-dot" style="--category:${c.color}"></i>${c.name}<span>${undone.filter(t => t.category === id).length}</span></button>`).join('');
  const titles = {
    calendar: ['计划日历', '安排未来几周，也掌握今天要做的事。'],
    today: ['今天', '今天的安排与此前未完成的计划，一起查看。'],
    inbox: ['提醒中心', '处理到时提醒，或选择稍后再次提醒。'],
    done: ['已完成', '查看已经完成的计划，回顾每天的进展。'],
  };
  $('#page-title').textContent = titles[state.view][0];
  $('#page-subtitle').textContent = titles[state.view][1];
  $('#search').value = state.query;
  renderSummary();
  $('#calendar-page').style.display = state.view === 'calendar' ? '' : 'none';
  const lp = $('#list-page');
  lp.style.display = state.view === 'calendar' ? 'none' : '';
  if (state.view === 'calendar') renderCalendar();
  else renderListPage(lp);
  renderMini();
}
function renderSummary() {
  const from = today(), to = fmtD(addD(new Date(), 27));
  const inRange = state.tasks.filter(t => t.date >= from && t.date <= to && (!state.filter || t.category === state.filter) && (!state.query || (t.title + t.notes).toLowerCase().includes(state.query.toLowerCase())));
  const done = inRange.filter(t => t.done);
  const hours = inRange.filter(t => !t.done).reduce((a, t) => a + t.duration, 0) / 60;
  const rate = inRange.length ? Math.round(done.length / inRange.length * 100) : 0;
  $('#summary').innerHTML = `
    <div class="stat-card"><div class="stat-icon">📅</div><div class="stat-copy"><span>未来四周</span>
      <div class="stat-number">${inRange.length}<small>项计划</small></div></div></div>
    <div class="stat-card"><div class="stat-icon">✓</div><div class="stat-copy"><span>已完成</span>
      <div class="stat-number">${done.length}<small>项计划</small></div></div>
      <div class="stat-caption">${rate}% 完成率</div></div>
    <div class="stat-card"><div class="stat-icon">⏱</div><div class="stat-copy"><span>待办预计用时</span>
      <div class="stat-number">${Math.round(hours * 10) / 10}<small>小时</small></div></div></div>`;
}

/* ---------- 日历 ---------- */
async function renderCalendar() {
  $$('#mode-switch button').forEach(b => b.classList.toggle('active', b.dataset.mode === state.mode));
  const mode = state.mode;
  const weekStart = state.settings.weekStart ?? 1;
  const anchor = state.anchor;
  const gridWrap = $('.calendar-wrap');
  if (mode === 'list') { // 清单模式：按日期分组全部
    gridWrap.style.display = 'none';
    $('#range-label').textContent = '全部计划清单';
    const lp = $('#list-page');
    lp.style.display = '';
    renderListPage(lp);
    return;
  }
  gridWrap.style.display = '';
  $('#list-page').style.display = 'none';
  let first, weeks, label;
  if (mode === 'month') {
    first = new Date(anchor.getFullYear(), anchor.getMonth(), 1);
    const dim = new Date(anchor.getFullYear(), anchor.getMonth() + 1, 0).getDate();
    const lead = (first.getDay() - weekStart + 7) % 7;
    weeks = Math.ceil((lead + dim) / 7);
    label = `${anchor.getFullYear()}年 ${anchor.getMonth() + 1}月`;
  } else {
    const lead = (anchor.getDay() - weekStart + 7) % 7;
    first = addD(anchor, -lead);
    weeks = mode === 'week' ? 1 : 4;
    const end = addD(first, weeks * 7 - 1);
    label = mode === 'week' ? `${fmtD(first)} — ${fmtD(end)}` : `${first.getFullYear()}年 ${fmtD(first)} — ${fmtD(end)}`;
  }
  $('#range-label').textContent = label;
  const cal = $('#calendar');
  cal.className = `calendar ${mode === 'week' ? 'weekly' : mode}`;
  cal.style.gridTemplateRows = `auto repeat(${weeks}, minmax(0, 1fr))`; // 撑满可视高度，整月一页显示
  const wds = weekStart === 1 ? ['一', '二', '三', '四', '五', '六', '日'] : ['日', '一', '二', '三', '四', '五', '六'];
  let html = wds.map(w => `<div class="weekday-head">${w}</div>`).join('');
  cal.innerHTML = html;
  for (let w = 0; w < weeks; w++) {
    for (let i = 0; i < 7; i++) {
      const d = addD(first, w * 7 + i), k = fmtD(d);
      const cell = document.createElement('div');
      cell.className = 'day-cell';
      if (k === today()) cell.classList.add('today');
      if (d.getDay() === 0 || d.getDay() === 6) cell.classList.add('weekend');
      if (k === state.selected) cell.classList.add('selected');
      if (mode === 'month' && d.getMonth() !== anchor.getMonth()) cell.classList.add('dim');
      cell.dataset.select = k;
      cal.appendChild(cell);
      fillDayCell(cell, k, mode === 'week' ? 6 : 2);
    }
  }
  renderDayPanel();
}
async function fillDayCell(cell, k, maxChips) {
  const hol = await holidayOf(k);
  const tasks = visibleTasks().filter(t => occursOn(t, k)).sort(sortTasks);
  let head = `<div class="day-top"><span class="day-number">${Number(k.slice(8))}</span>`;
  if (state.settings.showLunar && hol) head += `<span class="day-lunar">${hol.lunar.day === '初一' ? hol.lunar.month + '月' : hol.lunar.day}</span>`;
  head += '</div>';
  if (state.settings.showHolidays && hol) {
    if (hol.cn?.off) head += `<span class="badge off">休 ${esc(hol.cn.name)}</span> `;
    else if (hol.cn?.work) head += `<span class="badge work">班</span> `;
    if (hol.us) head += `<span class="badge us">${esc(hol.us.name)}</span> `;
    if (hol.lunar.term) head += `<span class="badge term">${esc(hol.lunar.term)}</span> `;
  }
  cell.insertAdjacentHTML('beforeend', head);
  const chips = tasks.slice(0, state.mode === 'week' ? 99 : maxChips);
  if (state.mode === 'week') {
    for (const t of chips) cell.insertAdjacentHTML('beforeend', weeklyTaskHtml(t));
  } else {
    for (const t of chips) {
      cell.insertAdjacentHTML('beforeend', `<button class="task-chip ${t.done ? 'done' : ''}" data-task="${t.id}" style="--chip:${catColor(t)}">${t.priority === 'high' ? '<span class="task-pri"></span>' : ''}${esc(t.title)}</button>`);
    }
    if (tasks.length > maxChips) cell.insertAdjacentHTML('beforeend', `<span class="more-link">还有 ${tasks.length - maxChips} 项…</span>`);
  }
}
function weeklyTaskHtml(t) {
  return `<button class="weekly-task ${t.done ? 'done' : ''}" data-task="${t.id}" style="--chip:${catColor(t)}">
    <b>${t.time} · ${t.duration < 1440 ? t.duration + '分' : '全天'}${t.priority === 'high' ? ' · 优先' : ''}</b>${esc(t.title)}</button>`;
}
function visibleTasks() {
  return state.tasks.filter(t =>
    (!state.filter || t.category === state.filter) &&
    (!state.query || (t.title + ' ' + t.notes).toLowerCase().includes(state.query.toLowerCase())));
}

/* ---------- 日面板 ---------- */
async function renderDayPanel() {
  const panel = $('#day-panel');
  const k = state.selected;
  const hol = await holidayOf(k);
  const tasks = visibleTasks().filter(t => occursOn(t, k)).sort(sortTasks);
  let html = `<h3>${k === today() ? '今天' : k.slice(5).replace('-', '月') + '日'}<span class="badge">${state.tasks.filter(t => occursOn(t, k) && !t.done).length} 待办</span></h3>`;
  html += `<div class="panel-lunar">${hol ? hol.lunar.month + '月' + hol.lunar.day + ' · ' + hol.lunar.yearGanZhi + hol.lunar.animal + '年' : ''}</div>`;
  if (!tasks.length) html += `<div class="panel-empty">这一天还没有计划。双击空白日期格可新建。</div>`;
  for (const t of tasks) {
    html += `<button class="panel-task" data-task="${t.id}">
      <span class="task-check ${t.done ? 'done' : ''}">${t.done ? '✓' : ''}</span>
      <span class="task-copy"><b class="${t.done ? 'done' : ''}">${t.priority === 'high' ? '<span class="task-pri"></span>' : ''}${esc(t.title)}</b>
      <small>${timeLabel(t)} · ${CATS[t.category]?.name}${spanDays(t) > 1 ? ` · 共${spanDays(t)}天` : ''}</small></span></button>`;
  }
  panel.innerHTML = html;
}

/* ---------- 清单 / 今天 / 提醒中心 / 已完成 ---------- */
async function renderListPage(container, isCalendarList) {
  const view = state.view;
  if (view === 'calendar') { // 清单模式：按日期分组全部
    const groups = groupByDate(visibleTasks().sort(sortTasks));
    container.innerHTML = `<div class="list-page">${await groupsHtml(groups)}</div>`;
    return;
  }
  if (view === 'today') {
    const k = today();
    const overdue = visibleTasks().filter(t => !t.done && taskEnd(t) < k).sort(sortTasks);
    const todays = visibleTasks().filter(t => !t.done && occursOn(t, k)).sort(sortTasks);
    container.innerHTML = `<div class="list-page">
      ${await listGroupHtml('已过期未完成', overdue)}
      ${await listGroupHtml('今天', todays)}</div>`;
    return;
  }
  if (view === 'inbox') {
    const r = await run(() => native('plans.due', {}));
    const due = (r?.tasks || []);
    const pending = visibleTasks().filter(t => !t.done && t.reminder >= 0 && t.date <= today()).sort(sortTasks);
    const seen = new Set(due.map(t => t.id));
    const extra = pending.filter(t => !seen.has(t.id));
    container.innerHTML = `<div class="list-page">
      ${due.length ? `<div class="list-group"><h4>⏰ 现在需要关注</h4>${due.map(listTaskHtml).join('')}</div>` : ''}
      ${extra.length ? `<div class="list-group"><h4>今日提醒安排</h4>${extra.map(listTaskHtml).join('')}</div>` : ''}
      ${!due.length && !extra.length ? '<div class="panel-empty">没有待处理的提醒。</div>' : ''}</div>`;
    return;
  }
  // done
  const done = visibleTasks().filter(t => t.done).sort((a, b) => b.date.localeCompare(a.date));
  const groups = groupByDate(done);
  container.innerHTML = `<div class="list-page">${await groupsHtml(groups, true)}</div>`;
}
function groupByDate(tasks) {
  const m = new Map();
  for (const t of tasks) {
    if (!m.has(t.date)) m.set(t.date, []);
    m.get(t.date).push(t);
  }
  return [...m.entries()];
}
async function groupsHtml(groups, desc) {
  if (desc) groups.reverse();
  let html = '';
  for (const [k, ts] of groups.slice(0, 60)) {
    const hol = await holidayOf(k);
    html += `<div class="list-group"><h4>${k} <span class="g-lunar">${hol ? hol.lunar.month + '月' + hol.lunar.day : ''}${hol?.cn?.off ? ' · ' + esc(hol.cn.name) : ''}</span></h4>${ts.map(listTaskHtml).join('')}</div>`;
  }
  if (!groups.length) html = '<div class="panel-empty">没有符合条件的计划。</div>';
  return html;
}
async function listGroupHtml(title, ts) {
  if (!ts.length) return '';
  return `<div class="list-group"><h4>${title}</h4>${ts.map(listTaskHtml).join('')}</div>`;
}
function listTaskHtml(t) {
  const overdue = !t.done && t.date < today() ? '<span class="overdue-flag">已过期</span>' : '';
  return `<div class="list-task" data-task="${t.id}">
    <span class="task-check ${t.done ? 'done' : ''}" data-toggle="${t.id}">${t.done ? '✓' : ''}</span>
    <span class="task-copy"><b class="${t.done ? 'done' : ''}">${t.priority === 'high' ? '<span class="task-pri"></span>' : ''}${esc(t.title)}${overdue}</b>
    <small>${t.date} ${timeLabel(t)} · ${CATS[t.category]?.name}${spanDays(t) > 1 ? ` · 共${spanDays(t)}天` : ''}</small></span>
    ${t.seriesId ? `<button class="series-btn" data-series="following" data-id="${t.id}">完成后续</button>` : ''}</div>`;
}

/* ---------- 迷你日历 ---------- */
function renderMini() {
  const a = new Date(state.selected + 'T00:00:00');
  const first = new Date(a.getFullYear(), a.getMonth(), 1);
  const weekStart = state.settings.weekStart ?? 1;
  const lead = (first.getDay() - weekStart + 7) % 7;
  const dim = new Date(a.getFullYear(), a.getMonth() + 1, 0).getDate();
  const wds = weekStart === 1 ? ['一', '二', '三', '四', '五', '六', '日'] : ['日', '一', '二', '三', '四', '五', '六'];
  let html = `<div class="mini-grid">${wds.map(w => `<span class="wd">${w}</span>`).join('')}`;
  for (let i = 0; i < lead; i++) html += '<span></span>';
  for (let d = 1; d <= dim; d++) {
    const k = fmtD(new Date(a.getFullYear(), a.getMonth(), d));
    const has = state.tasks.some(t => occursOn(t, k));
    html += `<span class="mini-day ${k === today() ? 'today' : ''} ${k === state.selected ? 'selected' : ''} ${has && k !== state.selected ? 'off' : ''}" data-mini="${k}">${d}</span>`;
  }
  $('#mini-calendar').innerHTML = html + '</div>';
}

/* ---------- 编辑器 ---------- */
function openEditor(task, presetDate) {
  state.editing = task;
  const f = $('#task-form');
  f.reset();
  $('#editor-title').textContent = task ? '编辑计划' : '新建计划';
  f.title.value = task?.title || '';
  f.date.value = task?.date || presetDate || today();
  f.time.value = task?.time || '09:00';
  f.duration.value = task?.duration || 60;
  f.endDate.value = task ? taskEnd(task) : '';
  f.spanDays.value = task ? spanDays(task) : 1;
  f.category.value = task?.category || state.filter || 'work';
  f.reminder.value = task ? String(task.reminder) : '15';
  f.priority.value = task?.priority || 'normal';
  f.notes.value = task?.notes || '';
  $('#repeat-field').style.display = task ? 'none' : '';
  $('#delete-task').hidden = !task;
  $('#form-error').textContent = '';
  syncRepeatUI();
  $('#editor').showModal();
  f.title.focus();
}
function syncRepeatUI() {
  const f = $('#task-form');
  const v = f.repeat.value;
  const ranged = !state.editing && (v === 'daily' || v === 'weekdays');
  f.repeatUntil.hidden = !ranged;
  if (ranged && !f.repeatUntil.value) f.repeatUntil.value = fmtD(addD(parseD(f.date.value || today()), 29));
  const weekly = !state.editing && ['4', '8', '12'].includes(v);
  $('#form-hint').textContent = ranged
    ? (v === 'daily' ? '每天生成一项，直到指定日期（含当天）。' : '仅周一至周五，跳过周末，直到指定日期。')
    : weekly ? `将每周一次共生成 ${v} 项独立计划，可分别修改。` : '';
}
async function saveEditor() {
  const f = $('#task-form');
  const err = $('#form-error');
  err.textContent = '';
  if (!f.title.value.trim()) { err.textContent = '请填写标题。'; return; }
  const draft = {
    title: f.title.value.trim(), date: f.date.value, time: f.time.value,
    endDate: f.endDate.value && f.endDate.value > f.date.value ? f.endDate.value : '',
    duration: Number(f.duration.value), reminder: Number(f.reminder.value),
    category: f.category.value, priority: f.priority.value, notes: f.notes.value.trim(),
  };
  let repeat = null;
  const rv = f.repeat.value;
  if (!state.editing) {
    if (rv === 'daily' || rv === 'weekdays') repeat = { frequency: rv, until: f.repeatUntil.value };
    else if (['4', '8', '12'].includes(rv)) repeat = Number(rv);
  }
  if (state.editing) draft.id = state.editing.id;
  const r = await run(() => native('plans.save', { task: draft, repeat, id: state.editing?.id || null }), '已保存');
  if (r) {
    $('#editor').close();
    // 保存后定位到该计划的日期，保证在日历中立刻可见
    state.selected = draft.date;
    state.anchor = parseD(draft.date);
    await reload();
  }
}
async function deleteEditor() {
  if (!state.editing) return;
  const t = state.editing;
  let body = `删除「${esc(t.title)}」？`;
  if (t.seriesId) {
    const p = await run(() => native('plans.previewAction', { id: t.id, action: 'delete' }));
    if (p && p.followingCount > 0) body = `「${esc(t.title)}」属于重复系列，其后还有 ${p.followingCount} 项。<br><br><button data-series-apply="single" data-token="${p.singleToken}">仅删除这一项</button> <button data-series-apply="following" data-token="${p.token}">删除后续全部</button>`;
  }
  if (!confirm(body.replace(/<[^>]+>/g, ' '))) return;
  await run(() => native('plans.delete', { id: t.id }), '已删除');
  $('#editor').close();
  await reload();
}

/* ---------- 提醒 ---------- */
window.__push = async function (kind, payload) {
  if (kind === 'refresh') { await reload(); return; }
  if (kind !== 'reminder') return;
  const tasks = payload || [];
  if (!tasks.length) return;
  const body = $('#reminder-body');
  body.innerHTML = tasks.map(t => `<div class="reminder-item"><b>${t.priority === 'high' ? '<span class="task-pri"></span>' : ''}${esc(t.title)}</b><small>${t.date} ${t.time} · ${CATS[t.category]?.name || ''}</small></div>`).join('');
  $('#reminder-overlay').hidden = false;
  state.reminderTasks = tasks.map(t => t.id);
};
async function reminderAction(action) {
  const ids = state.reminderTasks || [];
  for (const id of ids) {
    if (action === 'done') await run(() => native('plans.toggle', { id, done: true }));
    if (action === 'snooze') await run(() => native('plans.snooze', { id, minutes: 10 }));
  }
  $('#reminder-overlay').hidden = true;
  await reload();
}

/* ---------- 设置 ---------- */
async function openSettings() {
  const s = state.settings;
  $('#set-language').value = s.language || 'zh-CN';
  $('#set-week-start').value = String(s.weekStart ?? 1);
  $('#set-lunar').checked = s.showLunar !== false;
  $('#set-holidays').checked = s.showHolidays !== false;
  $('#set-ai-endpoint').value = s.aiEndpoint || '';
  $('#set-ai-model').value = s.aiModel || '';
  $('#set-ai-key').value = '';
  $('#data-dir').textContent = `数据目录：${state.dataDir}`;
  $('#settings-dialog').showModal();
}
async function saveSettings() {
  const patch = {
    language: $('#set-language').value, weekStart: Number($('#set-week-start').value),
    showLunar: $('#set-lunar').checked, showHolidays: $('#set-holidays').checked,
    aiEndpoint: $('#set-ai-endpoint').value.trim(), aiModel: $('#set-ai-model').value.trim(),
  };
  const key = $('#set-ai-key').value.trim();
  const r = await run(() => native('settings.set', { patch, apiKey: key || null }), '设置已保存');
  if (r) { await reload(); }
}
async function aiParse() {
  const text = $('#ai-input').value.trim();
  if (!text) return;
  const btn = $('#ai-parse'); btn.disabled = true; btn.textContent = '解析中…';
  const r = await run(() => native('ai.parse', { text }));
  btn.disabled = false; btn.textContent = 'AI 解析';
  if (!r?.draft) return;
  const d = r.draft;
  state.editing = null;
  openEditor(null, d.date || today());
  $('#ai-dialog').close();
  const f = $('#task-form');
  if (d.title) f.title.value = d.title;
  if (d.time) f.time.value = d.time;
  if (d.duration) f.duration.value = String(d.duration);
  if (d.reminder !== undefined) f.reminder.value = String(d.reminder);
  if (d.category && CATS[d.category]) f.category.value = d.category;
  if (d.priority) f.priority.value = d.priority;
  if (d.notes) f.notes.value = d.notes;
  toast('AI 已生成草稿，请确认后保存。');
}
async function exportBackup() {
  const r = await run(() => native('backup.export', {}));
  if (!r) return;
  const blob = new Blob([JSON.stringify(r, null, 2)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `my-schedule-backup-${today()}.json`;
  a.click();
  URL.revokeObjectURL(a.href);
  toast('备份已导出。');
}
function importBackup() {
  const input = document.createElement('input');
  input.type = 'file'; input.accept = '.json,application/json';
  input.onchange = async () => {
    if (!input.files[0]) return;
    try {
      const data = JSON.parse(await input.files[0].text());
      const r = await run(() => native('backup.import', { backup: data }), '备份已导入');
      if (r) { $('#settings-dialog').close(); await reload(); }
    } catch (e) { toast('文件解析失败: ' + e.message); }
  };
  input.click();
}

/* ---------- 事件绑定 ---------- */
document.addEventListener('click', async e => {
  const el = e.target.closest('[data-view],[data-mode],[data-category],[data-mini],[data-select],[data-task],[data-toggle],[data-series],[data-series-apply],[data-action],[data-reminder-action],.close-dialog,.close-reminder');
  if (!el) return;
  if (el.dataset.view) { state.view = el.dataset.view; render(); return; }
  if (el.dataset.mode) { state.mode = el.dataset.mode; render(); return; }
  if (el.dataset.category) { state.filter = state.filter === el.dataset.category ? null : el.dataset.category; render(); return; }
  if (el.dataset.mini) { state.selected = el.dataset.mini; state.anchor = parseD(state.selected); state.view = 'calendar'; render(); return; }
  if (el.dataset.select) { state.selected = el.dataset.select; render(); return; }
  if (el.dataset.task) {
    const t = state.tasks.find(t => t.id === el.dataset.task);
    if (t) openEditor(t);
    return;
  }
  if (el.dataset.toggle) {
    const t = state.tasks.find(t => t.id === el.dataset.toggle);
    if (t) await run(() => native('plans.toggle', { id: t.id, done: !t.done }));
    return;
  }
  if (el.dataset.series === 'following') {
    const id = el.dataset.id;
    const p = await run(() => native('plans.previewAction', { id, action: 'complete' }));
    if (!p) return;
    const r = await run(() => native('plans.applyAction', { action: 'complete', id, token: p.token }), '已完成系列后续计划');
    if (r) await reload();
    return;
  }
  if (el.dataset.seriesApply) { /* 由 confirm 流程处理 */ }
  if (el.dataset.reminderAction) { await reminderAction(el.dataset.reminderAction); return; }
  if (el.classList.contains('close-reminder')) { $('#reminder-overlay').hidden = true; await reload(); return; }
  if (el.classList.contains('close-dialog')) { el.closest('dialog').close(); return; }
  if (el.dataset.action === 'new') { openEditor(null, state.selected === today() ? today() : state.selected); return; }
  if (el.dataset.action === 'settings') { openSettings(); return; }
  if (el.dataset.action === 'ai') {
    $('#ai-input').value = '';
    $('#ai-dialog').showModal();
    setTimeout(() => $('#ai-input').focus(), 50);
    return;
  }
});
$('#prev').onclick = () => { shiftAnchor(-1); };
$('#next').onclick = () => { shiftAnchor(1); };
$('#today-btn').onclick = () => { state.anchor = new Date(); state.selected = today(); render(); };
function shiftAnchor(n) {
  if (state.mode === 'month') state.anchor = new Date(state.anchor.getFullYear(), state.anchor.getMonth() + n, 1);
  else state.anchor = addD(state.anchor, n * (state.mode === 'week' ? 7 : 28));
  render();
}
$('#search').oninput = e => { state.query = e.target.value.trim(); renderSummary(); if (state.view !== 'calendar') render(); else if (state.mode === 'list') render(); };
$('#task-form').onsubmit = e => { e.preventDefault(); saveEditor(); };
$('#task-form').oninput = e => {
  const f = $('#task-form');
  if (e.target.name === 'endDate' && f.endDate.value) f.spanDays.value = Math.round((parseD(f.endDate.value) - parseD(f.date.value)) / 86400000) + 1;
  if (e.target.name === 'spanDays' && Number(f.spanDays.value) > 0) f.endDate.value = fmtD(addD(parseD(f.date.value), Number(f.spanDays.value) - 1));
};
$('#task-form').onchange = e => { if (e.target.name === 'repeat') syncRepeatUI(); };
$('#delete-task').onclick = deleteEditor;
$('#save-settings').onclick = saveSettings;
$('#ai-parse').onclick = aiParse;
$('#ai-input').addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); aiParse(); } });
$('#export-backup').onclick = exportBackup;
$('#import-backup').onclick = importBackup;
document.addEventListener('dblclick', e => {
  const cell = e.target.closest('.day-cell');
  if (cell && cell.dataset.select) openEditor(null, cell.dataset.select);
});
document.addEventListener('keydown', e => {
  if (e.target.matches('input,textarea,select')) {
    if (e.key === 'Escape') e.target.blur();
    return;
  }
  if (e.key === 'n' || e.key === 'N') { e.preventDefault(); openEditor(null, state.selected); }
  else if (e.key === 't' || e.key === 'T') { $('#today-btn').click(); }
  else if (e.key === '/') { e.preventDefault(); $('#search').focus(); }
  else if (e.key === 'ArrowLeft') { $('#prev').click(); }
  else if (e.key === 'ArrowRight') { $('#next').click(); }
});
$('#set-language').onchange = () => { /* zh-CN 为内嵌主语言；English 预留 */ };

/* ---------- 启动 ---------- */
reload();
setInterval(reload, 60000); // 兜底刷新（60s 一次，无高频轮询）
