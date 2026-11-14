// Markdown プレビューのページ。ローカルのサーバーから SSE で届く消毒済み HTML を表示する。
'use strict';

(() => {
  const BASE = new URL('./', location.href); // http://127.0.0.1:PORT/<token>/ (token を書き込まない)
  const CONFIG = JSON.parse(document.getElementById('gp-config').textContent);
  let jumpEnabled = CONFIG.jump && CONFIG.currentDocument !== false;
  const root = document.documentElement;
  const header = document.getElementById('gp-header');
  const fileEl = document.getElementById('gp-file');
  const banner = document.getElementById('gp-banner');
  const content = document.getElementById('gp-content');
  const noticesPanel = document.getElementById('gp-notices');
  const noticesContent = document.getElementById('gp-notices-content');
  const noticesToggle = document.getElementById('gp-notices-toggle');
  const themeEl = document.getElementById('gp-theme');
  const widthEl = document.getElementById('gp-width');
  const shellStyle = document.querySelector('link[href="_/preview.css"]');
  const dark = window.matchMedia('(prefers-color-scheme: dark)');
  const pageAssetsId = CONFIG.assets ? CONFIG.assets.id : false;
  let officialAssets = CONFIG.assets || null;
  let styleNotice = CONFIG.notice || '';
  let styleGeneration = 0;
  let reloading = false;
  let fallbackStyle = null;
  const styleSets = new Map();
  const appearanceKey = 'gitlab-preview-appearance';
  let savedAppearance = {};
  try {
    const saved = JSON.parse(sessionStorage.getItem(appearanceKey) || '{}');
    if (saved && typeof saved === 'object') savedAppearance = saved;
  } catch {
    // 保存を使えないブラウザでも、設定値とこのページ内の操作は使える。
  }

  function rememberAppearance(key, value) {
    savedAppearance[key] = value;
    storeAppearance();
  }
  function forgetAppearance(key) {
    delete savedAppearance[key];
    storeAppearance();
  }
  function storeAppearance() {
    try {
      sessionStorage.setItem(appearanceKey, JSON.stringify(savedAppearance));
    } catch {
      // このページを閉じるまでだけ保持する。
    }
  }

  function reloadPage() {
    if (reloading) return;
    reloading = true;
    location.reload();
  }

  // ---- 表の固定 -----------------------------------------------------------------
  const stickyToggle = document.getElementById('gp-sticky-toggle');
  stickyToggle.hidden = Boolean(CONFIG.list);
  let stickyLoading = null;
  let stickyRestored = false;

  // md-table-sticky の内部状態・追加する属性への依存はここへまとめる。
  const stickyState = () => window.__mdTableSticky;
  function slot(live) {
    if (live.nodeType !== Node.ELEMENT_NODE || live.localName !== 'table') return { first: live, last: live };
    const floating = stickyState()?.floating;
    if (floating?.record.table === live) return { first: floating.placeholder, last: floating.placeholder };
    const wrap = live.parentElement;
    if (!wrap?.matches('[data-md-table-sticky-wrap]')) return { first: live, last: live };
    let first = wrap;
    while (first.previousSibling?.nodeType === Node.ELEMENT_NODE &&
      first.previousSibling.matches('[data-md-table-sticky-bar], [data-md-table-sticky-search], [data-md-table-sticky-columns]'))
      first = first.previousSibling;
    return { first, last: wrap };
  }
  function cellContainer(live) {
    return live.matches('td, th') && live.childNodes.length === 1 &&
      live.firstChild.nodeType === Node.ELEMENT_NODE && live.firstChild.matches('span[data-md-table-sticky-inner]')
      ? live.firstChild : live;
  }

  function updateStickyButton() {
    stickyToggle.setAttribute('aria-pressed', String(Boolean(stickyState())));
  }

  function setSticky(want) {
    if (stickyLoading) return stickyLoading;
    if (Boolean(stickyState()) === want) {
      updateStickyButton();
      return Promise.resolve();
    }
    // 原文は実行のたびに切り替わるので、読み込みを重ねず、使い終わった script は外す。
    stickyLoading = new Promise((resolve) => {
      const script = document.createElement('script');
      script.src = new URL('_/lib/md-table-sticky/sticky-table.js', BASE).href;
      script.onload = script.onerror = () => {
        script.remove();
        resolve();
      };
      document.head.appendChild(script);
    }).then(() => {
      stickyLoading = null;
      updateStickyButton();
      rememberAppearance('sticky', Boolean(stickyState()));
      queueLayout();
    });
    return stickyLoading;
  }

  function restoreSticky() {
    updateStickyButton();
    if (stickyRestored) return;
    stickyRestored = true;
    if (savedAppearance.sticky === true) void setSticky(true);
  }

  stickyToggle.addEventListener('click', () => void setSticky(!Boolean(stickyState())));

  // ---- GitLab との違い -----------------------------------------------------------
  const noticeLabels = new Map([
    ['reference', 'GitLab では参照のリンクになる書き方（手元ではリンクにしない）'],
    ['diagram', 'GitLab では図になるコードブロック（手元では描かない）'],
    ['suggestion', 'GitLab のマージリクエストでは提案になるコードブロック'],
    ['include', '取り込めなかった ::include'],
    ['emoji', '絵文字にならなかった短縮記法'],
    ['missing', '手元に見つからない画像・動画・音声'],
    ['more', 'ほかに'],
  ]);
  let noticesWanted = savedAppearance.notices === true;
  let noticeCount = 0;

  function toolbarHeight() {
    return (
      header.offsetHeight +
      (noticesPanel.hidden ? 0 : noticesPanel.offsetHeight) +
      (banner.hidden ? 0 : banner.offsetHeight)
    );
  }
  function applyNotices() {
    noticesPanel.hidden = CONFIG.list || !noticeCount || !noticesWanted;
    noticesToggle.hidden = CONFIG.list || !noticeCount;
    noticesToggle.textContent = 'GitLab との違い ' + noticeCount;
    noticesToggle.setAttribute('aria-expanded', String(!noticesPanel.hidden));
    queueLayout();
  }
  function buildNotices(values) {
    const notices = Array.isArray(values) ? values.filter((item) => item && noticeLabels.has(item.kind)) : [];
    noticeCount = notices.length;
    noticesContent.replaceChildren();
    for (const [kind, label] of noticeLabels) {
      const items = notices.filter((item) => item.kind === kind);
      if (!items.length) continue;
      const section = document.createElement('section');
      section.dataset.kind = kind;
      if (kind === 'more') {
        for (const item of items) {
          const heading = document.createElement('h3');
          heading.textContent = 'ほかに ' + String(item.text) + ' 件';
          section.append(heading);
        }
      } else {
        const heading = document.createElement('h3');
        heading.textContent = label + '（' + items.length + '件）';
        const list = document.createElement('ul');
        for (const item of items) {
          const li = document.createElement('li');
          const line = Number.isSafeInteger(item.line) && item.line >= 1 ? item.line : null;
          const row = document.createElement(line === null ? 'span' : 'button');
          if (line !== null) {
            row.type = 'button';
            row.addEventListener('click', () => {
              buildMap();
              // 次の scroll が届くまでは、欄や本文の高さが変わっても押した行へ合わせ直す。
              lastScroll = {
                line,
                ratio: 0,
                lines: lastScroll?.lines || map.reduce((end, entry) => Math.max(end, entry.end), line),
              };
              syncScroll();
            });
          }
          row.textContent = (line === null ? '' : String(line) + ' 行: ') + String(item.text);
          li.append(row);
          list.append(li);
        }
        section.append(heading, list);
      }
      noticesContent.append(section);
    }
    applyNotices();
  }
  noticesToggle.addEventListener('click', () => {
    noticesWanted = !noticesWanted;
    rememberAppearance('notices', noticesWanted);
    applyNotices();
  });

  // ---- 見出しと文書の一覧 ---------------------------------------------------------
  const outline = document.getElementById('gp-outline');
  const outlineList = document.getElementById('gp-outline-list');
  const outlineToggle = document.getElementById('gp-outline-toggle');
  const outlineResizer = document.getElementById('gp-outline-resizer');
  const wide = window.matchMedia('(min-width: 1100px)');
  const narrow = window.matchMedia('(max-width: 700px)');
  const outlineMin = 180;
  const outlineMax = 480;
  const outlineDefault = 260;
  const outlineBodyMin = 320;
  let outlineWanted = typeof savedAppearance.outline === 'boolean' ? savedAppearance.outline : wide.matches;
  let outlineLinks = [];
  const savedOutlineWidth = savedAppearance.outlineWidth;
  let outlineWidth = typeof savedOutlineWidth === 'number' && Number.isFinite(savedOutlineWidth) &&
    savedOutlineWidth >= outlineMin && savedOutlineWidth <= outlineMax ? Math.round(savedOutlineWidth) : outlineDefault;
  let outlineDrag = null;

  // preview.css の #gp-review-panel の幅。コメント欄を開いている間は、その幅も本文に残す。
  const reviewPanelWidth = 360;
  function outlineLimit() {
    const reserved = outlineBodyMin + (root.dataset.review === 'on' ? reviewPanelWidth : 0);
    return Math.max(outlineMin, Math.min(outlineMax, window.innerWidth - reserved));
  }
  function clampOutlineWidth(value) {
    return Math.min(Math.max(outlineMin, Math.round(value)), outlineLimit());
  }
  function canResizeOutline() {
    return !CONFIG.list && !outline.hidden && !narrow.matches;
  }
  function applyOutlineWidth() {
    outlineResizer.hidden = !canResizeOutline();
    if (outlineResizer.hidden) {
      if (outlineDrag) finishOutlineResize(false);
      return;
    }
    const width = clampOutlineWidth(outlineDrag ? outlineDrag.value : outlineWidth);
    const value = width + 'px';
    if (root.style.getPropertyValue('--gp-outline-user') !== value) root.style.setProperty('--gp-outline-user', value);
    outlineResizer.setAttribute('aria-valuenow', String(width));
    outlineResizer.setAttribute('aria-valuemin', String(outlineMin));
    outlineResizer.setAttribute('aria-valuemax', String(outlineLimit()));
    outlineResizer.setAttribute('aria-valuetext', width + ' ピクセル');
  }
  function finishOutlineResize(save) {
    const drag = outlineDrag;
    if (!drag) return;
    outlineDrag = null;
    delete root.dataset.outlineResizing;
    const width = clampOutlineWidth(drag.value);
    outlineWidth = save && width !== drag.startWidth ? width : drag.startUserWidth;
    applyOutlineWidth();
    if (outlineResizer.hasPointerCapture(drag.id)) outlineResizer.releasePointerCapture(drag.id);
    if (save && width !== drag.startWidth) rememberAppearance('outlineWidth', width);
    queueLayout();
  }
  function resetOutlineWidth() {
    outlineWidth = outlineDefault;
    forgetAppearance('outlineWidth');
    applyOutlineWidth();
    queueLayout();
  }
  outlineResizer.addEventListener('pointerdown', (event) => {
    if (!canResizeOutline() || outlineDrag || event.button !== 0 || !event.isPrimary) return;
    const width = clampOutlineWidth(outlineWidth);
    outlineResizer.setPointerCapture(event.pointerId);
    outlineDrag = { id: event.pointerId, startX: event.clientX, startWidth: width,
      startUserWidth: outlineWidth, value: width };
    root.dataset.outlineResizing = '';
  });
  outlineResizer.addEventListener('mousedown', (event) => {
    if (canResizeOutline() && event.button === 0) event.preventDefault();
  });
  outlineResizer.addEventListener('pointermove', (event) => {
    if (!outlineDrag || event.pointerId !== outlineDrag.id) return;
    outlineDrag.value = clampOutlineWidth(outlineDrag.startWidth + event.clientX - outlineDrag.startX);
    queueLayout();
  });
  outlineResizer.addEventListener('pointerup', (event) => {
    if (!outlineDrag || event.pointerId !== outlineDrag.id) return;
    outlineDrag.value = clampOutlineWidth(outlineDrag.startWidth + event.clientX - outlineDrag.startX);
    finishOutlineResize(canResizeOutline());
  });
  for (const name of ['pointercancel', 'lostpointercapture']) outlineResizer.addEventListener(name, (event) => {
    if (outlineDrag?.id === event.pointerId) finishOutlineResize(false);
  });
  // 編集・コメントの Esc より先に受け、ドラッグだけを取り消す。
  window.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !outlineDrag) return;
    finishOutlineResize(false);
    event.preventDefault();
    event.stopImmediatePropagation();
  }, true);
  outlineResizer.addEventListener('keydown', (event) => {
    // Alt+← の「戻る」や Ctrl+Home などのブラウザの操作は止めない。Shift だけを幅の刻みに使う。
    if (!canResizeOutline() || outlineDrag || event.ctrlKey || event.altKey || event.metaKey) return;
    const width = clampOutlineWidth(outlineWidth);
    let next;
    switch (event.key) {
      case 'ArrowLeft': next = width - (event.shiftKey ? 50 : 10); break;
      case 'ArrowRight': next = width + (event.shiftKey ? 50 : 10); break;
      case 'Home': next = outlineMin; break;
      case 'End': next = outlineLimit(); break;
      case 'Enter':
        event.preventDefault();
        resetOutlineWidth();
        return;
      default: return;
    }
    event.preventDefault();
    const value = clampOutlineWidth(next);
    // 画面の幅の上限に張り付いて見た目が変わらないときは、広い画面のための保存値を残す。
    if (value === width) return;
    outlineWidth = value;
    rememberAppearance('outlineWidth', outlineWidth);
    applyOutlineWidth();
    queueLayout();
  });
  outlineResizer.addEventListener('dblclick', () => {
    if (canResizeOutline() && !outlineDrag) resetOutlineWidth();
  });
  // コメント欄の開閉で上限が変わる。表示の幅と aria の値を合わせ直す。
  new MutationObserver(() => {
    applyOutlineWidth();
    queueLayout();
  }).observe(root, { attributes: true, attributeFilter: ['data-review'] });

  function applyOutline() {
    const visible = !CONFIG.list && outlineWanted && outlineLinks.length > 0;
    outline.hidden = !visible;
    root.dataset.outline = visible ? 'open' : 'closed';
    outlineToggle.disabled = !outlineLinks.length;
    outlineToggle.setAttribute('aria-expanded', String(visible));
    applyOutlineWidth();
    queueLayout();
  }
  function buildOutline(headings) {
    const focused = outlineLinks.find((link) => link.anchor === document.activeElement ||
      link.anchor.parentElement.contains(document.activeElement));
    outlineList.replaceChildren();
    outlineLinks = [];
    for (const [index, heading] of (headings || []).entries()) {
      if (!heading || typeof heading.id !== 'string' || !heading.id) continue;
      const item = document.createElement('li');
      const anchor = document.createElement('a');
      anchor.textContent = String(heading.text || '');
      anchor.href = '#' + encodeURIComponent('user-content-' + heading.id);
      item.style.paddingInlineStart = Math.max(0, Math.min(5, Number(heading.level) - 1)) * 12 + 'px';
      item.append(anchor);
      outlineList.append(item);
      outlineLinks.push({ anchor, index, id: heading.id, level: heading.level });
    }
    applyOutline();
    // 同じ題の見出しもあるので、番号と id の両方が合う項目だけに戻す。
    const restore = focused && outlineLinks.find((link) => link.index === focused.index && link.id === focused.id);
    restore?.anchor.focus({ preventScroll: true });
  }
  function markHeading() {
    // 閉じた details の中の見出しは、配置の情報が残っていても見えていない。checkVisibility はそれも見分ける。
    const shown = (element) =>
      !!element &&
      (typeof element.checkVisibility === 'function' ? element.checkVisibility() : element.getClientRects().length > 0);
    const visible = outlineLinks.filter((link) => shown(document.getElementById('user-content-' + link.id)));
    let current = visible[0];
    const top = toolbarHeight() + 12;
    // 文書の末尾の近くの見出しは、画面の上端まで上がらない。いちばん下まで来たら、画面に入っている最後の見出しを選ぶ。
    const atBottom = window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 2;
    for (const link of visible) {
      const element = document.getElementById('user-content-' + link.id);
      if (!element) continue;
      const position = element.getBoundingClientRect().top;
      if (position <= top || (atBottom && position < window.innerHeight)) current = link;
    }
    for (const link of outlineLinks) {
      if (link === current) link.anchor.setAttribute('aria-current', 'location');
      else link.anchor.removeAttribute('aria-current');
    }
  }
  let headingQueued = false;
  window.addEventListener(
    'scroll',
    () => {
      if (headingQueued) return;
      headingQueued = true;
      requestAnimationFrame(() => {
        headingQueued = false;
        markHeading();
      });
    },
    { passive: true },
  );
  outlineToggle.addEventListener('click', () => {
    outlineWanted = !outlineWanted;
    rememberAppearance('outline', outlineWanted);
    applyOutline();
  });
  wide.addEventListener('change', () => {
    if (typeof savedAppearance.outline !== 'boolean') {
      outlineWanted = wide.matches;
      applyOutline();
    }
  });

  function documentUrl(name, hash = '') {
    const url = new URL('./', BASE);
    url.searchParams.set('doc', name);
    url.hash = hash;
    return url.href;
  }
  async function renderList() {
    buildNotices([]);
    fileEl.textContent = '一覧';
    document.title = '文書の一覧 — GitLab プレビュー';
    outlineToggle.hidden = true;
    buildOutline([]);
    const title = document.createElement('h1');
    title.textContent = '文書の一覧';
    content.replaceChildren(title);
    try {
      const response = await fetch(new URL('_/list', BASE));
      if (!response.ok) throw new Error();
      const data = await response.json();
      const tree = { folders: new Map(), files: [] };
      for (const file of data.files) {
        const parts = file.split('/');
        let folder = tree;
        for (const part of parts.slice(0, -1)) {
          if (!folder.folders.has(part)) folder.folders.set(part, { folders: new Map(), files: [] });
          folder = folder.folders.get(part);
        }
        folder.files.push({ name: parts.at(-1), path: file });
      }
      function list(folder) {
        const ul = document.createElement('ul');
        for (const [name, child] of folder.folders) {
          const li = document.createElement('li');
          const label = document.createElement('span');
          label.textContent = name;
          li.append(label, list(child));
          ul.append(li);
        }
        for (const file of folder.files) {
          const li = document.createElement('li');
          const anchor = document.createElement('a');
          anchor.textContent = file.name;
          anchor.href = documentUrl(file.path);
          li.append(anchor);
          ul.append(li);
        }
        return ul;
      }
      const ul = list(tree);
      ul.className = 'gp-document-list';
      content.append(ul);
      if (!data.files.length) {
        const empty = document.createElement('p');
        empty.textContent = '文書がありません';
        content.append(empty);
      }
      renderMsg = data.truncated ? '一覧は一部だけを表示しています' : '';
    } catch {
      renderMsg = '一覧を読み込めません';
    }
    updateBanner();
  }

  // ---- 配色と本文幅 ---------------------------------------------------------------

  function effectiveTheme() {
    return themeEl.value === 'system' ? (dark.matches ? 'dark' : 'light') : themeEl.value;
  }

  function assetUrl(path) {
    return new URL('_/assets/' + encodeURIComponent(officialAssets.id) + '/' + path, BASE).href;
  }

  function localStyle(url) {
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = url;
    // 読み終わる前に、別テーマの一部だけが適用されないようにする。
    link.media = 'not all';
    const ready = new Promise((resolve, reject) => {
      link.onload = resolve;
      link.onerror = () => reject(new Error(url));
    });
    document.head.insertBefore(link, shellStyle);
    return { link, ready };
  }

  function useFallbackStyle() {
    root.dataset.style = 'fallback';
    for (const set of styleSets.values()) {
      for (const link of set.links) link.media = 'not all';
    }
    if (!fallbackStyle) {
      fallbackStyle = localStyle(new URL('_/fallback.css', BASE).href);
      fallbackStyle.link.id = 'gp-fallback-style';
      fallbackStyle.link.media = 'all';
      fallbackStyle.ready.then(queueLayout).catch(() => {
        styleNotice = 'スタイルを読み込めません。プレビューを開き直してください';
        updateBanner();
      });
    }
  }

  async function applyAppearance() {
    const request = ++styleGeneration;
    const theme = effectiveTheme();
    const changed = root.dataset.theme !== theme;
    root.dataset.theme = theme;
    root.dataset.width = widthEl.value;
    root.classList.toggle('gl-dark', theme === 'dark');
    if (officialAssets) {
      try {
        if (!styleSets.has(theme)) {
          const entries = officialAssets.styles[theme].map((path) => localStyle(assetUrl(path)));
          styleSets.set(theme, {
            links: entries.map((entry) => entry.link),
            ready: Promise.all(entries.map((entry) => entry.ready)),
          });
        }
        await styleSets.get(theme).ready;
        if (request !== styleGeneration) return;
        for (const [name, set] of styleSets) {
          for (const link of set.links) link.media = name === theme ? 'all' : 'not all';
        }
        root.dataset.style = 'official';
      } catch {
        if (request !== styleGeneration) return;
        officialAssets = null;
        styleNotice = '公式スタイルを読み込めません。標準のスタイルで表示しています';
        useFallbackStyle();
      }
    } else {
      useFallbackStyle();
    }
    updateBanner();
    // 本文を置き換えると、開いている表の全幅パネルや details が失われる。図だけを描き直す。
    if (changed && last) await renderMermaid(gen, true);
    queueLayout();
  }

  // ---- ライブラリ ---------------------------------------------------------------

  const loading = new Map();

  function addStyle(url) {
    return new Promise((resolve, reject) => {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = url;
      link.onload = resolve;
      link.onerror = () => reject(new Error(url));
      document.head.appendChild(link);
    });
  }

  // 必要な数式・図のライブラリだけを、同じサーバーから読む。
  function loadLibrary(name) {
    if (!loading.has(name)) {
      const path = name === 'katex' ? 'katex/katex.min.js' : 'mermaid/mermaid.min.js';
      const script = new Promise((resolve, reject) => {
        const element = document.createElement('script');
        element.src = new URL('_/lib/' + path, BASE).href;
        element.onload = () => (window[name] ? resolve(window[name]) : reject(new Error(name)));
        element.onerror = () => reject(new Error(name));
        document.head.appendChild(element);
      });
      const ready =
        name === 'katex'
          ? Promise.all([script, addStyle(new URL('_/lib/katex/katex.min.css', BASE).href)]).then(([value]) => value)
          : script;
      loading.set(
        name,
        ready.catch((error) => {
          loading.delete(name);
          throw error;
        }),
      );
    }
    return loading.get(name);
  }

  // ---- 接続とバナー ---------------------------------------------------------------

  let connMsg = ''; // 接続の状態 (こちらを優先して出す)
  let renderMsg = ''; // 近似表示の理由など

  function updateBanner() {
    const text = [connMsg, styleNotice, renderMsg].filter(Boolean).join(' ／ ');
    banner.textContent = text;
    banner.hidden = !text;
    banner.dataset.kind = connMsg ? 'conn' : renderMsg ? 'render' : 'style';
    queueLayout();
  }

  let last = null; // 最後の render
  let lastScroll = null; // 最後の scroll
  let scrollHeld = false;
  let editing = false;
  let stopped = null; // "stop" | "exit" | null
  let dropTimer = 0;

  const query = new URLSearchParams(location.search);
  document.getElementById('gp-back').hidden = !CONFIG.hasDocument || (!query.has('doc') && !CONFIG.list);
  let es = null;
  if (!CONFIG.list) {
    const eventsUrl = new URL('_/events', BASE);
    if (query.has('doc')) eventsUrl.searchParams.set('doc', query.get('doc'));
    es = new EventSource(eventsUrl);
    es.addEventListener('open', () => {
      clearTimeout(dropTimer);
      stopped = null;
      connMsg = '';
      updateBanner();
    });
    es.addEventListener('error', () => {
      if (stopped) return;
      // 再接続はブラウザが自動で行う。すぐに戻ることが多いので、少し待ってから出す
      clearTimeout(dropTimer);
      dropTimer = setTimeout(() => {
        connMsg = 'プレビューとの接続が切れた。再接続を待っている…';
        updateBanner();
      }, 1500);
    });
    es.addEventListener('stop', (e) => {
      const m = JSON.parse(e.data);
      stopped = m.reason;
      clearTimeout(dropTimer);
      if (m.reason === 'exit') {
        es.close();
        connMsg = 'プレビューが終了した';
      } else {
        // 同じ URL で再開したときに備えて、再接続は続ける
        connMsg = 'プレビューを止めた';
      }
      updateBanner();
      document.dispatchEvent(new CustomEvent('gp:stop', { detail: m }));
    });
    es.addEventListener('render', (e) => {
      clearTimeout(dropTimer);
      stopped = null;
      connMsg = '';
      render(JSON.parse(e.data));
    });
    es.addEventListener('reload', reloadPage);
    es.addEventListener('scroll', (e) => {
      lastScroll = JSON.parse(e.data);
      syncScroll(true);
    });
  }

  // ---- 描画 ---------------------------------------------------------------------

  let initialAnchor = true;
  let gen = 0; // 描画の世代。非同期の処理の途中で次の描画が来たら、古いほうは捨てる

  // 写しは数式・図・表の操作が変更する前の木。画面との対応だけを別に持つ。
  let shadow = null;
  let sourceIndex = null;
  let liveOf = new WeakMap();
  let shadowOf = new WeakMap();
  let replaced = new WeakSet();
  let shells = new WeakMap();
  let trees = new WeakMap();
  let plantumlLoaded = new Map();
  const positions = ['data-sourcepos', 'data-checkbox-sourcepos'];
  const tableParts = new Set(['table', 'thead', 'tbody', 'tfoot', 'tr']);

  function compareTrees(oldTree, newTree) {
    shells = new WeakMap();
    trees = new WeakMap();
    const ids = new Map();
    function visit(node) {
      const attrs = node.nodeType === Node.ELEMENT_NODE
        ? Array.from(node.attributes, (a) => [a.name, a.value]).filter(([name]) => !positions.includes(name))
          .sort(([a], [b]) => a.localeCompare(b)) : [];
      const shell = JSON.stringify([node.nodeType, node.namespaceURI, node.nodeName, attrs, node.data]);
      shells.set(node, shell);
      // 子の識別値だけを含めるので、深い部分木の文字列を何度も作らない。
      const key = JSON.stringify([shell, Array.from(node.childNodes, visit)]);
      if (!ids.has(key)) ids.set(key, ids.size);
      const id = ids.get(key);
      trees.set(node, id);
      return id;
    }
    visit(oldTree);
    visit(newTree);
  }
  const sameShell = (a, b) => shells.get(a) === shells.get(b);
  const sameDeep = (a, b) => trees.get(a) === trees.get(b);

  function bind(s, live) {
    liveOf.set(s, live);
    shadowOf.set(live, s);
  }
  function syncPositions(live, s) {
    if (live.nodeType !== Node.ELEMENT_NODE) return;
    for (const name of positions) {
      // 図の包みに位置が無いときは、描画時と同じく内側の位置を引き継ぐ。
      const value = name === 'data-sourcepos' && replaced.has(live) ? sourcepos(s) : s.getAttribute(name);
      if (value !== null) live.setAttribute(name, value);
      else live.removeAttribute(name);
    }
  }
  function inside(node, selector) {
    return [...(node.nodeType === Node.ELEMENT_NODE && node.matches(selector) ? [node] : []),
      ...node.querySelectorAll(selector)];
  }
  function materialize(s) {
    // template の文書のまま写す。挿入前には画像を読み込まない。
    const live = s.cloneNode(true);
    function register(source, target) {
      bind(source, target);
      source.childNodes.forEach((child, i) => register(child, target.childNodes[i]));
    }
    register(s, live);
    if (live.nodeType === Node.ELEMENT_NODE) {
      const holder = s.ownerDocument.createDocumentFragment();
      holder.append(live);
      preparePlantuml(holder);
      return holder.firstChild;
    }
    if (live.nodeType === Node.DOCUMENT_FRAGMENT_NODE) preparePlantuml(live);
    return live;
  }
  function containerFor(s) {
    if (s.nodeType === Node.DOCUMENT_FRAGMENT_NODE) return content;
    const live = liveOf.get(s);
    if (!live?.isConnected || replaced.has(live)) throw new Error('本文の親要素が見つかりません');
    return cellContainer(live);
  }
  function checkedLive(s) {
    const live = liveOf.get(s);
    if (!live?.isConnected || shadowOf.get(live) !== s) throw new Error('本文の対応が失われました');
    const place = slot(live);
    if (!place.first.isConnected || place.first.parentNode !== containerFor(s.parentNode) ||
      place.last.parentNode !== place.first.parentNode) throw new Error('本文の親が変わっています');
    return live;
  }
  function checkChildren(s) {
    let next = containerFor(s).firstChild;
    for (const child of s.childNodes) {
      const place = slot(checkedLive(child));
      if (next !== place.first) throw new Error('本文の並びが変わっています');
      while (next && next !== place.last) next = next.nextSibling;
      if (!next) throw new Error('表の範囲が見つかりません');
      next = next.nextSibling;
    }
    if (next) throw new Error('本文に対応のない節点があります');
  }
  function adopt(oldS, newS) {
    const live = checkedLive(oldS);
    if (!replaced.has(live) && oldS.nodeType === Node.ELEMENT_NODE) checkChildren(oldS);
    bind(newS, live);
    syncPositions(live, newS);
    if (!replaced.has(live)) oldS.childNodes.forEach((child, i) => adopt(child, newS.childNodes[i]));
  }
  function syncAttributes(live, oldS, newS) {
    for (const attr of oldS.attributes) {
      if (attr.name !== 'style' && !newS.hasAttribute(attr.name)) live.removeAttribute(attr.name);
    }
    for (const attr of newS.attributes) {
      if (attr.name !== 'style' && oldS.getAttribute(attr.name) !== attr.value) live.setAttribute(attr.name, attr.value);
    }
  }
  function tables() {
    return shadow ? Array.from(shadow.querySelectorAll('table'), (s) => liveOf.get(s)).filter((live) => live?.isConnected) : [];
  }
  function queryLive(selector) {
    const found = new Set(content.querySelectorAll(selector));
    for (const table of tables()) {
      if (!content.contains(table)) for (const node of inside(table, selector)) found.add(node);
    }
    return Array.from(found);
  }
  function collectPlantuml(live) {
    if (!live || (live.nodeType !== Node.ELEMENT_NODE && live.nodeType !== Node.DOCUMENT_FRAGMENT_NODE)) return;
    for (const img of inside(live, PLANTUML_IMAGES)) {
      if (!img.complete || !img.naturalWidth) continue;
      const loaded = plantumlLoaded.get(img.src) || new Set();
      loaded.add(img);
      plantumlLoaded.set(img.src, loaded);
    }
  }
  function removeLive(s, live) {
    collectPlantuml(live);
    // 全幅パネルへ移された子の表も外す。操作欄と包みは表の見張りが片付ける。
    if (s.nodeType === Node.ELEMENT_NODE) {
      for (const table of s.querySelectorAll('table')) {
        const moved = liveOf.get(table);
        if (moved?.isConnected && !live.contains(moved)) {
          collectPlantuml(moved);
          moved.remove();
        }
      }
    }
    live.remove();
  }
  function replaceRange(oldNodes, newNodes, nextS, container) {
    const oldLives = oldNodes.map(checkedLive);
    const next = nextS ? slot(liveOf.get(nextS)).first : null;
    if (next && (!next.isConnected || next.parentNode !== container)) throw new Error('本文の差し込み先が変わっています');
    oldNodes.forEach((s, i) => removeLive(s, oldLives[i]));
    for (const s of newNodes) container.insertBefore(materialize(s), next);
  }
  function patchNode(oldS, newS, nextS, container) {
    if (sameDeep(oldS, newS)) return adopt(oldS, newS);
    const live = checkedLive(oldS);
    if (!replaced.has(live) && oldS.nodeType === Node.ELEMENT_NODE && newS.nodeType === Node.ELEMENT_NODE) {
      if (oldS.localName === newS.localName && tableParts.has(oldS.localName)) {
        syncAttributes(live, oldS, newS);
      } else if (sameShell(oldS, newS)) {
        syncPositions(live, newS);
        if (live.matches('td, th')) {
          checkChildren(oldS);
          const target = containerFor(oldS);
          for (const child of oldS.childNodes) removeLive(child, liveOf.get(child));
          target.replaceChildren(...Array.from(newS.childNodes, materialize));
          bind(newS, live);
          return;
        }
      } else return replaceRange([oldS], [newS], nextS, container);
      patchChildren(oldS, newS);
      bind(newS, live);
      return;
    }
    replaceRange([oldS], [newS], nextS, container);
  }
  function patchRange(oldNodes, newNodes, nextS, container) {
    const isTable = (s) => s.nodeType === Node.ELEMENT_NODE && s.localName === 'table';
    // 数が同じでも、表の位置がずれていれば 1 組ずつは比べない (表が表でない節点と組になり、表を作り直してしまう)。
    if (oldNodes.length === newNodes.length && oldNodes.every((s, i) => isTable(s) === isTable(newNodes[i]))) {
      for (let i = oldNodes.length - 1; i >= 0; i--) {
        patchNode(oldNodes[i], newNodes[i], nextS, container);
        nextS = newNodes[i];
      }
      return;
    }
    const oldTables = oldNodes.filter(isTable);
    const newTables = newNodes.filter(isTable);
    if (!oldTables.length || oldTables.length !== newTables.length) return replaceRange(oldNodes, newNodes, nextS, container);
    let oldEnd = oldNodes.length;
    let newEnd = newNodes.length;
    for (let i = oldTables.length - 1; i >= 0; i--) {
      const oldIndex = oldNodes.lastIndexOf(oldTables[i], oldEnd - 1);
      const newIndex = newNodes.lastIndexOf(newTables[i], newEnd - 1);
      replaceRange(oldNodes.slice(oldIndex + 1, oldEnd), newNodes.slice(newIndex + 1, newEnd), nextS, container);
      patchNode(oldTables[i], newTables[i], newNodes[newIndex + 1] || nextS, container);
      nextS = newTables[i];
      oldEnd = oldIndex;
      newEnd = newIndex;
    }
    replaceRange(oldNodes.slice(0, oldEnd), newNodes.slice(0, newEnd), nextS, container);
  }
  function patchChildren(oldParent, newParent) {
    checkChildren(oldParent);
    const oldNodes = Array.from(oldParent.childNodes);
    const newNodes = Array.from(newParent.childNodes);
    let start = 0;
    let oldEnd = oldNodes.length;
    let newEnd = newNodes.length;
    while (start < oldEnd && start < newEnd && sameDeep(oldNodes[start], newNodes[start])) {
      adopt(oldNodes[start], newNodes[start]);
      start++;
    }
    while (start < oldEnd && start < newEnd && sameDeep(oldNodes[oldEnd - 1], newNodes[newEnd - 1])) {
      adopt(oldNodes[--oldEnd], newNodes[--newEnd]);
    }
    patchRange(oldNodes.slice(start, oldEnd), newNodes.slice(start, newEnd), newNodes[newEnd] || null, containerFor(oldParent));
  }
  function swapLive(oldLive, newLive) {
    const s = shadowOf.get(oldLive);
    oldLive.replaceWith(newLive);
    if (s) bind(s, newLive);
    replaced.add(newLive);
  }
  // 全幅表示で表が本文の外へ移っていても、原文の順に開閉を覚える。
  const detailsOpen = () => shadow
    ? Array.from(shadow.querySelectorAll('details'), (d) => Boolean(liveOf.get(d)?.open))
    : Array.from(content.querySelectorAll('details'), (d) => d.open);
  function replaceContent(nextShadow, open = detailsOpen()) {
    for (const img of queryLive(PLANTUML_IMAGES)) collectPlantuml(img);
    for (const table of tables()) if (!content.contains(table)) table.remove();
    liveOf = new WeakMap();
    shadowOf = new WeakMap();
    replaced = new WeakSet();
    content.replaceChildren(materialize(nextShadow));
    content.querySelectorAll('details').forEach((d, i) => {
      if (i < open.length) d.open = open[i];
    });
  }

  // 写しの節点は外へ渡さず、編集対象を公開の要素名と位置だけで照合する。
  function sourceOf(element) {
    for (let live = element; live && live !== content; live = live.parentElement) {
      if (!content.contains(live)) return null;
      const source = shadowOf.get(live);
      if (source?.nodeType !== Node.ELEMENT_NODE) continue;
      // 位置を持たない要素（details や生の HTML の包み）は、中の最初の段落などへ読み替えない。
      // 数式・図へ置き換えた表示だけ、もとの包みの内側の位置を引く。
      const positioned = source.hasAttribute('data-sourcepos') ? source
        : replaced.has(live) ? source.querySelector('[data-sourcepos]') : null;
      return positioned ? { tag: positioned.localName, range: positioned.getAttribute('data-sourcepos') } : null;
    }
    return null;
  }
  function shadowSource(tag, range) {
    if (!shadow || typeof tag !== 'string' || typeof range !== 'string') return null;
    if (!sourceIndex) {
      sourceIndex = new Map();
      for (const candidate of shadow.querySelectorAll('[data-sourcepos]')) {
        const key = candidate.localName + '\n' + candidate.getAttribute('data-sourcepos');
        if (!sourceIndex.has(key)) sourceIndex.set(key, candidate);
      }
    }
    return sourceIndex.get(tag + '\n' + range) || null;
  }
  function findSource(tag, range) {
    const source = shadowSource(tag, range);
    // 図の包みだけが写しと結び直された場合も、内側の pre・img から現在の表示へ戻れる。
    for (let node = source; node && node !== shadow; node = node.parentElement) {
      const live = liveOf.get(node);
      if (live?.isConnected && shadowOf.get(live) === node && (node === source || replaced.has(live))) return live;
    }
    return null;
  }

  // 本文の写しで連なりを決める。表の固定で画面の並びが動いても境は変わらない。
  const htmlPosition = /^[1-9][0-9]*:[1-9][0-9]*-[1-9][0-9]*:[0-9]+$/;
  function htmlBoundary(node) {
    if (node.nodeType !== Node.ELEMENT_NODE) return null;
    const source = node.matches('div.markdown-code-block') ? node.querySelector(':scope > pre') : node;
    const range = source?.getAttribute('data-sourcepos');
    return htmlPosition.test(range || '') ? { tag: source.localName, range } : null;
  }
  const sameHTMLBoundary = (a, b) => (!a && !b) || Boolean(a && b && a.tag === b.tag && a.range === b.range);
  let htmlRunShadow = null, htmlRunIndex = [];
  function htmlRuns() {
    if (htmlRunShadow === shadow) return htmlRunIndex;
    const runs = [];
    let prev = null, nodes = [];
    const flush = (next) => {
      if (nodes.some((n) => n.nodeType === Node.ELEMENT_NODE) &&
        !nodes.some((n) => n.nodeType === Node.ELEMENT_NODE && n.matches('ul.section-nav')))
        runs.push({ prev, next, nodes });
      nodes = [];
    };
    for (const node of shadow?.childNodes || []) {
      if (node.nodeType === Node.ELEMENT_NODE && node.matches('section[data-footnotes]')) continue;
      const boundary = htmlBoundary(node);
      if (boundary) { flush(boundary); prev = boundary; }
      else if (node.nodeType === Node.ELEMENT_NODE || node.nodeType === Node.TEXT_NODE && node.textContent.trim()) nodes.push(node);
    }
    flush(null);
    htmlRunShadow = shadow;
    htmlRunIndex = runs;
    return runs;
  }
  function htmlOf(element) {
    for (let live = element; live && live !== content; live = live.parentElement) {
      const source = shadowOf.get(live);
      if (!source) continue;
      let top = source;
      while (top.parentNode && top.parentNode !== shadow) top = top.parentNode;
      const run = htmlRuns().find((r) => r.nodes.includes(top));
      if (!run) return null;
      return Object.freeze({ tag: 'html', prev: run.prev && Object.freeze({ ...run.prev }), next: run.next && Object.freeze({ ...run.next }) });
    }
    return null;
  }
  function findHTML(target) {
    if (target?.tag !== 'html') return [];
    const run = htmlRuns().find((r) => sameHTMLBoundary(r.prev, target.prev) && sameHTMLBoundary(r.next, target.next));
    return run ? run.nodes.filter((n) => n.nodeType === Node.ELEMENT_NODE).map((n) => liveOf.get(n)).filter((n) => n?.isConnected) : [];
  }

  // 候補は写しから引く。公開するのは位置だけで、節点や表示後の文字は渡さない。
  function targetsInRange(start, end) {
    const blocks = [], html = [], sections = [];
    for (const node of shadow?.querySelectorAll('[data-sourcepos]') || []) {
      const range = node.getAttribute('data-sourcepos');
      const line = Number(range?.split(':')[0]);
      if (htmlPosition.test(range || '') && start <= line && line <= end) {
        blocks.push({ tag: node.localName, range });
        if (node.parentNode === shadow && /^h[1-6]$/.test(node.localName))
          sections.push({ kind: 'section', tag: node.localName, range });
      }
    }
    for (const run of htmlRuns()) {
      const first = run.prev ? Number(run.prev.range.split('-')[1].split(':')[0]) : 0;
      const last = run.next ? Number(run.next.range.split(':')[0]) : Infinity;
      if (first <= end && start <= last) html.push({ kind: 'html',
        ...(run.prev ? { prev: { ...run.prev } } : {}), ...(run.next ? { next: { ...run.next } } : {}) });
    }
    return { blocks, html, sections };
  }

  // コメントの対象の内容の指紋。写し（サーバーの HTML）から作るので、数式・図・表の固定など
  // 表示の後の書き換えに左右されない。位置・見出しのレベル・脚注の番号・token 入りの URL は含めない。
  function hashText(text, seed) {
    let h = seed >>> 0;
    for (let i = 0; i < text.length; i++) {
      h ^= text.charCodeAt(i);
      h = Math.imul(h, 0x01000193) >>> 0;
    }
    return h.toString(16).padStart(8, '0');
  }
  const digest = (text) => hashText(text, 0x811c9dc5) + hashText(text, 0x9747b28c);
  // BASE は URL なので、文字列にしてから比べて切る（token とポートを指紋に入れない）。
  const baseHref = BASE.href;
  const withoutBase = (value) => typeof value === 'string' && value.startsWith(baseHref) ? value.slice(baseHref.length) : value || '';
  function canonical(node, root, sectionLevel = null) {
    // コードの中だけ、改行と字下げも内容として保つ。
    if (node.nodeType === Node.TEXT_NODE) return node.parentElement?.closest('pre') ? node.data : node.data.replace(/\s+/g, ' ');
    if (node.nodeType !== Node.ELEMENT_NODE || node.matches('a.anchor, a.footnote-backref')) return '';
    // 目次には節の外の見出しも入るので、節では存在だけを比べる。
    if (sectionLevel !== null && node.matches('ul.section-nav')) return '\u0001[toc]';
    // タスクのチェックは内容の違いなので、入れるかどうかだけを含める。
    if (node.localName === 'input') return node.getAttribute('type') === 'checkbox' ? '\u0001[' + (node.hasAttribute('checked') ? 'x' : ' ') + ']' : '';
    if (node.matches('a[data-footnote-ref]')) return '\u0001[^]';
    const attrs = node.localName === 'img' ? ' alt=' + (node.getAttribute('alt') || '') + ' src=' + withoutBase(node.getAttribute('src'))
      : node.localName === 'a' ? ' href=' + withoutBase(node.getAttribute('href')) : '';
    const name = node === root && /^h[1-6]$/.test(node.localName)
      ? 'h' + (sectionLevel === null ? '' : Number(node.localName[1]) - sectionLevel) : node.localName;
    return '\u0001<' + name + attrs + '>' + Array.from(node.childNodes, (child) => canonical(child, root, sectionLevel)).join('') + '\u0001</>';
  }
  const fingerprintOf = (nodes, sectionLevel = null) => digest(nodes.map((node) => canonical(node, node, sectionLevel)).join('').trim());
  function fingerprint(target) {
    if (!shadow) return '';
    if (target?.kind === 'section' && /^h[1-6]$/.test(target.tag)) {
      const heading = shadowSource(target.tag, target.range);
      if (heading?.parentNode !== shadow) return '';
      // 子の節も含め、次の同じか上のレベルの最上位の見出しの直前までを指紋にする。
      // 節ごとのレベル変更に追従し、子だけの変更は区別するため、見出しのレベルは先頭との差で比べる。
      const nodes = [];
      const first = Number(target.range.split(':')[0]);
      let end = Infinity;
      for (let node = heading; node; node = node.nextSibling) {
        if (node.nodeType === Node.ELEMENT_NODE) {
          if (node.matches('section[data-footnotes]')) break;
          if (node !== heading && /^h[1-6]$/.test(node.localName) && node.localName <= heading.localName) {
            const boundary = htmlBoundary(node);
            if (boundary) end = Number(boundary.range.split(':')[0]);
            break;
          }
        }
        nodes.push(node);
      }
      // 脚注の表示は末尾へ集まるが、節への所属は原文の定義の開始行で決める。
      const definitions = Array.from(shadow.querySelectorAll('section[data-footnotes] li[data-sourcepos]'))
        .map((node) => ({ node, line: Number(node.getAttribute('data-sourcepos').split(':')[0]) }))
        .filter(({ line }) => first <= line && line < end)
        .sort((a, b) => a.line - b.line);
      nodes.push(...definitions.map(({ node }) => node));
      return 's:' + fingerprintOf(nodes, Number(heading.localName[1]));
    }
    if (target?.kind === 'block' && typeof target.tag === 'string' && typeof target.range === 'string') {
      const node = shadowSource(target.tag, target.range);
      return node ? 'b:' + fingerprintOf([node]) : '';
    }
    if (target?.kind === 'html') {
      const run = htmlRuns().find((r) => sameHTMLBoundary(r.prev, target.prev) && sameHTMLBoundary(r.next, target.next));
      return run ? 'h:' + fingerprintOf(run.nodes) : '';
    }
    return '';
  }

  // ---- 本文の外に置く閲覧用の操作 --------------------------------------------------

  const viewerStatus = document.createElement('div');
  viewerStatus.id = 'gp-viewer-status';
  viewerStatus.setAttribute('role', 'status');
  viewerStatus.hidden = true;
  document.body.append(viewerStatus);
  let viewerTimer = 0;
  function viewerNotice(text) {
    clearTimeout(viewerTimer);
    viewerStatus.textContent = text;
    viewerStatus.hidden = !text;
    if (text) viewerTimer = setTimeout(() => { viewerStatus.hidden = true; }, 5000);
  }
  // 描画の file の最後の部分。サーバーは / で区切るが、念のため \ でも区切る。無名なら空。
  function linkFileName() {
    const name = typeof last?.file === 'string' ? last.file.split(/[/\\]/).at(-1) : '';
    return name === '[無名]' ? '' : name;
  }
  // 見出しへのリンクを作れない理由。作れるときは空。
  function headingLinkReason(index, withFile) {
    const link = outlineLinks.find((link) => link.index === index);
    if (!link || last?.headings?.[index]?.id !== link.id) return '見出しの情報が古いため、リンクを作れません。開き直してください';
    if (withFile && !linkFileName()) return '無名の文書では、ファイル名つきのリンクを作れません';
    return '';
  }
  function headingLink(index, withFile) {
    if (headingLinkReason(index, withFile)) return null;
    const link = outlineLinks.find((link) => link.index === index);
    const escape = (text) => encodeURIComponent(text).replace(/[()]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase());
    // 複数行の見出しも、貼りやすいよう 1 行の題にする。
    const title = link.anchor.textContent.replace(/\s+/g, ' ').trim().replace(/[\\\[\]]/g, '\\$&');
    return '[' + title + '](' + (withFile ? escape(linkFileName()) : '') + '#' + escape(link.id) + ')';
  }
  async function copyHeadingLink(index, withFile) {
    const reason = headingLinkReason(index, withFile);
    if (reason) { viewerNotice(reason); return; }
    const text = headingLink(index, withFile);
    let copied = false;
    try {
      // 権限の要求を、利用者が押した操作の中で始める。
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        copied = true;
      }
    } catch {
      // API があっても許可されない場合は、一時的な欄を試す。
    }
    try {
      if (!copied) {
        const active = document.activeElement;
        const input = document.createElement('textarea');
        input.className = 'gp-copy-fallback';
        input.setAttribute('aria-label', 'コピー用の一時的な欄');
        input.value = text;
        document.body.append(input);
        try {
          input.focus({ preventScroll: true });
          input.select();
          if (!document.execCommand('copy')) throw new Error('コピーを拒否されました');
        } finally {
          input.remove();
          active?.focus({ preventScroll: true });
        }
      }
      viewerNotice('リンクをコピーしました');
    } catch {
      viewerNotice('リンクをコピーできませんでした。ブラウザーのクリップボードの権限を確認してください');
    }
  }

  const linkMenu = document.createElement('div');
  linkMenu.id = 'gp-heading-link-menu';
  linkMenu.setAttribute('role', 'menu');
  linkMenu.setAttribute('aria-label', '見出しへのリンク');
  linkMenu.hidden = true;
  document.body.append(linkMenu);
  let linkOpener = null;
  let linkIndex = null;
  const linkItems = [true, false].map((withFile) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.id = 'gp-heading-link-' + (withFile ? 'file' : 'fragment');
    button.textContent = withFile ? 'リンクをコピー（ファイル名つき）' : 'リンクをコピー（#id）';
    button.setAttribute('role', 'menuitem');
    button.tabIndex = -1;
    button.addEventListener('click', () => {
      if (button.getAttribute('aria-disabled') === 'true') { viewerNotice(button.title); return; }
      void copyHeadingLink(linkIndex, withFile);
      closeLinkMenu(true);
    });
    linkMenu.append(button);
    return button;
  });
  function closeLinkMenu(focus = false) {
    linkMenu.hidden = true;
    linkOpener?.setAttribute('aria-expanded', 'false');
    if (focus && linkOpener?.isConnected) linkOpener.focus({ preventScroll: true });
    linkOpener = null;
    linkIndex = null;
  }
  function openLinkMenu(link, point) {
    closeLinkMenu();
    linkOpener = link.anchor;
    linkIndex = link.index;
    linkOpener.setAttribute('aria-haspopup', 'menu');
    linkOpener.setAttribute('aria-controls', linkMenu.id);
    linkOpener.setAttribute('aria-expanded', 'true');
    linkItems.forEach((item, i) => {
      const reason = headingLinkReason(link.index, i === 0);
      item.setAttribute('aria-disabled', String(Boolean(reason)));
      item.title = reason;
    });
    linkMenu.hidden = false;
    const rect = link.anchor.getBoundingClientRect(), size = linkMenu.getBoundingClientRect();
    linkMenu.style.left = Math.max(8, Math.min(point?.x ?? rect.left, innerWidth - size.width - 8)) + 'px';
    linkMenu.style.top = Math.max(8, Math.min(point?.y ?? rect.bottom, innerHeight - size.height - 8)) + 'px';
    linkItems.find((item) => item.getAttribute('aria-disabled') !== 'true')?.focus({ preventScroll: true });
  }
  outlineList.addEventListener('contextmenu', (event) => {
    if (root.dataset.edit === 'on') return;
    const link = outlineLinks.find((link) => link.anchor.contains(event.target));
    if (!link) return;
    event.preventDefault();
    openLinkMenu(link, event.clientX || event.clientY ? { x: event.clientX, y: event.clientY } : null);
  });
  outlineList.addEventListener('keydown', (event) => {
    if (root.dataset.edit === 'on' || event.ctrlKey || event.altKey || event.metaKey ||
      !(event.key === 'ContextMenu' || event.key === 'F10' && event.shiftKey)) return;
    const link = outlineLinks.find((link) => link.anchor === event.target);
    if (!link) return;
    event.preventDefault();
    if (!event.repeat) openLinkMenu(link);
  });
  linkMenu.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' || event.key === 'Tab') {
      if (event.key === 'Escape') event.preventDefault();
      closeLinkMenu(true);
    } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault();
      const items = linkItems.filter((item) => item.getAttribute('aria-disabled') !== 'true');
      const index = items.indexOf(document.activeElement);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1
        : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
      items[next]?.focus();
    }
  });
  document.addEventListener('pointerdown', (event) => {
    if (!linkMenu.hidden && !linkMenu.contains(event.target)) closeLinkMenu();
  });
  window.addEventListener('resize', () => closeLinkMenu());
  window.addEventListener('blur', () => closeLinkMenu());
  window.addEventListener('scroll', (event) => {
    if (!linkMenu.contains(event.target)) closeLinkMenu();
  }, true);

  const detailsToggle = document.getElementById('gp-details-toggle');
  function updateDetailsToggle() {
    const details = queryLive('details');
    detailsToggle.hidden = !details.length;
    const text = details.some((d) => !d.open) ? 'すべて開く' : 'すべて閉じる';
    detailsToggle.textContent = text;
    detailsToggle.setAttribute('aria-label', '折りたたみを' + text);
  }
  detailsToggle.addEventListener('click', () => {
    const details = queryLive('details');
    const open = details.some((d) => !d.open);
    if (!open && !document.dispatchEvent(new CustomEvent('gp:before-details-close', { cancelable: true, detail: details }))) return;
    for (const d of details) d.open = open;
    updateDetailsToggle();
    buildMap();
    markHeading();
  });
  document.addEventListener('toggle', (event) => {
    if (event.target.matches?.('details') && queryLive('details').includes(event.target)) updateDetailsToggle();
  }, true);

  const imageDialog = document.createElement('dialog');
  imageDialog.id = 'gp-image-dialog';
  imageDialog.setAttribute('aria-label', '画像の拡大表示');
  const imageClose = document.createElement('button');
  imageClose.type = 'button';
  imageClose.textContent = '閉じる';
  const imageLarge = document.createElement('img');
  const imageCaption = document.createElement('p');
  imageCaption.id = 'gp-image-caption';
  imageDialog.setAttribute('aria-describedby', imageCaption.id);
  imageDialog.append(imageClose, imageLarge, imageCaption);
  document.body.append(imageDialog);
  let imageOpener = null;
  function closeImage() {
    if (!imageDialog.open) return;
    imageDialog.close();
    imageLarge.removeAttribute('src');
    const opener = imageOpener;
    imageOpener = null;
    if (!opener?.isConnected) return;
    const temporary = !opener.hasAttribute('tabindex') && opener.tabIndex < 0;
    if (temporary) {
      opener.tabIndex = -1;
      opener.addEventListener('blur', () => opener.removeAttribute('tabindex'), { once: true });
    }
    opener.focus({ preventScroll: true });
    if (temporary && document.activeElement !== opener) opener.removeAttribute('tabindex');
  }
  imageClose.addEventListener('click', closeImage);
  imageDialog.addEventListener('cancel', (event) => { event.preventDefault(); closeImage(); });
  // 全幅表示の表の Esc より先に、いちばん上の画像だけを閉じる。
  window.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !imageDialog.open) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    closeImage();
  }, true);
  // ダブルクリックの 2 回目が、開いたばかりの背景に当たっても閉じない。
  imageDialog.addEventListener('click', (event) => { if (event.target === imageDialog && event.detail < 2) closeImage(); });
  document.addEventListener('click', (event) => {
    if (editing || root.dataset.edit === 'on' || root.dataset.review === 'on' || event.button !== 0 ||
      event.ctrlKey || event.shiftKey || event.altKey || event.metaKey) return;
    const img = event.target.closest?.('img');
    if (!img || !queryLive('img').includes(img) || !img.getAttribute('src') ||
      img.matches('[data-diagram="plantuml"], .gp-plantuml') ||
      img.closest('a[data-diagram="plantuml"], .gp-mermaid, .js-render-mermaid, .js-render-math, .katex, .gp-math-display, .gp-math-inline')) return;
    const anchor = img.closest('a');
    if (anchor && (!anchor.classList.contains('no-attachment-icon') || anchor.href !== img.src)) return;
    event.preventDefault();
    imageOpener = anchor || img;
    imageLarge.src = img.src;
    imageLarge.alt = img.alt;
    imageCaption.textContent = img.alt;
    imageDialog.showModal();
    imageClose.focus();
  });
  document.addEventListener('gp:stop', () => { closeLinkMenu(); closeImage(); });
  window.addEventListener('pagehide', () => { closeLinkMenu(); closeImage(); });

  window.gpPreview = {
    config: CONFIG, base: BASE, doc: query.get('doc'), current: () => last, tables, sourceOf, findSource, htmlOf, findHTML,
    fingerprint, digest, targetsInRange,
    outline: () => outlineLinks.map((link) => ({ ...link })),
    holdScroll: (hold) => { scrollHeld = Boolean(hold); },
    setEditing: (active) => { editing = Boolean(active); if (editing) { closeLinkMenu(); closeImage(); } },
    headingLink, headingLinkReason, copyHeadingLink,
    rememberAppearance, appearance: savedAppearance,
  };

  async function render(m) {
    closeLinkMenu();
    closeImage();
    viewerNotice('');
    // 導入・更新の完了後は、HTML に埋めた資産情報も読み直す。
    if (Object.hasOwn(m, 'assets_id') && m.assets_id !== pageAssetsId) {
      reloadPage();
      return;
    }
    if (Object.hasOwn(m, 'currentDocument')) jumpEnabled = Boolean(CONFIG.jump && m.currentDocument);
    const my = ++gen;
    const previous = last;
    last = m;
    fileEl.textContent = m.file || '';
    document.title = (m.file ? m.file + ' — ' : '') + 'GitLab プレビュー';
    // template の中では画像を読まない。挿入前に相対 URL を直す。
    const tpl = document.createElement('template');
    if (Object.hasOwn(m, 'error')) {
      renderMsg = m.error;
      const pre = document.createElement('pre');
      pre.className = 'gp-raw';
      pre.textContent = m.markdown || '';
      tpl.content.appendChild(pre);
    } else {
      renderMsg = '';
      tpl.innerHTML = m.html;
    }
    updateBanner();
    rewriteUrls(tpl.content, m);
    // コード配色は要素自身の背景も塗るので、body には付けずコードの範囲へ限定する。
    for (const highlight of tpl.content.querySelectorAll('.highlight')) {
      highlight.classList.add('code-syntax-highlight-theme');
      const pre = highlight.closest('pre');
      if (pre) pre.classList.add('code-syntax-highlight-theme');
    }

    plantumlLoaded = new Map();
    if (!shadow || Object.hasOwn(m, 'error') || Object.hasOwn(previous, 'error') || previous.file !== m.file) {
      replaceContent(tpl.content, previous?.file === m.file ? detailsOpen() : []);
    } else {
      // 途中まで当てた後に全置換へ戻るときも、始める前の details の開閉を使う。
      const open = detailsOpen();
      try {
        compareTrees(shadow, tpl.content);
        patchChildren(shadow, tpl.content);
      } catch {
        // 拡張や利用者の操作で対応が崩れたら、従来の全置換へ戻す。
        replaceContent(tpl.content, open);
      }
    }
    shadow = tpl.content;
    sourceIndex = null;
    bind(shadow, content);
    updateDetailsToggle();
    plantumlLoaded.clear();
    buildNotices(Object.hasOwn(m, 'error') ? [] : m.notices);
    buildOutline(m.headings);
    restoreSticky();
    buildMap();
    syncScroll();
    document.dispatchEvent(new CustomEvent('gp:render', { detail: { render: m.render } }));

    await renderMath(my);
    await renderMermaid(my);
    if (my !== gen) return;
    buildMap();
    syncScroll();
    markHeading();
    if (initialAnchor && location.hash && !lastScroll && !scrollHeld) {
      let id;
      try {
        id = decodeURIComponent(location.hash.slice(1));
      } catch {
        id = location.hash.slice(1);
      }
      (document.getElementById(id) || document.getElementById('user-content-' + id))?.scrollIntoView({
        block: 'start',
      });
    }
    initialAnchor = false;
  }

  // ---- URL の書き換え -------------------------------------------------------------

  const SCHEME = /^[a-z][a-z0-9+.-]*:/i;

  function isRelative(value) {
    // ? で始まるリンク (クエリだけ) は、ファイルへの相対パスではないので書き換えない。
    return (
      !!value && !SCHEME.test(value) && !value.startsWith('//') && !value.startsWith('#') && !value.startsWith('?')
    );
  }

  function localReference(value, dir) {
    try {
      const parts = /^([^?#]*)(\?[^#]*)?(#.*)?$/.exec(value);
      const raw = decodeURIComponent(parts[1].replace(/%(?![0-9a-f]{2})/gi, '%25'));
      if (/[\\\x00-\x1f\x7f]/.test(raw)) return null;
      const stack = [];
      const segments = (raw.startsWith('/') ? [] : (dir || '').split('/')).concat(raw.split('/'));
      for (const part of segments) {
        if (!part || part === '.') continue;
        if (part === '..') {
          if (!stack.length) return null;
          stack.pop();
        } else stack.push(part);
      }
      const name = stack.join('/');
      const url = new URL(stack.map(encodeURIComponent).join('/'), BASE);
      url.search = parts[2] || '';
      url.hash = parts[3] || '';
      return { name, hash: parts[3] || '', url: url.href };
    } catch {
      return null;
    }
  }
  function rewriteUrls(fragment, message) {
    for (const element of fragment.querySelectorAll('img[src], video[src], audio[src], source[src]')) {
      const value = element.getAttribute('src');
      if (!isRelative(value)) continue;
      const local = localReference(value, message.dir);
      if (local) element.setAttribute('src', local.url);
      else element.removeAttribute('src');
    }
    for (const anchor of fragment.querySelectorAll('a[href]')) {
      const value = anchor.getAttribute('href');
      if (value.startsWith('#')) continue;
      if (isRelative(value)) {
        const local = localReference(value, message.dir);
        if (!local) {
          anchor.removeAttribute('href');
          anchor.removeAttribute('target');
          anchor.setAttribute('aria-disabled', 'true');
          continue;
        }
        if (/\.(?:md|markdown|mdown|mkd|mkdn)$/i.test(local.name)) {
          // 今の文書そのものへのリンク (ファイル名付きの見出しへのリンク) は、ページ内の移動にする。
          // ?doc= に移ると、編集中の文書のスクロールの同期が外れる。
          anchor.href = local.name === message.file ? local.hash || '#' : documentUrl(local.name, local.hash);
          anchor.removeAttribute('target');
          anchor.removeAttribute('rel');
          continue;
        }
        anchor.href = local.url;
      }
      anchor.target = '_blank';
      anchor.rel = 'noopener noreferrer';
    }
  }

  // ---- 数式と図 -------------------------------------------------------------------

  // 図の img。ふつうは ImageLink が包んだ a の中にある。利用者の a の中の図は包まれず、img が図の属性を持つ。
  const PLANTUML_IMAGES = 'a[data-diagram="plantuml"] > img, img[data-diagram="plantuml"]';
  // 図ごと置き換える要素 (包んだ a があればそれ、無ければ img)
  const plantumlHolder = (img) =>
    img.parentElement && img.parentElement.matches('a[data-diagram="plantuml"]') ? img.parentElement : img;

  function preparePlantuml(fragment) {
    for (let img of fragment.querySelectorAll(PLANTUML_IMAGES)) {
      const loaded = plantumlLoaded.get(img.src);
      const old = loaded?.values().next().value;
      if (old) {
        loaded.delete(old);
        syncPositions(old, img);
        swapLive(img, old);
        continue;
      }
      img.classList.add('gp-plantuml');
      img.alt = 'PlantUML の図';
      img.loading = 'lazy';
      img.addEventListener(
        'error',
        () => {
          const holder = plantumlHolder(img);
          let source = '';
          try {
            const encoded = holder.getAttribute('data-diagram-src') || '';
            if (encoded.startsWith('data:text/plain;base64,'))
              source = new TextDecoder('utf-8', { fatal: true }).decode(
                Uint8Array.from(atob(encoded.slice('data:text/plain;base64,'.length)), (char) => char.charCodeAt(0)),
              );
          } catch {
            // 壊れた埋め込みは HTML として解釈せず、原文を空にする。
          }
          const out = document.createElement('div');
          out.className = 'gp-plantuml-error';
          const pos = img.getAttribute('data-sourcepos');
          if (pos) out.setAttribute('data-sourcepos', pos);
          const label = document.createElement('p');
          label.textContent = 'PlantUML の図を描けませんでした';
          const pre = document.createElement('pre');
          const code = document.createElement('code');
          code.textContent = source;
          pre.append(code);
          out.append(label, pre);
          swapLive(holder, out);
          queueLayout();
        },
        { once: true },
      );
    }
  }

  // 画面の外の図も、印刷のレイアウトを作る前に読み込みを始める。
  // 開いたままのモーダルは本文を操作できなくするので、印刷の前に拡大とメニューを閉じる。
  window.addEventListener('beforeprint', () => {
    closeLinkMenu();
    closeImage();
    for (const img of queryLive('img.gp-plantuml')) img.loading = 'eager';
  });

  function sourcepos(el) {
    if (el.hasAttribute('data-sourcepos')) return el.getAttribute('data-sourcepos');
    const inner = el.querySelector('[data-sourcepos]');
    return inner ? inner.getAttribute('data-sourcepos') : null;
  }

  async function renderMath(my) {
    const els = queryLive('.js-render-math');
    if (!els.length) return;
    let katex;
    try {
      katex = await loadLibrary('katex');
    } catch {
      return; // 読めなければ TeX のまま見せる
    }
    if (my !== gen) return;
    for (const el of els) {
      if (!el.isConnected) continue;
      const display = el.getAttribute('data-math-style') === 'display';
      const box = display ? el.closest('.markdown-code-block') || el.closest('pre') || el : el;
      const out = document.createElement(display ? 'div' : 'span');
      out.className = display ? 'gp-math-display' : 'gp-math-inline';
      const pos = sourcepos(box);
      if (pos) out.setAttribute('data-sourcepos', pos);
      try {
        // GitLab と同じく、巨大なサイズやマクロの展開は抑える
        katex.render(el.textContent, out, {
          displayMode: display,
          throwOnError: false,
          maxSize: 20,
          maxExpand: 1000,
          trust: false,
        });
      } catch {
        out.textContent = el.textContent;
        out.classList.add('gp-error');
      }
      swapLive(box, out);
    }
  }

  let mermaidTheme = null;
  let mermaidSeq = 0;
  let mermaidGeneration = 0;
  const svgCache = new Map();
  const mermaidSources = new WeakMap();
  const mermaidThemes = new WeakMap();

  async function renderMermaid(my, redraw = false) {
    const pass = ++mermaidGeneration;
    const theme = effectiveTheme() === 'dark' ? 'dark' : 'default';
    const codes = queryLive('.js-render-mermaid, .gp-mermaid').filter(
      (el) => el.matches('.js-render-mermaid') ||
        (mermaidSources.has(el) && (redraw || mermaidThemes.get(el) !== theme)),
    );
    if (!codes.length) return;
    let mermaid;
    try {
      mermaid = await loadLibrary('mermaid');
    } catch {
      return; // 読めなければ図の原文を見せる
    }
    if (my !== gen || pass !== mermaidGeneration) return;
    if (theme !== mermaidTheme) {
      mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme });
      mermaidTheme = theme;
    }
    for (const code of codes) {
      if (my !== gen || pass !== mermaidGeneration) return;
      if (!code.isConnected) continue;
      const src = mermaidSources.get(code) || code.textContent;
      const box = code.matches('.gp-mermaid')
        ? code
        : code.closest('.markdown-code-block') || code.closest('pre') || code;
      const out = document.createElement('div');
      out.className = 'gp-mermaid';
      const pos = sourcepos(box);
      if (pos) out.setAttribute('data-sourcepos', pos);
      const key = theme + '\n' + src;
      const id = 'gp-mermaid-' + ++mermaidSeq;
      try {
        let svg = svgCache.get(key);
        if (!svg) {
          ({ svg } = await mermaid.render(id, src));
          if (svgCache.size > 100) svgCache.clear();
          svgCache.set(key, svg);
        }
        if (my !== gen || pass !== mermaidGeneration) return;
        out.innerHTML = svg;
        mermaidSources.set(out, src);
        mermaidThemes.set(out, theme);
      } catch (e) {
        document.getElementById('d' + id)?.remove(); // 失敗したときに mermaid が残す作業用の要素
        out.className = 'gp-error';
        out.textContent = 'mermaid: ' + (e && e.message ? e.message : String(e));
      }
      if (box.isConnected) swapLive(box, out);
    }
  }

  // ---- スクロールの同期 -------------------------------------------------------------
  // GitLab の HTML はブロックに data-sourcepos (開始行:列-終了行:列) を持つ。カーソルの行を含む要素を探し、
  // 要素の中は行で按分する。カーソルが編集画面のどの高さにあるか (ratio) にも合わせる。

  let map = [];

  function buildMap() {
    map = [];
    for (const el of content.querySelectorAll('[data-sourcepos]')) {
      const m = /^(\d+):\d+-(\d+):\d+/.exec(el.getAttribute('data-sourcepos'));
      if (m && el.getClientRects().length) map.push({ start: Number(m[1]), end: Number(m[2]), el });
    }
    // 脚注の定義は文書の末尾に描かれるので、開始行で並べ直す (同じ開始行なら文書の順 = 内側の要素が後ろ)
    map.sort((a, b) => a.start - b.start);
  }

  function topOf(el) {
    return el.getBoundingClientRect().top + window.scrollY;
  }

  function syncScroll(fromEvent = false) {
    if (scrollHeld && !fromEvent) return;
    const s = lastScroll;
    if (!s || !last) return;
    const head = toolbarHeight();
    let y;
    if (map.length) {
      let lo = 0;
      let hi = map.length - 1;
      let i = -1;
      while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (map[mid].start <= s.line) {
          i = mid;
          lo = mid + 1;
        } else {
          hi = mid - 1;
        }
      }
      if (i < 0) {
        y = 0;
      } else {
        const cur = map[i];
        const top = topOf(cur.el);
        const height = cur.el.getBoundingClientRect().height;
        if (s.line <= cur.end) {
          y = top + height * ((s.line - cur.start) / Math.max(1, cur.end - cur.start + 1));
        } else {
          const next = map[i + 1];
          const bottom = top + height;
          y = next
            ? bottom + (topOf(next.el) - bottom) * ((s.line - cur.end) / Math.max(1, next.start - cur.end))
            : bottom;
        }
      }
    } else {
      // data-sourcepos が無い (古い GitLab など) ときは、全体の行数との比で動かす
      y = document.documentElement.scrollHeight * ((s.line - 1) / Math.max(1, s.lines - 1));
    }
    window.scrollTo({ top: Math.max(0, y - head - s.ratio * (window.innerHeight - head)), behavior: 'auto' });
  }

  // 画像・フォント・本文幅などで高さが変わったら、1 フレームにまとめて合わせ直す。
  let syncQueued = false;
  function queueLayout() {
    if (syncQueued) return;
    syncQueued = true;
    requestAnimationFrame(() => {
      syncQueued = false;
      const height = header.offsetHeight;
      root.style.setProperty('--gp-header-height', height + 'px');
      root.style.setProperty('--gp-banner-top', height + (noticesPanel.hidden ? 0 : noticesPanel.offsetHeight) + 'px');
      root.style.setProperty('--gp-toolbar-height', toolbarHeight() + 'px');
      applyOutlineWidth();
      buildMap();
      syncScroll();
      markHeading();
    });
  }
  content.addEventListener('load', queueLayout, true);
  window.addEventListener('resize', queueLayout);
  if (typeof ResizeObserver !== 'undefined') {
    const observer = new ResizeObserver(queueLayout);
    for (const el of [header, noticesPanel, banner, content]) observer.observe(el);
  }
  if (document.fonts) {
    document.fonts.ready.then(queueLayout);
    document.fonts.addEventListener('loadingdone', queueLayout);
  }

  // ---- ダブルクリックでエディタの行へ ---------------------------------------------
  content.addEventListener('dblclick', (event) => {
    if (!jumpEnabled || editing) return;
    const element = event.target instanceof Element ? event.target : event.target.parentElement;
    if (
      !element ||
      element.closest('summary, input, button, copy-code, video, audio') ||
      (element.closest('a') && !element.closest('a[data-diagram="plantuml"]'))
    )
      return;
    const block = element.closest('[data-sourcepos]');
    if (!block || !content.contains(block)) return;
    const match = /^(\d+):\d+-\d+:\d+$/.exec(block.getAttribute('data-sourcepos'));
    const line = match && Number(match[1]);
    if (!Number.isSafeInteger(line) || line < 1) return;
    // 文字の選択を保つため、ダブルクリック自体は止めない。
    const jumpUrl = new URL('_/jump', BASE);
    if (query.has('doc')) jumpUrl.searchParams.set('doc', query.get('doc'));
    void fetch(jumpUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ line }),
    }).catch(() => {});
  });
  // PlantUML の図は、SVG へのリンクで包まれている (GitLab と同じ形)。ダブルクリックで行へ移れるときは、
  // その 2 回のクリックで新しいタブが 2 つ開かないよう、図のクリックではリンクを開かない。
  content.addEventListener('click', (event) => {
    if (!jumpEnabled || editing) return;
    const element = event.target instanceof Element ? event.target : event.target.parentElement;
    if (element && element.closest('a[data-diagram="plantuml"]')) event.preventDefault();
  });

  // ---- ページ内のリンク -------------------------------------------------------------
  // GitLab は見出しなどの id に user-content- を付ける ([[_TOC_]] や脚注の #… はそちらを指す)
  content.addEventListener('click', (e) => {
    const a = e.target.closest('a[href^="#"]');
    if (!a) return;
    let id = a.getAttribute('href').slice(1);
    try {
      id = decodeURIComponent(id);
    } catch {
      // そのまま使う
    }
    const target = document.getElementById('user-content-' + id) || document.getElementById(id);
    if (target) {
      e.preventDefault();
      target.scrollIntoView({ block: 'start' });
    }
  });

  const appearance = CONFIG.appearance || {};
  const theme = savedAppearance.theme || appearance.theme;
  const width = savedAppearance.width || appearance.width;
  themeEl.value = ['system', 'light', 'dark'].includes(theme) ? theme : 'system';
  widthEl.value = width === 'fixed' ? 'fixed' : 'fluid';
  themeEl.addEventListener('change', () => {
    rememberAppearance('theme', themeEl.value);
    void applyAppearance();
  });
  widthEl.addEventListener('change', () => {
    rememberAppearance('width', widthEl.value);
    root.dataset.width = widthEl.value;
    queueLayout();
  });
  dark.addEventListener('change', () => {
    if (themeEl.value === 'system') void applyAppearance();
  });
  void applyAppearance();
  if (CONFIG.list) void renderList();
})();
