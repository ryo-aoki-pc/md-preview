// 選択枠と入力欄を本文の上へ重ね、表とブロックの原文を編集する。
'use strict';

(() => {
  const preview = window.gpPreview;
  const config = preview?.config.edit;
  if (!config) return;
  const editGrid = window.gpEditGrid;

  const root = document.documentElement;
  const toggle = document.getElementById('gp-edit-toggle');
  const addButton = document.getElementById('gp-edit-add');
  const layer = document.createElement('div');
  layer.id = 'gp-edit-layer';
  const make = (tag, id, parent = layer) => {
    const element = document.createElement(tag);
    element.id = id;
    parent.append(element);
    return element;
  };
  const cursorStyle = make('style', 'gp-edit-cursor');
  const hoverStyle = make('style', 'gp-edit-hover');
  const frame = make('div', 'gp-edit-selection');
  const rangeFrame = make('div', 'gp-edit-range');
  const rangePieces = Array.from({ length: 4 }, (_, i) => {
    const piece = make('div', 'gp-edit-range-' + i, rangeFrame);
    piece.className = 'gp-edit-range';
    piece.hidden = true;
    return piece;
  });
  const cutFrame = make('div', 'gp-edit-cut');
  cutFrame.hidden = true;
  const cutPieces = Array.from({ length: 4 }, (_, i) => {
    const piece = make('div', 'gp-edit-cut-' + i, cutFrame);
    piece.className = 'gp-edit-cut';
    piece.hidden = true;
    return piece;
  });
  const input = make('textarea', 'gp-edit-input');
  input.setAttribute('aria-label', '表のセルの編集');
  input.spellcheck = false;
  const sourceLabel = make('span', 'gp-edit-source-label');
  sourceLabel.textContent = '原文';
  const blockBar = make('div', 'gp-edit-block-actions');
  blockBar.setAttribute('role', 'toolbar');
  blockBar.setAttribute('aria-label', 'ブロックの編集対象');
  const outerButton = make('button', 'gp-edit-outer', blockBar);
  outerButton.type = 'button';
  outerButton.title = 'Alt+↑ で外側を編集';
  const innerButton = make('button', 'gp-edit-inner', blockBar);
  innerButton.type = 'button';
  innerButton.textContent = '内側に戻る';
  innerButton.title = 'Alt+↓ で内側に戻る';
  const beforeButton = make('button', 'gp-edit-before', blockBar);
  beforeButton.type = 'button';
  beforeButton.textContent = '前に追加';
  const afterButton = make('button', 'gp-edit-after', blockBar);
  afterButton.type = 'button';
  afterButton.textContent = '後ろに追加';
  const insertTableButton = make('button', 'gp-edit-insert-table', blockBar);
  insertTableButton.type = 'button';
  insertTableButton.textContent = '表を入れる…';
  insertTableButton.setAttribute('aria-haspopup', 'dialog');
  insertTableButton.setAttribute('aria-controls', 'gp-edit-table-picker');
  insertTableButton.setAttribute('aria-expanded', 'false');
  insertTableButton.hidden = true;
  layer.append(document.getElementById('gp-edit-table-template').content.cloneNode(true));
  const tablePicker = layer.querySelector('#gp-edit-table-picker');
  const tableCols = layer.querySelector('#gp-edit-table-cols');
  const tableRows = layer.querySelector('#gp-edit-table-rows');
  let tableCaret = null;
  const bar = make('div', 'gp-edit-actions');
  bar.setAttribute('role', 'toolbar');
  bar.setAttribute('aria-label', '表の編集');
  const status = make('div', 'gp-edit-status');
  status.setAttribute('role', 'status');
  status.setAttribute('aria-live', 'polite');
  document.body.append(layer);

  let mode = false;
  let stopped = false;
  let selected = null;
  // 表の範囲は、ブロックの原文の位置（point.range）と分けて持つ。
  let tableSelection = null;
  let origin = '';
  let lastCopy = null;
  let cutState = null;
  let pendingInsert = null;
  let clipboardIntent = null;
  let editor = null;
  let composing = false;
  let models = [];
  let byCell = new WeakMap();
  let infoRender = 0;
  let fetching = null;
  let requestNumber = 0;
  let animation = 0;
  let noticeTimer = 0;
  let busy = null;
  let failed = null;
  let externalVersion = 0;
  let late = null;
  let documentEditable = false;
  let documentReason = '';
  // 空の判定はサーバーの原文で行う（参照の定義やコメントだけの文書は空ではなく、末尾へ追加する）。
  let documentEmpty = false;
  let documentStart = 1;
  let pendingBlock = null;
  let sourceFetching = null;
  let sourceNumber = 0;
  let observedRender = preview.current()?.render;
  const queue = [];
  const drafts = new Map();
  const buttons = new Map();
  const menus = new Map();
  const menuItems = [];
  const openMenus = [];
  const blockPoint = (point) => Boolean(point?.range || point?.document);
  const keyOf = (point) => point.document ? 'insert:document:' + point.document : point.range ? (point.insert ? 'insert:' + point.insert + ':' : 'range:') + (point.html ? JSON.stringify(point.html) : point.range) : [point.table, point.row, point.col].join(':');
  const samePoint = (a, b) => a && b && keyOf(a) === keyOf(b);
  const allowed = () => !preview.config.list &&
    (config.scope === 'all' || (config.scope === 'current' && preview.doc === null &&
      (preview.current()?.currentDocument ?? preview.config.currentDocument) !== false));
  const url = (path) => {
    const target = new URL('_/edit' + path, preview.base);
    if (preview.doc !== null) target.searchParams.set('doc', preview.doc);
    return target;
  };

  // 誤りの応答は {"error": {"code", "message"}}。文は利用者の文字を含みうるので、textContent だけに入れる。
  const messageOf = (data) => (typeof data?.error?.message === 'string' ? data.error.message : '');
  const { rangePattern, blockOf, findBlock, rawBlockOf, grid } = window.gpEditTargets;
  const outerLabels = { list: 'リスト全体を編集', quote: '引用全体を編集', deflist: '説明リスト全体を編集',
    alert: '注記全体を編集', item: '項目全体を編集', description: '説明全体を編集', html: 'HTML のブロック全体を編集' };
  function outerElement(element, target) {
    if (target?.tag === 'html') return findBlock(preview.htmlOf(element));
    if (!target || typeof target.tag !== 'string' || !rangePattern.test(target.range || '')) return null;
    const live = preview.findSource(target.tag, target.range);
    return live && live !== element && live.contains(element) && blockOf(live) === live ? live : null;
  }
  function moveBlock(outside) {
    if (!blockPoint(editor?.point) || editor.insert || composing || sourceFetching) return;
    const inner = editor.inner || [];
    // 描き直しで画面から外れた内側の要素は飛ばす。
    const rest = inner.filter(findBlock);
    const target = outside ? null : rest.pop();
    const element = outside ? outerElement(editor.element, editor.outer?.[0]) : findBlock(target);
    if (!element?.isConnected) {
      if (!outside) { editor.inner = []; innerButton.hidden = true; }
      return;
    }
    beginBlock(element, { preserve: true, inner: outside ? [...rest, editor.target] : rest,
      target: outside && editor.outer?.[0]?.tag === 'html' ? preview.htmlOf(editor.element) : target });
  }
  outerButton.addEventListener('click', () => moveBlock(true));
  innerButton.addEventListener('click', () => moveBlock(false));
  function beginInsert(side) {
    if (!editor?.anchor || editor.insert || composing || sourceFetching) return;
    const anchor = editor.anchor;
    const target = anchor.tag === 'html' ? editor.target : { tag: anchor.tag, range: anchor.range };
    const element = findBlock(target);
    if (!element?.isConnected) return;
    saveDraft();
    editor = null;
    openBlock({ point: { range: anchor.actual, insert: side, ...(target.tag === 'html' ? { html: target } : {}) }, insert: { anchor: anchor.actual, side },
      anchor, element, target, render: preview.current().render,
      version: externalVersion, original: '', value: '', outer: [], inner: [], previousHTML: preview.current().html });
  }
  beforeButton.addEventListener('click', () => beginInsert('before'));
  afterButton.addEventListener('click', () => beginInsert('after'));
  function documentInsertReason() {
    if (!mode || !allowed()) return '編集モードで操作してください';
    if (stopped) return 'プレビューが止まっています';
    if (infoRender !== preview.current()?.render) return '文書の情報を読み込んでいます';
    if (!documentEditable) return documentReason || 'この文書は編集できません';
    if (busy || queue.length || sourceFetching) return '前の書き込みの反映を待っています';
    if (composing) return '文字の入力を終えてから操作してください';
    return '';
  }
  function beginDocumentInsert(side) {
    const reason = documentInsertReason();
    if (reason) { notice(reason, true); return; }
    operationStart();
    clearSelection();
    const point = { document: side }, saved = drafts.get(keyOf(point));
    drafts.delete(keyOf(point));
    openBlock({ point, insert: { document: side }, element: document.getElementById('gp-content'),
      startCut: documentStart,
      render: preview.current().render, version: externalVersion, original: '',
      value: saved?.version === externalVersion ? saved.value : '', outer: [], inner: [], previousHTML: preview.current().html });
  }
  function beginTableInsert(side) {
    const model = models[selected?.table];
    const actual = model?.info.anchor;
    if (!rangePattern.test(actual || '')) return;
    const anchor = { tag: 'table', range: preview.sourceOf(model.table)?.range || actual, actual };
    operationStart();
    if (editor) endEdit(true);
    if (busy || queue.length) return;
    clearSelection();
    const point = { range: actual, insert: side }, saved = drafts.get(keyOf(point));
    drafts.delete(keyOf(point));
    openBlock({ point, insert: { anchor: actual, side }, anchor, tableInsert: true, tableIndex: model.info.index, element: model.table,
      target: { tag: 'table', range: anchor.range }, render: preview.current().render,
      version: externalVersion, original: '', value: saved?.version === externalVersion ? saved.value : '',
      outer: [], inner: [], previousHTML: preview.current().html });
  }
  function closeTablePicker(restore = false) {
    tablePicker.hidden = true;
    insertTableButton.setAttribute('aria-expanded', 'false');
    if (restore && editor?.insert) {
      focusInput();
      if (tableCaret) input.setSelectionRange(tableCaret.start, tableCaret.end);
    }
    tableCaret = null;
  }
  insertTableButton.addEventListener('click', () => {
    if (!editor?.insert || composing) return;
    if (!tablePicker.hidden) { closeTablePicker(true); return; }
    tableCaret = { start: input.selectionStart, end: input.selectionEnd };
    tablePicker.hidden = false;
    insertTableButton.setAttribute('aria-expanded', 'true');
    cancelAnimationFrame(animation);
    position();
    tableCols.focus({ preventScroll: true });
    tableCols.select();
  });
  layer.querySelector('#gp-edit-table-cancel').addEventListener('click', () => closeTablePicker(true));
  tablePicker.addEventListener('submit', (event) => {
    event.preventDefault();
    if (!editor?.insert || !tableCaret || !tablePicker.reportValidity()) return;
    const { start, end } = tableCaret;
    let value;
    try { value = editGrid.emptyPipeTable(tableCols.valueAsNumber, tableRows.valueAsNumber); }
    catch (error) { notice(error.message, true); return; }
    const left = input.value.slice(0, start), right = input.value.slice(end);
    const gap = (text, before) => {
      if (!text || (before ? /\n[ \t]*\n[ \t]*$/ : /^[ \t]*\n[ \t]*\n/).test(text)) return '';
      return (before ? /\n[ \t]*$/ : /^[ \t]*\n/).test(text) ? '\n' : '\n\n';
    };
    value = gap(left, true) + value + gap(right, false);
    input.setRangeText(value, start, end, 'end');
    editor.resize = true;
    saveDraft();
    closeTablePicker();
    focusInput();
  });
  addButton.addEventListener('click', () => {
    if (documentEmpty) beginDocumentInsert('end');
    else if (addButton.getAttribute('aria-expanded') === 'true') closeMenus();
    else openMenu('document', addButton);
  });
  function checkboxOf(element) {
    if (!element?.matches?.('input.task-list-item-checkbox[type="checkbox"][data-checkbox-sourcepos]:not([data-inapplicable])') ||
      !rangePattern.test(element.getAttribute('data-checkbox-sourcepos'))) return null;
    const table = element.closest('table');
    if (table) return element.closest('td, th') && models.some((model) => model.table === table && model.editable) ? element : null;
    return element.closest('#gp-content') ? element : null;
  }
  // セルの更新では子が入れ替わるので、表の中はセル内の番号、外は同じ親の同じ位置を引き継ぐ。
  function checkboxPosition(element) {
    const cell = element.closest('table') && element.closest('td, th');
    return cell ? { cell, ordinal: Array.prototype.indexOf.call(cell.querySelectorAll('input.task-list-item-checkbox'), element) }
      : { parent: element.parentElement, index: Array.prototype.indexOf.call(element.parentElement.children, element) };
  }
  function checkboxAt(position) {
    return position?.cell ? position.cell.isConnected && position.cell.querySelectorAll('input.task-list-item-checkbox')[position.ordinal]
      : position?.parent?.isConnected && position.parent.children[position.index];
  }
  function checkboxElement(item) {
    if (!item.checkbox || item.checkbox.isConnected) return item.checkbox;
    const element = checkboxAt(item.checkboxPosition);
    if (checkboxOf(element)) item.checkbox = element;
    return item.checkbox;
  }
  // 属性を変えても差分更新の節点の対応は保つ。描画が届いたら disabled と checked を当て直す。
  function updateCheckboxes(reset = false) {
    const pendingItems = [...(busy ? [busy.item] : []), ...queue];
    for (const item of pendingItems) checkboxElement(item);
    const elements = new Set(document.querySelectorAll('#gp-content input.task-list-item-checkbox'));
    // 全幅表示では本文の外へ移る。対応を失った表も、プレビューが持つ表から無効に戻す。
    for (const table of preview.tables()) if (!table.closest('#gp-content'))
      for (const element of table.querySelectorAll('input.task-list-item-checkbox')) elements.add(element);
    for (const element of elements) {
      element.disabled = !(checkboxOf(element) && mode && !stopped && documentEditable && infoRender === preview.current()?.render);
      if (reset) {
        const pending = pendingItems.slice().reverse().find((item) => item.checkbox === element);
        element.checked = pending ? pending.checked : element.hasAttribute('checked');
      }
    }
    // 切り替えたチェックボックスは、描き直しで別の要素になる。フォーカスがあったなら、また押せるようになったときに戻す。
    if (refocus && mode && infoRender === preview.current()?.render) {
      const element = checkboxAt(refocus);
      if (checkboxOf(element) && !element.disabled && (document.activeElement === document.body || !document.activeElement?.isConnected))
        element.focus({ preventScroll: true });
      refocus = null;
    }
  }
  let refocus = null;
  function cancelSource() {
    sourceNumber++;
    sourceFetching?.abort();
    sourceFetching = pendingBlock = null;
  }

  function notice(text, error = false) {
    clearTimeout(noticeTimer);
    status.textContent = text;
    status.hidden = !text;
    status.dataset.kind = error === 'wait' ? 'wait' : error ? 'error' : 'info';
    if (text && !error) noticeTimer = setTimeout(() => { status.hidden = true; }, 5000);
  }
  function operationStart() {
    if (status.dataset.kind === 'error') notice('');
  }

  function cancelInsert() {
    pendingInsert = null;
    if (status.dataset.kind === 'wait') notice('');
  }
  function cancelCut() {
    if (cutState) cutState.active = false;
    cutFrame.hidden = true;
  }
  function checkInsert() {
    if (pendingInsert && (pendingInsert.version !== externalVersion ||
      models[pendingInsert.table]?.table !== pendingInsert.tableElement)) cancelInsert();
  }

  // 本文には識別用の属性も足さない。番号だけで作ったセレクターを層の style に置く。
  function updateCursors() {
    const selectors = [];
    if (mode && !window.__mdTableSticky?.picking) for (const model of models) {
      if (!model?.editable || !model.table.isConnected) continue;
      let element = model.table;
      const path = [];
      while (element && !element.matches('.md')) {
        path.unshift(element.localName + ':nth-child(' +
          (Array.prototype.indexOf.call(element.parentElement.children, element) + 1) + ')');
        element = element.parentElement;
      }
      if (element) selectors.push(":root[data-edit='on'] .md > " + path.join(' > ') + ' :is(td, th)');
    }
    // pointerover のたびに呼ばれる。同じ内容を入れ直してスタイルを計算し直させない。
    const css = selectors.length ? selectors.join(',\n') + ' { cursor: cell; }' : '';
    if (cursorStyle.textContent !== css) cursorStyle.textContent = css;
  }

  function find(point) {
    const model = models[point?.table];
    if (!model?.editable) return null;
    const row = Math.max(0, Math.min(model.info.rows - 1, point.row));
    const col = Math.max(0, Math.min(model.info.cols - 1, point.col));
    return model.slots[row]?.[col] || model.cells.reduce((nearest, cell) =>
      !nearest || Math.abs(cell.row - row) + Math.abs(cell.col - col) <
        Math.abs(nearest.row - row) + Math.abs(nearest.col - col) ? cell : nearest, null);
  }
  function pointOf(cell) { return { table: cell.table, row: cell.row, col: cell.col }; }
  function selectionCell() { return selected && find(selected); }

  function selectionRect() {
    const model = models[selected?.table];
    return model && selected ? editGrid.rectOf(model, tableSelection || { anchor: selected, focus: selected }) : null;
  }
  function updateRange() {
    const rect = selectionRect(), model = models[selected?.table];
    rangeFrame.hidden = !tableSelection || !rect;
    if (tableSelection && rect && editGrid.crossing(model, rect)) rangeFrame.dataset.invalid = '';
    else delete rangeFrame.dataset.invalid;
    input.setAttribute('aria-label', '表のセルの編集' + (tableSelection && rect
      ? `（${rect.rows} 行 ${rect.cols} 列を選択中）` : ''));
  }
  function collapseRange() {
    tableSelection = null;
    updateRange();
  }
  function checkRange() {
    if (!tableSelection) return;
    const model = models[tableSelection.table];
    if (!model?.editable || model.table !== tableSelection.tableElement ||
      tableSelection.version !== externalVersion || !editGrid.rectOf(model, tableSelection)) {
      collapseRange();
      closeMenus();
    } else updateRange();
  }
  function extendTo(cell) {
    if (!selected || selected.table !== cell.table) return;
    cancelInsert();
    tableSelection ||= { table: selected.table, tableElement: models[selected.table].table,
      anchor: { row: selected.row, col: selected.col }, focus: { row: selected.row, col: selected.col },
      kind: 'cells', version: externalVersion };
    tableSelection.focus = {
      row: tableSelection.kind === 'cols' ? tableSelection.focus.row : cell.row,
      col: tableSelection.kind === 'rows' ? tableSelection.focus.col : cell.col,
    };
    updateRange();
    updateButtons();
    reveal(cell.element);
    focusInput();
  }
  function extend(direction) {
    if (!selected || (tableSelection?.kind === 'rows' && ['left', 'right'].includes(direction)) ||
      (tableSelection?.kind === 'cols' && ['up', 'down'].includes(direction))) return;
    const next = editGrid.step(models[selected.table], tableSelection?.focus || selected, direction);
    if (next) extendTo(next);
  }
  function selectRange(table, rect, kind = 'cells', keepActive = false) {
    const model = models[table];
    const cell = model?.editable && model.slots[rect.row]?.[rect.col];
    if (!cell) return;
    const previous = keepActive ? tableSelection || { anchor: selected, focus: selected } : null;
    if (!keepActive) select(cell);
    const reverseRows = previous && previous.anchor.row > previous.focus.row;
    const reverseCols = previous && previous.anchor.col > previous.focus.col;
    const candidate = { table, tableElement: model.table,
      anchor: { row: reverseRows ? rect.row + rect.rows - 1 : rect.row, col: reverseCols ? rect.col + rect.cols - 1 : rect.col },
      focus: { row: reverseRows ? rect.row : rect.row + rect.rows - 1, col: reverseCols ? rect.col : rect.col + rect.cols - 1 },
      kind, version: externalVersion };
    if (keepActive && kind === 'rows') {
      candidate.anchor.col = selected.col;
      candidate.focus.col = previous.focus.col;
    } else if (keepActive && kind === 'cols') {
      candidate.anchor.row = selected.row;
      candidate.focus.row = previous.focus.row;
    }
    if (!editGrid.rectOf(model, candidate)) return;
    tableSelection = candidate;
    updateRange();
    updateButtons();
    reveal(cell.element);
    focusInput();
  }

  async function loadTables() {
    fetching?.abort();
    fetching = null;
    const number = ++requestNumber;
    const render = preview.current()?.render;
    if ((!mode && !busy && !queue.length) || stopped || !allowed() || !render) return;
    const controller = new AbortController();
    fetching = controller;
    const target = url('/tables');
    target.searchParams.set('render', render);
    try {
      const response = await fetch(target, { signal: controller.signal });
      if (number !== requestNumber) return;
      if (response.status === 409) return;
      const data = await response.json();
      if (!response.ok) throw new Error(messageOf(data) || '表の情報を読み込めませんでした');
      if (number !== requestNumber || render !== preview.current()?.render) return;
      const tables = preview.tables();
      if (data.render !== render || !Array.isArray(data.tables) || data.tables.length !== tables.length ||
        data.tables.some((table, i) => table.index !== i)) throw new Error('画面と原文の表の数が合いません');
      documentEditable = data.document.editable === true;
      documentReason = data.document.reason || '';
      documentStart = Number.isSafeInteger(data.insertStart) && data.insertStart > 0 ? data.insertStart : 1;
      documentEmpty = data.empty === true;
      origin = typeof data.origin === 'string' && /^[0-9a-f]{32}$/i.test(data.origin) ? data.origin : '';
      byCell = new WeakMap();
      models = data.tables.map((info, i) => {
        const nested = tables[i].querySelector('table') || tables[i].parentElement.closest('table');
        const shape = info.editable && data.document.editable && !nested ? grid(tables[i], info) : null;
        const model = { info, table: tables[i], editable: Boolean(shape),
          reason: !data.document.editable ? data.document.reason : nested ? '入れ子の表' :
            info.reason || '画面と原文のセルの形が違います',
          cells: shape?.cells || [], slots: shape?.slots || [], rows: shape?.rows || [] };
        for (const cell of model.cells) byCell.set(cell.element, cell);
        return model;
      });
      infoRender = render;
      undoState = data.undo || {};
      if (!data.document.editable && (status.hidden || status.dataset.kind !== 'error'))
        notice(data.document.reason, true);
      if (selected) {
        const cell = find(selected);
        if (cell) {
          selected = pointOf(cell);
          if (editor && !busy?.item.structural) editor.point = { ...selected };
        } else clearSelection();
      }
      checkRange();
      checkInsert();
      refreshBlock();
      updateCursors();
      updateCheckboxes();
      updateButtons();
      completeSend();
      void pump();
      void loadBlock();
    } catch (error) {
      if (number !== requestNumber || error.name === 'AbortError') return;
      models = [];
      byCell = new WeakMap();
      infoRender = 0;
      documentEditable = false;
      updateCheckboxes();
      clearSelection();
      updateCursors();
      notice(error.message || '表の情報を読み込めませんでした', true);
    } finally {
      if (number === requestNumber) fetching = null;
    }
  }

  function focusInput() { input.focus({ preventScroll: true }); }
  // セルを、ページのヘッダーと下の操作欄の後ろへ入れずに見える位置へ出す。
  // ページの余白は動かす間だけ付ける（見出しへの移動などの位置を変えない）。全幅パネルの中は CSS が同じ変数を読む。
  function reveal(element) {
    // 操作欄が出ていないとき（ブロックの編集）は、下の余白を取らない。
    const barTop = bar.hidden ? innerHeight : bar.getBoundingClientRect().top;
    const bottom = Math.ceil(Math.max(0, innerHeight - barTop) + 8) + 'px';
    root.style.setProperty('--gp-edit-bottom', bottom);
    const top = root.style.scrollPaddingTop, low = root.style.scrollPaddingBottom;
    if (!top) root.style.scrollPaddingTop = Math.ceil(document.getElementById('gp-header').getBoundingClientRect().bottom) + 'px';
    if (!low) root.style.scrollPaddingBottom = bottom;
    element.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    if (!top) root.style.scrollPaddingTop = '';
    if (!low) root.style.scrollPaddingBottom = '';
  }
  function select(cell, focus = true, { keepRange = false } = {}) {
    if (!samePoint(selected, cell)) cancelInsert();
    if (!keepRange) collapseRange();
    selected = pointOf(cell);
    updateRange();
    input.hidden = false;
    if (!editor) input.value = '';
    bar.hidden = false;
    updateButtons();
    reveal(cell.element);
    if (focus) focusInput();
    if (models[cell.table].info.warnings?.length) notice(models[cell.table].info.warnings.join(' ／ '));
    cancelAnimationFrame(animation);
    position();
  }
  function saveDraft() {
    if (editor) drafts.set(keyOf(editor.point), { ...editor, value: input.value });
  }
  function clearSelection() {
    closeTablePicker();
    cancelInsert();
    closeMenus();
    collapseRange();
    saveDraft();
    editor = null;
    selected = null;
    composing = false;
    // 開き直した入力欄が閉じたら、次の送信を止めたままにしない。
    failed = null;
    input.value = '';
    input.hidden = frame.hidden = rangeFrame.hidden = sourceLabel.hidden = bar.hidden = blockBar.hidden = true;
    layer.dataset.typing = 'off';
    cancelAnimationFrame(animation);
    animation = 0;
    if (document.activeElement === input) input.blur();
    if (mode && cutState?.active) animation = requestAnimationFrame(position);
  }

  // スクロールする祖先の内側と固定行・列の下を除いた範囲へ切る。
  function bounds(cell) {
    let top = 0, left = 0, right = innerWidth, bottom = innerHeight;
    const style = getComputedStyle(cell);
    let wrapper = null;
    for (let parent = cell.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
      const css = getComputedStyle(parent);
      const clipsX = /(auto|scroll|hidden|clip)/.test(css.overflowX);
      const clipsY = /(auto|scroll|hidden|clip)/.test(css.overflowY);
      if (!clipsX && !clipsY && !parent.matches('[data-md-table-sticky-float-panel]')) continue;
      const rect = parent.getBoundingClientRect();
      if (clipsX) { left = Math.max(left, rect.left + parent.clientLeft); right = Math.min(right, rect.left + parent.clientLeft + parent.clientWidth); }
      if (clipsY) { top = Math.max(top, rect.top + parent.clientTop); bottom = Math.min(bottom, rect.top + parent.clientTop + parent.clientHeight); }
      if (!wrapper && (parent.matches('[data-md-table-sticky-wrap]') || clipsX || clipsY)) wrapper = parent;
    }
    if (wrapper) {
      const css = getComputedStyle(wrapper), rect = wrapper.getBoundingClientRect();
      if (style.top === 'auto') top = Math.max(top, rect.top + wrapper.clientTop + (parseFloat(css.scrollPaddingTop) || 0));
      if (style.left === 'auto') left = Math.max(left, rect.left + wrapper.clientLeft + (parseFloat(css.scrollPaddingLeft) || 0));
    }
    if (!cell.closest('[data-md-table-sticky-float-panel]'))
      top = Math.max(top, document.getElementById('gp-header').getBoundingClientRect().bottom);
    return { top, left, right, bottom };
  }
  // size を渡した要素（入力欄）は、セルより右と下へ大きく出す。固定した行・列に隠れる上と左だけを切る。
  function placeBox(element, cell, rect, size = null) {
    const clip = bounds(cell);
    const w = size?.width ?? rect.width;
    const h = size?.height ?? rect.height;
    const visible = rect.width > 0 && rect.height > 0 && rect.right > clip.left && rect.left < clip.right &&
      rect.bottom > clip.top && rect.top < clip.bottom && cell.getClientRects().length;
    element.style.left = rect.left + 'px';
    element.style.top = rect.top + 'px';
    element.style.width = w + 'px';
    element.style.height = h + 'px';
    element.style.clipPath = 'inset(' + [Math.max(0, clip.top - rect.top), size ? 0 : Math.max(0, rect.right - clip.right),
      size ? 0 : Math.max(0, rect.bottom - clip.bottom), Math.max(0, clip.left - rect.left)].map((n) => n + 'px').join(' ') + ')';
    element.style.visibility = visible ? 'visible' : 'hidden';
    return rect;
  }
  function place(element, cell, size = null) {
    return placeBox(element, cell, cell.getBoundingClientRect(), size);
  }
  // 固定した行と列の境で分ける。測るセルは各断片の角だけにする。
  function placeRect(pieces, model, rect) {
    const fixedCount = (count, cellAt, property) => {
      let lo = 0, hi = count;
      while (lo < hi) {
        const mid = Math.floor((lo + hi) / 2), cell = cellAt(mid);
        if (cell && getComputedStyle(cell.element)[property] !== 'auto') lo = mid + 1;
        else hi = mid;
      }
      return lo;
    };
    const fixedRows = fixedCount(model.info.rows, (row) => model.slots[row]?.[0], 'top');
    const fixedCols = fixedCount(model.info.cols, (col) => model.slots[0]?.[col], 'left');
    const split = (start, count, boundary) => [start, ...(boundary > start && boundary < start + count ? [boundary] : []), start + count];
    const ys = split(rect.row, rect.rows, fixedRows), xs = split(rect.col, rect.cols, fixedCols);
    let number = 0;
    for (let y = 0; y < ys.length - 1; y++) for (let x = 0; x < xs.length - 1; x++) {
      const piece = pieces[number++];
      const first = model.slots[ys[y]]?.[xs[x]]?.element;
      const last = model.slots[ys[y + 1] - 1]?.[xs[x + 1] - 1]?.element;
      piece.hidden = !first || !last;
      if (piece.hidden) continue;
      const a = first.getBoundingClientRect(), b = last.getBoundingClientRect();
      const left = Math.min(a.left, b.left), top = Math.min(a.top, b.top);
      const right = Math.max(a.right, b.right), bottom = Math.max(a.bottom, b.bottom);
      placeBox(piece, first, { left, top, right, bottom, width: right - left, height: bottom - top });
    }
    for (; number < pieces.length; number++) pieces[number].hidden = true;
  }
  function refreshBlock() {
    if (!blockPoint(editor?.point) || editor.element.isConnected) return;
    // 探し直すのは、数式・図の描き直しや配色の切り替えで置き換わったときだけ。
    // 文書が外で変わった後は、同じ位置に別の内容が来ているかもしれないので付け替えない。
    const element = editor.version === externalVersion && (editor.tableInsert
      ? models[editor.tableIndex]?.info.anchor === editor.anchor.actual && models[editor.tableIndex].table
      : findBlock(editor.target));
    if (element) editor.element = element;
    else {
      clearSelection();
      notice('文書が変わったので、もう一度操作してください', true);
    }
  }
  function position() {
    refreshBlock();
    animation = 0;
    const cutModel = models[cutState?.table];
    cutFrame.hidden = !mode || !cutState?.active || cutModel?.table !== cutState.tableElement;
    if (!cutFrame.hidden) placeRect(cutPieces, cutModel, cutState.rect);
    const cell = blockPoint(editor?.point) ? { element: editor.tableInsert
      ? editor.element.closest('[data-md-table-sticky-wrap]') || editor.element : editor.element } : selectionCell();
    if (!cell || !mode) {
      if (!cutFrame.hidden) animation = requestAnimationFrame(position);
      return;
    }
    const range = tableSelection && selectionRect();
    rangeFrame.hidden = !range || Boolean(editor);
    if (range && !editor) placeRect(rangePieces, models[tableSelection.table], range);
    frame.hidden = blockPoint(editor?.point);
    frame.dataset.pending = String([...queue, ...(busy ? [busy.item] : [])].some((item) => samePoint(item.point, selected)));
    let rect = place(frame, cell.element);
    if (editor?.target?.tag === 'html') {
      const boxes = preview.findHTML(editor.target).map((n) => n.getBoundingClientRect());
      if (boxes.length) {
        const left = Math.min(...boxes.map((r) => r.left)), top = Math.min(...boxes.map((r) => r.top));
        const right = Math.max(...boxes.map((r) => r.right)), bottom = Math.max(...boxes.map((r) => r.bottom));
        rect = { left, top, right, bottom, width: Math.max(1, right - left), height: Math.max(1, bottom - top) };
      }
    }
    input.hidden = false;
    input.style.left = rect.left + 'px';
    input.style.top = rect.top + 'px';
    if (editor) {
      // 狭いセルでも読めるように、入力欄は右へ広げる（画面の右端まで）。原文は長いので、さらに広くする。
      const width = Math.max(rect.width, Math.min((editor.source ? 24 : 10) * editor.em, innerWidth - rect.left - 8));
      // 中身の高さは、入力か幅が変わったときだけ測り直す（毎フレームの測り直しでレイアウトを強制しない）。
      if (editor.resize || editor.width !== width) {
        input.style.width = width + 'px';
        input.style.height = '0px';
        editor.content = input.scrollHeight + 4;
        editor.width = width;
        editor.resize = false;
      }
      let height = blockPoint(editor.point)
        ? Math.min(Math.max(64, editor.content), Math.max(64, innerHeight - rect.top - 8))
        : Math.min(Math.max(rect.height, editor.content), Math.max(rect.height, innerHeight - rect.top - 8));
      blockBar.hidden = !blockPoint(editor.point) || (!editor.insert && !editor.outer?.length && !editor.inner?.length && !editor.anchor);
      if (editor.insert) {
        const header = document.getElementById('gp-header').getBoundingClientRect().bottom;
        height = Math.min(height, Math.max(64, innerHeight - header - 24));
        const top = Math.max(header + 8, Math.min(editor.insert.document === 'start' && !documentEmpty ? rect.top :
          editor.insert.side === 'before' ? rect.top - height - 6 : rect.bottom + 6,
          innerHeight - height - 8));
        rect = { ...rect, left: rect.left, right: rect.right, width: rect.width, top, bottom: top + height, height };
      }
      if (!blockBar.hidden) {
        const toolbarHeight = blockBar.offsetHeight, gap = 6;
        const header = document.getElementById('gp-header').getBoundingClientRect().bottom;
        const above = rect.top - toolbarHeight - gap >= Math.max(8, header + gap);
        if (!above) height = Math.min(height, Math.max(1, innerHeight - rect.top - toolbarHeight - gap - 8));
        // 下へ置くときは右端へ寄せ、すぐ次の表の左端を操作欄でふさがない。
        const left = above ? rect.left : rect.left + width - blockBar.offsetWidth;
        blockBar.style.left = Math.max(8, Math.min(left, innerWidth - blockBar.offsetWidth - 8)) + 'px';
        const top = above ? rect.top - toolbarHeight - gap : rect.top + height + gap;
        blockBar.style.top = Math.max(header + gap, Math.min(top, innerHeight - toolbarHeight - 8)) + 'px';
      }
      // 表の外へ追加する入力欄は、表を固定するスクロールの包みで切らない。
      placeBox(input, editor.tableInsert ? document.getElementById('gp-content') : cell.element, rect, { width, height });
      if (!tablePicker.hidden) {
        const box = blockBar.getBoundingClientRect(), size = tablePicker.getBoundingClientRect();
        tablePicker.style.left = Math.max(8, Math.min(box.left, innerWidth - size.width - 8)) + 'px';
        tablePicker.style.top = Math.max(8, Math.min(box.bottom + 4, innerHeight - size.height - 8)) + 'px';
      }
      if (input.style.visibility === 'hidden') blockBar.hidden = true;
      // 「原文」の札は、入力欄の右下の内側に重ねる（外に出すと、前後の本文やページのヘッダーに重なる）。
      sourceLabel.hidden = !editor.source || input.style.visibility === 'hidden';
      if (!sourceLabel.hidden) {
        sourceLabel.style.left = rect.left + width - sourceLabel.offsetWidth - 4 + 'px';
        sourceLabel.style.top = rect.top + height - sourceLabel.offsetHeight - 4 + 'px';
      }
    } else {
      input.style.width = input.style.height = '1px';
      input.style.clipPath = '';
      input.style.visibility = 'visible';
      sourceLabel.hidden = true;
      blockBar.hidden = true;
    }
    for (const item of [...queue, ...(busy ? [busy.item] : [])]) {
      if (!item.mask) continue;
      const pending = find(item.point);
      if (pending) place(item.mask, pending.element);
      else item.mask.style.visibility = 'hidden';
    }
    animation = requestAnimationFrame(position);
  }

  function openBlock(draft) {
    closeTablePicker();
    cancelInsert();
    saveDraft();
    collapseRange();
    closeMenus();
    selected = null;
    editor = { ...draft, source: true, em: 16, resize: true };
    sourceLabel.textContent = editor.insert ? '追加' : '原文';
    beforeButton.hidden = afterButton.hidden = !editor.anchor || Boolean(editor.insert);
    insertTableButton.hidden = !editor.insert;
    outerButton.hidden = !editor.outer?.length;
    outerButton.textContent = outerLabels[editor.outer?.[0]?.kind] || '外側のブロックを編集';
    innerButton.hidden = !editor.inner?.length;
    input.value = draft.value;
    input.setAttribute('aria-label', editor.insert ? 'ブロックの追加' : 'ブロックの原文の編集');
    input.style.fontFamily = 'sans-serif';
    input.style.fontSize = '16px';
    input.style.fontWeight = 'normal';
    input.style.lineHeight = '1.6';
    input.hidden = false;
    bar.hidden = frame.hidden = true;
    layer.dataset.typing = 'on';
    reveal(draft.element);
    if (draft.point.document) draft.element.scrollIntoView({ block: draft.point.document === 'start' && !documentEmpty ? 'start' : 'end', inline: 'nearest' });
    focusInput();
    input.setSelectionRange(input.value.length, input.value.length);
    cancelAnimationFrame(animation);
    position();
    if (draft.kind === 'footnote') notice('ラベルを変えると、本文の参照と対応しなくなります');
  }
  async function loadBlock() {
    const pending = pendingBlock;
    let element = pending && findBlock(pending.target);
    if (!element || sourceFetching || busy || queue.length || failed || stopped || !mode ||
      infoRender !== preview.current()?.render) return;
    pendingBlock = null;
    if (!documentEditable) { notice(documentReason || 'この文書は編集できません', true); return; }
    if (!element.isConnected || pending.target.tag !== 'html' && blockOf(element) !== element) return;
    const source = pending.target;
    const render = preview.current().render, version = externalVersion, number = ++sourceNumber;
    const controller = new AbortController();
    sourceFetching = controller;
    const target = url('/source');
    target.searchParams.set('render', render);
    if (source.tag === 'html') {
      for (const side of ['prev', 'next']) if (source[side]) {
        target.searchParams.set(side + 'Tag', source[side].tag);
        target.searchParams.set(side + 'Range', source[side].range);
      }
    } else target.searchParams.set('range', source.range);
    target.searchParams.set('tag', source.tag);
    try {
      const response = await fetch(target, { signal: controller.signal });
      if (number !== sourceNumber || response.status === 409) return;
      const data = await response.json();
      element = findBlock(source);
      if (!mode || stopped || number !== sourceNumber || render !== preview.current()?.render ||
        version !== externalVersion || !element) return;
      if (!response.ok) {
        const outer = response.status === 422 && Array.isArray(data.outer) && data.outer[0];
        const next = outerElement(element, outer);
        if (next) {
          const fallbackNotice = data.error?.code === 'term' && outer.kind === 'deflist'
            ? 'この用語だけの範囲を決められないので、説明リスト全体を開きました'
            : 'このブロックだけの範囲を確かめられないので、外側のブロックを開きました';
          beginBlock(next, { preserve: true, inner: pending.inner, fallbackNotice });
          return;
        }
        throw new Error(messageOf(data) || '原文を読み込めませんでした');
      }
      if (data.render !== render || typeof data.text !== 'string' || typeof data.range !== 'string' ||
        !rangePattern.test(data.range)) throw new Error('原文の応答の形が正しくありません');
      const point = { range: data.range, ...(source.tag === 'html' ? { html: source } : {}) }, saved = drafts.get(keyOf(point));
      const value = saved?.original === data.text ? saved.value : data.text;
      drafts.delete(keyOf(point));
      openBlock({ point, kind: data.kind, element, target: source, render, version, original: data.text, value,
        outer: Array.isArray(data.outer) ? data.outer : [], inner: pending.inner,
        anchor: data.anchor && typeof data.anchor.tag === 'string' && rangePattern.test(data.anchor.range || '') &&
          rangePattern.test(data.anchor.actual || '') ? data.anchor : null });
      if (pending.fallbackNotice) notice(pending.fallbackNotice);
    } catch (error) {
      if (number === sourceNumber && error.name !== 'AbortError') notice(error.message || '原文を読み込めませんでした', true);
    } finally {
      if (number === sourceNumber) sourceFetching = null;
    }
  }
  function beginBlock(element, { preserve = false, inner = [], fallbackNotice = '', target = null } = {}) {
    target ||= preview.sourceOf(element) || rawBlockOf(element);
    if (!target) return;
    cancelSource();
    if (editor && !preserve) endEdit(true);
    clearSelection();
    pendingBlock = { target, inner, fallbackNotice };
    operationStart();
    void loadBlock();
  }

  function currentValue(cell, source) {
    const pending = [...(busy ? [busy.item] : []), ...queue].reverse().find((item) =>
      samePoint(item.point, cell) && item.value !== undefined && item.source === source);
    return pending ? pending.value : source ? cell.source : cell.text;
  }
  function beginEdit({ source = false, replace = false, value, composition = false } = {}) {
    const cell = selectionCell();
    if (!cell || stopped) return;
    cancelInsert();
    collapseRange();
    closeMenus();
    operationStart();
    sourceLabel.textContent = '原文';
    source ||= cell.raw;
    const original = currentValue(cell, source).replace(/\r\n?/g, '\n');
    // 下書きは、書き始めたときと同じ中身のセルにだけ戻す。行や列がずれた後の別のセルには出さない。
    const saved = !replace && value === undefined ? drafts.get(keyOf(selected)) : null;
    const draft = saved && saved.source === source && saved.original === original ? saved : null;
    if (saved && !draft) drafts.delete(keyOf(selected));
    editor = { point: { ...selected }, source, original, value: '', version: externalVersion };
    if (draft) editor = { ...draft, point: { ...selected } };
    // 下書きを自分で開き直した操作は、現在の文書への再入力として扱う。
    editor.version = externalVersion;
    editor.original = original;
    input.value = value ?? (replace ? '' : draft?.value ?? original);
    editor.value = input.value;
    // 文字の大きさと行間は、編集を始めるときに 1 回だけセルに合わせる。
    const css = getComputedStyle(cell.element);
    input.style.fontFamily = css.fontFamily;
    input.style.fontSize = css.fontSize;
    input.style.fontWeight = css.fontWeight;
    input.style.lineHeight = css.lineHeight;
    editor.em = parseFloat(css.fontSize) || 16;
    editor.resize = true;
    layer.dataset.typing = 'on';
    reveal(cell.element);
    if (!composition) {
      focusInput();
      input.setSelectionRange(input.value.length, input.value.length);
    }
    cancelAnimationFrame(animation);
    position();
  }
  function endEdit(commit) {
    if (!editor) return;
    closeTablePicker();
    const draft = { ...editor, point: { ...editor.point }, value: input.value };
    editor = null;
    composing = false;
    layer.dataset.typing = 'off';
    input.value = '';
    drafts.delete(keyOf(draft.point));
    const retry = failed && samePoint(failed.point, draft.point);
    if (retry) failed = null;
    if (blockPoint(draft.point)) clearSelection();
    if (commit && draft.value !== draft.original) {
      if (draft.version !== externalVersion) {
        drafts.set(keyOf(draft.point), draft);
        discardQueue();
        notice('文書が変わったので、もう一度操作してください', true);
        void loadTables();
        updateButtons();
        return;
      }
      if (blockPoint(draft.point)) {
        enqueue({ ...draft }, retry);
        return;
      }
      const op = draft.source ? { type: 'setSource', row: draft.point.row, col: draft.point.col, source: draft.value }
        : { type: 'set', cells: [{ row: draft.point.row, col: draft.point.col, text: draft.value }] };
      enqueue({ point: draft.point, op, value: draft.value, source: draft.source, original: draft.original }, retry);
    } else void pump();
    updateButtons();
  }

  function move(direction) {
    const cell = selectionCell();
    if (!cell) return;
    const next = editGrid.step(models[cell.table], cell, direction);
    collapseRange();
    if (next) select(next);
    else focusInput();
  }

  function addMask(item) {
    if (item.value === undefined || blockPoint(item.point)) return;
    const mask = make('div', 'gp-edit-pending-' + ++maskNumber);
    mask.className = 'gp-edit-pending';
    mask.textContent = item.value;
    mask.setAttribute('aria-label', '送信中');
    item.mask = mask;
    if (!animation && selected) position();
  }
  let maskNumber = 0;
  function enqueue(item, first = false) {
    operationStart();
    item.tableElement = models[item.point?.table]?.table;
    item.version = externalVersion;
    item.structural = Boolean(item.insert || item.history || /^(insert|delete|duplicate|move)/.test(item.op?.type || ''));
    if (first) queue.unshift(item);
    else queue.push(item);
    addMask(item);
    updateButtons();
    void pump();
  }
  function discardQueue() {
    const discarded = queue.splice(0);
    for (const item of discarded) {
      item.mask?.remove();
      if (item.value !== undefined) drafts.set(keyOf(item.point), { ...item });
    }
    for (const item of discarded.reverse()) if (checkboxElement(item)?.isConnected) item.checkbox.checked = item.originalChecked;
  }
  function discardTableInputs() {
    let discarded = false;
    const items = queue.filter((item) => item.op);
    for (let i = queue.length - 1; i >= 0; i--) if (queue[i].op) queue.splice(i, 1);
    for (const item of items) {
      item.mask?.remove();
      if (item.value !== undefined) drafts.set(keyOf(item.point), { ...item });
      discarded = true;
    }
    // まだ確定していない入力も、変わる前の座標と原文を下書きに残す。
    if (editor && !blockPoint(editor.point)) {
      saveDraft();
      editor = null;
      composing = false;
      input.value = '';
      layer.dataset.typing = 'off';
      discarded = true;
    }
    if (discarded) notice('行・列が変わったので、並んでいた入力は送らずに下書きへ戻しました。もう一度操作してください', true);
  }
  function finishSend(changed = false, ready = false) {
    if (!busy) return;
    const item = busy.item;
    clearTimeout(busy.timer);
    item.mask?.remove();
    if (item.structural && changed) {
      discardTableInputs();
      collapseRange();
    }
    busy = null;
    if (ready && item.after && mode && !stopped && item.version === externalVersion)
      selectRange(item.point.table, item.after, item.afterKind);
    updateButtons();
    void pump();
    void loadBlock();
  }
  function completeSend() {
    if (busy?.ok && preview.current()?.render > busy.render && infoRender === preview.current()?.render)
      finishSend(true, true);
  }
  // 連なりの前後に段落が増えると、その段落が新しい境になる。
  // 追加前の入力の要素で下書きを照合し、追加しなかった側の境から現在の連なりへ移す。
  function rebindHTMLDraft(item) {
    if (item.target?.tag !== 'html' && !item.insert?.document) return;
    const candidates = new Map();
    for (const node of document.querySelector('#gp-content').children) {
      const target = preview.htmlOf(node);
      if (target) candidates.set(JSON.stringify(target), target);
    }
    const side = item.insert.side === 'before' || item.insert.document === 'start' ? 'next' : 'prev';
    const same = (a, b) => !a && !b || Boolean(a && b && a.tag === b.tag && a.range === b.range);
    for (const [key, draft] of [...drafts]) {
      if (draft.target?.tag !== 'html' || !item.insert.document && draft.element !== item.element || draft.version !== item.version) continue;
      const targets = [...candidates.values()].filter((target) => same(target[side], draft.target[side]));
      if (targets.length !== 1) continue;
      const target = targets[0], point = { ...draft.point, html: target };
      drafts.delete(key);
      drafts.set(keyOf(point), { ...draft, point, target, element: findBlock(target), render: preview.current().render });
    }
  }
  // 前へ追加したときは、既存の原文は同じまま行だけがずれる。追加前の下書きも、その同じ対象へ戻せるようにする。
  function shiftInsertDrafts(item) {
    if (!item.insert) return;
    if (item.insert.side !== 'before' && item.insert.document !== 'start' || typeof item.previousHTML !== 'string' || typeof preview.current()?.html !== 'string') {
      rebindHTMLDraft(item);
      return;
    }
    const positioned = (html) => {
      const template = document.createElement('template');
      // 入れるのは、サーバーが消毒した pipeline.Result.HTML のみ。
      template.innerHTML = html;
      return [...template.content.querySelectorAll('[data-sourcepos]')].map((element) =>
        ({ tag: element.localName, range: element.getAttribute('data-sourcepos') }));
    };
    const before = positioned(item.previousHTML), after = positioned(preview.current().html);
    const coordinates = (range) => rangePattern.test(range || '') ? range.split(/[:-]/).map(Number) : null;
    const raw = item.target?.tag === 'html';
    const cut = item.insert.document === 'start' ? item.startCut : coordinates(item.anchor.actual)?.[0];
    const reference = item.insert.document === 'start' || item.tableInsert ? before.find((target, i) => {
      const from = coordinates(target.range), candidate = after[i + after.length - before.length];
      const to = coordinates(candidate?.range);
      // 値が変わらない、利用者が書いた位置は基準にしない。
      return from?.[0] >= cut && candidate?.tag === target.tag && to && to[0] > from[0] &&
        to[0] - from[0] === to[2] - from[2] && from[1] === to[1] && from[3] === to[3];
    })
      : raw ? item.target.next || before.find((target) => coordinates(target.range)?.[0] >= cut) : item.anchor;
    const index = reference ? before.findIndex((target) => target.tag === reference.tag && target.range === reference.range) : -1;
    const anchor = index < 0 ? null : after[index + after.length - before.length];
    const from = coordinates(reference?.range), to = coordinates(anchor?.range);
    // 後ろに位置が一つもなければ、連なりの境は変わらず、行をずらす下書きもない。
    if (index < 0 || anchor?.tag !== reference.tag || !from || !to || from[1] !== to[1] || from[3] !== to[3] ||
      to[0] <= from[0] || to[0] - from[0] !== to[2] - from[2]) { rebindHTMLDraft(item); return; }
    const delta = to[0] - from[0];
    const shift = (range) => {
      const c = coordinates(range);
      if (!c || c[0] < cut) return range;
      return (c[0] + delta) + ':' + c[1] + '-' + (c[2] + delta) + ':' + c[3];
    };
    const shiftTarget = (target) => target?.tag === 'html'
      ? { tag: 'html', prev: target.prev && { ...target.prev, range: shift(target.prev.range) },
        next: target.next && { ...target.next, range: shift(target.next.range) } }
      : target && { ...target, range: shift(target.range) };
    const shifted = [];
    for (const [key, draft] of drafts) {
      if (!draft.point.range || draft.version !== item.version) continue;
      const range = shift(draft.point.range);
      if (range === draft.point.range) continue;
      drafts.delete(key);
      const target = shiftTarget(draft.target);
      const point = { ...draft.point, range, ...(target?.tag === 'html' ? { html: target } : {}) };
      shifted.push({ ...draft, point, target, inner: draft.inner?.map(shiftTarget), outer: draft.outer?.map(shiftTarget),
        anchor: draft.anchor && { ...draft.anchor, range: shift(draft.anchor.range), actual: shift(draft.anchor.actual) },
        ...(draft.insert ? { insert: { ...draft.insert, anchor: shift(draft.insert.anchor) }, previousHTML: preview.current().html } : {}),
        render: preview.current().render });
    }
    for (const draft of shifted) drafts.set(keyOf(draft.point), draft);
    rebindHTMLDraft(item);
  }
  function reopen(item) {
    failed = item.value === undefined ? null : item;
    if (!failed) return;
    drafts.set(keyOf(item.point), { ...item });
    if (blockPoint(item.point)) {
      if (mode && !stopped && item.element.isConnected && item.render === preview.current()?.render && item.version === externalVersion) {
        openBlock(item);
      } else failed = null;
      return;
    }
    const cell = mode && find(item.point);
    // 開き直せないとき（編集モードの外・セルが無い）は、下書きだけ残して次の送信を止めない。
    if (!cell) { failed = null; return; }
    saveDraft();
    editor = null;
    select(cell);
    beginEdit({ source: item.source, value: item.value });
  }
  async function pump() {
    if (busy || failed || stopped || !queue.length || infoRender !== preview.current()?.render) return;
    // 並べた後に文書が外で変わっていたら、行や列がずれているかもしれない古い位置へ書かない。
    if (queue[0].version !== externalVersion) {
      discardQueue();
      notice('文書が変わったので、もう一度操作してください', true);
      updateButtons();
      return;
    }
    const item = queue.shift();
    const target = !item.history && !blockPoint(item.point) && models[item.point.table];
    if (!item.history && !blockPoint(item.point) && (!target?.editable || target.table !== item.tableElement ||
      !target.slots[item.point.row]?.[item.point.col])) {
      item.mask?.remove();
      discardQueue();
      notice('表の形が変わったので、もう一度操作してください', true);
      updateButtons();
      return;
    }
    const render = preview.current().render;
    if (item.op) {
      item.after = editGrid.placed(item.op, target.info);
      item.afterKind = /Rows(?:From)?$/.test(item.op.type) ? 'rows' : /Columns(?:From)?$/.test(item.op.type) ? 'cols' : 'cells';
    }
    checkboxElement(item);
    if (blockPoint(item.point) && (!documentEditable || (item.checkbox ? !item.checkbox.isConnected : item.render !== render))) {
      if (item.value !== undefined) drafts.set(keyOf(item.point), { ...item });
      if (item.checkbox?.isConnected) item.checkbox.checked = item.originalChecked;
      discardQueue();
      notice('文書が変わったので、もう一度操作してください', true);
      updateButtons();
      return;
    }
    busy = { item, render, ok: false, timer: 0 };
    updateButtons();
    try {
      // 待ち行列に入れたときの番号は使わず、直前に確かめた描画へ送る。
      const body = item.history ? { [item.history]: true } : item.checkbox
        ? { render, checkbox: item.checkbox.getAttribute('data-checkbox-sourcepos'), checked: item.checked }
        : item.insert ? { render, insert: item.insert, text: item.value }
          : item.point.range ? { render, range: item.point.range, text: item.value }
          : { render, table: item.point.table, op: item.op };
      const response = await fetch(url(''), { method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body) });
      const data = await response.json();
      if (!response.ok) {
        item.mask?.remove();
        busy = null;
        if (item.checkbox?.isConnected) item.checkbox.checked = item.originalChecked;
        if (response.status === 409) {
          // 文書が外で変わっている。開いている入力も、古い位置のまま確定させない。
          externalVersion++;
          collapseRange();
          cancelCut();
          cancelInsert();
          closeMenus();
          discardQueue();
          saveDraft();
          if (item.value !== undefined) drafts.set(keyOf(item.point), { ...item });
          notice(['stale', 'changed'].includes(data?.error?.code)
            ? '文書が変わったので、もう一度操作してください'
            : messageOf(data) || '書き込めませんでした', true);
          void loadTables();
        } else if (response.status === 422) {
          reopen(item);
          const hint = item.clipboard ? '。文字だけで貼るには Ctrl+Shift+V を押してください' : '';
          notice((messageOf(data) || 'この操作では書き込めませんでした') + hint, true);
          // 入力欄を開き直さなかったとき（行・列の操作、編集モードの外）は、並んでいる送信を続ける。
          if (!failed) void pump();
        } else {
          cancelCut();
          cancelInsert();
          discardQueue();
          reopen(item);
          notice('書き込めませんでした' + (messageOf(data) ? ' ／ ' + messageOf(data) : ''), true);
        }
        if (item.checkbox?.isConnected) {
          const pending = [...(busy ? [busy.item] : []), ...queue].reverse().find((next) => next.checkbox === item.checkbox);
          item.checkbox.checked = pending ? pending.checked : item.originalChecked;
        }
        updateButtons();
        void loadBlock();
        return;
      }
      const messages = [...(data.warnings || []), item.successNotice].filter(Boolean);
      if (messages.length) notice(messages.join(' ／ '));
      busy.ok = true;
      if (data.unchanged) { finishSend(false, true); void loadTables(); return; }
      completeSend();
      if (busy?.item === item) busy.timer = setTimeout(() => {
        // 遅れて届くこの書き込みの描画を、外の変更と数えない。
        if (!busy.seen) late = busy.render;
        cancelCut();
        cancelInsert();
        if (!stopped) notice('書き込み後の表示が届きません。表の情報を読み直します', true);
        infoRender = 0;
        finishSend(true);
        void loadTables();
      }, 3000);
    } catch (error) {
      item.mask?.remove();
      busy = null;
      cancelCut();
      cancelInsert();
      discardQueue();
      if (item.checkbox?.isConnected) item.checkbox.checked = item.originalChecked;
      reopen(item);
      notice('書き込めませんでした' + (error.message ? ' ／ ' + error.message : ''), true);
      updateButtons();
      void loadBlock();
    }
  }

  let undoState = {};
  const cutCancelled = '切り取りは取り消されています。もう一度切り取ってください';
  const pasteInstruction = 'ブラウザーの決まりで、メニューからはクリップボードを読めません。Ctrl+V を押してください';
  function isCancelled(envelope) {
    return envelope.cut && envelope.origin === origin &&
      (envelope.cut.render !== preview.current()?.render || (cutState?.id === envelope.cut.id && !cutState.active));
  }
  function sourceOf(envelope, axis = null) {
    const cut = envelope.cut;
    const common = { table: cut.table, render: cut.render, hash: cut.hash, origin: envelope.origin };
    return axis === 'row' ? { ...common, row: cut.row, count: cut.rows }
      : axis === 'col' ? { ...common, col: cut.col, count: cut.cols }
        : { ...common, row: cut.row, col: cut.col, rows: cut.rows, cols: cut.cols };
  }
  function foreignNotice(envelope) {
    if (!envelope || envelope.origin === origin) return '';
    return [envelope.cut ? '別の文書から切り取った内容は、元を消さずにコピーとして貼りました' : '',
      editGrid.foreignHints(envelope.fragment) ? '別の文書から貼りました。参照リンク・脚注・相対パスは、この文書の場所で解釈されます' : '']
      .filter(Boolean).join(' ／ ');
  }
  function insertReason(copy, axis, at, side, table) {
    if (!copy) return `このページでコピーした${axis === 'row' ? '行' : '列'}がありません`;
    if (isCancelled(copy)) return cutCancelled;
    const info = models[table].info, row = axis === 'row';
    const fragment = copy.fragment;
    if (!fragment) return '範囲が大きすぎるので、挿入する範囲を小さくしてコピーし直してください';
    if (fragment.shape === (row ? 'cols' : 'rows')) return `コピーしたのは${row ? '列' : '行'}です`;
    const count = row ? fragment.cols : fragment.rows, expected = row ? info.cols : info.rows;
    if (count !== expected) return `コピーした${row ? '行の列' : '列の行'}の数（${count}）が表（${expected}）と違います`;
    if (copy.cut && copy.origin === origin) {
      const source = models[copy.cut.table]?.info;
      if (!source || (row ? copy.cut.col !== 0 || copy.cut.cols !== source.cols : copy.cut.row !== 0 || copy.cut.rows !== source.rows))
        return `切り取った${row ? '行' : '列'}の全体を選び直してください`;
      if (row && source.kind === 'pipe' && copy.cut.row === 0) return 'パイプの表の見出し行は切り取りできません';
      if (copy.cut.table === table) {
        const start = row ? copy.cut.row : copy.cut.col, count = row ? copy.cut.rows : copy.cut.cols;
        const boundary = at + (side === 'after' ? 1 : 0);
        if (boundary > start && boundary < start + count) return `切り取った${row ? '行' : '列'}の中へは移動できません`;
        if (boundary === start || boundary === start + count) return '同じ位置です';
      }
    }
    return '';
  }
  function sendInsert(pending, envelope, values = null, extraNotice = '') {
    const { axis, index: at, side, table } = pending;
    const reason = envelope && insertReason(envelope, axis, at, side, table);
    if (reason) { notice(reason, true); return; }
    const moving = envelope?.cut && envelope.origin === origin;
    const op = { type: (moving ? 'move' : 'insert') + (axis === 'row' ? 'Rows' : 'Columns') + (moving ? '' : 'From'),
      [axis === 'row' ? 'row' : 'col']: at, side };
    if (moving) op.from = sourceOf(envelope, axis);
    else if (envelope) op.fragment = envelope.fragment;
    else op.values = values;
    enqueue({ point: { ...selected, table }, op, clipboard: true,
      successNotice: [extraNotice, foreignNotice(envelope)].filter(Boolean).join(' ／ ') });
  }
  function startInsert(action, rect) {
    const [axis, , side] = action.split('-');
    const table = selected.table;
    pendingInsert = { axis, side, table, tableElement: models[table].table,
      index: rect[axis === 'row' ? 'row' : 'col'] + (side === 'after' ? rect[axis === 'row' ? 'rows' : 'cols'] - 1 : 0),
      version: externalVersion };
    notice('挿入する内容を Ctrl+V で貼り付けてください（Esc でやめる）', 'wait');
    focusInput();
  }
  function copySelection(event, kind) {
    if (!mode || stopped || document.activeElement !== input || editor || !event.clipboardData) return;
    const action = kind === 'cut' ? (tableSelection?.kind === 'rows' ? 'row-cut' : tableSelection?.kind === 'cols' ? 'col-cut' : 'cut') : 'copy';
    const reason = reasonFor(action);
    if (reason) { notice(reason, true); return; }
    const rect = selectionRect(), model = models[selected.table], info = model.info;
    const shape = tableSelection?.kind || 'cells';
    const values = editGrid.matrix(info, rect);
    const locked = info.copyLocked || editGrid.cellsIn(info, rect).some((cell) => cell.copyLocked);
    const raw = locked ? null : editGrid.fragment(info, rect, shape);
    const fragment = raw || editGrid.valuesFragment(values);
    if (fragment && !raw) fragment.shape = shape;
    const cut = kind === 'cut' && fragment && /^[0-9a-f]{32}$/i.test(origin) && /^[0-9a-f]{64}$/i.test(info.hash)
      ? { id: Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) => value.toString(16).padStart(2, '0')).join(''),
        table: selected.table, render: infoRender, hash: info.hash, ...rect } : null;
    const envelope = fragment && origin ? editGrid.envelope({ origin, fragment, cut }) : null;
    // ここまでに断った場合は既存のクリップボードを保つ。文字・HTML は DOM へ入れない。
    event.clipboardData.setData('text/plain', editGrid.toTSV(values));
    const html = editGrid.toHTML(info, rect);
    if (html) event.clipboardData.setData('text/html', html);
    if (envelope) event.clipboardData.setData(editGrid.TYPE, JSON.stringify(envelope));
    event.preventDefault();
    if (clipboardIntent) clipboardIntent.handled = true;
    cancelCut();
    lastCopy = { origin, fragment, cut: envelope?.cut || null };
    if (envelope?.cut) {
      cutState = { ...cut, tableElement: model.table, rect, shape, active: true };
      cutFrame.hidden = false;
      if (!animation) position();
    }
    let message = `${rect.rows} 行 ${rect.cols} 列を${envelope?.cut ? '切り取りました' : 'コピーしました'}`;
    if (!raw || !envelope) message += locked ? '。原文を保てないセルがあるので、値だけをコピーしました'
      : '。範囲が大きいか写しの情報が無いので、原文を保つ写しは作りませんでした';
    if (kind === 'cut' && !envelope?.cut) message += '。切り取りはできないので、コピーとして扱います';
    notice(message);
    updateButtons();
  }
  function menuCopy(kind) {
    const intent = { kind, handled: false };
    clipboardIntent = intent;
    focusInput();
    let copied = false;
    try { copied = document.execCommand('copy'); } catch { /* キーからの操作を案内する。 */ }
    finally { clipboardIntent = null; }
    if (!copied || !intent.handled) notice('メニューからコピーできませんでした。Ctrl+C を押してください', true);
  }
  function actionRect(action) {
    const rect = selectionRect(), model = models[selected?.table];
    if (!rect) return null;
    return action.startsWith('row-') ? editGrid.axisRect(model, rect, 'rows')
      : action.startsWith('col-') ? editGrid.axisRect(model, rect, 'cols') : rect;
  }
  // 表示・メニュー・実行は、必ず同じ理由を使う。DOM の行の表示状態はこの時だけ読む。
  function reasonFor(action) {
    if (action === 'document-start' || action === 'document-end') return documentInsertReason();
    if (action === 'paste') return pasteInstruction;
    const cell = selectionCell();
    if (!cell) return 'セルを選んでください';
    if (stopped) return 'プレビューが止まっています';
    if (infoRender !== preview.current()?.render) return '表の情報を読み込んでいます';
    if (action === 'table') return '';
    if (action === 'table-before' || action === 'table-after') {
      if (busy || queue.length || sourceFetching) return '前の書き込みの反映を待っています';
      if (composing) return '文字の入力を終えてから操作してください';
      const info = models[cell.table].info;
      return rangePattern.test(info.anchor || '') ? '' : info.anchorReason || 'この表の前後には追加できません';
    }
    const selecting = ['rows', 'cols', 'row-select', 'col-select'].includes(action);
    if (!selecting && (busy || queue.length)) return '前の書き込みの反映を待っています';
    const model = models[cell.table], info = model.info, rect = actionRect(action);
    if (!rect) return '範囲を選び直してください';
    if (selecting) return '';
    const rowAction = action.startsWith('row-'), colAction = action.startsWith('col-');
    const copying = ['copy', 'cut'].includes(action) || /-(copy|cut)$/.test(action);
    if (!copying && rowAction && info.rowsLocked) return info.rowsLocked;
    if (!copying && colAction && info.colsLocked) return info.colsLocked;
    if (rowAction || colAction || copying || action === 'clear') {
      if (model.rows.slice(rect.row, rect.row + rect.rows).some((row) => getComputedStyle(row).display === 'none'))
        return '表の検索で隠れた行をまたいでいます。検索を解除してから選び直してください';
      // 空の挿入と削除は、既存の窓口が結合を伸縮するので許す。
      const crossing = (copying || action.endsWith('-duplicate') || action === 'clear') && editGrid.crossing(model, rect);
      if (crossing) return `結合セル（${crossing.row + 1} 行 ${crossing.col + 1} 列目から ${crossing.rowspan} 行 ${crossing.colspan} 列）の途中で範囲が切れています。結合セル全体を含むように選び直してください`;
    }
    if (info.kind === 'pipe' && rect.row === 0) {
      if (['row-before', 'row-insert-before', 'row-paste-before'].includes(action)) return 'パイプの表の見出し行の上には挿入できません';
      if (action === 'row-delete') return 'パイプの表の見出し行は削除できません';
      if (action === 'row-cut') return 'パイプの表の見出し行は切り取りできません';
    }
    if (action === 'row-delete' && rect.rows === info.rows) return 'すべての行は削除できません';
    if (action === 'col-delete' && rect.cols === info.cols) return 'すべての列は削除できません';
    if (action === 'source' && info.kind !== 'html') return 'HTML の表だけで使えます';
    if ((action === 'undo' || action === 'redo') && !undoState[action]) return '履歴がありません';
    if (/^(row|col)-insert-/.test(action)) {
      const [axis, , side] = action.split('-'), row = axis === 'row';
      return insertReason(lastCopy, axis, rect[row ? 'row' : 'col'] + (side === 'after' ? rect[row ? 'rows' : 'cols'] - 1 : 0), side, selected.table);
    }
    return '';
  }
  function showMenuReason(menu, item) {
    const reason = item?.title || '';
    if (menu.reason.textContent === reason) return;
    menu.reason.textContent = reason;
    const entry = openMenus.find((entry) => entry.menu === menu);
    // 長い理由で高さが増えても、画面の端と操作欄の内側に収める。
    if (entry && !menu.element.hidden) placeMenu(menu, entry.rect || entry.opener.getBoundingClientRect(), entry.placement);
  }
  function updateMenus() {
    for (const { menu, action, item, description } of menuItems) {
      if (!openMenus.some((entry) => entry.menu === menu)) continue;
      const reason = reasonFor(action);
      if (/^(row|col)-insert-/.test(action)) {
        const [axis, , side] = action.split('-'), row = axis === 'row';
        const count = lastCopy?.fragment?.[row ? 'rows' : 'cols'];
        item.textContent = `${lastCopy?.cut ? '切り取った' : 'コピーした'}${count ? ' ' + count + ' ' : ''}${row ? '行' : '列'}を${row ? side === 'before' ? '上' : '下' : side === 'before' ? '左' : '右'}に挿入`;
      }
      item.setAttribute('aria-disabled', String(Boolean(reason)));
      item.title = description.textContent = reason;
      if (reason) item.setAttribute('aria-describedby', description.id);
      else item.removeAttribute('aria-describedby');
    }
    for (const { menu } of openMenus) {
      const focused = menu.items.find((item) => item === document.activeElement);
      showMenuReason(menu, focused);
    }
  }
  function buildMenu(name, label, definitions) {
    // transform がある操作欄の子にせず、層の直下へ置く。
    const element = make('div', 'gp-edit-menu-' + name);
    element.className = 'gp-edit-menu';
    element.setAttribute('role', 'menu');
    element.setAttribute('aria-label', label);
    element.hidden = true;
    const menu = { element, items: [], reason: null };
    menus.set(name, menu);
    for (const definition of definitions) {
      if (!definition) {
        const separator = document.createElement('div');
        separator.setAttribute('role', 'separator');
        element.append(separator);
        continue;
      }
      const [action, text, id = action] = definition;
      const item = make('button', 'gp-edit-' + id, element);
      item.type = 'button';
      item.textContent = text;
      item.setAttribute('role', 'menuitem');
      item.tabIndex = -1;
      item.dataset.action = action;
      if (['rows', 'cols', 'table'].includes(action)) {
        item.setAttribute('aria-haspopup', 'menu');
        item.setAttribute('aria-expanded', 'false');
        item.setAttribute('aria-controls', 'gp-edit-menu-' + action);
        item.dataset.submenu = '';
      }
      const description = make('span', item.id + '-reason', element);
      description.hidden = true;
      menuItems.push({ menu, action, item, description });
      menu.items.push(item);
      item.addEventListener('focus', () => showMenuReason(menu, item));
      item.addEventListener('pointerover', () => showMenuReason(menu, item));
      item.addEventListener('click', () => {
        if (reasonFor(action)) { updateMenus(); showMenuReason(menu, item); return; }
        if (['rows', 'cols', 'table'].includes(action)) openSubmenu(action, item);
        else runAction(action);
      });
    }
    menu.reason = make('div', element.id + '-reason', element);
    menu.reason.className = 'gp-edit-menu-reason';
    return menu;
  }
  for (const [prefix, name, directions] of [['row', 'rows', ['上', '下']], ['col', 'cols', ['左', '右']]]) {
    buildMenu(name, prefix === 'row' ? '行の操作' : '列の操作', [
      [prefix + '-before', directions[0] + 'に挿入'], [prefix + '-after', directions[1] + 'に挿入'],
      [prefix + '-duplicate', '複製'], [prefix + '-delete', '削除'], null, [prefix + '-select', '選択'],
      [prefix + '-copy', 'コピー'], [prefix + '-cut', '切り取り'], null,
      [prefix + '-insert-before', 'コピーした' + (prefix === 'row' ? '行' : '列') + 'を' + directions[0] + 'に挿入'],
      [prefix + '-insert-after', 'コピーした' + (prefix === 'row' ? '行' : '列') + 'を' + directions[1] + 'に挿入'],
      [prefix + '-paste-before', '貼り付けて' + directions[0] + 'に挿入…'],
      [prefix + '-paste-after', '貼り付けて' + directions[1] + 'に挿入…'],
    ]);
  }
  buildMenu('cell', 'セルの操作', [['copy', 'コピー（Ctrl+C）', 'cell-copy'], ['cut', '切り取り（Ctrl+X）', 'cell-cut'],
    ['paste', '貼り付け（Ctrl+V）', 'cell-paste'], null, ['rows', '行', 'cell-rows'], ['cols', '列', 'cell-cols'], ['table', '表', 'cell-table'], null,
    ['source', '原文を編集', 'cell-source']]);
  buildMenu('table', '表の操作', [['table-before', '表の前にブロックを追加'], ['table-after', '表の後ろにブロックを追加']]);
  buildMenu('document', '文書への追加', [['document-start', '文書の先頭に追加'], ['document-end', '文書の末尾に追加']]);

  function closeMenus(restore = false) {
    const opener = openMenus[0]?.opener;
    while (openMenus.length) {
      const entry = openMenus.pop();
      entry.menu.element.hidden = true;
      if (entry.opener !== input) entry.opener.setAttribute('aria-expanded', 'false');
    }
    // 文書への追加のメニューはセルを選ばずに開くので、開いたボタンへ戻す。
    if (restore && mode && (selected || opener === addButton)) {
      if (opener?.isConnected && !opener.disabled) opener.focus({ preventScroll: true });
      else focusInput();
    }
  }
  function closeSubmenu() {
    const entry = openMenus.pop();
    entry.menu.element.hidden = true;
    entry.opener.setAttribute('aria-expanded', 'false');
    entry.opener.focus({ preventScroll: true });
  }
  function placeMenu(menu, rect, placement) {
    const element = menu.element;
    element.hidden = false;
    const size = element.getBoundingClientRect();
    let left = rect.left, top = rect.bottom + 4;
    if (placement === 'toolbar') top = rect.top - size.height - 4;
    else if (placement === 'child') {
      left = rect.right + 4;
      if (left + size.width > innerWidth - 8) left = rect.left - size.width - 4;
      top = rect.top;
    } else if (top + size.height > innerHeight - 8) top = rect.top - size.height - 4;
    element.style.left = Math.max(8, Math.min(left, innerWidth - size.width - 8)) + 'px';
    element.style.top = Math.max(8, Math.min(top, innerHeight - size.height - 8)) + 'px';
  }
  function openMenu(name, opener, { last = false, point = null } = {}) {
    if (name === 'document') {
      const reason = documentInsertReason();
      if (reason) { notice(reason, true); return; }
    }
    cancelInsert();
    if (name === 'document') clearSelection();
    else if (editor) endEdit(true);
    closeMenus();
    const menu = menus.get(name);
    const placement = name === 'cell' ? 'cell' : name === 'document' ? 'header' : 'toolbar';
    const rect = point ? { left: point.x, right: point.x, top: point.y, bottom: point.y }
      : name === 'cell' ? selectionCell().element.getBoundingClientRect() : opener.getBoundingClientRect();
    openMenus.push({ menu, opener, placement, rect: name === 'cell' ? rect : null });
    if (opener !== input) opener.setAttribute('aria-expanded', 'true');
    updateMenus();
    placeMenu(menu, rect, placement);
    (last ? menu.items.at(-1) : menu.items[0]).focus({ preventScroll: true });
  }
  function openSubmenu(name, opener) {
    if (openMenus.length > 1) {
      const previous = openMenus.pop();
      previous.menu.element.hidden = true;
      previous.opener.setAttribute('aria-expanded', 'false');
    }
    const menu = menus.get(name);
    openMenus.push({ menu, opener, placement: 'child' });
    opener.setAttribute('aria-expanded', 'true');
    updateMenus();
    placeMenu(menu, opener.getBoundingClientRect(), 'child');
    menu.items[0].focus({ preventScroll: true });
  }
  function runAction(action) {
    if (action === 'document-start' || action === 'document-end') { beginDocumentInsert(action.slice('document-'.length)); return; }
    const reason = reasonFor(action);
    if (reason) {
      if (openMenus.length) {
        updateMenus();
        const menu = openMenus.at(-1).menu;
        showMenuReason(menu, menu.items.find((item) => item.dataset.action === action));
      } else notice(reason, true);
      return;
    }
    operationStart();
    closeMenus();
    if (action === 'table-before' || action === 'table-after') { beginTableInsert(action.slice('table-'.length)); return; }
    if (action === 'source') { if (editor) endEdit(true); beginEdit({ source: true }); return; }
    if (editor) endEdit(true);
    // 入力の確定が先に送られたなら、座標を使う次の操作は反映を待つ。
    const afterCommit = reasonFor(action);
    if (afterCommit) { notice(afterCommit, true); focusInput(); return; }
    const rect = actionRect(action), model = models[selected.table];
    if (['copy', 'cut'].includes(action) || /-(copy|cut)$/.test(action)) {
      if (action.startsWith('row-') || action.startsWith('col-'))
        selectRange(selected.table, rect, action.startsWith('row-') ? 'rows' : 'cols', true);
      menuCopy(action.endsWith('cut') ? 'cut' : 'copy');
      return;
    }
    if (/^(row|col)-paste-/.test(action)) { startInsert(action, rect); return; }
    if (/^(row|col)-insert-/.test(action)) {
      const [axis, , side] = action.split('-'), row = axis === 'row';
      sendInsert({ axis, side, table: selected.table,
        index: rect[row ? 'row' : 'col'] + (side === 'after' ? rect[row ? 'rows' : 'cols'] - 1 : 0) }, lastCopy);
      focusInput();
      return;
    }
    if (action.endsWith('-select')) {
      selectRange(selected.table, rect, action.startsWith('row-') ? 'rows' : 'cols', true);
      return;
    }
    if (action === 'undo' || action === 'redo') enqueue({ history: action, point: { ...selected } });
    else if (action === 'clear') {
      enqueue({ point: { ...selected }, op: { type: 'set', cells: editGrid.cellsIn(model, rect)
        .map((cell) => ({ row: cell.row, col: cell.col, text: '' })) } });
    } else {
      const [axis, side] = action.split('-'), row = axis === 'row';
      const op = { type: (side === 'duplicate' ? 'duplicate' : side === 'delete' ? 'delete' : 'insert') + (row ? 'Rows' : 'Columns'),
        [row ? 'row' : 'col']: (row ? rect.row : rect.col) + (side === 'after' ? (row ? rect.rows : rect.cols) - 1 : 0),
        count: row ? rect.rows : rect.cols };
      if (side === 'before' || side === 'after') op.side = side;
      enqueue({ point: { ...selected }, op });
    }
    focusInput();
  }
  function handleMenuKey(event) {
    const key = event.key;
    if (!openMenus.length) {
      if (document.activeElement === addButton && !addButton.disabled && !documentEmpty &&
        ['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(key)) {
        openMenu('document', addButton, { last: key === 'ArrowUp' });
        event.preventDefault();
        event.stopImmediatePropagation();
        return true;
      }
      const action = [...buttons].find(([, button]) => button === document.activeElement)?.[0];
      if (!['rows', 'cols', 'table'].includes(action) || buttons.get(action).disabled || !['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(key)) return false;
      openMenu(action, buttons.get(action), { last: key === 'ArrowUp' });
    } else {
      const menu = openMenus.at(-1).menu;
      const index = menu.items.indexOf(document.activeElement), item = menu.items[Math.max(0, index)];
      if (key === 'ArrowDown' || key === 'ArrowUp')
        menu.items[(index + (key === 'ArrowDown' ? 1 : -1) + menu.items.length) % menu.items.length].focus({ preventScroll: true });
      else if (key === 'Home' || key === 'End') (key === 'Home' ? menu.items[0] : menu.items.at(-1)).focus({ preventScroll: true });
      else if (key === 'Enter' || key === ' ') item.click();
      else if (key === 'ArrowRight') {
        if (item.hasAttribute('aria-haspopup') && !reasonFor(item.dataset.action)) openSubmenu(item.dataset.action, item);
      } else if (key === 'ArrowLeft') { if (openMenus.length > 1) closeSubmenu(); }
      else if (key === 'Escape') { if (openMenus.length > 1) closeSubmenu(); else closeMenus(true); }
      else if (key === 'Tab') { closeMenus(); focusInput(); }
      else return false;
    }
    event.preventDefault();
    event.stopImmediatePropagation();
    return true;
  }
  for (const [action, label] of [['rows', '行'], ['cols', '列'], ['table', '表'], ['source', '原文を編集'], ['undo', '元に戻す'], ['redo', 'やり直し']]) {
    if ((action === 'undo' || action === 'redo') && !config.undo) continue;
    const button = make('button', 'gp-edit-' + action, bar);
    button.type = 'button';
    button.textContent = label;
    buttons.set(action, button);
    if (['rows', 'cols', 'table'].includes(action)) {
      button.setAttribute('aria-haspopup', 'menu');
      button.setAttribute('aria-expanded', 'false');
      button.setAttribute('aria-controls', 'gp-edit-menu-' + action);
    }
    button.addEventListener('click', () => {
      if (['rows', 'cols', 'table'].includes(action)) {
        if (button.getAttribute('aria-expanded') === 'true') closeMenus();
        else openMenu(action, button);
      } else runAction(action);
    });
  }
  function updateButtons() {
    addButton.hidden = !mode;
    addButton.disabled = Boolean(documentInsertReason());
    addButton.title = documentInsertReason();
    addButton.textContent = documentEmpty ? '最初のブロックを書く' : '追加';
    if (documentEmpty) addButton.removeAttribute('aria-haspopup');
    else addButton.setAttribute('aria-haspopup', 'menu');
    layer.setAttribute('aria-busy', String(Boolean(busy || queue.length)));
    layer.dataset.render = String(infoRender);
    for (const [action, button] of buttons) {
      const reason = reasonFor(action);
      button.disabled = Boolean(reason);
      button.title = reason;
    }
    updateMenus();
  }

  function setMode(want) {
    if (want) window.gpReviewMode?.exit();
    cancelSource();
    if (!want && editor) endEdit(true);
    mode = want && allowed();
    toggle.setAttribute('aria-pressed', String(mode));
    if (mode) root.dataset.edit = 'on';
    else { cancelCut(); cancelInsert(); delete root.dataset.edit; root.style.removeProperty('--gp-edit-bottom'); clearSelection(); }
    preview.holdScroll(mode);
    preview.setEditing(mode);
    preview.rememberAppearance('edit', mode);
    updateCursors();
    updateHover(null);
    updateCheckboxes();
    updateButtons();
    if (mode) void loadTables();
  }
  window.gpEditMode = { active: () => mode, busy: () => Boolean(editor || composing || busy || queue.length || sourceFetching), exit: () => setMode(false) };
  toggle.addEventListener('click', () => setMode(!mode));

  // ページと md-table-sticky の操作欄、本文の入力の部品は触らない。対象外の表の中の入力は、セルの操作として扱う。
  const ignored = (element) => !element || window.__mdTableSticky?.picking ||
    element.closest('#gp-controls, #gp-header, #gp-notices, #gp-outline, #gp-outline-resizer, [data-md-table-sticky-bar], [data-md-table-sticky-search], [data-md-table-sticky-columns]') ||
    (!element.closest('td, th') && element.closest('input, textarea, select, button, [contenteditable]'));
  let down = null;
  let dragged = false;
  // 登録は一度だけ。全幅表示を開いたときに登録される Esc の受け手より先に動かす。
  window.addEventListener('pointerdown', (event) => {
    if (!mode) return;
    if (!tablePicker.hidden && event.target !== insertTableButton && !tablePicker.contains(event.target)) closeTablePicker();
    if (openMenus.length && !event.target.closest?.('.gp-edit-menu, #gp-edit-rows, #gp-edit-cols, #gp-edit-table, #gp-edit-add')) closeMenus();
    down = { x: event.clientX, y: event.clientY };
    dragged = false;
  }, true);
  const shiftOnly = (event) => event.shiftKey && !event.ctrlKey && !event.altKey && !event.metaKey;
  window.addEventListener('mousedown', (event) => {
    const cell = byCell.get(event.target.closest?.('td, th'));
    // click より前に止め、ブラウザーの Shift による文字の選択を始めさせない。
    if (mode && !stopped && !editor && !ignored(event.target) && event.button === 0 && shiftOnly(event) &&
      cell && selected?.table === cell.table) event.preventDefault();
  }, true);
  window.addEventListener('contextmenu', (event) => {
    if (!mode || stopped || window.__mdTableSticky?.picking || (event.target === input && editor)) return;
    const cell = event.target === input ? selectionCell() : byCell.get(event.target.closest?.('td, th'));
    if (!cell) return;
    event.preventDefault();
    event.stopPropagation();
    if (editor) endEdit(true);
    const rect = selectionRect();
    const inside = selected?.table === cell.table && rect && editGrid.cellsIn(models[cell.table], rect).includes(cell);
    if (!inside) select(cell);
    openMenu('cell', input, { point: { x: event.clientX, y: event.clientY } });
  }, true);
  window.addEventListener('resize', () => closeMenus());
  window.addEventListener('blur', () => closeMenus());
  window.addEventListener('scroll', (event) => {
    if (openMenus[0]?.placement === 'cell' && !event.target.closest?.('.gp-edit-menu')) closeMenus();
  }, true);
  window.addEventListener('pointermove', (event) => {
    if (down && event.buttons && Math.hypot(event.clientX - down.x, event.clientY - down.y) > 4) dragged = true;
  }, true);
  window.addEventListener('click', (event) => {
    if (event.target.closest?.('#gp-outline-resizer')) return;
    if (!mode || stopped || layer.contains(event.target)) return;
    // コメントへの切り替えは、入力を確定せず busy() の確認へ渡す。
    if (event.target.closest?.('#gp-review-toggle')) return;
    if (event.target.closest?.('#gp-edit-add')) return;
    cancelSource();
    // 表の固定の「セルを選んで固定」の最中は、セルの中のタスクを押しても境界の選択だけに任せる。
    const checkbox = !window.__mdTableSticky?.picking && checkboxOf(event.target);
    if (checkbox && documentEditable && infoRender === preview.current()?.render && !dragged) {
      event.stopPropagation();
      if (editor) endEdit(true);
      clearSelection();
      const position = checkboxPosition(checkbox);
      if (document.activeElement === checkbox) refocus = position;
      enqueue({ point: { range: checkbox.getAttribute('data-checkbox-sourcepos') }, checkbox,
        checkboxPosition: position,
        checked: checkbox.checked, originalChecked: !checkbox.checked });
      return;
    }
    if (ignored(event.target)) {
      if (editor && !window.__mdTableSticky?.picking && !(blockPoint(editor.point) && event.target.closest('#gp-theme'))) endEdit(true);
      return;
    }
    const element = event.target.closest?.('td, th');
    const cell = element && byCell.get(element);
    // 編集モードでは、原文を直せるブロックの中のリンクで移動しない（ページ内の移動も。ダブルクリックで直すため）。
    // Ctrl・Shift などを押しながらのクリック（新しいタブで開く）は、ブラウザに任せる。
    if (!(event.ctrlKey || event.shiftKey || event.altKey || event.metaKey) && event.target.closest?.('a') && blockOf(event.target)) {
      event.preventDefault();
      event.stopPropagation();
    }
    if (cell) {
      event.preventDefault();
      event.stopPropagation();
    }
    if (dragged) return;
    operationStart();
    if (cell && !editor && selected?.table === cell.table && shiftOnly(event)) {
      extendTo(cell);
      return;
    }
    if (editor) endEdit(true);
    if (cell) select(cell);
    else {
      const model = models.find((item) => item.table === element?.closest('table'));
      if (model && !model.editable) notice(model.reason, true);
      clearSelection();
    }
  }, true);
  window.addEventListener('dblclick', (event) => {
    if (checkboxOf(event.target)) return;
    if (!mode || stopped || ignored(event.target) || layer.contains(event.target) || dragged ||
      event.ctrlKey || event.shiftKey || event.altKey || event.metaKey) return;
    const cell = byCell.get(event.target.closest?.('td, th'));
    if (!cell) {
      const block = blockOf(event.target), raw = !block && rawBlockOf(event.target);
      if (!block && !raw) return;
      event.preventDefault();
      beginBlock(block || findBlock(raw), { target: raw || null });
      return;
    }
    event.preventDefault();
    select(cell);
    beginEdit();
  }, true);
  window.addEventListener('pointerover', (event) => {
    updateHover(event.target);
    if (!mode) return;
    updateCursors();
    if (ignored(event.target)) return;
    const table = event.target.closest?.('table');
    const model = models.find((item) => item.table === table);
    // 載せただけの知らせは数秒で消し、出ている誤りの知らせは上書きしない。
    if (model && !model.editable && (status.hidden || status.dataset.kind !== 'error')) notice(model.reason);
  }, true);
  // 載せた位置とダブルクリックの対象を同じ関数で決め、本文には印を足さない。
  function updateHover(element) {
    const block = mode && !stopped && !ignored(element) && blockOf(element);
    const raw = mode && !stopped && !ignored(element) && !block && rawBlockOf(element);
    const selectors = [];
    for (let target of block ? [block] : raw ? preview.findHTML(raw) : []) {
      const path = [];
      while (target && target.id !== 'gp-content') {
        path.unshift(':nth-child(' + (Array.prototype.indexOf.call(target.parentElement.children, target) + 1) + ')');
        target = target.parentElement;
      }
      if (target && path.length) selectors.push('#gp-content > ' + path.join(' > '));
    }
    const css = selectors.length ? selectors.join(',\n') + ' { outline: 1px dashed var(--gp-border); outline-offset: 2px; }' : '';
    if (hoverStyle.textContent !== css) hoverStyle.textContent = css;
  }
  let hoverPoint = null;
  window.addEventListener('pointermove', (event) => {
    hoverPoint = { x: event.clientX, y: event.clientY };
    updateHover(event.target);
  }, true);
  window.addEventListener('pointerout', (event) => {
    if (!event.relatedTarget) hoverPoint = null;
    updateHover(event.relatedTarget);
  }, true);
  window.addEventListener('blur', () => { hoverPoint = null; updateHover(null); });
  // 本文の変更やスクロールの後の枠の当て直しは、編集モードの間だけ、1 フレームに 1 回にまとめる
  // （elementFromPoint はレイアウトを強制するので、描画のたびに呼ばない）。
  let hoverFrame = 0;
  const refreshHover = () => {
    if (!mode || !hoverPoint) { updateHover(null); return; }
    if (hoverFrame) return;
    hoverFrame = requestAnimationFrame(() => {
      hoverFrame = 0;
      updateHover(mode && hoverPoint ? document.elementFromPoint(hoverPoint.x, hoverPoint.y) : null);
    });
  };
  window.addEventListener('scroll', refreshHover, true);
  window.addEventListener('resize', refreshHover);
  new MutationObserver(refreshHover).observe(document.getElementById('gp-content'), { childList: true, subtree: true });
  window.addEventListener('keydown', (event) => {
    if (!mode || stopped) return;
    if (!tablePicker.hidden && event.key === 'Escape' && !event.isComposing && !composing) {
      event.preventDefault(); event.stopImmediatePropagation(); closeTablePicker(true); return;
    }
    if (event.isComposing || event.keyCode === 229 || composing) {
      // IME の取り消しは入力欄へ渡すが、全幅表示の Esc へは渡さない。
      if (event.key === 'Escape') event.stopImmediatePropagation();
      return;
    }
    if (handleMenuKey(event)) return;
    if ((document.activeElement !== input && !(blockPoint(editor?.point) && blockBar.contains(document.activeElement))) ||
      (!selected && !blockPoint(editor?.point))) return;
    const key = event.key;
    if (selected && ((key === 'F10' && shiftOnly(event)) || key === 'ContextMenu')) {
      event.preventDefault(); event.stopImmediatePropagation();
      openMenu('cell', input);
      return;
    }
    if (editor) {
      if (blockPoint(editor.point)) {
        if (event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey && (key === 'ArrowUp' || key === 'ArrowDown')) {
          event.preventDefault(); event.stopImmediatePropagation();
          moveBlock(key === 'ArrowUp');
        } else if (key === 'Escape') {
          event.preventDefault(); event.stopImmediatePropagation();
          endEdit(false);
        } else if (key === 'Enter' && (event.ctrlKey || event.metaKey)) {
          event.preventDefault();
          endEdit(true);
        }
        return;
      }
      if (key === 'Escape') {
        event.preventDefault(); event.stopImmediatePropagation();
        endEdit(false); focusInput();
      } else if (key === 'Enter' && event.altKey) {
        event.preventDefault();
        input.setRangeText('\n', input.selectionStart, input.selectionEnd, 'end');
        editor.resize = true;
        saveDraft();
      } else if (key === 'Tab' || (key === 'Enter' && (!editor.source || event.ctrlKey))) {
        event.preventDefault();
        endEdit(true);
        if (key === 'Tab') move(event.shiftKey ? 'previous' : 'next');
        else if (!event.ctrlKey) move(event.shiftKey ? 'up' : 'down');
        else focusInput();
      }
      return;
    }
    if (key === 'Escape') {
      if (pendingInsert) {
        cancelInsert(); event.preventDefault(); event.stopImmediatePropagation(); return;
      }
      if (cutState?.active) {
        cancelCut(); updateButtons(); event.preventDefault(); event.stopImmediatePropagation(); return;
      }
      if (tableSelection) {
        collapseRange(); updateButtons();
        event.preventDefault(); event.stopImmediatePropagation();
        return;
      }
      clearSelection(); return;
    }
    const direction = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' }[key];
    if (direction) { event.preventDefault(); operationStart(); if (shiftOnly(event)) extend(direction); else move(direction); }
    else if (key === 'Tab') { event.preventDefault(); operationStart(); move(event.shiftKey ? 'previous' : 'next'); }
    else if (key === 'Enter' || key === 'F2') { event.preventDefault(); beginEdit(); }
    else if (key === 'Delete' || key === 'Backspace') {
      event.preventDefault();
      if (key === 'Delete' && tableSelection) runAction('clear');
      else { beginEdit({ replace: true }); endEdit(true); }
    } else if (config.undo && event.ctrlKey && ['z', 'y'].includes(key.toLowerCase())) {
      event.preventDefault();
      const history = key.toLowerCase() === 'y' || event.shiftKey ? 'redo' : 'undo';
      runAction(history);
    }
  }, true);
  window.addEventListener('compositionstart', (event) => {
    if (event.target !== input || !mode || (!selected && !blockPoint(editor?.point))) return;
    if (!editor) beginEdit({ replace: true, value: input.value, composition: true });
    composing = true;
  }, true);
  window.addEventListener('compositionend', (event) => {
    if (event.target === input) { composing = false; saveDraft(); }
  }, true);
  window.addEventListener('input', (event) => {
    if (event.target !== input || !mode || (!selected && !blockPoint(editor?.point))) return;
    closeTablePicker();
    if (!editor) beginEdit({ replace: true, value: input.value, composition: event.isComposing });
    if (event.isComposing) composing = true;
    editor.resize = true;
    saveDraft();
  }, true);
  window.addEventListener('paste', (event) => {
    if (!mode || stopped || document.activeElement !== input || !selected || editor) return;
    const custom = event.clipboardData?.getData(editGrid.TYPE);
    const text = event.clipboardData?.getData('text/plain');
    // 文字の無い貼り付け（画像など）では、セルを空にする編集を始めない。
    if (!custom && !text) return;
    event.preventDefault();
    const pending = pendingInsert;
    cancelInsert();
    operationStart();
    const reason = reasonFor('clipboard-write');
    if (reason) { notice(reason, true); return; }
    const envelope = custom ? editGrid.parseEnvelope(custom) : null;
    const fallbackNotice = custom && !envelope ? '写しの形が正しくないので、文字として貼りました' : '';
    if (envelope && isCancelled(envelope)) { notice(cutCancelled, true); return; }
    if (pending) {
      if (pending.version !== externalVersion || models[pending.table]?.table !== pending.tableElement) {
        notice('文書が変わったので、もう一度操作してください', true); return;
      }
      try {
        const values = envelope ? null : editGrid.parseTSV(text || '');
        if (envelope || values.length) sendInsert(pending, envelope, values, fallbackNotice);
      } catch (error) { notice(error.message, true); }
      return;
    }
    const rect = selectionRect();
    if (envelope) {
      const moving = envelope.cut && envelope.origin === origin;
      const op = { type: moving ? 'moveCells' : 'pasteFragment', row: rect.row, col: rect.col };
      if (moving) op.from = sourceOf(envelope);
      else op.fragment = envelope.fragment;
      enqueue({ point: { ...selected }, op, clipboard: true, successNotice: foreignNotice(envelope) });
    } else if (text && /[\t\r\n]/.test(text)) {
      try {
        const values = editGrid.parseTSV(text);
        if (values.length) enqueue({ point: { ...selected }, clipboard: true,
          op: { type: 'paste', row: rect.row, col: rect.col, values }, successNotice: fallbackNotice });
      } catch (error) { notice(error.message, true); }
    } else if (text) {
      const cell = models[selected.table].slots[rect.row]?.[rect.col];
      if (cell) select(cell);
      beginEdit({ replace: true, value: text });
      if (fallbackNotice) notice(fallbackNotice);
    } else if (fallbackNotice) notice(fallbackNotice, true);
  }, true);
  // 空の textarea でも Chromium の cut は届く。入力中はブラウザーのふつうのコピー・切り取りに任せる。
  window.addEventListener('copy', (event) => copySelection(event, clipboardIntent?.kind || 'copy'), true);
  window.addEventListener('cut', (event) => copySelection(event, 'cut'), true);
  document.addEventListener('gp:render', () => {
    updateButtons();
    const render = preview.current()?.render;
    if (render !== observedRender) {
      cancelCut();
      closeMenus();
      // 自分の書き込みの描画は、送信ごとに最初の 1 回だけ（時間切れの後に遅れて届く 1 回を含む）。ほかは外の変更と数える。
      if (busy && !busy.seen && render > busy.render) {
        busy.seen = true;
        shiftInsertDrafts(busy.item);
      }
      else if (late !== null && render > late) late = null;
      else externalVersion++;
      checkInsert();
    }
    observedRender = render;
    stopped = false;
    updateCheckboxes(true);
    toggle.hidden = !allowed();
    if (!allowed()) { setMode(false); return; }
    if (mode || busy || queue.length) void loadTables();
  });
  document.addEventListener('gp:stop', () => {
    stopped = true;
    updateHover(null);
    cancelCut();
    cancelInsert();
    cancelSource();
    updateCheckboxes();
    fetching?.abort();
    saveDraft();
    editor = null;
    // 止まっている間に並べた送信は持ち越さない。文字は下書きに残す。
    discardQueue();
    busy?.item.mask?.remove();
    clearSelection();
    updateButtons();
  });

  clearSelection();
  status.hidden = true;
  toggle.hidden = !allowed();
  if (allowed() && preview.appearance.edit === true) setMode(true);
})();
