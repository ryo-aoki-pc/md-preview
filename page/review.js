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
  const skipStale = button('gp-review-skip-stale', actions, '要確認をまとめて送らない', () => {
    if (locked()) return;
    if (bulkSkipped.size) {
      for (const item of comments) if (bulkSkipped.has(item.id)) item.skip = false;
      bulkSkipped.clear();
    } else {
      for (const item of comments) {
        if (item.stale && item.state === 'pending' && !item.skip) {
          item.skip = true;
          bulkSkipped.add(item.id);
        }
      }
    }
    changed();
  });
  const promptLabel = make('label', '', panel, '依頼文の形 ');
  const promptMode = make('select', 'gp-review-mode', promptLabel);
  for (const [value, label] of [['plan', '/plan で計画'], ['plan-text', '日本語で計画'], ['direct', '直接直す']]) {
    const option = make('option', '', promptMode, label);
    option.value = value;
  }
  // 依頼文の形は保存せず、ページを開くたびに計画から始める。
  promptMode.value = 'plan';
  promptMode.addEventListener('change', changed);
  const output = make('section', 'gp-review-output', panel);
  make('h3', '', output, '依頼文');
  const promptInput = make('textarea', 'gp-review-prompt', output);
  promptInput.setAttribute('aria-label', '依頼文');
  promptInput.readOnly = true;
  promptInput.rows = 10;
  const launch = button('gp-review-launch', output, 'コピーして Agents Window を開く', () => copyPrompt(true));
  const copy = button('gp-review-copy', output, 'コピー', () => copyPrompt());
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
  // 内容の指紋（ブロック b:・生の HTML h:・セル c:）と、セルの行と列の鍵。版 1 で保存したものは空から始める。
  const fingerprintPattern = /^(?:|[bhc]:[0-9a-f]{16})$/;
  const keyPattern = /^[0-9a-f]{16}$/;
  const versionPattern = /^(?:|[0-9a-f]{32})$/i;
  // 1 つの文書の分を読む。形が違えば、その文書の分だけを捨てる（ほかの文書のコメントは残す）。
  function readItems(items, version) {
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
      const extra = version === 2 ? item : { fp: '', version: '', skip: false };
      const keys = extra.keys;
      if (typeof extra.fp !== 'string' || !fingerprintPattern.test(extra.fp) || typeof extra.version !== 'string' ||
        !versionPattern.test(extra.version) || typeof extra.skip !== 'boolean' ||
        (keys !== undefined && (target.kind !== 'cell' || !plain(keys) || !keyPattern.test(keys.row) || !keyPattern.test(keys.col) ||
          (keys.start !== undefined && !integer(keys.start)) ||
          (keys.dupCol !== undefined && typeof keys.dupCol !== 'boolean') || (keys.dupRow !== undefined && typeof keys.dupRow !== 'boolean'))))
        return null;
      seen.add(item.id);
      list.push({ id: item.id, target, label: item.label, text: item.text, render: item.render,
        state: item.state, stale: item.stale, error: item.error, ...(item.selection ? { selection: item.selection } : {}),
        fp: extra.fp, ...(keys ? { keys: { row: keys.row, col: keys.col, ...(keys.start !== undefined ? { start: keys.start } : {}),
          ...(keys.dupCol !== undefined ? { dupCol: keys.dupCol } : {}), ...(keys.dupRow !== undefined ? { dupRow: keys.dupRow } : {}) } } : {}),
        version: extra.version.toLowerCase(), skip: extra.skip });
    }
    return list;
  }
  function readStorage() {
    const empty = () => ({ version: 2, docs: Object.create(null) });
    try {
      const saved = JSON.parse(sessionStorage.getItem(storageKey));
      if (!plain(saved) || ![1, 2].includes(saved.version) || !plain(saved.docs)) return empty();
      const result = empty();
      for (const [key, items] of Object.entries(saved.docs).slice(0, 100)) {
        if (!shortOK(key, 2100)) continue;
        const list = readItems(items, saved.version);
        if (list) result.docs[key] = list;
      }
      return result;
    } catch { return empty(); }
  }
  const saved = readStorage();
  let comments = saved.docs[documentKey] || [];
  // 一括で変えた印だけを戻せるようにし、ページを開き直したら忘れる。
  const bulkSkipped = new Set();
  // 版 1 から移した項目のうち、読み込んだときに要確認でなかったもの。最初に確かめた内容を指紋にしてよい
  // （途中で表の情報が読めず一度要確認になっても、内容が見つかれば戻す）。
  const adoptable = new Set(comments.filter((item) => !item.fp && !item.stale).map((item) => item.id));
  let selected = null;
  let editing = null;
  let mode = false;
  let stopped = false;
  let models = [];
  let byCell = new WeakMap();
  let infoRender = 0;
  let versionRender = 0, currentVersion = '';
  // このページの編集だけを保持し、再読み込みには持ち越さない。古い対応は件数と総区画数で捨てる。
  const changes = [];
  let keepCount = 0;
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
    // 消えたカードと、直して保存したなどで印が外れたカードは、一括で戻す記録から外す。
    for (const id of bulkSkipped) if (!comments.some((item) => item.id === id && item.skip)) bulkSkipped.delete(id);
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
    prepare.disabled = locked() || !preview.current()?.render || !comments.some((item) => item.state === 'pending' && !item.skip);
    clearCopied.disabled = locked() || !comments.some((item) => item.state === 'copied');
    skipStale.textContent = bulkSkipped.size ? 'まとめて送らないを戻す' : '要確認をまとめて送らない';
    const skipReason = locked() ? '処理中またはプレビューが終了しています' :
      !bulkSkipped.size && !comments.some((item) => item.stale && item.state === 'pending' && !item.skip) ?
        '送る対象になっている未コピーの要確認コメントがありません' : '';
    skipStale.disabled = Boolean(skipReason);
    skipStale.title = skipReason;
    promptMode.disabled = locked();
    promptMode.title = locked() ? '処理中またはプレビューが終了しています' : '';
    copy.disabled = launch.disabled = locked() || !prepared;
    output.hidden = !prepared;
    cards.replaceChildren();
    for (const item of comments) {
      const card = make('article', '', cards);
      card.className = 'gp-review-card';
      card.dataset.id = item.id;
      make('h3', '', card, item.label);
      if (item.selection) make('p', '', card, '選んだ文字: ' + item.selection);
      make('p', '', card, item.text);
      const state = make('p', '', card, (item.error ? '誤り: ' + item.error : item.stale ? '要確認' : item.state === 'copied' ? 'コピー済み' : '未コピー') +
        (item.state === 'pending' && item.skip ? '（今回は送らない）' : ''));
      state.className = 'gp-review-state';
      if (item.stale) {
        const hint = make('p', '', card, '対象の内容が変わったか、見つかりません。「直す」で選び直すか、「今回は送らない」にしてください。');
        hint.className = 'gp-review-hint';
      }
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
      fix.dataset.role = 'fix';
      fix.disabled = remove.disabled = move.disabled = locked();
      if (item.state === 'pending') {
        const skip = button('', buttons, item.skip ? '今回は送る' : '今回は送らない', () => {
          item.skip = !item.skip;
          // 手で変え直した印は、一括の取り消しでは戻さない。
          bulkSkipped.delete(item.id);
          changed();
        });
        skip.setAttribute('aria-pressed', String(item.skip));
        skip.disabled = locked();
      }
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
    element.closest('#gp-controls, #gp-header, #gp-notices, #gp-outline, #gp-outline-resizer, #gp-review-panel, #gp-review-layer, #gp-review-status, [data-md-table-sticky-bar], [data-md-table-sticky-search], [data-md-table-sticky-columns]') ||
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
    // 今の描画で対象を確かめ、内容の指紋を取ってから保存する。見つからない対象には付けない。
    const facts = factsOf(selected.target);
    if (!facts) { notice('選んだ対象が見つかりません。もう一度選んでください。'); return; }
    const item = { id: editing || crypto.randomUUID(), target: selected.target, label: selected.label,
      ...(selected.selection ? { selection: selected.selection } : {}), text, render: preview.current().render,
      state: 'pending', stale: false, error: '', fp: facts.fp, ...(facts.keys ? { keys: facts.keys } : {}),
      version: versionRender === preview.current()?.render ? currentVersion : '', skip: false };
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
    // 列幅のドラッグを離したときの click は、md-table-sticky が後の受け手で捨てる。
    if (window.__mdTableSticky?.resizeClick) return;
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

  // セルの内容の指紋と、同じ値のセルを見分ける行と列の鍵。表の情報のセル用 Markdown から作る。
  function cellFacts(target) {
    if (infoRender !== preview.current()?.render) return null;
    const model = models[target.table];
    const cell = model?.cells.find((item) => item.row === target.row && item.col === target.col);
    if (!cell || typeof cell.text !== 'string') return null;
    const covers = (item, row, col) => item.row <= row && row < item.row + item.rowspan && item.col <= col && col < item.col + item.colspan;
    // 行の鍵は、その行に掛かるほかのセル（上の行から結合で覆うセルも）の文字。列の鍵は、その列の先頭の行のセルの文字。
    const inRow = model.cells.filter((item) => item !== cell && item.row <= cell.row && cell.row < item.row + item.rowspan);
    const row = inRow.sort((a, b) => a.col - b.col).map((item) => item.text).join('\u0001');
    const header = model.cells.find((item) => covers(item, 0, cell.col));
    const inCol = model.cells.filter((item) => item !== cell && item.col <= cell.col && cell.col < item.col + item.colspan);
    // 同じ文字のセルが同じ列（行）にあれば、並べ替え（列の入れ替え）で別のセルが同じ座標に来うる。
    const sameInCol = inCol.some((item) => item.text === cell.text), sameInRow = inRow.some((item) => item.text === cell.text);
    return { fp: 'c:' + preview.digest(cell.text), sameInCol, sameInRow,
      keys: { row: preview.digest(row), col: preview.digest(header?.text || ''), start: model.start, dupCol: sameInCol, dupRow: sameInRow } };
  }
  function factsOf(target) {
    if (target.kind === 'cell') return cellFacts(target);
    if (!elementsOf(target).length) return null;
    const fp = preview.fingerprint(target);
    return fp ? { fp } : null;
  }
  // 対象が今の描画の同じ位置にあり、内容の指紋（セルは行の鍵も）が同じときだけ有効にする。
  // 位置が同じでも内容が違えば要確認にし、別の対象へ黙って付かないようにする。描画のたびに判定し直す。
  // セルは、同じ文字のセルが同じ列にあれば行の鍵も、同じ行にあれば列の鍵も一致するときだけ同じセルとみなす。
  const sameFacts = (item, facts) => facts.fp === item.fp &&
    (!item.keys || ((!facts.sameInCol || item.keys.row === facts.keys.row) && (!facts.sameInRow || item.keys.col === facts.keys.col)));

  // 版を再訪する取り消しもあるので、新しい開始点から、隣同士の版が一致する鎖を探す。
  function chainFrom(version) {
    if (!version || !currentVersion || version === currentVersion) return null;
    for (let start = changes.length - 1; start >= 0; start--) {
      if (changes[start].before !== version) continue;
      let next = version;
      const chain = [];
      for (let i = start; i < changes.length && changes[i].before === next; i++) {
        chain.push(changes[i]);
        next = changes[i].after;
        if (next === currentVersion) return chain;
      }
    }
    return null;
  }
  const startLine = (boundary) => Number(boundary.range.split(':')[0]);
  const endLine = (boundary) => Number(boundary.range.split('-')[1].split(':')[0]);
  const containsLine = (spans, line) => spans.some(([start, end]) => start <= line && line <= end);
  // keep の内側は平行移動し、外側は両隣が囲う新しい変更区間へ写す。
  // 削除で区間が空になった場合や、節の入れ替えで境が交差する場合は候補を作らない。
  function mapSpans(spans, change) {
    // 空の対応は全部の行が変わったことを表す（位置が分からない編集では、サーバーが change を返さない）。
    const keep = [...change.keep].sort((a, b) => a[0] - b[0]);
    const newOrder = [...keep].sort((a, b) => a[1] - b[1]);
    const overlaps = (start, end) => {
      let low = 0, high = newOrder.length;
      while (low < high) {
        const mid = (low + high) >>> 1, part = newOrder[mid];
        if (part[1] + part[2] <= start) low = mid + 1;
        else high = mid;
      }
      return low < newOrder.length && newOrder[low][1] <= end;
    };
    const parts = [];
    for (let i = 0; i <= keep.length; i++) {
      const prev = keep[i - 1], next = keep[i];
      const oldStart = prev ? prev[0] + prev[2] : 1;
      const oldEnd = next ? next[0] - 1 : Infinity;
      const newStart = prev ? prev[1] + prev[2] : 1;
      const newEnd = next ? next[1] - 1 : Infinity;
      // 新しい変更区間に、別の場所から移った keep が入っていれば対応を決められない。
      const gapOK = oldStart <= oldEnd && newStart <= newEnd &&
        !overlaps(newStart, newEnd);
      for (const [start, end] of spans) {
        if (next) {
          const first = Math.max(start, next[0]), last = Math.min(end, next[0] + next[2] - 1);
          if (first <= last) parts.push([first + next[1] - next[0], last + next[1] - next[0]]);
        }
        if (gapOK && start <= oldEnd && oldStart <= end) {
          // 前後で行数が同じ変わった区間（見出しのレベルの変更など）は、同じ位置の行へ写し、区間の別の行へ広げない。
          if (Number.isFinite(oldEnd) && Number.isFinite(newEnd) && oldEnd - oldStart === newEnd - newStart)
            parts.push([newStart + Math.max(start, oldStart) - oldStart, newStart + Math.min(end, oldEnd) - oldStart]);
          else parts.push([newStart, newEnd]);
        }
      }
      if (parts.length > 4096) return [];
    }
    parts.sort((a, b) => a[0] - b[0]);
    const merged = [];
    for (const part of parts) {
      const last = merged.at(-1);
      if (last && part[0] <= last[1] + 1) last[1] = Math.max(last[1], part[1]);
      else merged.push(part);
    }
    return merged;
  }
  const mapped = (line, chain) => chain.reduce((spans, change) => mapSpans(spans, change), [[line, line]]);
  function followTarget(item, chain) {
    if (!item.fp) return null;
    let candidates = [];
    if (item.target.kind === 'block') {
      for (const [start, end] of mapped(startLine(item.target), chain))
        for (const block of preview.targetsInRange(start, end).blocks) {
          const tagOK = /^h[1-6]$/.test(item.target.tag) ? /^h[1-6]$/.test(block.tag) : block.tag === item.target.tag;
          if (tagOK) candidates.push({ kind: 'block', ...block });
        }
    } else if (item.target.kind === 'cell') {
      if (!item.keys?.start || infoRender !== preview.current()?.render) return null;
      const spans = mapped(item.keys.start, chain);
      const tables = models.filter((model) => model.start > 0 && containsLine(spans, model.start));
      if (tables.length !== 1) return null;
      candidates = tables[0].cells.map((cell) => ({ kind: 'cell', table: cell.table, row: cell.row, col: cell.col }));
    } else {
      // 連なりの前後の境を写し、境があった側は、写した区間に候補の境があるものだけにする。
      const prev = item.target.prev ? mapped(endLine(item.target.prev), chain) : null;
      const next = item.target.next ? mapped(startLine(item.target.next), chain) : null;
      candidates = preview.targetsInRange(1, Infinity).html.filter((target) =>
        (!prev || (target.prev && containsLine(prev, endLine(target.prev)))) &&
        (!next || (target.next && containsLine(next, startLine(target.next)))));
    }
    // 同じ位置が複数の区間から得られても候補は一つとして数える。
    let matches = [...new Map(candidates.map((target) => [JSON.stringify(target), { target, facts: factsOf(target) }])).values()]
      .filter(({ facts }) => facts?.fp === item.fp);
    if (item.target.kind === 'cell') {
      // 付けたときに同じ列（行）に同じ文字があれば、行（列）の鍵が合うセルだけにする。
      // 行と列の鍵がどちらも変わったセルへは移さない（別の行・列にある同じ文字のセル）。
      const rowOK = (facts) => item.keys.row === facts.keys.row, colOK = (facts) => item.keys.col === facts.keys.col;
      matches = matches.filter(({ facts }) => (!item.keys.dupCol || rowOK(facts)) && (!item.keys.dupRow || colOK(facts)) &&
        (rowOK(facts) || colOK(facts)));
      if (matches.length > 1) matches = matches.filter(({ facts }) => rowOK(facts) && colOK(facts));
    }
    return matches.length === 1 ? matches[0] : null;
  }
  function checkTargets() {
    // 先に描画が届いても、応答の鎖と描画の版が揃うまで前の状態を保つ。
    if (window.gpEditMode?.writing?.() || versionRender !== preview.current()?.render) return;
    let changed = false, lost = false;
    for (const item of comments) {
      const chain = chainFrom(item.version);
      const match = chain ? followTarget(item, chain) : null;
      const facts = chain ? match?.facts : factsOf(item.target);
      let ok = Boolean(facts);
      if (ok && item.fp) ok = chain ? facts.fp === item.fp : sameFacts(item, facts);
      else if (ok) {
        // 版 1 で保存したコメントは、読み込んだときに要確認でなかったものだけ、最初に確かめた内容を指紋にする。
        ok = adoptable.has(item.id);
        if (ok) {
          adoptable.delete(item.id);
          item.fp = facts.fp;
          if (facts.keys) item.keys = facts.keys;
          changed = true;
        }
      }
      if (ok) {
        if (match) {
          if (!sameTarget(item.target, match.target)) {
            const wasSelected = selected && sameTarget(selected.target, item.target);
            item.target = match.target;
            if (item.target.kind === 'cell') item.label = '表 ' + (item.target.table + 1) + ' の ' +
              (item.target.row + 1) + ' 行 ' + (item.target.col + 1) + ' 列';
            if (wasSelected) selected = { ...selected, target: item.target, label: item.label };
            changed = true;
          }
          if (facts.keys && JSON.stringify(item.keys) !== JSON.stringify(facts.keys)) { item.keys = facts.keys; changed = true; }
        }
        // 初回に位置だけで照合した古い保存にも、表の開始行を補う。
        if (facts.keys && item.keys?.start !== facts.keys.start) { item.keys = facts.keys; changed = true; }
        // 版が分からないとき（表の情報の読み込みの失敗）は、最後に確かめた版を空で上書きしない。
        if (currentVersion && item.version !== currentVersion) { item.version = currentVersion; changed = true; }
      }
      if (item.stale === !ok) continue;
      item.stale = !ok;
      lost ||= item.stale;
      changed = true;
    }
    // 作った依頼文の対象が要確認になったら、その依頼文は使わない。
    if (lost && prepared) discardPrompt();
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
      // 描画が古い（409）だけなら、次の描画で読み直す。セルのコメントを要確認にしない。
      if (response.status === 409) return;
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
        return { table, cells: shape?.cells || [], reason, start: integer(info.start) ? info.start : 0 };
      });
      infoRender = render;
      versionRender = render;
      currentVersion = typeof data.version === 'string' && /^[0-9a-f]{32}$/i.test(data.version) ? data.version.toLowerCase() : '';
      layer.dataset.render = String(render);
      checkTargets();
      refreshFrames();
    } catch (error) {
      if (number !== tableNumber || error.name === 'AbortError') return;
      models = [];
      byCell = new WeakMap();
      infoRender = 0;
      versionRender = render;
      currentVersion = '';
      // 表とセルを確かめられないので、セルのコメントは要確認にする。
      checkTargets();
      if (mode) notice(error.message);
    } finally {
      if (number === tableNumber) tableFetching = null;
    }
  }
  async function preparePrompt() {
    if (locked()) return;
    const sent = comments.filter((item) => item.state === 'pending' && !item.skip);
    if (!sent.length) return;
    // 描画の版と、セルを含む場合は表の情報も揃うまで待ってもらう。
    if (window.gpEditMode?.writing?.() || versionRender !== preview.current()?.render ||
      sent.some((item) => item.target.kind === 'cell') && infoRender !== preview.current()?.render) {
      notice('表の情報を読み込んでいます。少し待ってから依頼文を作ってください。');
      return;
    }
    // 送る直前に、今の描画で対象と内容を確かめ直す。
    checkTargets();
    const stale = sent.filter((item) => item.stale);
    if (stale.length) {
      notice('要確認のコメントが ' + stale.length + ' 件あります。「直す」で対象を選び直すか、「今回は送らない」にしてから依頼文を作ってください。');
      cards.querySelector('[data-id="' + CSS.escape(stale[0].id) + '"] [data-role="fix"]')?.focus();
      return;
    }
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
        body: JSON.stringify({ render, mode: promptMode.value, comments: sent.map((item) => ({ target: item.target,
          ...(item.selection ? { selection: item.selection } : {}), text: item.text })) }) });
      const data = await response.json();
      if (number !== prepareNumber || stopped || render !== preview.current()?.render) return;
      if (!response.ok) {
        const message = messageOf(data) || '依頼文を作れませんでした';
        // 前の失敗の印は消し、今回断られた全部のカード（番号があるときだけ）に付け直す。
        for (const item of sent) item.error = '';
        const failures = Array.isArray(data?.error?.failures) ? data.error.failures : [];
        for (const failure of failures) {
          if (plain(failure) && integer(failure.index) && sent[failure.index])
            sent[failure.index].error = shortOK(failure.message, 500) && failure.message ? failure.message : message;
        }
        if (!failures.length && integer(data?.error?.index) && sent[data.error.index]) sent[data.error.index].error = message;
        save();
        throw new Error(message);
      }
      if (typeof data.prompt !== 'string' || !integer(data.bytes) || typeof data.id !== 'string' || !/^[0-9a-f]{32}$/.test(data.id))
        throw new Error('依頼文の応答の形が正しくありません');
      for (const item of sent) { item.stale = false; item.error = ''; }
      prepared = { id: data.id, prompt: data.prompt, render, sent: sent.map((item) => ({ id: item.id, signature: signature(item) })) };
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
  async function copyPrompt(openAgents = false) {
    if (locked() || !prepared) return;
    const snapshot = prepared;
    copying = true;
    draw();
    let copied = false;
    let launchFailure = 'Agents Window を開けませんでした。';
    try {
      // 利用者のクリックの中で直接呼ぶ。許可を別の操作へ持ち越さない。
      await navigator.clipboard.writeText(snapshot.prompt);
      if (prepared !== snapshot || stopped) return;
      for (const sent of snapshot.sent) {
        const item = comments.find((item) => item.id === sent.id && signature(item) === sent.signature);
        if (item) item.state = 'copied';
      }
      // 「今回は送らない」は今回の依頼文だけの印なので、コピーできたら外す。
      for (const item of comments) item.skip = false;
      bulkSkipped.clear();
      save();
      copied = true;
      draw();
      if (openAgents) {
        notice('依頼文をコピーしました。Agents Window を開いています。');
        const response = await fetch(url('launch'), { method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id: snapshot.id }) });
        const data = await response.json();
        // 起動を待つ間に描き直されて依頼文の欄が閉じても、コピーと起動の結果は知らせる。
        if (stopped) return;
        if (!response.ok) {
          launchFailure = messageOf(data) || launchFailure;
          throw new Error(launchFailure);
        }
        if (data.launched !== true) {
          launchFailure = '起動の応答の形が正しくありません。';
          throw new Error(launchFailure);
        }
        notice(data.again ? 'Agents Window はもう開いています。貼り付けて送ってください。' :
          '依頼文をコピーし、Agents Window を開きました。Copilot か Claude のセッションで作業場所を Branch にし、貼り付けて送ってください。');
        return;
      }
      notice('依頼文をコピーしました。');
    } catch {
      if (stopped) return;
      if (copied) {
        notice(launchFailure + ' 依頼文はコピーしました。');
        return;
      }
      if (prepared !== snapshot) return;
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
    }
    versionRender = 0;
    currentVersion = '';
    observedRender = render;
    if (!allowed()) setMode(false);
    draw();
    refreshFrames();
    void loadTables();
  });
  document.addEventListener('gp:edited', (event) => {
    const change = event.detail?.change;
    if (change) {
      changes.push({ before: change.before.toLowerCase(), after: change.after.toLowerCase(), keep: change.keep });
      keepCount += change.keep.length;
      while (changes.length > 128 || keepCount > 32768) keepCount -= changes.shift().keep.length;
    }
    checkTargets();
    refreshFrames();
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
  if (observedRender) void loadTables();
})();
