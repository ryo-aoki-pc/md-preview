// コメントはページに保存し、サーバーが確かめた原文から依頼文を作る。
'use strict';

(() => {
  const preview = window.gpPreview;
  if (!preview?.config.review) return;
  const targets = window.gpEditTargets;
  const root = document.documentElement;
  const content = document.getElementById('gp-content');
  const toggle = document.getElementById('gp-review-toggle');
  const make = (tag, id, parent, text) => {
    const element = document.createElement(tag);
    if (id) element.id = id;
    if (text !== undefined) element.textContent = text;
    parent.append(element);
    return element;
  };
  const button = (id, parent, text, action) => {
    const element = make('button', id, parent, text);
    element.type = 'button';
    element.addEventListener('click', action);
    return element;
  };
  const layer = make('div', 'gp-review-layer', document.body);
  const hoverStyle = make('style', 'gp-review-hover', layer);
  const selectedStyle = make('style', 'gp-review-selection', layer);
  const hoverCell = make('div', 'gp-review-hover-cell', layer);
  const selectedCell = make('div', 'gp-review-selected-cell', layer);
  const panel = make('aside', 'gp-review-panel', document.body);
  panel.setAttribute('aria-label', 'コメント');
  const heading = make('div', 'gp-review-heading', panel);
  const title = make('h2', 'gp-review-title', heading);
  const close = button('gp-review-close', heading, '閉じる', () => setMode(false));
  const chosen = make('p', 'gp-review-target', panel);
  const quote = make('p', 'gp-review-selection-text', panel);
  const input = make('textarea', 'gp-review-input', panel);
  input.setAttribute('aria-label', 'コメント');
  input.rows = 4;
  const add = button('gp-review-add', panel, '追加', addComment);
  const cards = make('div', 'gp-review-cards', panel);
  const actions = make('div', 'gp-review-actions', panel);
  const prepare = button('gp-review-prepare', actions, '依頼文を作る', preparePrompt);
  const clearCopied = button('gp-review-clear-copied', actions, 'コピー済みを消す', () => {
    comments = comments.filter((item) => item.state !== 'copied');
    if (!comments.some((item) => item.id === editing)) editing = null;
    changed();
  });
  const output = make('section', 'gp-review-output', panel);
  make('h3', '', output, '依頼文');
  const promptInput = make('textarea', 'gp-review-prompt', output);
  promptInput.setAttribute('aria-label', '依頼文');
  promptInput.readOnly = true;
  promptInput.rows = 10;
  const copy = button('gp-review-copy', output, 'コピー', copyPrompt);
  const bytes = make('span', 'gp-review-bytes', output);
  const status = make('div', 'gp-review-status', document.body);
  status.setAttribute('role', 'status');
  status.setAttribute('aria-live', 'polite');

  const storageKey = 'gitlab-preview-review';
  // 編集中の文書と、?doc= で開いたファイルを混同しない。
  const documentKey = preview.doc === null ? 'current' : 'file:' + preview.doc;
  const plain = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
  const integer = (value) => Number.isSafeInteger(value) && value >= 0;
  const boundaryOK = (value) => plain(value) && typeof value.tag === 'string' && /^[a-z0-9]+$/.test(value.tag) &&
    typeof value.range === 'string' && targets.rangePattern.test(value.range);
  function validTarget(value) {
    if (!plain(value)) return null;
    if (value.kind === 'block' && boundaryOK(value)) return { kind: 'block', tag: value.tag, range: value.range };
    if (value.kind === 'cell' && [value.table, value.row, value.col].every(integer))
      return { kind: 'cell', table: value.table, row: value.row, col: value.col };
    if (value.kind !== 'html' || ['prev', 'next'].some((key) => Object.hasOwn(value, key) && !boundaryOK(value[key]))) return null;
    const target = { kind: 'html' };
    for (const key of ['prev', 'next']) if (value[key]) target[key] = { tag: value[key].tag, range: value[key].range };
    return target;
  }
  // サーバー（internal/server/review.go）と同じく、タブと改行のほかの制御文字を受けない。
  const controlPattern = /[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/;
  const length = (value) => Array.from(value).length;
  const textOK = (value) => typeof value === 'string' && value.trim() !== '' && length(value) <= 2000 && !controlPattern.test(value);
  const shortOK = (value, max) => typeof value === 'string' && length(value) <= max && !controlPattern.test(value);
  // 1 つの文書の分を読む。形が違えば、その文書の分だけを捨てる（ほかの文書のコメントは残す）。
  function readItems(items) {
    if (!Array.isArray(items) || items.length > 200) return null;
    const seen = new Set();
    const list = [];
    for (const item of items) {
      const target = validTarget(item?.target);
      if (!plain(item) || typeof item.id !== 'string' || !/^[0-9A-Za-z-]{1,64}$/.test(item.id) || seen.has(item.id) || !target ||
        !shortOK(item.label, 200) || !textOK(item.text) || !integer(item.render) || item.render === 0 ||
        !['pending', 'copied'].includes(item.state) || typeof item.stale !== 'boolean' || !shortOK(item.error, 500) ||
        (item.selection !== undefined && !textOK(item.selection)))
        return null;
      seen.add(item.id);
      list.push({ id: item.id, target, label: item.label, text: item.text, render: item.render,
        state: item.state, stale: item.stale, error: item.error, ...(item.selection ? { selection: item.selection } : {}) });
    }
    return list;
  }
  function readStorage() {
    const empty = () => ({ version: 1, docs: Object.create(null) });
    try {
      const saved = JSON.parse(sessionStorage.getItem(storageKey));
      if (!plain(saved) || saved.version !== 1 || !plain(saved.docs)) return empty();
      const result = empty();
      for (const [key, items] of Object.entries(saved.docs).slice(0, 100)) {
        if (!shortOK(key, 2100)) continue;
        const list = readItems(items);
        if (list) result.docs[key] = list;
      }
      return result;
    } catch { return empty(); }
  }
  const saved = readStorage();
  let comments = saved.docs[documentKey] || [];
  let selected = null;
  let editing = null;
  let mode = false;
  let stopped = false;
  let models = [];
  let byCell = new WeakMap();
  let infoRender = 0;
  let observedRender = preview.current()?.render;
  let tableNumber = 0;
  let tableFetching = null;
  let prepareNumber = 0;
  let preparing = null;
  let prepared = null;
  let copying = false;
  let hoverPoint = null;
  let frame = 0;
  const allowed = () => !preview.config.list;
  const locked = () => stopped || Boolean(preparing) || copying;
  const sameTarget = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  const signature = (item) => JSON.stringify([item.target, item.selection, item.text]);
  const url = (path) => {
    const target = new URL('_/review/' + path, preview.base);
    if (preview.doc !== null) target.searchParams.set('doc', preview.doc);
    return target;
  };
  const messageOf = (data) => typeof data?.error?.message === 'string' ? data.error.message : '';
  function notice(text) {
    status.textContent = text;
    status.hidden = !text;
  }
  function save() {
    saved.docs[documentKey] = comments;
    try { sessionStorage.setItem(storageKey, JSON.stringify(saved)); } catch { /* 保存できなくてもメモリーで続ける。 */ }
  }
  function discardPrompt() {
    prepared = null;
    promptInput.value = '';
    output.hidden = true;
  }
  function changed() {
    discardPrompt();
    save();
    draw();
  }
  function draw() {
    toggle.hidden = !allowed();
    toggle.disabled = stopped;
    toggle.textContent = 'コメント' + (comments.length ? ' ' + comments.length : '');
    title.textContent = 'コメント（' + comments.length + '）';
    panel.hidden = !mode;
    layer.hidden = !mode;
    chosen.textContent = selected?.label || '本文のブロックか表のセルを選んでください';
    quote.textContent = selected?.selection ? '選んだ文字: ' + selected.selection : '';
    quote.hidden = !selected?.selection;
    input.disabled = locked();
    // 直している間に別の対象を選ぶと、そのコメントの対象を変える。黙って変えないよう文言に出す。
    const original = editing && comments.find((item) => item.id === editing);
    add.textContent = !editing ? '追加' : original && selected && !sameTarget(original.target, selected.target) ? '保存（対象を変える）' : '保存';
    const reason = stopped ? 'プレビューが終了しています' : preparing || copying ? '処理が終わるまでお待ちください' :
      !selected ? '本文のブロックか表のセルを選んでください' : !input.value.trim() ? 'コメントを入力してください' : '';
    add.disabled = Boolean(reason);
    add.title = reason;
    prepare.disabled = locked() || !preview.current()?.render || !comments.some((item) => item.state === 'pending');
    clearCopied.disabled = locked() || !comments.some((item) => item.state === 'copied');
    copy.disabled = locked() || !prepared;
    output.hidden = !prepared;
    cards.replaceChildren();
    for (const item of comments) {
      const card = make('article', '', cards);
      card.className = 'gp-review-card';
      card.dataset.id = item.id;
      make('h3', '', card, item.label);
      if (item.selection) make('p', '', card, '選んだ文字: ' + item.selection);
      make('p', '', card, item.text);
      const state = make('p', '', card, item.error ? '誤り: ' + item.error : item.stale ? '要確認' : item.state === 'copied' ? 'コピー済み' : '未コピー');
      state.className = 'gp-review-state';
      const buttons = make('div', '', card);
      const fix = button('', buttons, '直す', () => {
        editing = item.id;
        selected = { target: item.target, label: item.label, ...(item.selection ? { selection: item.selection } : {}) };
        input.value = item.text;
        draw();
        refreshFrames();
        input.focus();
      });
      const remove = button('', buttons, '消す', () => {
        comments = comments.filter((other) => other.id !== item.id);
        if (editing === item.id) editing = null;
        changed();
      });
      const move = button('', buttons, '本文へ移る', () => {
        const elements = elementsOf(item.target);
        if (!elements.length) { notice('コメントの対象が見つかりません。本文を確認して対象を選び直してください。'); return; }
        selected = { target: item.target, label: item.label, ...(item.selection ? { selection: item.selection } : {}) };
        elements[0].scrollIntoView({ block: 'center', inline: 'nearest' });
        draw();
        refreshFrames();
      });
      fix.disabled = remove.disabled = move.disabled = locked();
    }
  }
  function elementsOf(target) {
    if (target.kind === 'block') {
      const element = preview.findSource(target.tag, target.range);
      return element ? [element] : [];
    }
    if (target.kind === 'html') return preview.findHTML({ tag: 'html', ...target });
    if (infoRender !== preview.current()?.render) return [];
    const model = models[target.table];
    const cell = model?.cells.find((item) => item.row === target.row && item.col === target.col);
    return cell ? [cell.element] : [];
  }
  const shortText = (text) => {
    const letters = Array.from(text.trim().replace(/\s+/g, ' '));
    return letters.slice(0, 40).join('') + (letters.length > 40 ? '…' : '');
  };
  const blockLabels = { p: '段落', li: 'リストの項目', ul: 'リスト', ol: 'リスト', dl: '説明リスト',
    dt: '用語', dd: '説明', blockquote: '引用', pre: 'コード', hr: '水平線', div: '注記', img: '図' };
  // 表の固定の全幅表示は、表を本文の外（body の直下の欄）へ移す。対応づけたセルなら本文の外でも受ける。
  const cellOutside = (element) => Boolean(element?.closest && byCell.get(element.closest('td, th')) && infoRender === preview.current()?.render);
  const inBody = (element) => Boolean(element?.closest) && (content.contains(element) || cellOutside(element));
  function targetOf(element) {
    if (!inBody(element)) return null;
    const table = element.closest('table');
    if (table) {
      const cell = byCell.get(element.closest('td, th'));
      if (!cell || infoRender !== preview.current()?.render) return null;
      return { target: { kind: 'cell', table: cell.table, row: cell.row, col: cell.col },
        label: '表 ' + (cell.table + 1) + ' の ' + (cell.row + 1) + ' 行 ' + (cell.col + 1) + ' 列' };
    }
    const block = targets.blockOf(element);
    if (block) {
      const source = preview.sourceOf(block);
      if (!source) return null;
      const kind = /^h[1-6]$/.test(source.tag) ? '見出し' : blockLabels[source.tag] || 'ブロック';
      return { target: { kind: 'block', tag: source.tag, range: source.range }, label: kind + ': ' + shortText(block.textContent) };
    }
    const raw = targets.rawBlockOf(element);
    if (!raw) return null;
    const target = { kind: 'html' };
    for (const key of ['prev', 'next']) if (raw[key]) target[key] = { ...raw[key] };
    return { target, label: 'HTML のブロック' };
  }
  const ignored = (element) => !element?.closest || window.__mdTableSticky?.picking ||
    element.closest('#gp-controls, #gp-header, #gp-notices, #gp-outline, #gp-review-panel, #gp-review-layer, #gp-review-status, [data-md-table-sticky-bar], [data-md-table-sticky-search], [data-md-table-sticky-columns]') ||
    (!element.closest('td, th') && element.closest('input, textarea, select, button, [contenteditable]'));
  function selectTarget(point) {
    selected = point;
    draw();
    refreshFrames();
  }
  function addComment() {
    if (locked() || !selected) return;
    const text = input.value.trim();
    if (!text) return;
    if (Array.from(text).length > 2000) { notice('コメントが長すぎます（2,000 文字まで）'); return; }
    if (controlPattern.test(text)) { notice('コメントに使えない制御文字が入っています。'); return; }
    if (comments.filter((item) => item.state === 'pending' && item.id !== editing).length >= 50) {
      notice('一度に依頼できるコメントは 50 件までです。依頼文をコピーしてから追加してください。'); return;
    }
    const item = { id: editing || crypto.randomUUID(), target: selected.target, label: selected.label,
      ...(selected.selection ? { selection: selected.selection } : {}), text, render: preview.current().render,
      state: 'pending', stale: false, error: '' };
    const index = comments.findIndex((other) => other.id === editing);
    if (index >= 0) comments[index] = item;
    else comments.push(item);
    editing = null;
    input.value = '';
    changed();
    notice(index >= 0 ? 'コメントを保存しました。' : 'コメントを追加しました。');
  }
  input.addEventListener('input', draw);
  input.addEventListener('keydown', (event) => {
    if (event.isComposing || event.keyCode === 229) return;
    if (event.ctrlKey && event.key === 'Enter') { event.preventDefault(); addComment(); }
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      editing = null;
      selectTarget(null);
    }
  });
  function setMode(want) {
    if (want && (!allowed() || stopped)) return;
    if (want && window.gpEditMode?.busy()) {
      notice('編集を確定するか取り消してから切り替えてください'); return;
    }
    if (want) window.gpEditMode?.exit();
    mode = Boolean(want);
    toggle.setAttribute('aria-pressed', String(mode));
    if (mode) root.dataset.review = 'on';
    else delete root.dataset.review;
    preview.setEditing(mode);
    preview.holdScroll(mode);
    hoverPoint = null;
    draw();
    refreshFrames();
    if (mode) void loadTables();
  }
  window.gpReviewMode = { active: () => mode, exit: () => setMode(false) };
  toggle.addEventListener('click', () => setMode(!mode));

  // 本文に印を足さず、子の番号だけで枠を当てる。印刷時には規則も無効にする。
  function styleFrame(style, point, selectedFrame) {
    const selectors = [];
    if (mode && !stopped && point && point.target.kind !== 'cell') for (let element of elementsOf(point.target)) {
      const path = [];
      while (element && element !== content) {
        path.unshift(':nth-child(' + (Array.prototype.indexOf.call(element.parentElement.children, element) + 1) + ')');
        element = element.parentElement;
      }
      if (element && path.length) selectors.push('#gp-content > ' + path.join(' > '));
    }
    const css = selectors.length ? '@media screen { ' + selectors.join(',\n') +
      ' { outline: ' + (selectedFrame ? '2px solid var(--gp-fg)' : '1px dashed var(--gp-border)') + '; outline-offset: 2px; } }' : '';
    if (style.textContent !== css) style.textContent = css;
  }
  function boxFrame(box, point) {
    const element = mode && !stopped && point?.target.kind === 'cell' && elementsOf(point.target)[0];
    box.hidden = !element;
    if (!element) return;
    const rect = element.getBoundingClientRect();
    Object.assign(box.style, { left: rect.left + 'px', top: rect.top + 'px', width: rect.width + 'px', height: rect.height + 'px' });
  }
  function refreshFrames() {
    if (frame) return;
    frame = requestAnimationFrame(() => {
      frame = 0;
      const element = mode && hoverPoint ? document.elementFromPoint(hoverPoint.x, hoverPoint.y) : null;
      const hovered = !ignored(element) ? targetOf(element) : null;
      styleFrame(hoverStyle, hovered, false);
      styleFrame(selectedStyle, selected, true);
      boxFrame(hoverCell, hovered);
      boxFrame(selectedCell, selected);
    });
  }
  window.addEventListener('pointermove', (event) => {
    if (!mode) return;
    hoverPoint = { x: event.clientX, y: event.clientY };
    refreshFrames();
  }, true);
  window.addEventListener('pointerout', (event) => {
    if (!event.relatedTarget) { hoverPoint = null; refreshFrames(); }
  }, true);
  window.addEventListener('blur', () => { hoverPoint = null; refreshFrames(); });
  window.addEventListener('scroll', refreshFrames, true);
  window.addEventListener('resize', refreshFrames);
  new MutationObserver(refreshFrames).observe(content, { childList: true, subtree: true });

  // Element の境では、選択に含まれる側の子を引く。終端は含まない。
  function selectionElement(node, offset, end) {
    if (node.nodeType === Node.TEXT_NODE) return node.parentElement;
    const child = node.childNodes[end ? offset - 1 : offset];
    return child?.nodeType === Node.TEXT_NODE ? child.parentElement : child || node;
  }
  function readSelection() {
    const selection = window.getSelection();
    if (!selection?.rangeCount || selection.isCollapsed) return false;
    const range = selection.getRangeAt(0);
    const startsInside = content.contains(range.startContainer), endsInside = content.contains(range.endContainer);
    if (!startsInside && !endsInside) return false;
    const first = startsInside && targetOf(selectionElement(range.startContainer, range.startOffset, false));
    const last = endsInside && targetOf(selectionElement(range.endContainer, range.endOffset, true));
    // 表示の文字の引用なので、サーバーが受けない制御文字は落とす（タブと改行は残す）。
    const text = selection.toString().replace(new RegExp(controlPattern.source, 'g'), '').trim();
    if (!text) return false;
    if (!first || !last || !sameTarget(first.target, last.target)) {
      if (selected) { delete selected.selection; draw(); }
      notice('選んだ文字が 2 つ以上の対象にまたがっています。1 つのブロックかセルの中で選んでください。');
      return true;
    }
    selectTarget(first);
    if (Array.from(text).length > 2000) notice('選んだ文字が長すぎます（2,000 文字まで）');
    else { selected.selection = text; draw(); }
    return true;
  }
  document.addEventListener('selectionchange', () => { if (mode && !stopped) readSelection(); });
  window.addEventListener('click', (event) => {
    if (!mode || stopped || ignored(event.target) || !inBody(event.target)) return;
    event.preventDefault();
    event.stopPropagation();
    if (readSelection()) return;
    const point = targetOf(event.target);
    selectTarget(point);
    if (point) { notice(''); return; }
    const model = models.find((item) => item.table === event.target.closest('table'));
    notice(model?.reason || 'ここにはコメントを付けられません');
  }, true);
  window.addEventListener('dblclick', (event) => {
    if (!mode || stopped || ignored(event.target) || !inBody(event.target)) return;
    event.preventDefault();
    event.stopPropagation();
  }, true);

  function checkTargets(cells = false) {
    let changed = false;
    for (const item of comments) {
      if ((item.target.kind === 'cell') !== cells || item.stale || elementsOf(item.target).length) continue;
      item.stale = true;
      changed = true;
    }
    if (changed) { save(); draw(); }
  }
  async function loadTables() {
    tableFetching?.abort();
    tableFetching = null;
    const number = ++tableNumber;
    const render = preview.current()?.render;
    if (stopped || !allowed() || !render) return;
    const controller = new AbortController();
    tableFetching = controller;
    const request = url('tables');
    request.searchParams.set('render', render);
    try {
      const response = await fetch(request, { signal: controller.signal });
      const data = await response.json();
      if (number !== tableNumber || render !== preview.current()?.render || stopped) return;
      if (!response.ok) throw new Error(messageOf(data) || '表の情報を読み込めませんでした');
      const tables = preview.tables();
      if (data.render !== render || !Array.isArray(data.tables) || data.tables.length !== tables.length ||
        data.tables.some((info, index) => info.index !== index)) throw new Error('画面と原文の表の数が合いません');
      byCell = new WeakMap();
      models = data.tables.map((info, index) => {
        const table = tables[index];
        const nested = table.querySelector('table') || table.parentElement.closest('table');
        const shape = info.editable && data.document.editable && !nested ? targets.grid(table, info) : null;
        const reason = !data.document.editable ? data.document.reason : nested ? '入れ子の表' :
          info.reason || (!shape ? '画面と原文のセルの形が違います' : '');
        for (const cell of shape?.cells || []) byCell.set(cell.element, cell);
        return { table, cells: shape?.cells || [], reason };
      });
      infoRender = render;
      layer.dataset.render = String(render);
      checkTargets(true);
      refreshFrames();
    } catch (error) {
      if (number !== tableNumber || error.name === 'AbortError') return;
      models = [];
      byCell = new WeakMap();
      infoRender = 0;
      // 表とセルを確かめられないので、セルのコメントは要確認にする。
      checkTargets(true);
      if (mode) notice(error.message);
    } finally {
      if (number === tableNumber) tableFetching = null;
    }
  }
  async function preparePrompt() {
    if (locked()) return;
    const sent = comments.filter((item) => item.state === 'pending');
    if (!sent.length) return;
    discardPrompt();
    const render = preview.current()?.render;
    const number = ++prepareNumber;
    const controller = new AbortController();
    preparing = controller;
    draw();
    notice('依頼文を作っています。');
    try {
      const response = await fetch(url('prepare'), { method: 'POST', signal: controller.signal,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ render, comments: sent.map((item) => ({ target: item.target,
          ...(item.selection ? { selection: item.selection } : {}), text: item.text })) }) });
      const data = await response.json();
      if (number !== prepareNumber || stopped || render !== preview.current()?.render) return;
      if (!response.ok) {
        const message = messageOf(data) || '依頼文を作れませんでした';
        // 前の失敗の印は消し、今回断られた 1 件（番号があるときだけ）に付け直す。
        for (const item of sent) item.error = '';
        if (integer(data?.error?.index) && sent[data.error.index]) sent[data.error.index].error = message;
        save();
        throw new Error(message);
      }
      if (typeof data.prompt !== 'string' || !integer(data.bytes)) throw new Error('依頼文の応答の形が正しくありません');
      for (const item of sent) { item.stale = false; item.error = ''; }
      prepared = { prompt: data.prompt, render, sent: sent.map((item) => ({ id: item.id, signature: signature(item) })) };
      promptInput.value = data.prompt;
      bytes.textContent = data.bytes.toLocaleString('ja-JP') + ' バイト';
      save();
      notice('依頼文を作りました。内容を確認してコピーしてください。');
    } catch (error) {
      if (number === prepareNumber && error.name !== 'AbortError') notice(error.message);
    } finally {
      if (number === prepareNumber) { preparing = null; draw(); }
    }
  }
  async function copyPrompt() {
    if (locked() || !prepared) return;
    const snapshot = prepared;
    copying = true;
    draw();
    try {
      // 利用者のクリックの中で直接呼ぶ。許可を別の操作へ持ち越さない。
      await navigator.clipboard.writeText(snapshot.prompt);
      if (prepared !== snapshot || stopped) return;
      for (const sent of snapshot.sent) {
        const item = comments.find((item) => item.id === sent.id && signature(item) === sent.signature);
        if (item) item.state = 'copied';
      }
      save();
      notice('依頼文をコピーしました。');
    } catch {
      if (prepared !== snapshot || stopped) return;
      promptInput.focus();
      promptInput.select();
      notice('コピーできませんでした。依頼文を選んだので、Ctrl+C でコピーしてください。');
    } finally { copying = false; draw(); }
  }
  document.addEventListener('gp:render', () => {
    const render = preview.current()?.render;
    stopped = false;
    if (render !== observedRender) {
      prepareNumber++;
      preparing?.abort();
      preparing = null;
      discardPrompt();
      checkTargets();
    }
    observedRender = render;
    if (!allowed()) setMode(false);
    draw();
    refreshFrames();
    void loadTables();
  });
  document.addEventListener('gp:stop', () => {
    stopped = true;
    tableNumber++;
    tableFetching?.abort();
    prepareNumber++;
    preparing?.abort();
    preparing = null;
    discardPrompt();
    draw();
    refreshFrames();
    notice('プレビューが終了しました。');
  });
  // モードの開閉は外観へ保存せず、毎回閉じて始める。
  close.title = 'コメントの入力中の文は、このページを開いている間は残ります';
  output.hidden = panel.hidden = layer.hidden = status.hidden = true;
  draw();
  if (observedRender) { checkTargets(); void loadTables(); }
})();
