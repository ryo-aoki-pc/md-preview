// 選択枠と入力欄を本文の上へ重ね、表とブロックの原文を編集する。
'use strict';

(() => {
  const preview = window.gpPreview;
  const config = preview?.config.edit;
  if (!config) return;

  const root = document.documentElement;
  const toggle = document.getElementById('gp-edit-toggle');
  const layer = document.createElement('div');
  layer.id = 'gp-edit-layer';
  const make = (tag, id, parent = layer) => {
    const element = document.createElement(tag);
    element.id = id;
    parent.append(element);
    return element;
  };
  const cursorStyle = make('style', 'gp-edit-cursor');
  const frame = make('div', 'gp-edit-selection');
  const input = make('textarea', 'gp-edit-input');
  input.setAttribute('aria-label', '表のセルの編集');
  input.spellcheck = false;
  const sourceLabel = make('span', 'gp-edit-source-label');
  sourceLabel.textContent = '原文';
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
  let pendingBlock = null;
  let sourceFetching = null;
  let sourceNumber = 0;
  let observedRender = preview.current()?.render;
  const queue = [];
  const drafts = new Map();
  const buttons = new Map();
  const keyOf = (point) => point.range ? 'range:' + point.range : [point.table, point.row, point.col].join(':');
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
  const rangePattern = /^[1-9][0-9]*:[1-9][0-9]*-[1-9][0-9]*:[0-9]+$/;
  const blockSelector = 'p, h1, h2, h3, h4, h5, h6, li, dt, dd, blockquote, pre';
  const excludedBlock = 'table, section[data-footnotes], .section-nav, .gp-math-display, .gp-math-inline, .gp-mermaid, .gp-plantuml-error, [data-diagram], .js-render-math, .js-render-mermaid';
  function blockOf(element) {
    if (!element?.closest || !element.closest('#gp-content') || element.closest(excludedBlock)) return null;
    for (let node = element; node && node.id !== 'gp-content'; node = node.parentElement) {
      if (node.matches(blockSelector) && rangePattern.test(node.getAttribute('data-sourcepos') || '')) return node;
    }
    return null;
  }
  function checkboxOf(element) {
    return element?.matches?.('input.task-list-item-checkbox[type="checkbox"][data-checkbox-sourcepos]:not([data-inapplicable])') &&
      element.closest('#gp-content') && !element.closest('table, section[data-footnotes]') &&
      rangePattern.test(element.getAttribute('data-checkbox-sourcepos')) ? element : null;
  }
  // checked の属性の変更では input だけが置き換わることがある。同じ親の同じ位置だけを引き継ぐ。
  function checkboxElement(item) {
    if (!item.checkbox || item.checkbox.isConnected) return item.checkbox;
    const element = item.checkboxParent?.isConnected && item.checkboxParent.children[item.checkboxIndex];
    if (checkboxOf(element)) item.checkbox = element;
    return item.checkbox;
  }
  // 属性を変えても差分更新の節点の対応は保つ。描画が届いたら disabled と checked を当て直す。
  function updateCheckboxes(reset = false) {
    const pendingItems = [...(busy ? [busy.item] : []), ...queue];
    for (const item of pendingItems) checkboxElement(item);
    for (const element of document.querySelectorAll('#gp-content input.task-list-item-checkbox')) {
      if (!checkboxOf(element)) continue;
      element.disabled = !(mode && !stopped && documentEditable && infoRender === preview.current()?.render);
      if (reset) {
        const pending = pendingItems.slice().reverse().find((item) => item.checkbox === element);
        element.checked = pending ? pending.checked : element.hasAttribute('checked');
      }
    }
    // 切り替えたチェックボックスは、描き直しで別の要素になる。フォーカスがあったなら、また押せるようになったときに戻す。
    if (refocus && mode && infoRender === preview.current()?.render) {
      const element = refocus.parent.isConnected && refocus.parent.children[refocus.index];
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
    status.dataset.kind = error ? 'error' : 'info';
    if (text && !error) noticeTimer = setTimeout(() => { status.hidden = true; }, 5000);
  }
  function operationStart() {
    if (status.dataset.kind === 'error') notice('');
  }

  // サーバーと同じく、区画を木の順にたどる。table.rows は tfoot の順序が変わる。
  function grid(table, info) {
    const groups = [];
    let direct = [];
    const flush = () => { if (direct.length) groups.push(direct); direct = []; };
    for (const child of table.children) {
      if (child.localName === 'tr') { direct.push(child); continue; }
      flush();
      if (['thead', 'tbody', 'tfoot'].includes(child.localName))
        groups.push(Array.from(child.children).filter((row) => row.localName === 'tr'));
    }
    flush();
    if (groups.reduce((n, rows) => n + rows.length, 0) !== info.rows) return null;
    const cells = [];
    const slots = [];
    let row = 0, width = 0;
    for (const group of groups) {
      const occupied = new Map();
      for (let local = 0; local < group.length; local++, row++) {
        let col = 0;
        for (const element of group[local].children) {
          if (!element.matches('th, td')) continue;
          while ((occupied.get(col) || 0) > local) col++;
          const colspan = element.colSpan;
          let rowspan = element.rowSpan;
          if (!rowspan || rowspan > group.length - local) rowspan = group.length - local;
          const cell = info.cells[cells.length];
          if (!cell || col + colspan > info.cols || cell.row !== row || cell.col !== col ||
            cell.rowspan !== rowspan || cell.colspan !== colspan || cell.header !== (element.localName === 'th'))
            return null;
          const item = { ...cell, element, table: info.index };
          for (let x = col; x < col + colspan; x++) {
            if ((occupied.get(x) || 0) > local) return null;
            occupied.set(x, local + rowspan);
            for (let y = row; y < row + rowspan; y++) {
              slots[y] ??= [];
              slots[y][x] = item;
            }
          }
          cells.push(item);
          col += colspan;
          width = Math.max(width, col);
        }
      }
    }
    return cells.length === info.cells.length && width === info.cols ? { cells, slots } : null;
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
      byCell = new WeakMap();
      models = data.tables.map((info, i) => {
        const nested = tables[i].querySelector('table') || tables[i].parentElement.closest('table');
        const shape = info.editable && data.document.editable && !nested ? grid(tables[i], info) : null;
        const model = { info, table: tables[i], editable: Boolean(shape),
          reason: !data.document.editable ? data.document.reason : nested ? '入れ子の表' :
            info.reason || '画面と原文のセルの形が違います',
          cells: shape?.cells || [], slots: shape?.slots || [] };
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
          if (editor) editor.point = { ...selected };
        } else clearSelection();
      }
      // 編集中のブロックが描き直しで別の要素になったら、見えない入力欄を残さない。文字は下書きに残す。
      if (editor?.point.range && !editor.element.isConnected) {
        clearSelection();
        notice('文書が変わったので、もう一度操作してください', true);
      }
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
  function select(cell, focus = true) {
    input.setAttribute('aria-label', '表のセルの編集');
    selected = pointOf(cell);
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
    saveDraft();
    editor = null;
    selected = null;
    composing = false;
    // 開き直した入力欄が閉じたら、次の送信を止めたままにしない。
    failed = null;
    input.value = '';
    input.hidden = frame.hidden = sourceLabel.hidden = bar.hidden = true;
    layer.dataset.typing = 'off';
    cancelAnimationFrame(animation);
    animation = 0;
    if (document.activeElement === input) input.blur();
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
  function place(element, cell, size = null) {
    const rect = cell.getBoundingClientRect();
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
  function position() {
    animation = 0;
    const cell = editor?.point.range ? { element: editor.element } : selectionCell();
    if (!cell || !mode) return;
    frame.hidden = Boolean(editor?.point.range);
    frame.dataset.pending = String([...queue, ...(busy ? [busy.item] : [])].some((item) => samePoint(item.point, selected)));
    const rect = place(frame, cell.element);
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
      const height = editor.point.range
        ? Math.min(Math.max(64, editor.content), Math.max(64, innerHeight - rect.top - 8))
        : Math.min(Math.max(rect.height, editor.content), Math.max(rect.height, innerHeight - rect.top - 8));
      place(input, cell.element, { width, height });
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
    saveDraft();
    selected = null;
    editor = { ...draft, source: true, em: 16, resize: true };
    input.value = draft.value;
    input.setAttribute('aria-label', 'ブロックの原文の編集');
    input.style.fontFamily = 'sans-serif';
    input.style.fontSize = '16px';
    input.style.fontWeight = 'normal';
    input.style.lineHeight = '1.6';
    input.hidden = false;
    bar.hidden = frame.hidden = true;
    layer.dataset.typing = 'on';
    reveal(draft.element);
    focusInput();
    input.setSelectionRange(input.value.length, input.value.length);
    cancelAnimationFrame(animation);
    position();
  }
  async function loadBlock() {
    const element = pendingBlock;
    if (!element || sourceFetching || busy || queue.length || failed || stopped || !mode ||
      infoRender !== preview.current()?.render) return;
    pendingBlock = null;
    if (!documentEditable) { notice(documentReason || 'この文書は編集できません', true); return; }
    if (!element.isConnected || blockOf(element) !== element) return;
    const range = element.getAttribute('data-sourcepos');
    const render = preview.current().render, version = externalVersion, number = ++sourceNumber;
    const controller = new AbortController();
    sourceFetching = controller;
    const target = url('/source');
    target.searchParams.set('render', render);
    target.searchParams.set('range', range);
    try {
      const response = await fetch(target, { signal: controller.signal });
      if (number !== sourceNumber || response.status === 409) return;
      const data = await response.json();
      if (!response.ok) throw new Error(messageOf(data) || '原文を読み込めませんでした');
      if (!mode || stopped || number !== sourceNumber || render !== preview.current()?.render ||
        version !== externalVersion || !element.isConnected) return;
      if (data.render !== render || typeof data.text !== 'string') throw new Error('原文の応答の形が正しくありません');
      const point = { range }, saved = drafts.get(keyOf(point));
      const value = saved?.original === data.text ? saved.value : data.text;
      drafts.delete(keyOf(point));
      openBlock({ point, element, render, version, original: data.text, value });
    } catch (error) {
      if (number === sourceNumber && error.name !== 'AbortError') notice(error.message || '原文を読み込めませんでした', true);
    } finally {
      if (number === sourceNumber) sourceFetching = null;
    }
  }
  function beginBlock(element) {
    cancelSource();
    if (editor) endEdit(true);
    clearSelection();
    pendingBlock = element;
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
    operationStart();
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
    const draft = { ...editor, point: { ...editor.point }, value: input.value };
    editor = null;
    composing = false;
    layer.dataset.typing = 'off';
    input.value = '';
    drafts.delete(keyOf(draft.point));
    const retry = failed && samePoint(failed.point, draft.point);
    if (retry) failed = null;
    if (draft.point.range) clearSelection();
    if (commit && draft.value !== draft.original) {
      if (draft.version !== externalVersion) {
        drafts.set(keyOf(draft.point), draft);
        discardQueue();
        notice('文書が変わったので、もう一度操作してください', true);
        void loadTables();
        updateButtons();
        return;
      }
      if (draft.point.range) {
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
    const model = models[cell.table];
    let next;
    if (direction === 'next' || direction === 'previous') {
      next = model.cells[model.cells.indexOf(cell) + (direction === 'next' ? 1 : -1)];
    } else {
      const row = direction === 'up' ? cell.row - 1 : direction === 'down' ? cell.row + cell.rowspan : cell.row;
      const col = direction === 'left' ? cell.col - 1 : direction === 'right' ? cell.col + cell.colspan : cell.col;
      next = model.slots[row]?.[col];
    }
    if (next) select(next);
    else focusInput();
  }

  function addMask(item) {
    if (item.value === undefined || item.point.range) return;
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
  function finishSend() {
    if (!busy) return;
    clearTimeout(busy.timer);
    busy.item.mask?.remove();
    busy = null;
    updateButtons();
    void pump();
    void loadBlock();
  }
  function completeSend() {
    if (busy?.ok && preview.current()?.render > busy.render && infoRender === preview.current()?.render)
      finishSend();
  }
  function reopen(item) {
    failed = item.value === undefined ? null : item;
    if (!failed) return;
    drafts.set(keyOf(item.point), { ...item });
    if (item.point.range) {
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
    const target = !item.history && !item.point.range && models[item.point.table];
    if (!item.history && !item.point.range && (!target?.editable || target.table !== item.tableElement ||
      !target.slots[item.point.row]?.[item.point.col])) {
      item.mask?.remove();
      discardQueue();
      notice('表の形が変わったので、もう一度操作してください', true);
      updateButtons();
      return;
    }
    const render = preview.current().render;
    checkboxElement(item);
    if (item.point.range && (!documentEditable || (item.checkbox ? !item.checkbox.isConnected : item.render !== render))) {
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
          discardQueue();
          saveDraft();
          if (item.value !== undefined) drafts.set(keyOf(item.point), { ...item });
          notice(['stale', 'changed'].includes(data?.error?.code)
            ? '文書が変わったので、もう一度操作してください'
            : messageOf(data) || '書き込めませんでした', true);
          void loadTables();
        } else if (response.status === 422) {
          reopen(item);
          notice(messageOf(data) || 'この操作では書き込めませんでした', true);
          // 入力欄を開き直さなかったとき（行・列の操作、編集モードの外）は、並んでいる送信を続ける。
          if (!failed) void pump();
        } else {
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
      if (data.warnings?.length) notice(data.warnings.join(' ／ '));
      busy.ok = true;
      if (data.unchanged) { finishSend(); void loadTables(); return; }
      completeSend();
      if (busy?.item === item) busy.timer = setTimeout(() => {
        // 遅れて届くこの書き込みの描画を、外の変更と数えない。
        if (!busy.seen) late = busy.render;
        if (!stopped) notice('書き込み後の表示が届きません。表の情報を読み直します', true);
        infoRender = 0;
        finishSend();
        void loadTables();
      }, 3000);
    } catch (error) {
      item.mask?.remove();
      busy = null;
      discardQueue();
      if (item.checkbox?.isConnected) item.checkbox.checked = item.originalChecked;
      reopen(item);
      notice('書き込めませんでした' + (error.message ? ' ／ ' + error.message : ''), true);
      updateButtons();
      void loadBlock();
    }
  }

  // 引用符内の改行・タブと二重の引用符を含む TSV を読む。
  function parseTSV(text) {
    const rows = [];
    let row = [], value = '', quoted = false, start = true;
    text = text.replace(/\r\n?/g, '\n');
    for (let i = 0; i < text.length; i++) {
      const char = text[i];
      if (quoted) {
        if (char === '"' && text[i + 1] === '"') { value += '"'; i++; }
        else if (char === '"') quoted = false;
        else value += char;
      } else if (char === '"' && start) { quoted = true; start = false; }
      else if (char === '\t' || char === '\n') {
        row.push(value); value = ''; start = true;
        if (char === '\n') { rows.push(row); row = []; }
      } else { value += char; start = false; }
    }
    if (quoted) throw new Error('貼り付ける値の引用符が閉じていません');
    row.push(value);
    rows.push(row);
    while (rows.length && rows.at(-1).every((cell) => cell === '')) rows.pop();
    return rows;
  }

  let undoState = {};
  const actions = [
    ['row-before', '上に行を挿入'], ['row-after', '下に行を挿入'], ['row-delete', '行を削除'],
    ['col-before', '左に列を挿入'], ['col-after', '右に列を挿入'], ['col-delete', '列を削除'],
    ['source', '原文を編集'], ['undo', '元に戻す'], ['redo', 'やり直し'],
  ];
  for (const [action, label] of actions) {
    if ((action === 'undo' || action === 'redo') && !config.undo) continue;
    const button = make('button', 'gp-edit-' + action, bar);
    button.type = 'button';
    button.textContent = label;
    buttons.set(action, button);
    button.addEventListener('click', () => {
      operationStart();
      if (action === 'source') { if (editor) endEdit(true); beginEdit({ source: true }); return; }
      if (editor) endEdit(true);
      const cell = selectionCell();
      if (!cell) return;
      if (action === 'undo' || action === 'redo') enqueue({ history: action, point: { ...selected } });
      else {
        const [axis, side] = action.split('-');
        const row = axis === 'row';
        const position = row ? cell.row : cell.col;
        const span = row ? cell.rowspan : cell.colspan;
        const op = { type: (side === 'delete' ? 'delete' : 'insert') + (row ? 'Rows' : 'Columns'),
          [row ? 'row' : 'col']: position + (side === 'after' ? span - 1 : 0),
          count: side === 'delete' ? span : 1 };
        if (side !== 'delete') op.side = side;
        enqueue({ point: { ...selected }, op });
      }
      focusInput();
    });
  }
  function updateButtons() {
    layer.setAttribute('aria-busy', String(Boolean(busy || queue.length)));
    layer.dataset.render = String(infoRender);
    const cell = selectionCell();
    const info = cell && models[cell.table]?.info;
    for (const [action, button] of buttons) {
      let reason = !cell ? 'セルを選んでください' : stopped ? 'プレビューが止まっています' : '';
      if (!reason && (busy || queue.length)) reason = '前の書き込みの反映を待っています';
      if (!reason && action.startsWith('row-')) {
        reason = info.rowsLocked;
        if (!reason && info.kind === 'pipe' && cell.row === 0 && action !== 'row-after')
          reason = 'パイプの表の見出し行は移動・削除できません';
      }
      if (!reason && action.startsWith('col-')) reason = info.colsLocked;
      if (!reason && action === 'source' && info.kind !== 'html') reason = 'HTML の表だけで使えます';
      if (!reason && (action === 'undo' || action === 'redo') && !undoState[action]) reason = '履歴がありません';
      button.disabled = Boolean(reason);
      button.title = reason || '';
    }
  }

  function setMode(want) {
    cancelSource();
    if (!want && editor) endEdit(true);
    mode = want && allowed();
    toggle.setAttribute('aria-pressed', String(mode));
    if (mode) root.dataset.edit = 'on';
    else { delete root.dataset.edit; root.style.removeProperty('--gp-edit-bottom'); clearSelection(); }
    preview.holdScroll(mode);
    preview.setEditing(mode);
    preview.rememberAppearance('edit', mode);
    updateCursors();
    updateCheckboxes();
    if (mode) void loadTables();
  }
  toggle.addEventListener('click', () => setMode(!mode));

  // ページと md-table-sticky の操作欄、本文の入力の部品は触らない。表のセルの中の入力（表の中のタスクのチェックボックス）は、セルの操作として扱う。
  const ignored = (element) => !element || window.__mdTableSticky?.picking ||
    element.closest('#gp-controls, #gp-header, #gp-notices, #gp-outline, [data-md-table-sticky-bar], [data-md-table-sticky-search], [data-md-table-sticky-columns]') ||
    (!element.closest('td, th') && element.closest('input, textarea, select, button, [contenteditable]'));
  let down = null;
  let dragged = false;
  // 登録は一度だけ。全幅表示を開いたときに登録される Esc の受け手より先に動かす。
  window.addEventListener('pointerdown', (event) => {
    if (!mode) return;
    down = { x: event.clientX, y: event.clientY };
    dragged = false;
  }, true);
  window.addEventListener('pointermove', (event) => {
    if (down && event.buttons && Math.hypot(event.clientX - down.x, event.clientY - down.y) > 4) dragged = true;
  }, true);
  window.addEventListener('click', (event) => {
    if (!mode || stopped || layer.contains(event.target)) return;
    cancelSource();
    const checkbox = checkboxOf(event.target);
    if (checkbox && documentEditable && infoRender === preview.current()?.render && !dragged) {
      event.stopPropagation();
      if (editor) endEdit(true);
      clearSelection();
      const index = Array.prototype.indexOf.call(checkbox.parentElement.children, checkbox);
      if (document.activeElement === checkbox) refocus = { parent: checkbox.parentElement, index };
      enqueue({ point: { range: checkbox.getAttribute('data-checkbox-sourcepos') }, checkbox,
        checkboxParent: checkbox.parentElement, checkboxIndex: index,
        checked: checkbox.checked, originalChecked: !checkbox.checked });
      return;
    }
    if (ignored(event.target)) {
      if (editor && !window.__mdTableSticky?.picking) endEdit(true);
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
    if (editor) endEdit(true);
    if (cell) select(cell);
    else {
      const model = models.find((item) => item.table === element?.closest('table'));
      if (model && !model.editable) notice(model.reason, true);
      clearSelection();
    }
  }, true);
  window.addEventListener('dblclick', (event) => {
    if (!mode || stopped || ignored(event.target) || layer.contains(event.target) || dragged ||
      event.ctrlKey || event.shiftKey || event.altKey || event.metaKey) return;
    const cell = byCell.get(event.target.closest?.('td, th'));
    if (!cell) {
      const block = blockOf(event.target);
      if (!block) return;
      event.preventDefault();
      beginBlock(block);
      return;
    }
    event.preventDefault();
    select(cell);
    beginEdit();
  }, true);
  window.addEventListener('pointerover', (event) => {
    if (!mode) return;
    updateCursors();
    if (ignored(event.target)) return;
    const table = event.target.closest?.('table');
    const model = models.find((item) => item.table === table);
    // 載せただけの知らせは数秒で消し、出ている誤りの知らせは上書きしない。
    if (model && !model.editable && (status.hidden || status.dataset.kind !== 'error')) notice(model.reason);
  }, true);
  window.addEventListener('keydown', (event) => {
    if (!mode || stopped || document.activeElement !== input || (!selected && !editor?.point.range) ||
      event.isComposing || event.keyCode === 229 || composing) return;
    const key = event.key;
    if (editor) {
      if (editor.point.range) {
        if (key === 'Escape') {
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
    if (key === 'Escape') { clearSelection(); return; }
    const direction = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' }[key];
    if (direction) { event.preventDefault(); operationStart(); move(direction); }
    else if (key === 'Tab') { event.preventDefault(); operationStart(); move(event.shiftKey ? 'previous' : 'next'); }
    else if (key === 'Enter' || key === 'F2') { event.preventDefault(); beginEdit(); }
    else if (key === 'Delete' || key === 'Backspace') {
      event.preventDefault(); beginEdit({ replace: true }); endEdit(true);
    } else if (config.undo && event.ctrlKey && ['z', 'y'].includes(key.toLowerCase())) {
      event.preventDefault();
      const history = key.toLowerCase() === 'y' || event.shiftKey ? 'redo' : 'undo';
      if (undoState[history] || busy || queue.length) enqueue({ history, point: { ...selected } });
    }
  }, true);
  window.addEventListener('compositionstart', (event) => {
    if (event.target !== input || !mode || (!selected && !editor?.point.range)) return;
    if (!editor) beginEdit({ replace: true, value: input.value, composition: true });
    composing = true;
  }, true);
  window.addEventListener('compositionend', (event) => {
    if (event.target === input) { composing = false; saveDraft(); }
  }, true);
  window.addEventListener('input', (event) => {
    if (event.target !== input || !mode || (!selected && !editor?.point.range)) return;
    if (!editor) beginEdit({ replace: true, value: input.value, composition: event.isComposing });
    if (event.isComposing) composing = true;
    editor.resize = true;
    saveDraft();
  }, true);
  window.addEventListener('paste', (event) => {
    if (!mode || stopped || document.activeElement !== input || !selected || editor) return;
    const text = event.clipboardData?.getData('text/plain');
    // 文字の無い貼り付け（画像など）では、セルを空にする編集を始めない。
    if (!text) return;
    event.preventDefault();
    operationStart();
    if (/[\t\r\n]/.test(text)) {
      try {
        const values = parseTSV(text);
        if (values.length) enqueue({ point: { ...selected }, op: { type: 'paste', row: selected.row, col: selected.col, values } });
      } catch (error) { notice(error.message, true); }
    } else beginEdit({ replace: true, value: text });
  }, true);
  // 選択中のコピーは、セル用 Markdown（原文の編集になるセルは原文）を渡す。編集中は入力欄のふつうのコピー。
  window.addEventListener('copy', (event) => {
    if (!mode || document.activeElement !== input || editor || !event.clipboardData) return;
    const cell = selectionCell();
    if (!cell) return;
    event.preventDefault();
    event.clipboardData.setData('text/plain', cell.raw ? cell.source : cell.text);
  }, true);
  document.addEventListener('gp:render', () => {
    const render = preview.current()?.render;
    if (render !== observedRender) {
      // 自分の書き込みの描画は、送信ごとに最初の 1 回だけ（時間切れの後に遅れて届く 1 回を含む）。ほかは外の変更と数える。
      if (busy && !busy.seen && render > busy.render) busy.seen = true;
      else if (late !== null && render > late) late = null;
      else externalVersion++;
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
