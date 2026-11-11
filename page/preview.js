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

  function updateStickyButton() {
    stickyToggle.setAttribute('aria-pressed', String(Boolean(window.__mdTableSticky)));
  }

  function setSticky(want) {
    if (stickyLoading) return stickyLoading;
    if (Boolean(window.__mdTableSticky) === want) {
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
      rememberAppearance('sticky', Boolean(window.__mdTableSticky));
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

  stickyToggle.addEventListener('click', () => void setSticky(!Boolean(window.__mdTableSticky)));

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
  const wide = window.matchMedia('(min-width: 1100px)');
  let outlineWanted = typeof savedAppearance.outline === 'boolean' ? savedAppearance.outline : wide.matches;
  let outlineLinks = [];

  function applyOutline() {
    const visible = !CONFIG.list && outlineWanted && outlineLinks.length > 0;
    outline.hidden = !visible;
    root.dataset.outline = visible ? 'open' : 'closed';
    outlineToggle.disabled = !outlineLinks.length;
    outlineToggle.setAttribute('aria-expanded', String(visible));
    queueLayout();
  }
  function buildOutline(headings) {
    outlineList.replaceChildren();
    outlineLinks = [];
    for (const heading of headings || []) {
      if (!heading || typeof heading.id !== 'string' || !heading.id) continue;
      const item = document.createElement('li');
      const anchor = document.createElement('a');
      anchor.textContent = String(heading.text || '');
      anchor.href = '#' + encodeURIComponent('user-content-' + heading.id);
      item.style.paddingInlineStart = Math.max(0, Math.min(5, Number(heading.level) - 1)) * 12 + 'px';
      item.append(anchor);
      outlineList.append(item);
      outlineLinks.push({ anchor, id: 'user-content-' + heading.id });
    }
    applyOutline();
  }
  function markHeading() {
    // 閉じた details の中の見出しは、配置の情報が残っていても見えていない。checkVisibility はそれも見分ける。
    const shown = (element) =>
      !!element &&
      (typeof element.checkVisibility === 'function' ? element.checkVisibility() : element.getClientRects().length > 0);
    const visible = outlineLinks.filter((link) => shown(document.getElementById(link.id)));
    let current = visible[0];
    const top = toolbarHeight() + 12;
    // 文書の末尾の近くの見出しは、画面の上端まで上がらない。いちばん下まで来たら、画面に入っている最後の見出しを選ぶ。
    const atBottom = window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 2;
    for (const link of visible) {
      const element = document.getElementById(link.id);
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
    if (changed && last) await renderMermaid(gen);
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
      syncScroll();
    });
  }

  // ---- 描画 ---------------------------------------------------------------------

  let initialAnchor = true;
  let gen = 0; // 描画の世代。非同期の処理の途中で次の描画が来たら、古いほうは捨てる

  async function render(m) {
    // 導入・更新の完了後は、HTML に埋めた資産情報も読み直す。
    if (Object.hasOwn(m, 'assets_id') && m.assets_id !== pageAssetsId) {
      reloadPage();
      return;
    }
    if (Object.hasOwn(m, 'currentDocument')) jumpEnabled = Boolean(CONFIG.jump && m.currentDocument);
    const my = ++gen;
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
    preparePlantuml(tpl.content);
    // コード配色は要素自身の背景も塗るので、body には付けずコードの範囲へ限定する。
    for (const highlight of tpl.content.querySelectorAll('.highlight')) {
      highlight.classList.add('code-syntax-highlight-theme');
      const pre = highlight.closest('pre');
      if (pre) pre.classList.add('code-syntax-highlight-theme');
    }

    // 開いていた <details> は、描き直しても開いたままにする
    const open = Array.from(content.querySelectorAll('details'), (d) => d.open);
    content.replaceChildren(tpl.content);
    content.querySelectorAll('details').forEach((d, i) => {
      if (open[i]) d.open = true;
    });
    buildNotices(Object.hasOwn(m, 'error') ? [] : m.notices);
    buildOutline(m.headings);
    restoreSticky();
    buildMap();
    syncScroll();

    await renderMath(my);
    await renderMermaid(my);
    if (my !== gen) return;
    buildMap();
    syncScroll();
    markHeading();
    if (initialAnchor && location.hash && !lastScroll) {
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
    const loaded = new Map();
    for (const img of content.querySelectorAll(PLANTUML_IMAGES)) {
      if (!img.complete || !img.naturalWidth) continue;
      const same = loaded.get(img.src) || [];
      same.push(img);
      loaded.set(img.src, same);
    }
    for (let img of fragment.querySelectorAll(PLANTUML_IMAGES)) {
      const old = loaded.get(img.src)?.shift();
      if (old) {
        const pos = img.getAttribute('data-sourcepos');
        if (pos) old.setAttribute('data-sourcepos', pos);
        else old.removeAttribute('data-sourcepos');
        img.replaceWith(old);
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
          holder.replaceWith(out);
          queueLayout();
        },
        { once: true },
      );
    }
  }

  // 画面の外の図も、印刷のレイアウトを作る前に読み込みを始める。
  window.addEventListener('beforeprint', () => {
    for (const img of content.querySelectorAll('img.gp-plantuml')) img.loading = 'eager';
  });

  function sourcepos(el) {
    if (el.hasAttribute('data-sourcepos')) return el.getAttribute('data-sourcepos');
    const inner = el.querySelector('[data-sourcepos]');
    return inner ? inner.getAttribute('data-sourcepos') : null;
  }

  async function renderMath(my) {
    const els = Array.from(content.querySelectorAll('.js-render-math'));
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
      box.replaceWith(out);
    }
  }

  let mermaidTheme = null;
  let mermaidSeq = 0;
  let mermaidGeneration = 0;
  const svgCache = new Map();
  const mermaidSources = new WeakMap();

  async function renderMermaid(my) {
    const pass = ++mermaidGeneration;
    const codes = Array.from(content.querySelectorAll('.js-render-mermaid, .gp-mermaid')).filter(
      (el) => el.matches('.js-render-mermaid') || mermaidSources.has(el),
    );
    if (!codes.length) return;
    let mermaid;
    try {
      mermaid = await loadLibrary('mermaid');
    } catch {
      return; // 読めなければ図の原文を見せる
    }
    if (my !== gen || pass !== mermaidGeneration) return;
    const theme = effectiveTheme() === 'dark' ? 'dark' : 'default';
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
      } catch (e) {
        document.getElementById('d' + id)?.remove(); // 失敗したときに mermaid が残す作業用の要素
        out.className = 'gp-error';
        out.textContent = 'mermaid: ' + (e && e.message ? e.message : String(e));
      }
      if (box.isConnected) box.replaceWith(out);
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

  function syncScroll() {
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
    if (!jumpEnabled) return;
    const element = event.target instanceof Element ? event.target : event.target.parentElement;
    if (
      !element ||
      element.closest('summary, input, button, video, audio') ||
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
    if (!jumpEnabled) return;
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
