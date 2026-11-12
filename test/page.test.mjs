// ページの配線を、本物の HTTP / SSE・KaTeX・mermaid・ブラウザで確かめる。
import assert from 'node:assert/strict';
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import test from 'node:test';
import { prepareGoPageTests, renderGoPageDocument, startGoPageServer, startGoStdio, temp } from './helpers.mjs';
import { assetFixture } from './assets-fixture.mjs';
import { pathToFileURL } from 'node:url';

const require = createRequire(import.meta.url);
let chromium;
try {
  ({ chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright'));
} catch {
  // 依存がない場合は理由を添えて未実施にする。追加の導入はしない。
}
const browserPath = [
  process.env.EDGE_BIN,
  process.env.CHROME_BIN,
  process.platform === 'win32' && join(process.env['ProgramFiles(x86)'] || '', 'Microsoft/Edge/Application/msedge.exe'),
  process.platform === 'win32' && join(process.env.ProgramFiles || '', 'Microsoft/Edge/Application/msedge.exe'),
  process.platform === 'win32' && join(process.env.ProgramFiles || '', 'Google/Chrome/Application/chrome.exe'),
  process.platform === 'win32' && join(process.env.LOCALAPPDATA || '', 'Google/Chrome/Application/chrome.exe'),
  process.platform === 'darwin' && '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  process.platform === 'linux' && '/usr/bin/chromium',
  process.platform === 'linux' && '/usr/bin/google-chrome',
  chromium && chromium.executablePath(),
].find((path) => path && existsSync(path));
const goCommands = await prepareGoPageTests();
const skip =
  goCommands.skip ||
  (!chromium
    ? 'Playwright が見つからない（PLAYWRIGHT_MODULE または playwright が必要）'
    : !browserPath
      ? 'Edge / Chrome / Chromium が見つからない（EDGE_BIN または CHROME_BIN を指定できる）'
      : false);

test('PlantUML の画像・失敗時の原文・読み込み済み画像の使い回しと印刷を確かめる', { skip }, async (t) => {
  const server = await startGoPageServer(t, { live: true, jump: true, plantuml: true });
  const jumps = server.jumps;
  const markdown = (heading, source) => '# ' + heading + '\n\n```plantuml\n' + source + '\n```\n';
  await server.setDocument({ name: '図.md', text: markdown('最初', '日本語'), rev: 1 });
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const context = await browser.newContext();
  t.after(() => context.close());
  const outside = [];
  await context.route('**/*', (route) => {
    if (route.request().url().startsWith(server.url)) return route.continue();
    outside.push(route.request().url());
    return route.abort();
  });
  const page = await context.newPage();
  await page.goto(server.url);
  await page.waitForFunction(() => document.querySelector('img.gp-plantuml')?.naturalWidth > 0);
  assert.equal(await page.locator('img.gp-plantuml').getAttribute('alt'), 'PlantUML の図');
  assert.equal(await page.locator('img.gp-plantuml').getAttribute('loading'), 'lazy');
  assert.equal(await page.locator('#gp-content svg').count(), 0);
  // 行へ移れるときは、図のダブルクリックで行へ移り、SVG へのリンク (新しいタブ) は開かない。
  const opened = [];
  context.on('page', (other) => opened.push(other));
  const jumped = page.waitForResponse(
    (value) => value.url().endsWith('/_/jump') && value.request().method() === 'POST',
  );
  await page.locator('img.gp-plantuml').dblclick();
  await jumped;
  await server.sync();
  assert.deepEqual(jumps, [{ line: 3 }]);
  assert.deepEqual(opened, []);
  await page.evaluate(() => {
    window.savedPlantuml = document.querySelector('img.gp-plantuml');
  });
  await server.setDocument({ name: '図.md', text: markdown('次の見出し', '日本語'), rev: 2 });
  await page.waitForFunction(() => document.querySelector('#gp-content h1')?.textContent.includes('次の見出し'));
  assert.equal(await page.evaluate(() => window.savedPlantuml === document.querySelector('img.gp-plantuml')), true);
  // 文書が変わって全置換になっても、外す範囲の読み込み済み画像を使い回す。
  await server.setDocument({ name: '次の図.md', text: markdown('次の見出し', '日本語'), rev: 3 });
  await page.waitForFunction(() => window.gpPreview.current().rev === 3);
  assert.equal(await page.evaluate(() => window.savedPlantuml === document.querySelector('img.gp-plantuml')), true);
  assert.deepEqual(await page.evaluate(() => {
    const anchor = document.querySelector('a[data-diagram="plantuml"]');
    const prevented = [];
    const record = (event) => { prevented.push(event.defaultPrevented); event.preventDefault(); };
    document.addEventListener('click', record);
    window.gpPreview.setEditing(true);
    anchor.click();
    window.gpPreview.setEditing(false);
    anchor.click();
    document.removeEventListener('click', record);
    return prevented;
  }), [false, true]);
  await page.evaluate(() => window.dispatchEvent(new Event('beforeprint')));
  assert.equal(await page.locator('img.gp-plantuml').getAttribute('loading'), 'eager');
  await page.emulateMedia({ media: 'print' });
  assert.equal(await page.locator('img.gp-plantuml').isVisible(), true);
  const source = 'plain 日本語 "><script>危険な原文</script>';
  await server.setDocument({ name: '図.md', text: markdown('失敗', source), rev: 4 });
  await page.waitForSelector('.gp-plantuml-error');
  assert.equal(await page.locator('.gp-plantuml-error p').textContent(), 'PlantUML の図を描けませんでした');
  assert.equal(await page.locator('.gp-plantuml-error code').textContent(), source);
  assert.equal(await page.locator('.gp-plantuml-error').getAttribute('data-sourcepos'), '3:1-5:3');
  assert.equal(await page.locator('.gp-plantuml-error script').count(), 0);
  assert.deepEqual(outside, []);
});

test('書き出した単一 HTML を file:// で開き、数式と図を外部要求と CSP 違反なしで描く', { skip }, async (t) => {
  const root = temp(t),
    file = join(root, 'sample.md'),
    output = join(root, 'out.html');
  writeFileSync(file, '# 書き出し\n\n$`x^2`$\n\n```mermaid\ngraph TD; A-->B\n```\n');
  await renderGoPageDocument({ file, root, output });
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const context = await browser.newContext();
  t.after(() => context.close());
  const outside = [],
    violations = [],
    errors = [];
  await context.exposeBinding('recordExportViolation', (_source, value) => violations.push(value));
  await context.addInitScript(() => {
    document.addEventListener(
      'securitypolicyviolation',
      (event) => void window.recordExportViolation({ directive: event.violatedDirective, uri: event.blockedURI }),
    );
  });
  context.on('request', (req) => {
    if (!/^(file:|data:|blob:)/.test(req.url())) outside.push(req.url());
  });
  await context.route('**/*', (route) =>
    /^(file:|data:|blob:)/.test(route.request().url()) ? route.continue() : route.abort(),
  );
  const page = await context.newPage();
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(pathToFileURL(output).href);
  await page.waitForSelector('.katex');
  await page.waitForSelector('.gp-mermaid svg');
  await page.evaluate(() => document.fonts.ready);
  assert.deepEqual(violations, []);
  assert.deepEqual(outside, []);
  assert.deepEqual(errors, []);
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.waitForFunction(() => getComputedStyle(document.body).backgroundColor === 'rgb(31, 30, 36)');
  await page.waitForSelector('.gp-mermaid svg');
  assert.deepEqual(violations, []);
  assert.deepEqual(outside, []);
});

test('小さな公式 CSS の読み込み・配色と失敗時の同梱スタイルへの復帰', { skip }, async (t) => {
  const f = assetFixture(t);
  f.save();
  const server = await startGoPageServer(t, {
    render: { file: 'sample.md', dir: '', rev: 1, html: '<p>本文</p>', headings: [] },
    assetsDir: f.directory,
    assetsLock: readFileSync(f.lockPath, 'utf8'),
  });
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  for (const failure of [false, true]) {
    const context = await browser.newContext({ colorScheme: 'light' });
    t.after(() => context.close());
    const outside = [];
    await context.route('**/*', (route) => {
      const url = route.request().url();
      if (!url.startsWith(server.url)) {
        outside.push(url);
        return route.abort();
      }
      if (failure && url.endsWith('/app.css')) return route.abort();
      return route.continue();
    });
    const page = await context.newPage();
    await page.goto(server.url);
    await page.waitForFunction(
      (want) => document.documentElement.dataset.style === want,
      failure ? 'fallback' : 'official',
    );
    if (failure) {
      await page.waitForSelector('#gp-fallback-style', { state: 'attached' }); // <link> は画面に見えない要素
      assert.match(await page.locator('#gp-banner').textContent(), /公式スタイルを読み込めません/);
    } else {
      assert.equal(await page.locator('#gp-content').evaluate((el) => getComputedStyle(el).color), 'rgb(1, 2, 3)');
      await page.selectOption('#gp-theme', 'dark');
      await page.waitForFunction(
        () => getComputedStyle(document.querySelector('#gp-content')).color === 'rgb(4, 5, 6)',
      );
      assert.equal(await page.locator('#gp-fallback-style').count(), 0);
    }
    assert.deepEqual(outside, []);
  }
});

const payload = (html, extra = {}) => ({
  file: 'docs/試験.md',
  dir: 'docs/',
  rev: 1,
  html,
  headings: [],
  ...extra,
});

async function fixturePage(
  t,
  browser,
  render = payload('<h1>見出し</h1><p>本文</p>'),
  prepareRoot = () => {},
  { jump = false, live = false, directory = false, edit = '', review = false, launchFail = false, plantuml = false, beforeNavigate, waitUntil = 'load' } = {},
) {
  const root = temp(t);
  prepareRoot(root);
  const jumpRequests = [];
  const server = await startGoPageServer(t, { root, live, jump, directory, edit, review, launchFail, plantuml, render });
  const jumps = server.jumps;
  if (live && !directory)
    await server.setDocument({
      path: join(root, 'README.md'),
      name: 'README.md',
      text: readFileSync(join(root, 'README.md'), 'utf8'),
      rev: 1,
    });
  const eventRequests = [];
  const context = await browser.newContext({ viewport: { width: 2000, height: 900 }, colorScheme: 'light' });
  const attempted = [];
  const errors = [];
  const blockedAcrossPages = [];
  await context.exposeBinding('gpRecordViolation', (_source, value) => blockedAcrossPages.push(value));
  // CSP で止まった場合も記録するため、route だけでなく request も監視する。
  context.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname.endsWith('/_/events')) eventRequests.push(request.url());
    if (url.pathname.endsWith('/_/jump')) jumpRequests.push({ method: request.method(), body: request.postDataJSON() });
    if (url.protocol !== 'data:' && url.protocol !== 'blob:' && url.hostname !== '127.0.0.1') {
      attempted.push({ url: request.url(), type: request.resourceType() });
    }
  });
  await context.route('**/*', (route) => {
    const url = new URL(route.request().url());
    return url.hostname === '127.0.0.1' ? route.continue() : route.abort();
  });
  // 通信開始前の拒否やインラインの違反も、文書の移動をまたいで記録する。
  await context.addInitScript(() => {
    window.gpBlocked = [];
    document.addEventListener('securitypolicyviolation', (event) => {
      const blocked = { url: event.blockedURI, type: event.violatedDirective };
      window.gpBlocked.push(blocked);
      void window.gpRecordViolation(blocked).catch(() => {});
    });
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on('pageerror', (error) => errors.push(error.message));
  t.after(async () => {
    const blocked = page.isClosed() ? [] : await page.evaluate(() => window.gpBlocked);
    await context.close();
    await server.close();
    assert.deepEqual(errors, [], 'ページの例外がない');
    assert.deepEqual(attempted, [], '本文の外部画像を含まない試験では、外への要求を試みない');
    assert.deepEqual(blocked, [], 'CSP 違反もない');
    assert.deepEqual(blockedAcrossPages, [], '文書を移動する前の CSP 違反もない');
  });
  if (beforeNavigate) await beforeNavigate(page);
  await page.goto(server.url, { waitUntil });
  await page.waitForFunction(() => document.querySelector('#gp-content').textContent !== '読み込み中…');
  await page.waitForSelector('#gp-fallback-style', { state: 'attached' });
  return {
    page,
    server,
    root,
    jumps,
    jumpRequests,
    eventRequests,
    send(name, data) {
      return server.broadcast(name, data);
    },
  };
}

// 属性順と、文書ごとに生成する脚注・mermaid の識別子だけをそろえて木を比べる。
async function contentTree(page) {
  return page.locator('#gp-content').evaluate((root) => {
    const ids = new Map();
    for (const element of root.querySelectorAll('[id]')) {
      if (element.closest('.gp-mermaid') || /^user-content-(?:fn|fnref)-/.test(element.id))
        ids.set(element.id, '比較用-' + ids.size);
    }
    const normalize = (value) => {
      for (const [id, replacement] of ids) value = value.replaceAll(id, replacement);
      return value;
    };
    function visit(node) {
      if (node.nodeType !== Node.ELEMENT_NODE) return [node.nodeType, normalize(node.data || '')];
      return [node.namespaceURI, node.localName,
        Array.from(node.attributes, (a) => [a.name, normalize(a.value)]).sort(([a], [b]) => a.localeCompare(b)),
        Array.from(node.childNodes, visit)];
    }
    return Array.from(root.childNodes, visit);
  });
}

test('画面のコメント', { skip, timeout: 240000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const source = '# コメントの試験\n\n最初の段落の文字。\n\n次の段落。\n\n```js\nconst n = 1;\n```\n\n<div>HTML の一つ目</div>\n\n<div>HTML の二つ目</div>\n\n境の段落。\n\n| 列 | 数 |\n| --- | --- |\n| 値 | 2 |\n\n<table><tr><td colspan="2">結合セル</td></tr><tr><td>左</td><td>右</td></tr></table>\n';
  const reviewPage = (t, text = source, options = {}) => fixturePage(t, browser, undefined, (root) => {
    writeFileSync(join(root, 'README.md'), text);
    for (const [name, contents] of Object.entries(options.files || {})) writeFileSync(join(root, name), contents);
  }, { live: true, review: true, ...options });
  const enter = async (page) => {
    await page.locator('#gp-review-toggle').click();
    await page.waitForFunction(() => document.querySelector('#gp-review-layer').dataset.render === String(window.gpPreview.current().render));
    assert.equal(await page.locator('#gp-review-toggle').getAttribute('aria-pressed'), 'true');
  };
  const addComment = async (page, target, text) => {
    await target.click();
    await page.locator('#gp-review-input').fill(text);
    await page.locator('#gp-review-add').click();
  };
  const card = (page, text) => page.locator('.gp-review-card').filter({ has: page.locator('p').filter({ hasText: text }) });
  const prepare = async (page) => {
    const response = page.waitForResponse((value) => value.url().includes('/_/review/prepare') && value.request().method() === 'POST');
    await page.locator('#gp-review-prepare').click();
    const result = await response;
    await page.waitForFunction(() => !document.querySelector('#gp-review-output').hidden);
    assert.equal(result.status(), 200, await result.text());
    return page.locator('#gp-review-prompt').inputValue();
  };
  const selectText = (page, start, startOffset, end = start, endOffset = startOffset + 2) => page.evaluate((value) => {
    const range = document.createRange();
    range.setStart(document.querySelector(value.start).firstChild, value.startOffset);
    range.setEnd(document.querySelector(value.end).firstChild, value.endOffset);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event('selectionchange'));
  }, { start, startOffset, end, endOffset });
  const clipboard = (page, fail = false) => page.addInitScript((fail) => {
    window.reviewClipboard = [];
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      async writeText(text) {
        if (fail) throw new Error('試験用のコピー拒否');
        window.reviewClipboard.push(text);
      },
    } });
  }, fail);

  await t.test('指定した画面だけにボタンを出し、毎回閉じて始め、印刷と狭い画面で配置する', async (t) => {
    const disabled = await reviewPage(t, source, { review: false });
    assert.equal(await disabled.page.locator('#gp-review-toggle').isVisible(), false);
    assert.equal(await disabled.page.locator('#gp-review-panel').count(), 0);
    const { page } = await reviewPage(t);
    assert.equal(await page.locator('#gp-review-toggle').isVisible(), true);
    assert.equal(await page.locator('#gp-review-panel').isVisible(), false);
    await enter(page);
    assert.equal(await page.locator('#gp-review-add').isDisabled(), true);
    assert.match(await page.locator('#gp-review-add').getAttribute('title'), /選んで/);
    assert.equal(await page.locator('#gp-review-prepare').isDisabled(), true);
    assert.equal(Math.round((await page.locator('#gp-review-panel').boundingBox()).width), 360);
    await page.setViewportSize({ width: 600, height: 900 });
    const bounds = await page.locator('#gp-review-panel').boundingBox();
    assert.equal(Math.round(bounds.height), 405);
    assert.equal(Math.round(bounds.width), 600);
    await page.emulateMedia({ media: 'print' });
    for (const selector of ['#gp-review-panel', '#gp-review-layer', '#gp-review-toggle'])
      assert.equal(await page.locator(selector).isVisible(), false);
    await page.emulateMedia({ media: 'screen' });
    await page.reload();
    await page.waitForSelector('#gp-review-toggle');
    assert.equal(await page.locator('#gp-review-panel').isVisible(), false);
    assert.equal(await page.locator('#gp-review-toggle').getAttribute('aria-pressed'), 'false');
    assert.equal(await page.evaluate(() => JSON.parse(sessionStorage.getItem('gitlab-preview-appearance') || '{}').review), undefined);
  });

  await t.test('編集中と原文の読み込み中は切り替えず、編集とコメントは同時に開かない', async (t) => {
    const { page, root } = await reviewPage(t, source, { edit: 'file' });
    await page.locator('#gp-edit-toggle').click();
    await editIdle(page);
    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    await page.route('**/_/edit/source?**', async (route) => { await gate; await route.continue(); });
    await page.locator('#gp-content > p').first().dblclick();
    await page.waitForFunction(() => window.gpEditMode.busy());
    await page.locator('#gp-review-toggle').click();
    assert.match(await page.locator('#gp-review-status').textContent(), /編集を確定するか取り消して/);
    assert.equal(await page.evaluate(() => window.gpEditMode.active() && !window.gpReviewMode.active()), true);
    release();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    await page.locator('#gp-edit-input').fill('まだ確定しない文');
    await page.locator('#gp-review-toggle').click();
    assert.equal(await page.locator('#gp-edit-input').inputValue(), 'まだ確定しない文');
    assert.equal(readFileSync(join(root, 'README.md'), 'utf8'), source);
    assert.equal(await page.locator('#gp-review-panel').isVisible(), false);
    await page.locator('#gp-edit-input').press('Escape');
    await enter(page);
    assert.equal(await page.evaluate(() => !window.gpEditMode.active() && window.gpReviewMode.active()), true);
    await page.locator('#gp-review-input').fill('切り替えても残すコメント');
    await page.locator('#gp-edit-toggle').click();
    assert.equal(await page.evaluate(() => window.gpEditMode.active() && !window.gpReviewMode.active()), true);
    await editIdle(page);
    await enter(page);
    assert.equal(await page.locator('#gp-review-input').inputValue(), '切り替えても残すコメント');
  });

  await t.test('段落・見出し・コード・HTML の連なり・両方の表のセルを選び、本文に印を足さない', async (t) => {
    const { page, jumpRequests } = await reviewPage(t, source + '\n[リンク](#user-content-コメントの試験)\n', { jump: true });
    const before = await page.locator('#gp-content').evaluate((content) => Array.from(content.querySelectorAll('p, h1, pre'),
      (element) => Array.from(element.attributes, (a) => [a.name, a.value])));
    await enter(page);
    for (const [selector, label, text] of [
      ['#gp-content > p', '段落:', '段落へのコメント'],
      ['#gp-content h1', '見出し:', '見出しへのコメント'],
      ['#gp-content pre', 'コード:', 'コードへのコメント'],
      ['#gp-content > div:not(.markdown-code-block)', 'HTML のブロック', 'HTML へのコメント'],
      ['#gp-content td', '表 1 の 2 行 1 列', 'パイプのセルへのコメント'],
      ['#gp-content td[colspan="2"]', '表 2 の 1 行 1 列', 'HTML のセルへのコメント'],
    ]) {
      await page.locator(selector).first().click();
      assert.match(await page.locator('#gp-review-target').textContent(), new RegExp(label));
      await page.locator('#gp-review-input').fill(text);
      await page.locator('#gp-review-add').click();
    }
    assert.equal(await page.locator('.gp-review-card').count(), 6);
    assert.equal(await page.locator('#gp-review-toggle').textContent(), 'コメント 6');
    await page.locator('#gp-content > div:not(.markdown-code-block)').first().hover();
    await page.waitForFunction(() => document.querySelector('#gp-review-hover').textContent.includes(':nth-child('));
    const rule = await page.locator('#gp-review-hover').textContent();
    assert.equal(rule.includes('data-sourcepos'), false);
    assert.equal((rule.match(/#gp-content/g) || []).length, 2, 'HTML の連なり全体に枠を出す');
    const after = await page.locator('#gp-content').evaluate((content) => Array.from(content.querySelectorAll('p, h1, pre'),
      (element) => Array.from(element.attributes, (a) => [a.name, a.value])));
    assert.deepEqual(after, before);
    await page.locator('#gp-content a').last().click();
    assert.equal(new URL(page.url()).hash, '');
    await page.locator('#gp-content h1').dblclick();
    assert.deepEqual(jumpRequests, []);
    await page.evaluate(() => window.getSelection().removeAllRanges());
    await page.locator('#gp-content').dispatchEvent('click');
    assert.equal(await page.locator('#gp-review-status').textContent(), 'ここにはコメントを付けられません');
    assert.equal(await page.locator('#gp-review-add').isDisabled(), true);
    assert.match(await prepare(page), /^\/plan\n/);
  });

  await t.test('対象外の表は理由を知らせ、選べない', async (t) => {
    const { page } = await reviewPage(t, '```json:table\n{"markdown":true,"items":[{"列":"JSON の値"}]}\n```\n\n<table><tr><td><table><tr><td>入れ子</td></tr></table></td></tr></table>\n');
    await enter(page);
    await page.locator('#gp-content td').filter({ hasText: 'JSON の値' }).click();
    assert.match(await page.locator('#gp-review-status').textContent(), /JSON から作った表/);
    await page.locator('#gp-content table table td').click();
    assert.match(await page.locator('#gp-review-status').textContent(), /入れ子の表/);
    assert.equal(await page.locator('#gp-review-add').isDisabled(), true);
  });

  await t.test('一つの段落内の選択だけを添え、またがる選択と長すぎる選択を知らせる', async (t) => {
    const { page } = await reviewPage(t, '最初の段落。\n\n次の段落。\n\n' + '長'.repeat(2001) + '\n');
    await enter(page);
    await selectText(page, '#gp-content > p:nth-child(1)', 0, '#gp-content > p:nth-child(1)', 2);
    assert.equal(await page.locator('#gp-review-selection-text').textContent(), '選んだ文字: 最初');
    await page.locator('#gp-review-input').fill('選んだ文字へのコメント');
    await page.locator('#gp-review-input').press('Control+Enter');
    assert.match(await page.locator('.gp-review-card').textContent(), /選んだ文字: 最初/);
    await selectText(page, '#gp-content > p:nth-child(1)', 0, '#gp-content > p:nth-child(2)', 2);
    assert.match(await page.locator('#gp-review-status').textContent(), /2 つ以上の対象にまたがって/);
    assert.equal(await page.locator('#gp-review-selection-text').isVisible(), false);
    await selectText(page, '#gp-content > p:nth-child(3)', 0, '#gp-content > p:nth-child(3)', 2001);
    assert.equal(await page.locator('#gp-review-status').textContent(), '選んだ文字が長すぎます（2,000 文字まで）');
    assert.equal(await page.locator('#gp-review-selection-text').isVisible(), false);
    await page.locator('#gp-review-input').fill('残す下書き');
    await page.locator('#gp-review-input').press('Escape');
    assert.equal(await page.locator('#gp-review-input').inputValue(), '残す下書き');
    assert.equal(await page.locator('#gp-review-add').isDisabled(), true);
  });

  await t.test('コメントを直し、本文へ移り、消す', async (t) => {
    const { page } = await reviewPage(t);
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), '最初のコメント');
    await page.getByRole('button', { name: '直す', exact: true }).click();
    assert.equal(await page.locator('#gp-review-input').inputValue(), '最初のコメント');
    await page.locator('#gp-review-input').fill('直したコメント');
    await page.locator('#gp-review-input').press('Control+Enter');
    assert.equal(await page.locator('.gp-review-card').count(), 1);
    assert.match(await page.locator('.gp-review-card').textContent(), /直したコメント/);
    await page.getByRole('button', { name: '本文へ移る', exact: true }).click();
    assert.match(await page.locator('#gp-review-target').textContent(), /段落: 最初/);
    await page.waitForFunction(() => document.querySelector('#gp-review-selection').textContent.includes(':nth-child('));
    await page.getByRole('button', { name: '消す', exact: true }).click();
    assert.equal(await page.locator('.gp-review-card').count(), 0);
    assert.equal(await page.locator('#gp-review-prepare').isDisabled(), true);
  });

  await t.test('再読み込みで戻り、文書ごとに保存し、消えた対象は要確認にする', async (t) => {
    const { page, server, root } = await reviewPage(t, source, { files: { 'other.md': '別の文書。\n' } });
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), '保存するコメント');
    await page.reload();
    await page.waitForSelector('#gp-review-toggle');
    assert.equal(await page.locator('#gp-review-panel').isVisible(), false);
    await enter(page);
    assert.equal(await page.locator('.gp-review-card').count(), 1);
    const saved = await page.evaluate(() => JSON.parse(sessionStorage.getItem('gitlab-preview-review')));
    assert.equal(saved.version, 1);
    assert.equal(saved.docs.current[0].text, '保存するコメント');
    await page.goto(server.url + '?doc=other.md');
    await page.waitForFunction(() => document.querySelector('#gp-content').textContent.includes('別の文書。'));
    await enter(page);
    assert.equal(await page.locator('.gp-review-card').count(), 0);
    await addComment(page, page.locator('#gp-content > p').first(), '別の文書のコメント');
    const posted = page.waitForResponse((r) => r.url().includes('/_/review/prepare?doc=other.md'));
    await prepare(page);
    await posted;
    await page.goto(server.url);
    await page.waitForSelector('#gp-review-toggle');
    await enter(page);
    assert.match(await page.locator('.gp-review-card').textContent(), /保存するコメント/);
    writeFileSync(join(root, 'README.md'), '# 変更後\n');
    await server.setDocument({ path: join(root, 'README.md'), text: '# 変更後\n', rev: 2 });
    await page.waitForFunction(() => document.querySelector('.gp-review-state').textContent === '要確認');
    assert.equal(await page.locator('.gp-review-card').count(), 1);
    await page.getByRole('button', { name: '本文へ移る', exact: true }).click();
    assert.match(await page.locator('#gp-review-status').textContent(), /対象が見つかりません/);
  });

  await t.test('依頼文に相対パス・原文の行と抜粋・選んだ文字・コメントを入れる', async (t) => {
    const { page, root, server } = await reviewPage(t);
    await enter(page);
    await selectText(page, '#gp-content > p:nth-child(2)', 0, '#gp-content > p:nth-child(2)', 2);
    await page.locator('#gp-review-input').fill('この表現を詳しくしてください');
    await page.locator('#gp-review-add').click();
    const prompt = await prepare(page);
    for (const text of ['/plan\n', '対象のファイル: `README.md`\n', '## コメント 1: 段落（3 行）\n', '最初の段落の文字。', '選んだ文字:\n```text\n最初\n```', 'コメント:\nこの表現を詳しくしてください\n'])
      assert.ok(prompt.includes(text), text);
    // 依頼文はサーバーが作り、端末の絶対パスと token を入れない。
    for (const secret of [root, root.replaceAll('\\', '/'), server.token])
      assert.equal(prompt.includes(secret), false, '依頼文に入れてはいけない値が入った');
    assert.equal(await page.locator('#gp-review-prompt').getAttribute('readonly'), '');
    assert.match(await page.locator('#gp-review-bytes').textContent(), /バイト/);
    assert.equal(await page.locator('#gp-review-prompt').evaluate((el) => el.value.length > 0 && el.textContent === ''), true);
  });

  await t.test('prepare が断った対象の番号を、そのカードの誤りにする', async (t) => {
    const { page } = await reviewPage(t, '# 見出し\n\n::include{file=part.md}\n', { files: { 'part.md': '取り込んだ段落。\n' } });
    await enter(page);
    await addComment(page, page.locator('#gp-content h1'), '正しい対象');
    await addComment(page, page.locator('#gp-content p'), '断られる対象');
    const response = page.waitForResponse((value) => value.url().includes('/_/review/prepare'));
    await page.locator('#gp-review-prepare').click();
    const result = await response;
    const data = await result.json();
    assert.equal(result.status(), 422);
    assert.equal(data.error.index, 1);
    await page.waitForFunction(() => document.querySelectorAll('.gp-review-state')[1].textContent.startsWith('誤り:'));
    assert.equal(await page.locator('.gp-review-state').first().textContent(), '未コピー');
    assert.match(await page.locator('.gp-review-state').nth(1).textContent(), /誤り:.*取り込/);
    assert.equal(await page.locator('#gp-review-output').isVisible(), false);
    assert.match(await page.locator('#gp-review-status').textContent(), /取り込/);
  });

  await t.test('コピー成功で送ったコメントだけをコピー済みにし、まとめて消せる', async (t) => {
    const { page } = await reviewPage(t, source, { beforeNavigate: (page) => clipboard(page) });
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), 'コピーするコメント');
    const prompt = await prepare(page);
    await page.locator('#gp-review-copy').click();
    await page.waitForFunction(() => document.querySelector('.gp-review-state').textContent === 'コピー済み');
    assert.deepEqual(await page.evaluate(() => window.reviewClipboard), [prompt]);
    assert.equal(await page.locator('#gp-review-status').textContent(), '依頼文をコピーしました。');
    assert.equal(await page.locator('#gp-review-prepare').isDisabled(), true);
    await addComment(page, page.locator('#gp-content h1'), '次にコピーするコメント');
    const response = page.waitForResponse((value) => value.url().includes('/_/review/prepare'));
    const next = await prepare(page);
    assert.equal((await response).request().postDataJSON().comments.length, 1);
    assert.equal(next.includes('\nコピーするコメント\n'), false);
    await page.locator('#gp-review-clear-copied').click();
    assert.equal(await page.locator('.gp-review-card').count(), 1);
    assert.match(await page.locator('.gp-review-card').textContent(), /次にコピーするコメント/);
  });

  await t.test('コピー失敗では依頼文を全選択し、コメントの状態を変えない', async (t) => {
    const { page } = await reviewPage(t, source, { beforeNavigate: (page) => clipboard(page, true) });
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), 'コピーできないコメント');
    const prompt = await prepare(page);
    await page.locator('#gp-review-copy').click();
    await page.waitForFunction(() => document.querySelector('#gp-review-status').textContent.includes('Ctrl+C'));
    assert.deepEqual(await page.locator('#gp-review-prompt').evaluate((el) => [el.selectionStart, el.selectionEnd, el === document.activeElement]), [0, prompt.length, true]);
    assert.equal(await page.locator('.gp-review-state').textContent(), '未コピー');
    assert.deepEqual(await page.evaluate(() => window.reviewClipboard), []);
  });

  await t.test('コピーして Agents Window を開き、同じ依頼文では一度だけ起動する', async (t) => {
    const { page, server, root } = await reviewPage(t, source, { beforeNavigate: (page) => clipboard(page) });
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), '起動して送るコメント');
    const prompt = await prepare(page);
    const launches = [];
    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    await page.route('**/_/review/launch*', async (route) => {
      launches.push(route.request().postDataJSON());
      await gate;
      await route.continue();
    });
    await page.locator('#gp-review-launch').click();
    await page.waitForFunction(() => document.querySelector('.gp-review-state').textContent === 'コピー済み');
    assert.equal(await page.locator('#gp-review-launch').isDisabled(), true);
    assert.equal(await page.locator('#gp-review-copy').isDisabled(), true);
    release();
    const success = '依頼文をコピーし、Agents Window を開きました。Copilot か Claude のセッションで作業場所を Branch にし、貼り付けて送ってください。';
    await page.waitForFunction((text) => document.querySelector('#gp-review-status').textContent === text, success);
    await server.sync();
    assert.deepEqual(await page.evaluate(() => window.reviewClipboard), [prompt]);
    assert.deepEqual(server.launches, [{ root }]);
    assert.equal(launches.length, 1);
    assert.deepEqual(Object.keys(launches[0]), ['id']);
    assert.match(launches[0].id, /^[0-9a-f]{32}$/);
    assert.equal(await page.locator('#gp-review-launch').isDisabled(), false);
    await page.locator('#gp-review-launch').click();
    await page.waitForFunction(() => document.querySelector('#gp-review-status').textContent === 'Agents Window はもう開いています。貼り付けて送ってください。');
    await server.sync();
    assert.deepEqual(server.launches, [{ root }]);
    assert.deepEqual(await page.evaluate(() => window.reviewClipboard), [prompt, prompt]);
    assert.deepEqual(launches[1], launches[0]);

    await addComment(page, page.locator('#gp-content h1'), '新しい依頼文のコメント');
    const next = await prepare(page);
    await page.locator('#gp-review-launch').click();
    await page.waitForFunction((text) => document.querySelector('#gp-review-status').textContent === text, success);
    await server.sync();
    assert.equal(server.launches.length, 2);
    assert.notEqual(launches[2].id, launches[0].id);
    assert.deepEqual(await page.evaluate(() => window.reviewClipboard), [prompt, prompt, next]);
  });

  await t.test('起動を待つ間に描き直されても、起動の結果を知らせる', async (t) => {
    const { page, server, root } = await reviewPage(t, source, { beforeNavigate: (page) => clipboard(page) });
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), '描き直しをまたぐ起動');
    await prepare(page);
    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    await page.route('**/_/review/launch*', async (route) => { await gate; await route.continue(); });
    await page.locator('#gp-review-launch').click();
    await page.waitForFunction(() => /Agents Window を開いています/.test(document.querySelector('#gp-review-status').textContent));
    const before = await page.evaluate(() => window.gpPreview.current().render);
    const changed = source.replace('次の段落。', '次の段落を変えた。');
    writeFileSync(join(root, 'README.md'), changed);
    await server.setDocument({ path: join(root, 'README.md'), text: changed, rev: 2 });
    await page.waitForFunction((n) => window.gpPreview.current().render !== n, before);
    release();
    await page.waitForFunction(() => /Agents Window を開きました/.test(document.querySelector('#gp-review-status').textContent));
    await server.sync();
    assert.deepEqual(server.launches, [{ root }]);
  });

  await t.test('ファイル文書の起動にも doc を付け、prepare のルートを開く', async (t) => {
    const { page, server, root } = await reviewPage(t, source, {
      files: { 'other.md': '別の文書。\n' }, beforeNavigate: (page) => clipboard(page),
    });
    await page.goto(server.url + '?doc=other.md');
    await page.waitForFunction(() => document.querySelector('#gp-content').textContent.includes('別の文書。'));
    await enter(page);
    await addComment(page, page.locator('#gp-content > p'), '別の文書で起動');
    await prepare(page);
    const response = page.waitForResponse((r) => r.url().includes('/_/review/launch?doc=other.md'));
    await page.locator('#gp-review-launch').click();
    assert.equal((await response).status(), 200);
    await server.sync();
    assert.deepEqual(server.launches, [{ root }]);
  });

  await t.test('起動が失敗してもコピー済みを保ち、サーバーの文を知らせて再試行できる', async (t) => {
    const { page, server, root } = await reviewPage(t, source, { launchFail: true, beforeNavigate: (page) => clipboard(page) });
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), '起動できなくてもコピーする');
    const prompt = await prepare(page);
    for (let i = 0; i < 2; i++) {
      await page.locator('#gp-review-launch').click();
      await page.waitForFunction(() => document.querySelector('#gp-review-status').textContent.includes('依頼文はコピーしました。') && !document.querySelector('#gp-review-launch').disabled);
      assert.match(await page.locator('#gp-review-status').textContent(), /VS Code の code コマンドが見つかりません。.*依頼文はコピーしました。/);
      assert.equal(await page.locator('.gp-review-state').textContent(), 'コピー済み');
      await server.sync();
      assert.equal(server.launches.length, i + 1);
    }
    assert.deepEqual(server.launches, [{ root }, { root }]);
    assert.deepEqual(await page.evaluate(() => window.reviewClipboard), [prompt, prompt]);
    await page.route('**/_/review/launch*', (route) => route.fulfill({ status: 503, contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'launch', message: '<img src=x onerror="window.reviewInjected=1">' } }) }));
    await page.locator('#gp-review-launch').click();
    await page.waitForFunction(() => document.querySelector('#gp-review-status').textContent.startsWith('<img'));
    assert.equal(await page.locator('#gp-review-status img').count(), 0);
    assert.equal(await page.evaluate(() => window.reviewInjected), undefined);
  });

  await t.test('起動ボタンでもコピーの失敗では全選択し、起動の要求を送らない', async (t) => {
    const { page, server } = await reviewPage(t, source, { beforeNavigate: (page) => clipboard(page, true) });
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), 'コピーできないので起動しない');
    const prompt = await prepare(page);
    const requests = [];
    page.on('request', (r) => { if (r.url().includes('/_/review/launch')) requests.push(r.url()); });
    await page.locator('#gp-review-launch').click();
    await page.waitForFunction(() => document.querySelector('#gp-review-status').textContent.includes('Ctrl+C'));
    assert.deepEqual(await page.locator('#gp-review-prompt').evaluate((el) => [el.selectionStart, el.selectionEnd, el === document.activeElement]), [0, prompt.length, true]);
    assert.equal(await page.locator('.gp-review-state').textContent(), '未コピー');
    assert.equal(await page.locator('#gp-review-launch').isDisabled(), false);
    assert.deepEqual(await page.evaluate(() => window.reviewClipboard), []);
    await server.sync();
    assert.deepEqual(requests, []);
    assert.deepEqual(server.launches, []);
  });

  await t.test('HTML を文字として表示し、保存が無効でも続け、壊れた保存は捨てる', async (t) => {
    const { page } = await reviewPage(t);
    await enter(page);
    await page.evaluate(() => { Storage.prototype.setItem = () => { throw new DOMException('保存を拒否', 'QuotaExceededError'); }; });
    const text = '<img src=x onerror="window.reviewInjected=1">';
    await addComment(page, page.locator('#gp-content > p').first(), text);
    assert.match(await card(page, text).textContent(), /<img src=x onerror=/);
    assert.equal(await page.locator('#gp-review-panel img').count(), 0);
    assert.equal(await page.evaluate(() => window.reviewInjected), undefined);
    await page.locator('#gp-review-close').click();
    await enter(page);
    assert.equal(await page.locator('.gp-review-card').count(), 1);
    await page.reload();
    await page.waitForSelector('#gp-review-toggle');
    await page.evaluate(() => sessionStorage.setItem('gitlab-preview-review', '{壊れた JSON'));
    await page.reload();
    await page.waitForSelector('#gp-review-toggle');
    await enter(page);
    assert.equal(await page.locator('.gp-review-card').count(), 0);
    await page.evaluate(() => sessionStorage.setItem('gitlab-preview-review', JSON.stringify({ version: 1, docs: { current: [{ id: '壊れた項目' }] } })));
    await page.reload();
    await page.waitForSelector('#gp-review-toggle');
    await enter(page);
    assert.equal(await page.locator('.gp-review-card').count(), 0);
    // 壊れた文書の分だけを捨て、ほかの文書のコメントは残す。制御文字や長すぎる値も壊れたものとして扱う。
    const good = { id: 'a1', target: { kind: 'block', tag: 'p', range: '3:1-3:27' }, label: '段落: 残る', text: '残るコメント',
      render: 1, state: 'pending', stale: false, error: '' };
    await page.evaluate((good) => sessionStorage.setItem('gitlab-preview-review', JSON.stringify({ version: 1, docs: {
      current: [good], 'file:other.md': [{ ...good, text: '制御文字\u0001' }], 'file:long.md': [{ ...good, label: 'x'.repeat(201) }] } })), good);
    await page.reload();
    await page.waitForSelector('#gp-review-toggle');
    await enter(page);
    assert.equal(await page.locator('.gp-review-card').count(), 1);
    assert.match(await page.locator('.gp-review-card').textContent(), /残るコメント/);
  });

  await t.test('制御文字のコメントを断り、直す間の対象の変更と保存を知らせる', async (t) => {
    const { page } = await reviewPage(t);
    await enter(page);
    await page.locator('#gp-content > p').first().click();
    await page.locator('#gp-review-input').evaluate((element) => {
      element.value = '制御文字\u0007入り';
      element.dispatchEvent(new Event('input'));
    });
    await page.locator('#gp-review-add').click();
    assert.match(await page.locator('#gp-review-status').textContent(), /制御文字/);
    assert.equal(await page.locator('.gp-review-card').count(), 0);
    await page.locator('#gp-review-input').fill('最初のコメント');
    await page.locator('#gp-review-add').click();
    assert.match(await page.locator('#gp-review-status').textContent(), /追加しました/);
    await page.getByRole('button', { name: '直す', exact: true }).click();
    assert.equal(await page.locator('#gp-review-add').textContent(), '保存');
    await page.locator('#gp-content > p').nth(1).click();
    assert.equal(await page.locator('#gp-review-add').textContent(), '保存（対象を変える）');
    await page.locator('#gp-review-add').click();
    assert.match(await page.locator('#gp-review-status').textContent(), /保存しました/);
    assert.match(await page.locator('.gp-review-card').textContent(), /次の段落/);
  });

  await t.test('前の失敗の印を消し、表を確かめられないときはセルを要確認にする', async (t) => {
    const { page } = await reviewPage(t);
    await enter(page);
    await addComment(page, page.locator('#gp-content > p').first(), '一つ目');
    await addComment(page, page.locator('#gp-content td').first(), 'セルへ');
    // 1 回目は 2 件目を断り、2 回目は番号の無い失敗にする。
    let calls = 0;
    await page.route('**/_/review/prepare*', (route) => route.fulfill({ status: 422, contentType: 'application/json',
      body: JSON.stringify(++calls === 1 ? { error: { code: 'excluded', message: '断られた対象', index: 1 } } : { error: { code: 'changed', message: '文書が変わりました' } }) }));
    await page.locator('#gp-review-prepare').click();
    await page.waitForFunction(() => /誤り: 断られた対象/.test(document.querySelector('#gp-review-cards').textContent));
    await page.locator('#gp-review-prepare').click();
    await page.waitForFunction(() => /文書が変わりました/.test(document.querySelector('#gp-review-status').textContent));
    assert.doesNotMatch(await page.locator('#gp-review-cards').textContent(), /誤り/);
    await page.unroute('**/_/review/prepare*');
    // 表の情報を取れなければ、セルのコメントは要確認にする。
    await page.route('**/_/review/tables*', (route) => route.fulfill({ status: 500, contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'internal', message: '試験用の失敗' } }) }));
    await page.locator('#gp-review-close').click();
    await page.locator('#gp-review-toggle').click();
    await page.waitForFunction(() => /要確認/.test(document.querySelector('#gp-review-cards').textContent));
    assert.match(await card(page, 'セルへ').textContent(), /要確認/);
    assert.doesNotMatch(await card(page, '一つ目').textContent(), /要確認/);
  });

  await t.test('表の固定の全幅表示に移ったセルにも、コメントを付けられる', async (t) => {
    const { page } = await reviewPage(t);
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForSelector('[data-md-table-sticky-bar]');
    await enter(page);
    await page.locator('[data-md-table-sticky-action="float"]').first().click();
    await page.waitForSelector('[data-md-table-sticky-float-panel]');
    const cell = page.locator('[data-md-table-sticky-float-panel] td').first();
    await cell.click();
    assert.equal(await page.locator('#gp-review-target').textContent(), '表 1 の 2 行 1 列');
    // 枠と右の欄は、全幅表示より上に出る。
    const rect = await cell.boundingBox();
    assert.equal(await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.closest('[data-md-table-sticky-float-panel]') !== null, { x: rect.x + 2, y: rect.y + 2 }), true);
    const panel = await page.locator('#gp-review-input').boundingBox();
    assert.equal(await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.id, { x: panel.x + 5, y: panel.y + 5 }), 'gp-review-input');
    await page.locator('#gp-review-input').fill('全幅のセルへ');
    await page.locator('#gp-review-add').click();
    assert.match(await card(page, '全幅のセルへ').textContent(), /表 1 の 2 行 1 列/);
  });
});

// 編集では画面だけでなく、本物のファイルに残ったバイトも確かめる。
async function editablePage(t, browser, text, options = {}) {
  const fixture = await fixturePage(t, browser, undefined,
    (root) => {
      writeFileSync(join(root, 'README.md'), text);
      for (const [name, contents] of Object.entries(options.files || {})) writeFileSync(join(root, name), contents);
    }, { live: true, edit: 'file', ...options });
  const { page } = fixture;
  if (options.edit !== '') {
    await page.locator('#gp-edit-toggle').click();
    await page.waitForFunction(() => Number(document.querySelector('#gp-edit-layer').dataset.render) > 0);
  }
  return { ...fixture, file: join(fixture.root, 'README.md') };
}
async function editIdle(page) {
  await page.waitForFunction(() => document.querySelector('#gp-edit-layer').getAttribute('aria-busy') === 'false' &&
    Number(document.querySelector('#gp-edit-layer').dataset.render) === window.gpPreview.current().render);
}
async function replaceCell(page, cell, value, key = 'Enter') {
  await cell.click();
  await page.keyboard.insertText(value);
  await page.keyboard.press(key);
  await editIdle(page);
}
async function menuItem(page, id) {
  const axis = id.startsWith('row-') ? 'rows' : 'cols';
  await page.locator('#gp-edit-' + axis).click();
  return page.locator('#gp-edit-' + id);
}
async function pasteText(page, text) {
  await page.evaluate((text) => {
    const data = new DataTransfer();
    data.setData('text/plain', text);
    document.querySelector('#gp-edit-input').dispatchEvent(new ClipboardEvent('paste', { bubbles: true, clipboardData: data }));
  }, text);
}
async function captureCopy(page) {
  await page.evaluate(() => {
    window.gpCapturedCopies = [];
    if (window.gpCaptureInstalled) return;
    window.gpCaptureInstalled = true;
    const capture = (event) => window.gpCapturedCopies.push({ kind: event.type, prevented: event.defaultPrevented,
      text: event.clipboardData.getData('text/plain'), html: event.clipboardData.getData('text/html'),
      custom: event.clipboardData.getData('application/x-gp-table') });
    document.addEventListener('copy', capture);
    document.addEventListener('cut', capture);
  });
}
async function copied(page, key = 'Control+c') {
  await captureCopy(page);
  await page.keyboard.press(key);
  const result = await page.evaluate(() => window.gpCapturedCopies.at(-1));
  assert.ok(result, 'クリップボードのイベントを受け取る');
  return result;
}
async function pasteEnvelope(page, custom, text = '文字として貼る') {
  await page.evaluate(({ custom, text }) => {
    const data = new DataTransfer();
    data.setData('text/plain', text);
    data.setData('application/x-gp-table', custom);
    document.querySelector('#gp-edit-input').dispatchEvent(new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: data }));
  }, { custom, text });
}
// 本物の表情報のうち、異常系で必要な項目だけを差し替える。
async function changeClipboardMetadata(page, change) {
  await page.route('**/_/edit/tables?*', async (route) => {
    const response = await route.fetch(), data = await response.json();
    change(data);
    await route.fulfill({ response, json: data });
  });
  await page.locator('#gp-edit-toggle').click();
  await page.locator('#gp-edit-toggle').click();
  await editIdle(page);
}

test('プレビューで表を編集し、ファイル・固定の状態・安全な表示を確かめる', { skip }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const pipe = '# 表の編集\n\n前の段落。\n\n| 名前 | 数 | 備考 |\n| --- | --- | --- |\n| りんご | 1 | 赤 |\n| みかん | 2 | 橙 |\n| ぶどう | 3 | 紫 |\n\n後の段落。\n';

  await t.test('編集の出入りと再読み込み、指定しない画面、一覧と scope', async (t) => {
    const { page, server } = await editablePage(t, browser, pipe);
    assert.equal(await page.locator('#gp-edit-toggle').getAttribute('aria-pressed'), 'true');
    assert.equal(await page.locator('html').getAttribute('data-edit'), 'on');
    await page.reload();
    await page.waitForFunction(() => document.documentElement.dataset.edit === 'on');
    await page.locator('#gp-edit-toggle').click();
    assert.equal(await page.locator('#gp-edit-toggle').getAttribute('aria-pressed'), 'false');
    assert.equal(await page.locator('html').getAttribute('data-edit'), null);
    await page.goto(server.url + '?list');
    assert.equal(await page.locator('#gp-edit-toggle').isVisible(), false);
    const other = await editablePage(t, browser, pipe, { edit: '' });
    assert.equal(await other.page.locator('#gp-edit-toggle').isVisible(), false);
    assert.equal(await other.page.locator('#gp-edit-layer').count(), 0);
    await page.route((url) => url.pathname === new URL(server.url).pathname, async (route) => {
      const response = await route.fetch();
      await route.fulfill({ response, body: (await response.text()).replace('"scope":"all"', '"scope":"current"') });
    });
    await page.goto(server.url);
    assert.equal(await page.locator('#gp-edit-toggle').isVisible(), true);
    await page.goto(server.url + '?doc=README.md');
    assert.equal(await page.locator('#gp-edit-toggle').isVisible(), false);
  });

  await t.test('セルの選択、置換、移動、F2、取り消し、削除、改行と縦棒', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    const cells = page.locator('#gp-content td');
    await cells.nth(0).click();
    assert.equal(await page.locator('#gp-edit-input').evaluate((el) => el === document.activeElement), true);
    await page.keyboard.insertText('青りんご');
    await page.keyboard.press('Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('りんご', '青りんご'));
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), 'みかん');
    await page.keyboard.insertText('追加');
    await page.keyboard.press('Escape');
    assert.equal(readFileSync(file, 'utf8').includes('追加'), false);
    await page.keyboard.press('Shift+Enter');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), 'みかん');
    await page.keyboard.press('Shift+Enter');
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '青りんご');
    await page.keyboard.press('Tab');
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '1');
    await page.keyboard.press('Shift+Tab');
    await page.keyboard.press('Delete');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8').includes('青りんご'), false);
    await page.keyboard.insertText('左|右');
    await page.keyboard.press('Alt+Enter');
    await page.keyboard.insertText('次の行');
    await page.keyboard.press('Enter');
    await editIdle(page);
    assert.match(readFileSync(file, 'utf8'), /左\\\|右<br>次の行/);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('りんご', '左\\|右<br>次の行'));
  });

  await t.test('Tab で続けて入力した三つのセルを順に保存する', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    const sent = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) sent.push(request.postDataJSON());
    });
    for (const text of ['一つ目', '二つ目', '三つ目']) {
      await page.keyboard.insertText(text);
      await page.keyboard.press('Tab');
    }
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| りんご | 1 | 赤 |', '| 一つ目 | 二つ目 | 三つ目 |'));
    assert.equal(sent.length, 3);
    assert.ok(sent[1].render > sent[0].render && sent[2].render > sent[1].render);
  });

  await t.test('セルの DOM を変えず、リンクを動かさず、チェックボックスは切り替え、外のクリックで確定する', async (t) => {
    const { page, file } = await editablePage(t, browser,
      '| 列 |\n| --- |\n| [リンク](#先) |\n\n<table><tr><td>\n\n- [ ] 作業\n\n</td></tr></table>\n\n## 先\n');
    const cell = page.locator('#gp-content td').first();
    const before = await cell.evaluate((element) => element.outerHTML);
    await cell.locator('a').click();
    assert.equal(new URL(page.url()).hash, '');
    assert.equal(await cell.evaluate((element) => element.outerHTML), before);
    await page.keyboard.press('F2');
    assert.equal(await cell.evaluate((element) => element.outerHTML), before);
    await page.keyboard.press('Escape');
    // 生の HTML の表のセルの中のタスクは、編集モード中に押すと切り替わる（セルは選ばない）。
    await page.locator('#gp-content input').click();
    await editIdle(page);
    assert.equal(await page.locator('#gp-content input').isChecked(), true);
    assert.match(readFileSync(file, 'utf8'), /- \[x\] 作業/);
    await replaceCell(page, cell, '外を押す前');
    await cell.dblclick();
    await page.locator('#gp-edit-input').fill('外を押して確定');
    await page.locator('#gp-theme').selectOption('dark');
    await page.locator('#gp-controls label').first().click();
    await editIdle(page);
    assert.match(readFileSync(file, 'utf8'), /外を押して確定/);
    await page.emulateMedia({ media: 'print' });
    assert.equal(await page.locator('#gp-edit-layer').isVisible(), false);
  });

  await t.test('POST の応答と次の描画と表情報が揃うまで、次の確定を送らない', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    const initial = await page.evaluate(() => window.gpPreview.current().render);
    let releasePost, releaseTables;
    const postGate = new Promise((resolve) => { releasePost = resolve; });
    const tableGate = new Promise((resolve) => { releaseTables = resolve; });
    t.after(() => { releasePost(); releaseTables(); });
    let sent = 0, heldTables = false;
    await page.route('**/_/edit', async (route) => {
      sent++;
      if (sent !== 1) return route.continue();
      const response = await route.fetch();
      await postGate;
      await route.fulfill({ response });
    });
    await page.route('**/_/edit/tables?*', async (route) => {
      const response = await route.fetch();
      if (Number(new URL(route.request().url()).searchParams.get('render')) > initial && !heldTables) {
        heldTables = true;
        await tableGate;
      }
      await route.fulfill({ response });
    });
    await page.locator('#gp-content td').first().click();
    await page.keyboard.insertText('応答待ち');
    await page.keyboard.press('Tab');
    await page.keyboard.insertText('次の入力');
    await page.keyboard.press('Tab');
    await page.waitForFunction((initial) => window.gpPreview.current().render > initial, initial);
    assert.equal(sent, 1);
    const replied = page.waitForResponse((response) => new URL(response.url()).pathname.endsWith('/_/edit'));
    releasePost();
    await replied;
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert.equal(sent, 1);
    releaseTables();
    await editIdle(page);
    assert.equal(sent, 2);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| りんご | 1 | 赤 |', '| 応答待ち | 次の入力 | 赤 |'));
  });

  await t.test('断られた行の操作の後も並んだ送信を続け、外で変わった後は並んだ送信を捨てる', async (t) => {
    const { page, file, server } = await editablePage(t, browser, pipe);
    let releaseFirst, releaseSecond;
    const first = new Promise((resolve) => { releaseFirst = resolve; });
    const second = new Promise((resolve) => { releaseSecond = resolve; });
    t.after(() => { releaseFirst(); releaseSecond(); });
    let sent = 0;
    await page.route('**/_/edit', async (route) => {
      if (++sent !== 1) return route.continue();
      await first;
      await route.fulfill({ status: 422, contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'range', message: '試験で断った行の操作' } }) });
    });
    const cells = page.locator('#gp-content td');
    await cells.first().click();
    await (await menuItem(page, 'row-after')).click();
    await cells.nth(1).click();
    await page.keyboard.insertText('並んだ入力');
    await page.keyboard.press('Enter');
    assert.equal(sent, 1);
    releaseFirst();
    await editIdle(page);
    assert.equal(sent, 2);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| りんご | 1 |', '| りんご | 並んだ入力 |'));
    assert.match(await page.locator('#gp-edit-status').textContent(), /試験で断った行の操作/);

    // 応答を待つ間に外で変わったら、並べた入力は、ずれたかもしれない位置へ書かずに下書きへ戻す。
    await page.unroute('**/_/edit');
    let held = false;
    await page.route('**/_/edit', async (route) => {
      if (held) return route.continue();
      held = true;
      const response = await route.fetch();
      await second;
      await route.fulfill({ response });
    });
    const before = await page.evaluate(() => window.gpPreview.current().render);
    await cells.first().click();
    await page.keyboard.insertText('先の入力');
    await page.keyboard.press('Tab');
    await page.keyboard.insertText('後の入力');
    await page.keyboard.press('Tab');
    await page.waitForFunction((before) => window.gpPreview.current().render > before, before);
    const external = readFileSync(file, 'utf8').replace('みかん', '外のみかん');
    assert.match(external, /先の入力/);
    writeFileSync(file, external);
    await server.setDocument({ path: file, text: external, rev: 30 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 30);
    releaseSecond();
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), external);
    // 捨てた入力は、同じ中身のままのセルを開くと戻る。
    await cells.nth(1).click();
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '後の入力');
    await page.keyboard.press('Escape');
  });

  await t.test('表の数やセルの結合の照合が失敗したら書き込まない', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    for (const mismatch of ['count', 'span']) {
      await page.locator('#gp-edit-toggle').click();
      await page.route('**/_/edit/tables?*', async (route) => {
        const response = await route.fetch();
        const data = await response.json();
        if (mismatch === 'count') data.tables.push(data.tables[0]);
        else data.tables[0].cells[0].colspan = 2;
        await route.fulfill({ response, json: data });
      });
      const loaded = page.waitForResponse((response) => response.url().includes('/_/edit/tables?'));
      await page.locator('#gp-edit-toggle').click();
      await loaded;
      await page.locator('#gp-content td').first().click();
      assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
      assert.equal(readFileSync(file, 'utf8'), pipe);
      await page.unroute('**/_/edit/tables?*');
    }
  });

  await t.test('HTML の結合セルと書式を編集し、矢印は結合セルを一つとして通る', async (t) => {
    const text = '<table><tbody><tr><td rowspan="2">結合</td><td>右上</td></tr><tr><td>右下</td></tr></tbody></table>\n';
    const { page, file } = await editablePage(t, browser, text);
    const first = page.locator('#gp-content td').first();
    await first.click();
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '右上');
    await page.keyboard.press('Escape');
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('ArrowLeft');
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '結合');
    await page.keyboard.press('Escape');
    await replaceCell(page, first, '**太字**');
    assert.match(readFileSync(file, 'utf8'), /\*\*太字\*\*/);
    assert.equal(await first.locator('strong').textContent(), '太字');
  });

  await t.test('Shift の矢印とクリックで範囲を伸縮し、起点のセルと本文を保つ', async (t) => {
    const { page } = await editablePage(t, browser, pipe);
    const cells = page.locator('#gp-content td'), input = page.locator('#gp-edit-input');
    await cells.first().click();
    const original = await cells.first().evaluate((element) => element.outerHTML);
    await page.keyboard.press('Shift+ArrowRight');
    await page.keyboard.press('Shift+ArrowDown');
    assert.equal(await input.getAttribute('aria-label'), '表のセルの編集（2 行 2 列を選択中）');
    await page.waitForFunction(() => !document.querySelector('#gp-edit-range-0').hidden);
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(resolve)));
    const boxes = await page.evaluate(() => {
      const first = document.querySelector('#gp-content td').getBoundingClientRect();
      const end = document.querySelectorAll('#gp-content td')[4].getBoundingClientRect();
      const range = document.querySelector('#gp-edit-range-0').getBoundingClientRect();
      const active = document.querySelector('#gp-edit-selection').getBoundingClientRect();
      return { range: [range.left, range.top, range.right, range.bottom],
        expected: [first.left, first.top, end.right, end.bottom], active: [active.left, active.top], origin: [first.left, first.top] };
    });
    boxes.range.forEach((value, i) => assert.ok(Math.abs(value - boxes.expected[i]) < 1, JSON.stringify(boxes)));
    assert.deepEqual(boxes.active, boxes.origin);
    assert.equal(await cells.first().evaluate((element) => element.outerHTML), original);
    await page.keyboard.press('Shift+ArrowLeft');
    assert.match(await input.getAttribute('aria-label'), /2 行 1 列/);
    assert.equal(await cells.last().evaluate((element) => {
      const event = new MouseEvent('mousedown', { bubbles: true, cancelable: true, shiftKey: true, button: 0 });
      element.dispatchEvent(event);
      return event.defaultPrevented;
    }), true);
    await cells.last().click({ modifiers: ['Shift'] });
    assert.match(await input.getAttribute('aria-label'), /3 行 3 列/);
    assert.equal(await page.evaluate(() => window.getSelection().toString()), '');
    for (const modifier of ['Control', 'Alt', 'Meta']) {
      await cells.nth(1).click({ modifiers: [modifier, 'Shift'] });
      assert.equal(await page.locator('#gp-edit-range').isVisible(), false);
      await page.keyboard.press('F2');
      assert.equal(await input.inputValue(), '1');
      await page.keyboard.press('Escape');
      await page.keyboard.press('Shift+ArrowDown');
    }
    // 入力中と別の表では、Shift を範囲の操作に使わない。
    await page.keyboard.press('F2');
    await cells.last().click({ modifiers: ['Shift'] });
    assert.equal(await page.locator('#gp-edit-range').isVisible(), false);
  });

  await t.test('範囲の Delete は全セルを一つの set で空にし、Backspace は一つのセルだけを空にする', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    const sent = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) sent.push(request.postDataJSON());
    });
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowRight');
    await page.keyboard.press('Shift+ArrowDown');
    await page.keyboard.press('Delete');
    await editIdle(page);
    assert.deepEqual(sent[0].op, { type: 'set', cells: [
      { row: 1, col: 0, text: '' }, { row: 1, col: 1, text: '' },
      { row: 2, col: 0, text: '' }, { row: 2, col: 1, text: '' },
    ] });
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| りんご | 1 |', '|  |  |').replace('| みかん | 2 |', '|  |  |'));
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /2 行 2 列/);
    await page.keyboard.press('Control+z');
    await editIdle(page);
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowRight');
    await page.keyboard.press('Backspace');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('りんご', ''));
    assert.equal(await page.locator('#gp-edit-range').isVisible(), false);
  });

  await t.test('行と列の選択は範囲の数で挿入・削除し、直交する Shift の矢印を無視する', async (t) => {
    const { page } = await editablePage(t, browser, pipe);
    const sent = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) sent.push(request.postDataJSON());
    });
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowDown');
    await (await menuItem(page, 'row-select')).click();
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /2 行 3 列/);
    await page.keyboard.press('Shift+ArrowRight');
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /2 行 3 列/);
    // 行の選択では、Shift+クリックでも列の位置を変えない。
    await page.locator('#gp-content td').nth(3).click({ modifiers: ['Shift'] });
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /2 行 3 列/);
    await (await menuItem(page, 'row-after')).click();
    await editIdle(page);
    assert.deepEqual(sent.at(-1).op, { type: 'insertRows', row: 2, count: 2, side: 'after' });
    assert.equal(await page.locator('#gp-content tr').count(), 6);
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowRight');
    await (await menuItem(page, 'col-select')).click();
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /6 行 2 列/);
    await page.keyboard.press('Shift+ArrowDown');
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /6 行 2 列/);
    await (await menuItem(page, 'col-delete')).click();
    await editIdle(page);
    assert.deepEqual(sent.at(-1).op, { type: 'deleteColumns', col: 0, count: 2 });
    assert.equal(await page.locator('#gp-content th').count(), 1);
  });

  await t.test('HTML の行の複製は原文と属性を保ち、複製先を選び、一回の undo で戻る', async (t) => {
    const rows = ['  <tr class="一"><td style="color:red" data-x="値">甲</td><td>乙</td></tr>\n',
      '  <tr><td>丙</td><td>丁</td></tr>\n'];
    const text = '<table>\n<tbody>\n' + rows.join('') + '</tbody>\n</table>\n';
    const { page, file } = await editablePage(t, browser, text);
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowDown');
    await (await menuItem(page, 'row-select')).click();
    const reply = page.waitForResponse((response) => response.request().method() === 'POST' &&
      new URL(response.url()).pathname.endsWith('/_/edit'));
    await (await menuItem(page, 'row-duplicate')).click();
    assert.equal((await reply).status(), 200, 'サーバーが duplicateRows を受け付ける');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), '<table>\n<tbody>\n' + rows.join('').repeat(2) + '</tbody>\n</table>\n');
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /2 行 2 列/);
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '甲');
    await page.keyboard.press('Escape');
    await page.keyboard.press('Control+z');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), text);
  });

  await t.test('本物のサーバーで行列を複製して複製先を選び、op を一回だけ送る', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    const received = [];
    page.on('request', (request) => {
      if (request.method() !== 'POST' || !new URL(request.url()).pathname.endsWith('/_/edit')) return;
      const op = request.postDataJSON().op;
      if (op?.type.startsWith('duplicate')) received.push(op);
    });
    await page.locator('#gp-content td').first().click();
    await (await menuItem(page, 'row-duplicate')).click();
    await editIdle(page);
    assert.deepEqual(received, [{ type: 'duplicateRows', row: 1, count: 1 }]);
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /1 行 3 列/);
    assert.equal(await page.evaluate(() => {
      const selected = document.querySelector('#gp-edit-selection').getBoundingClientRect();
      const inserted = document.querySelectorAll('#gp-content tr')[2].cells[0].getBoundingClientRect();
      return selected.top === inserted.top && selected.left === inserted.left;
    }), true);
    await page.keyboard.press('Control+z');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe);
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowRight');
    await (await menuItem(page, 'col-select')).click();
    await (await menuItem(page, 'col-duplicate')).click();
    await editIdle(page);
    assert.deepEqual(received.at(-1), { type: 'duplicateColumns', col: 0, count: 2 });
    assert.equal(received.length, 2);
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /4 行 2 列/);
    assert.equal(await page.evaluate(() => {
      const selected = document.querySelector('#gp-edit-selection').getBoundingClientRect();
      const inserted = document.querySelector('#gp-content tr').cells[2].getBoundingClientRect();
      return selected.top === inserted.top && selected.left === inserted.left;
    }), true);
    await page.keyboard.press('Control+z');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe);
  });

  await t.test('結合を切る範囲は点線と理由を出し、複製と Delete を断る', async (t) => {
    const text = '<table><tr><td>左上</td><td rowspan="2">結合</td><td>右上</td></tr>' +
      '<tr><td>左下</td><td>右下</td></tr><tr><td>左末</td><td>中末</td><td>右末</td></tr></table>\n';
    const { page, file } = await editablePage(t, browser, text);
    await page.locator('#gp-content td').first().click();
    await page.locator('#gp-content tr').first().locator('td').last().click({ modifiers: ['Shift'] });
    assert.equal(await page.locator('#gp-edit-range').getAttribute('data-invalid'), '');
    assert.equal(await page.locator('#gp-edit-range-0').evaluate((element) => getComputedStyle(element).borderTopStyle), 'dotted');
    const duplicate = await menuItem(page, 'row-duplicate');
    assert.equal(await duplicate.getAttribute('aria-disabled'), 'true');
    const description = await duplicate.getAttribute('aria-describedby');
    assert.match(await page.locator('#' + description).textContent(), /結合セル.*選び直してください/);
    await duplicate.click({ force: true });
    assert.equal(await page.locator('#gp-edit-menu-rows').isVisible(), true);
    assert.match(await page.locator('#gp-edit-menu-rows-reason').textContent(), /結合/);
    assert.equal(await page.locator('#gp-edit-menu-rows').evaluate((element) => {
      const rect = element.getBoundingClientRect();
      return rect.top >= 8 && rect.bottom <= innerHeight - 8;
    }), true);
    assert.equal(await page.locator('#gp-edit-row-after').getAttribute('aria-disabled'), 'false');
    assert.equal(await page.locator('#gp-edit-row-delete').getAttribute('aria-disabled'), 'false');
    await page.keyboard.press('Escape');
    await page.locator('#gp-edit-input').focus();
    await page.keyboard.press('Delete');
    assert.match(await page.locator('#gp-edit-status').textContent(), /結合/);
    assert.equal(readFileSync(file, 'utf8'), text);
  });

  await t.test('パイプの見出しの複製は見出し直下のデータ行になり、上への挿入は無効になる', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content th').first().click();
    const duplicate = await menuItem(page, 'row-duplicate');
    assert.equal(await page.locator('#gp-edit-row-before').getAttribute('aria-disabled'), 'true');
    assert.match(await page.locator('#gp-edit-row-before').getAttribute('title'), /見出し行の上/);
    assert.equal(await duplicate.getAttribute('aria-disabled'), 'false');
    const reply = page.waitForResponse((response) => response.request().method() === 'POST' &&
      new URL(response.url()).pathname.endsWith('/_/edit'));
    await duplicate.click();
    assert.equal((await reply).status(), 200, 'サーバーが duplicateRows を受け付ける');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| --- | --- | --- |\n', '| --- | --- | --- |\n| 名前 | 数 | 備考 |\n'));
    assert.equal(await page.locator('#gp-content th').count(), 3);
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /1 行 3 列/);
    await page.keyboard.press('Control+z');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe);
  });

  await t.test('検索で隠れた行をまたぐ範囲は削除も複製も断る', async (t) => {
    const text = '| 名前 | 値 |\n| --- | --- |\n| 一致 上 | 1 |\n| 隠す | 2 |\n| 一致 下 | 3 |\n';
    const { page, file } = await editablePage(t, browser, text);
    await page.locator('#gp-sticky-toggle').click();
    await page.locator('[data-md-table-sticky-action="search"]').click();
    await page.locator('[data-md-table-sticky-search-input]').fill('一致');
    await page.locator('[data-md-table-sticky-action="search-filter"]').check();
    await page.waitForFunction(() => getComputedStyle(document.querySelectorAll('#gp-content tr')[2]).display === 'none');
    await page.locator('#gp-content td').first().click();
    await page.locator('#gp-content td').last().click({ modifiers: ['Shift'] });
    const duplicate = await menuItem(page, 'row-duplicate');
    assert.equal(await duplicate.getAttribute('aria-disabled'), 'true');
    assert.equal(await page.locator('#gp-edit-row-delete').getAttribute('aria-disabled'), 'true');
    await duplicate.focus();
    assert.match(await page.locator('#gp-edit-menu-rows-reason').textContent(), /検索.*解除/);
    await duplicate.click({ force: true });
    assert.equal(readFileSync(file, 'utf8'), text);
    await page.keyboard.press('Escape');
    await page.locator('#gp-edit-input').focus();
    await page.keyboard.press('Delete');
    assert.match(await page.locator('#gp-edit-status').textContent(), /検索/);
    assert.equal(readFileSync(file, 'utf8'), text);
  });

  await t.test('右クリックとキーボードで同じ行列の子のメニューを開き、無効理由を読める', async (t) => {
    const { page } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click({ button: 'right' });
    const cellMenu = page.locator('#gp-edit-menu-cell'), rows = page.locator('#gp-edit-menu-rows'), cols = page.locator('#gp-edit-menu-cols');
    assert.equal(await cellMenu.getAttribute('role'), 'menu');
    assert.equal(await page.locator('#gp-edit-actions > button').count(), 5);
    assert.equal(await page.locator('#gp-edit-actions [role="menu"]').count(), 0);
    assert.equal(await page.locator('#gp-edit-layer > [role="menu"]').count(), 3);
    assert.equal(await page.locator('#gp-edit-cell-rows').getAttribute('role'), 'menuitem');
    for (let i = 0; i < 4; i++) await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');
    assert.equal(await cellMenu.isVisible(), true);
    assert.equal(await cols.isVisible(), true);
    assert.equal(await page.locator('#gp-edit-cell-cols').getAttribute('aria-expanded'), 'true');
    await page.keyboard.press('ArrowLeft');
    assert.equal(await cols.isVisible(), false);
    assert.equal(await page.locator('#gp-edit-cell-cols').evaluate((element) => element === document.activeElement), true);
    await page.locator('#gp-edit-cell-rows').focus();
    await page.keyboard.press('ArrowRight');
    assert.equal(await rows.isVisible(), true);
    await page.locator('#gp-edit-row-select').focus();
    await page.keyboard.press('Space');
    assert.equal(await cellMenu.isVisible(), false);
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /1 行 3 列/);
    await page.keyboard.press('Shift+F10');
    await page.keyboard.press('End');
    await page.keyboard.press('Enter');
    assert.equal(await cellMenu.isVisible(), true);
    assert.equal(await page.locator('#gp-edit-cell-source').getAttribute('aria-disabled'), 'true');
    assert.match(await page.locator('#gp-edit-menu-cell-reason').textContent(), /HTML/);
    await page.keyboard.press('Escape');
    await page.keyboard.press('ContextMenu');
    assert.equal(await cellMenu.isVisible(), true);
    await page.keyboard.press('Shift+Tab');
    assert.equal(await cellMenu.isVisible(), false);
    assert.equal(await page.locator('#gp-edit-input').evaluate((element) => element === document.activeElement), true);
    await page.locator('#gp-edit-rows').focus();
    await page.keyboard.press('ArrowUp');
    assert.equal(await page.locator('#gp-edit-row-paste-after').evaluate((element) => element === document.activeElement), true);
    await page.keyboard.press('ArrowDown');
    assert.equal(await page.locator('#gp-edit-row-before').evaluate((element) => element === document.activeElement), true);
    await page.keyboard.press('ArrowUp');
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-rows').evaluate((element) => element === document.activeElement), true);
    await page.locator('#gp-edit-input').focus();
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').evaluate((element) => {
      const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true });
      element.dispatchEvent(event);
      return event.defaultPrevented;
    }), false);
    await page.keyboard.press('Escape');
    const blocked = await editablePage(t, browser, '```json:table\n{"markdown":true,"fields":[{"key":"a","label":"列"}],"items":[{"a":"値"}]}\n```\n');
    assert.equal(await blocked.page.locator('#gp-content td').evaluate((element) => {
      const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true });
      element.dispatchEvent(event);
      return event.defaultPrevented;
    }), false);
    assert.equal(await blocked.page.locator('#gp-edit-menu-cell').isVisible(), false);
  });

  await t.test('固定行列と横スクロールでは範囲を四つに分け、Esc はメニュー・範囲・全幅の順に解く', async (t) => {
    const text = '| 列一 | 列二 | 列三 |\n| --- | --- | --- |\n' +
      Array.from({ length: 16 }, (_, row) => `| ${row} ${'長い値'.repeat(15)} | 二 | 三 |`).join('\n') + '\n';
    const { page } = await editablePage(t, browser, text);
    await page.setViewportSize({ width: 700, height: 460 });
    await page.locator('#gp-sticky-toggle').click();
    await page.locator('[data-md-table-sticky-action="rows-plus"]').click();
    await page.locator('[data-md-table-sticky-action="cols-plus"]').click();
    await page.locator('[data-md-table-sticky-action="float"]').click();
    const panel = page.locator('[data-md-table-sticky-float-panel]');
    await panel.locator('th').first().click();
    await panel.locator('td').last().click({ modifiers: ['Shift'] });
    await page.waitForFunction(() => Array.from(document.querySelector('#gp-edit-range').children).filter((element) => !element.hidden).length === 4);
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(resolve)));
    assert.equal(await page.locator('#gp-edit-range .gp-edit-range:not([hidden])').evaluateAll((elements) =>
      elements.every((element) => getComputedStyle(element).pointerEvents === 'none' && element.style.clipPath.startsWith('inset('))), true);
    await page.locator('#gp-edit-input').evaluate((element) => element.dispatchEvent(new KeyboardEvent('keydown',
      { bubbles: true, key: 'Escape', keyCode: 229, isComposing: true })));
    assert.equal(await panel.count(), 1);
    assert.equal(await page.locator('#gp-edit-range').isVisible(), true);
    await copied(page, 'Control+x');
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), true);
    await page.keyboard.press('Shift+F10');
    await page.locator('#gp-edit-cell-rows').focus();
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-menu-cell').isVisible(), true);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-range').isVisible(), true);
    assert.equal(await panel.count(), 1);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-range').isVisible(), true);
    assert.equal(await panel.count(), 1);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-range').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), true);
    assert.equal(await panel.count(), 1);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
    assert.equal(await panel.count(), 0);
  });

  await t.test('構造変更の応答待ちで確定した入力を送らず下書きへ戻す', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    let sent = 0;
    await page.route('**/_/edit', async (route) => {
      sent++;
      if (sent !== 1) return route.continue();
      const response = await route.fetch();
      await gate;
      await route.fulfill({ response });
    });
    const initial = await page.evaluate(() => window.gpPreview.current().render);
    await page.locator('#gp-content td').first().click();
    await (await menuItem(page, 'row-after')).click();
    await page.locator('#gp-content td').nth(1).click();
    await page.keyboard.insertText('構造待ちの入力');
    await page.keyboard.press('Tab');
    await page.waitForFunction((initial) => window.gpPreview.current().render > initial, initial);
    release();
    await editIdle(page);
    assert.equal(sent, 1);
    assert.equal(readFileSync(file, 'utf8').includes('構造待ちの入力'), false);
    assert.match(await page.locator('#gp-edit-status').textContent(), /行・列が変わった.*下書き/);
    await page.locator('#gp-content td').nth(1).click();
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '構造待ちの入力');
    await page.keyboard.press('Escape');
  });

  await t.test('外の変更と 409 では範囲を捨て、メニューも閉じる', async (t) => {
    const { page, file, server } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowRight');
    await page.keyboard.press('Shift+F10');
    writeFileSync(file, pipe.replace('ぶどう', '外のぶどう'));
    await server.setDocument({ path: file, text: readFileSync(file, 'utf8'), rev: 40 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 40);
    await editIdle(page);
    assert.equal(await page.locator('#gp-edit-range').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-menu-cell').isVisible(), false);
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowDown');
    await page.route('**/_/edit', (route) => route.fulfill({ status: 409, contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'changed', message: '試験の競合' } }) }));
    await page.keyboard.press('Delete');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    await editIdle(page);
    assert.equal(await page.locator('#gp-edit-range').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), true);
  });

  await t.test('六つの行列操作と見出し行・rowsLocked の制限', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content th').first().click();
    await page.locator('#gp-edit-rows').click();
    assert.equal(await page.locator('#gp-edit-row-before').getAttribute('aria-disabled'), 'true');
    assert.equal(await page.locator('#gp-edit-row-delete').getAttribute('aria-disabled'), 'true');
    await page.locator('#gp-content td').first().click();
    for (const [action, rows, cols] of [['row-before', 5, 3], ['row-after', 6, 3], ['row-delete', 5, 3],
      ['col-before', 5, 4], ['col-after', 5, 5], ['col-delete', 5, 4]]) {
      await (await menuItem(page, action)).click();
      await editIdle(page);
      assert.equal(await page.locator('#gp-content tr').count(), rows);
      assert.equal(await page.locator('#gp-content th').count(), cols);
      assert.equal(await page.locator('#gp-edit-selection').isVisible(), true);
      assert.notEqual(readFileSync(file, 'utf8'), pipe);
    }
    const locked = await editablePage(t, browser,
      '<table><tbody><tr><td>上</td></tr></tbody><thead><tr><th>下</th></tr></thead></table>\n');
    await locked.page.locator('#gp-content td').first().click();
    await locked.page.locator('#gp-edit-rows').click();
    assert.equal(await locked.page.locator('#gp-edit-row-after').getAttribute('aria-disabled'), 'true');
    assert.ok(await locked.page.locator('#gp-edit-row-after').getAttribute('title'));
  });

  await t.test('選んだセルをヘッダーと操作欄の後ろへ入れず、狭いセルの入力欄を広げる', async (t) => {
    const rows = Array.from({ length: 60 }, (_, i) => `| ${i + 1} | 品目 ${i + 1} |`).join('\n');
    const { page } = await editablePage(t, browser, '| 番 | 名前 |\n| --- | --- |\n' + rows + '\n');
    await page.setViewportSize({ width: 900, height: 500 });
    const place = () => page.evaluate(() => {
      const frame = document.querySelector('#gp-edit-selection').getBoundingClientRect();
      return { top: frame.top, bottom: frame.bottom, header: document.querySelector('#gp-header').getBoundingClientRect().bottom,
        bar: document.querySelector('#gp-edit-actions').getBoundingClientRect().top };
    });
    const settle = () => page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await page.locator('#gp-content td').first().click();
    for (let i = 0; i < 40; i++) await page.keyboard.press('ArrowDown');
    await settle();
    let at = await place();
    assert.ok(at.bottom <= at.bar + 1 && at.top >= at.header - 1, JSON.stringify(at));
    for (let i = 0; i < 35; i++) await page.keyboard.press('ArrowUp');
    await settle();
    at = await place();
    assert.ok(at.top >= at.header - 1 && at.bottom <= at.bar + 1, JSON.stringify(at));
    // 余白は動かす間だけ付け、ページに残さない（見出しへの移動の位置を変えない）。
    assert.equal(await page.evaluate(() => document.documentElement.style.scrollPaddingTop + document.documentElement.style.scrollPaddingBottom), '');
    await page.keyboard.press('F2');
    await settle();
    const size = await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input');
      return { input: input.getBoundingClientRect().width, cell: document.querySelector('#gp-edit-selection').getBoundingClientRect().width,
        em: parseFloat(getComputedStyle(input).fontSize) };
    });
    assert.ok(size.cell < size.em * 10 && size.input >= size.em * 10 - 1, JSON.stringify(size));
    await page.keyboard.press('Escape');
    await page.locator('#gp-edit-toggle').click();
    assert.equal(await page.evaluate(() => document.documentElement.style.getPropertyValue('--gp-edit-bottom')), '');
  });

  await t.test('引用符内の改行とタブを貼り付け、範囲外ではファイルを変えない', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    await pasteText(page, '"一行目\n二行目"\t"引用""符"\n次\t"中\tのタブ"\n');
    await editIdle(page);
    const saved = readFileSync(file, 'utf8');
    assert.match(saved, /一行目<br>二行目/);
    assert.match(saved, /引用"符/);
    assert.match(saved, /中\tのタブ/);
    await page.locator('#gp-content td').last().click();
    await pasteText(page, '外\t側');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').dataset.kind === 'error');
    assert.equal(readFileSync(file, 'utf8'), saved);
    // 知らせには、決まった文ではなくサーバーの理由が出る。
    const reason = await page.locator('#gp-edit-status').textContent();
    assert.ok(reason && !reason.includes('この操作では書き込めませんでした'), reason);
    // 文字の無い貼り付けでは、編集を始めない。
    await pasteText(page, '');
    assert.equal(await page.locator('#gp-edit-layer').getAttribute('data-typing'), 'off');
    // 選択中のコピーは、セル用 Markdown を渡す。
    await page.locator('#gp-content td').first().click();
    assert.equal(await page.evaluate(() => {
      const data = new DataTransfer();
      const event = new ClipboardEvent('copy', { bubbles: true, cancelable: true, clipboardData: data });
      document.querySelector('#gp-edit-input').dispatchEvent(event);
      return [event.defaultPrevented, data.getData('text/plain')].join('|');
    }), 'true|"一行目\n二行目"');
  });

  await t.test('HTML の原文、422 からの再入力、raw セル', async (t) => {
    const { page, file } = await editablePage(t, browser, '<table><tr><td>元の文字</td><td><div>複雑</div></td></tr></table>\n');
    await page.locator('#gp-content td').first().click();
    await page.locator('#gp-edit-source').click();
    assert.equal(await page.locator('#gp-edit-source-label').isVisible(), true);
    await page.locator('#gp-edit-input').fill('<em>原文の変更</em>');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.match(readFileSync(file, 'utf8'), /<em>原文の変更<\/em>/);
    await page.locator('#gp-edit-source').click();
    await page.locator('#gp-edit-input').fill('x<y');
    await page.keyboard.press('Control+Enter');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').dataset.kind === 'error');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), 'x<y');
    assert.equal(await page.locator('#gp-edit-layer').getAttribute('data-typing'), 'on');
    assert.match(readFileSync(file, 'utf8'), /<em>原文の変更<\/em>/);
    const reason = await page.locator('#gp-edit-status').textContent();
    assert.ok(reason && !reason.includes('この操作では書き込めませんでした'), reason);
    await page.keyboard.press('Escape');
    // 断られた入力をやめた後は、次の編集がそのまま送られる（書式のあるセルは原文の編集になる）。
    await page.keyboard.press('F2');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '<em>原文の変更</em>');
    await page.locator('#gp-edit-input').fill('断られた後の編集');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.match(readFileSync(file, 'utf8'), /<td>断られた後の編集<\/td>/);
    await page.locator('#gp-content td').last().dblclick();
    assert.equal(await page.locator('#gp-edit-source-label').isVisible(), true);
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '<div>複雑</div>');
  });

  await t.test('Ctrl+Z と Ctrl+Y でファイルを取り消し・やり直しする', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await replaceCell(page, page.locator('#gp-content td').first(), '変更');
    const changed = readFileSync(file, 'utf8');
    await page.keyboard.press('Control+z');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe);
    await page.keyboard.press('Control+y');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), changed);
  });

  await t.test('合成した IME の最初の文字と変換中の Enter、stop と再開', async (t) => {
    const { page, file, server } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input');
      input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
      input.value = 'に';
      input.dispatchEvent(new InputEvent('input', { bubbles: true, data: 'に', isComposing: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Enter', keyCode: 229, isComposing: true }));
    });
    assert.equal(readFileSync(file, 'utf8'), pipe);
    assert.equal(await page.locator('#gp-edit-input').inputValue(), 'に');
    await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input');
      input.value = '日本語';
      input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '日本語' }));
      input.dispatchEvent(new InputEvent('input', { bubbles: true, data: '日本語' }));
    });
    await page.keyboard.press('Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('りんご', '日本語'));
    await page.keyboard.press('F2');
    await server.broadcast('stop', { reason: 'pause' });
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').hidden);
    assert.equal(await page.locator('html').getAttribute('data-edit'), 'on');
    await server.setDocument({ path: file, text: readFileSync(file, 'utf8'), rev: 9 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 9);
    await editIdle(page);
    await page.locator('#gp-content td').first().click();
    assert.equal(await page.locator('#gp-edit-input').evaluate((el) => document.activeElement === el), true);
  });

  await t.test('固定・検索・全幅表示を保ち、入力を最前面に置き、Esc を二段階で扱う', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForSelector('[data-md-table-sticky-bar]');
    await page.locator('[data-md-table-sticky-action="cols-plus"]').click();
    await page.locator('[data-md-table-sticky-action="search"]').click();
    await page.locator('[data-md-table-sticky-search-input]').fill('りんご');
    await page.locator('[data-md-table-sticky-action="float"]').click();
    await page.waitForSelector('[data-md-table-sticky-float-panel]');
    await page.evaluate(() => {
      window.editStickyTable = window.gpPreview.tables()[0];
      window.editStickyRecord = window.__mdTableSticky.records.get(window.editStickyTable);
      window.editStickyFloating = window.__mdTableSticky.floating;
      window.editStickyCols = window.editStickyRecord.override.cols;
    });
    const cell = page.locator('[data-md-table-sticky-float-panel] td').first();
    await cell.dblclick();
    await page.waitForFunction(() => {
      const input = document.querySelector('#gp-edit-input'), rect = input.getBoundingClientRect();
      return document.elementFromPoint(rect.left + 5, rect.top + 5) === input;
    });
    await page.locator('#gp-edit-input').fill('全幅で変更');
    await page.keyboard.press('Enter');
    await editIdle(page);
    assert.match(readFileSync(file, 'utf8'), /全幅で変更/);
    assert.equal(await cell.textContent(), '全幅で変更');
    assert.equal(await page.evaluate(() => window.editStickyTable === window.gpPreview.tables()[0] &&
      window.editStickyRecord === window.__mdTableSticky.records.get(window.editStickyTable) &&
      window.editStickyFloating === window.__mdTableSticky.floating &&
      window.editStickyCols === window.editStickyRecord.override.cols), true);
    assert.equal(await page.locator('[data-md-table-sticky-search-input]').inputValue(), 'りんご');
    await cell.dblclick();
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('[data-md-table-sticky-float-panel]').count(), 1);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('[data-md-table-sticky-float-panel]').count(), 0);
    await page.locator('[data-md-table-sticky-action="pick"]').click();
    await page.locator('#gp-content td').first().click();
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
  });

  await t.test('JSON と入れ子の表は選べず、外の変更を上書きしない', async (t) => {
    const text = '```json:table\n{"markdown":true,"fields":[{"key":"a","label":"列"}],"items":[{"a":"値"}]}\n```\n\n<table><tr><td><table><tr><td>内側</td></tr></table></td></tr></table>\n';
    const { page } = await editablePage(t, browser, text);
    await page.locator('#gp-content table').first().locator('td').first().click();
    assert.match(await page.locator('#gp-edit-status').textContent(), /JSON/);
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
    await page.locator('#gp-content table table td').click();
    assert.match(await page.locator('#gp-edit-status').textContent(), /入れ子|対応/);
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
    const other = await editablePage(t, browser, pipe);
    await other.page.locator('#gp-content td').first().dblclick();
    await other.page.locator('#gp-edit-input').fill('画面の変更');
    const external = pipe.replace('りんご', '外の変更');
    writeFileSync(other.file, external);
    await other.page.keyboard.press('Enter');
    await other.page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    assert.equal(readFileSync(other.file, 'utf8'), external);
    await other.server.setDocument({ path: other.file, text: external, rev: 20 });
    await other.page.waitForFunction(() => window.gpPreview.current().rev === 20);
    await editIdle(other.page);
    await other.page.locator('#gp-content td').first().dblclick();
    await other.page.locator('#gp-edit-input').fill('再描画前の入力');
    const refreshed = external.replace('外の変更', '外で再び変更');
    writeFileSync(other.file, refreshed);
    await other.server.setDocument({ path: other.file, text: refreshed, rev: 21 });
    await other.page.waitForFunction(() => window.gpPreview.current().rev === 21);
    await other.page.keyboard.press('Enter');
    await other.page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    assert.equal(readFileSync(other.file, 'utf8'), refreshed);
  });

  await t.test('セルと誤りに含まれる HTML を UI から要素にしない', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await replaceCell(page, page.locator('#gp-content td').first(), '`<img src=x onerror=alert(1)>`');
    assert.match(readFileSync(file, 'utf8'), /<img src=x onerror=alert\(1\)>/);
    assert.equal(await page.locator('#gp-content td img, #gp-edit-layer img').count(), 0);
    let refused = 0;
    await page.route('**/_/edit', (route) => {
      refused++;
      return route.fulfill({ status: 422, contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'invalid', message: '<img src=x onerror=alert(1)>' } }) });
    });
    await page.locator('#gp-content td').first().click();
    await page.keyboard.insertText('送信する文字');
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('<img'));
    assert.equal(await page.locator('#gp-edit-status img').count(), 0);
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '送信する文字');
    // 編集モードを出ると、開いていた入力をもう一度送る。その外で断られた入力は開き直さず、後の送信も止めない。
    await page.locator('#gp-edit-toggle').click();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').getAttribute('aria-busy') === 'false');
    assert.equal(refused, 2);
    assert.equal(await page.locator('#gp-edit-layer').getAttribute('data-typing'), 'off');
    await page.unroute('**/_/edit');
    await page.locator('#gp-edit-toggle').click();
    await page.waitForFunction(() => Number(document.querySelector('#gp-edit-layer').dataset.render) > 0);
    await replaceCell(page, page.locator('#gp-content td').nth(1), '次の送信');
    assert.match(readFileSync(file, 'utf8'), /次の送信/);
  });
});

test('外側のブロックへ移り、下書きと操作欄の位置を保って編集する', { skip, timeout: 120000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const valueIs = (page, value) => page.waitForFunction((value) => {
    const input = document.querySelector('#gp-edit-input');
    return !input.hidden && input.value === value && document.querySelector('#gp-edit-layer').dataset.typing === 'on';
  }, value);

  for (const method of ['ボタン', 'キー']) {
    await t.test('項目からリスト全体を開き、確定する/' + method, async (t) => {
      const original = '前。\n\n- a\n- b\n\n後。\n';
      const { page, file } = await editablePage(t, browser, original);
      await page.locator('#gp-content li').first().dblclick();
      await valueIs(page, '- a');
      await page.locator('#gp-edit-input').fill('- 項目の下書き');
      if (method === 'ボタン') await page.getByRole('button', { name: 'リスト全体を編集', exact: true }).click();
      else await page.keyboard.press('Alt+ArrowUp');
      await valueIs(page, '- a\n- b');
      await page.locator('#gp-edit-input').fill('- 外側の下書き\n- b');
      if (method === 'ボタン') await page.getByRole('button', { name: '内側に戻る', exact: true }).click();
      else await page.keyboard.press('Alt+ArrowDown');
      await valueIs(page, '- 項目の下書き');
      assert.equal(readFileSync(file, 'utf8'), original, '移動だけでは確定しない');
      await page.keyboard.press('Alt+ArrowUp');
      await valueIs(page, '- 外側の下書き\n- b');
      await page.locator('#gp-edit-input').fill('- changed\n- c');
      await page.keyboard.press('Control+Enter');
      await editIdle(page);
      assert.equal(readFileSync(file, 'utf8'), '前。\n\n- changed\n- c\n\n後。\n');
      assert.equal(await page.locator('#gp-edit-block-actions').isVisible(), false);
    });
  }

  await t.test('何段でも内側へ戻り、変換中の Alt キーでは移らない', async (t) => {
    const original = '> - parent\n>   - child\n';
    const { page, file } = await editablePage(t, browser, original);
    await page.locator('#gp-content li li').dblclick();
    await valueIs(page, '- child');
    const sources = [];
    page.on('request', (r) => { if (r.url().includes('/_/edit/source?')) sources.push(r.url()); });
    await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input');
      input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowUp', altKey: true }));
      input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowUp', altKey: true, isComposing: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowUp', altKey: true, keyCode: 229 }));
    });
    assert.deepEqual(sources, []);
    await valueIs(page, '- child');
    // 項目が 1 つだけのリストは項目と同じ原文なので、外側には数えない。子の項目の次は親の項目、その次は引用。
    assert.equal(await page.locator('#gp-edit-outer').textContent(), '項目全体を編集');
    await page.keyboard.press('Alt+ArrowUp');
    await valueIs(page, '- parent\n>   - child');
    assert.equal(await page.locator('#gp-edit-outer').textContent(), '引用全体を編集');
    await page.keyboard.press('Alt+ArrowUp');
    await valueIs(page, '> - parent\n>   - child');
    assert.equal(await page.locator('#gp-edit-outer').isVisible(), false);
    await page.keyboard.press('Alt+ArrowDown');
    await valueIs(page, '- parent\n>   - child');
    await page.keyboard.press('Alt+ArrowDown');
    await page.waitForFunction(() => document.querySelector('#gp-edit-inner').hidden);
    await valueIs(page, '- child');
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.keyboard.press('Escape');
  });

  await t.test('注記の題名から種類・題名・閉じの行を含む全体を開く', async (t) => {
    const original = '前。\n\n>>> [!note] 題\nbody\n>>>\n\n後。\n';
    const { page, file } = await editablePage(t, browser, original);
    await page.locator('#gp-content .markdown-alert-title').dblclick();
    await valueIs(page, '>>> [!note] 題\nbody\n>>>');
    await page.locator('#gp-edit-input').fill('>>> [!tip] 新しい題\nchanged\n>>>');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('>>> [!note] 題\nbody', '>>> [!tip] 新しい題\nchanged'));
  });

  await t.test('範囲を断った項目はリスト全体を開いて理由を知らせる', async (t) => {
    const original = '>>>\n- ab cd\n- ef gh\n>>>\n';
    const { page, file } = await editablePage(t, browser, original);
    await page.locator('#gp-content li').last().dblclick();
    await valueIs(page, '- ab cd\n- ef gh');
    assert.equal(await page.locator('#gp-edit-status').textContent(), 'このブロックだけの範囲を確かめられないので、外側のブロックを開きました');
    assert.equal(await page.getByRole('button', { name: '引用全体を編集', exact: true }).isVisible(), true);
    await page.locator('#gp-edit-input').fill('- changed');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), '>>>\n- changed\n>>>\n');
  });

  await t.test('用語の範囲を断った応答は説明リストを開いて理由を知らせる', async (t) => {
    const original = 'Apple\n: red\n\n後。\n';
    const { page, file } = await editablePage(t, browser, original);
    const range = await page.locator('#gp-content dl').getAttribute('data-sourcepos');
    // エンジンの版で用語の位置が変わっても、term の応答からの移動を確かめる。
    await page.route('**/_/edit/source?*', (route) => {
      if (new URL(route.request().url()).searchParams.get('tag') !== 'dt') return route.continue();
      return route.fulfill({ status: 422, json: { error: { code: 'term', message: '用語の範囲を決められません。' },
        outer: [{ tag: 'dl', range, kind: 'deflist' }] } });
    });
    await page.locator('#gp-content dt').dblclick();
    await valueIs(page, 'Apple\n: red');
    assert.equal(await page.locator('#gp-edit-status').textContent(), 'この用語だけの範囲を決められないので、説明リスト全体を開きました');
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.keyboard.press('Escape');
  });

  for (const colorScheme of ['light', 'dark']) {
    for (const placement of ['上', '下']) {
      await t.test('操作欄が入力欄・ヘッダー・画面の外に重ならない/' + colorScheme + '/' + placement, async (t) => {
        const original = (placement === '上' ? '# 題\n\n前の段落。\n\n' : '') + '- a\n- b\n';
        const { page } = await editablePage(t, browser, original);
        await page.emulateMedia({ colorScheme });
        if (placement === '下') {
          // 最上部のリストの直上をヘッダーへ寄せ、下へ置く経路も確かめる。
          await page.evaluate(() => {
            document.querySelector('#gp-content').style.paddingTop = '0px';
            document.querySelector('#gp-content ul').style.marginTop = '0px';
          });
        }
        await page.locator('#gp-content li').first().dblclick();
        await valueIs(page, '- a');
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
        const geometry = await page.evaluate(() => {
          const box = (selector) => {
            const r = document.querySelector(selector).getBoundingClientRect();
            return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
          };
          return { toolbar: box('#gp-edit-block-actions'), input: box('#gp-edit-input'),
            header: box('#gp-header').bottom, width: innerWidth, height: innerHeight };
        });
        const { toolbar, input, header, width, height } = geometry;
        assert.ok(toolbar.bottom <= input.top || toolbar.top >= input.bottom, JSON.stringify(geometry));
        assert.ok(toolbar.top >= header && toolbar.left >= 0 && toolbar.right <= width && toolbar.bottom <= height, JSON.stringify(geometry));
        assert.ok(placement === '上' ? toolbar.bottom <= input.top : toolbar.top >= input.bottom, JSON.stringify(geometry));
        await page.keyboard.press('Escape');
      });
    }
  }
});

test('脚注の定義を画面で編集し、ラベルの注意とリンクの操作を確かめる', { skip, timeout: 120000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const definitionSelector = '#gp-content section[data-footnotes] > ol > li[data-sourcepos]';
  const open = async (page, target, source) => {
    await target.dblclick();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), source);
    assert.equal(await page.locator('#gp-edit-status').textContent(), 'ラベルを変えると、本文の参照と対応しなくなります');
    assert.equal(await page.locator('#gp-edit-status').getAttribute('data-kind'), 'info');
    assert.equal(await page.locator('#gp-edit-outer').isVisible(), false);
  };

  for (const [name, source] of [
    ['一行', '[^a]: 脚注の定義'],
    ['続きの行', '[^a]: 脚注の定義\n    続きの行'],
    ['二つの段落', '[^a]: 脚注の定義\n\n    次の段落'],
  ]) {
    for (const ending of ['\n', '\r\n']) {
      await t.test(name + '/' + JSON.stringify(ending), async (t) => {
        const original = ('本文[^a]\n\n' + source + '\n\n後の段落\n').replaceAll('\n', ending);
        const { page, file } = await editablePage(t, browser, original);
        const definition = page.locator(definitionSelector);
        const requests = [];
        page.on('request', (request) => {
          if (request.url().includes('/_/edit/source?')) requests.push(new URL(request.url()));
        });
        await definition.locator('p').last().hover();
        assert.equal(await definition.evaluate((element) => getComputedStyle(element).outlineStyle), 'dashed');
        assert.equal(await definition.locator('p').last().evaluate((element) => getComputedStyle(element).outlineStyle), 'none');
        await open(page, definition.locator('p').last(), source);
        assert.equal(requests.at(-1).searchParams.get('tag'), 'li');
        assert.equal(requests.at(-1).searchParams.get('range'), await definition.getAttribute('data-sourcepos'));
        const changed = source.replace('脚注の定義', '変更した定義');
        await page.locator('#gp-edit-input').fill(changed);
        const responsePromise = page.waitForResponse((response) => response.request().method() === 'POST' &&
          new URL(response.url()).pathname.endsWith('/_/edit'));
        await page.keyboard.press('Control+Enter');
        assert.deepEqual((await (await responsePromise).json()).warnings, []);
        await editIdle(page);
        assert.equal(readFileSync(file, 'utf8'), original.replace('脚注の定義', '変更した定義'));
        assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
      });
    }
  }

  await t.test('ラベルを変えても確定し、範囲の編集の応答から注意を表示する', async (t) => {
    const original = '本文[^a]\n\n[^a]: 脚注の定義\n\n後\n';
    const { page, file } = await editablePage(t, browser, original);
    await open(page, page.locator(definitionSelector).locator('p'), '[^a]: 脚注の定義');
    await page.locator('#gp-edit-input').fill('[^b]: ラベルの変更');
    await page.keyboard.press('Control+Enter');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent ===
      '脚注のラベルが変わりました。本文の参照を確かめてください。');
    await editIdle(page);
    assert.equal(await page.locator('#gp-edit-status').getAttribute('data-kind'), 'info');
    assert.equal(readFileSync(file, 'utf8'), '本文[^a]\n\n[^b]: ラベルの変更\n\n後\n');
  });

  await t.test('編集モードの脚注のリンクは一回のクリックで移動せず、モードの外では戻りのリンクで移動する', async (t) => {
    const original = '本文[^a]\n\n[^a]: [定義のリンク](#末尾)\n\n## 末尾\n';
    const { page, file } = await editablePage(t, browser, original);
    const definition = page.locator(definitionSelector);
    const before = page.url();
    await page.evaluate(() => {
      window.footnoteScrolls = [];
      const scroll = Element.prototype.scrollIntoView;
      Element.prototype.scrollIntoView = function (options) {
        window.footnoteScrolls.push(this.id);
        return scroll.call(this, options);
      };
    });
    await definition.locator('a').first().click();
    assert.equal(page.url(), before);
    const backref = definition.locator('a.footnote-backref');
    await backref.click();
    assert.equal(page.url(), before);
    assert.deepEqual(await page.evaluate(() => window.footnoteScrolls), []);
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    await open(page, backref, '[^a]: [定義のリンク](#末尾)');
    await page.keyboard.press('Escape');
    await page.locator('#gp-edit-toggle').click();
    const target = await backref.evaluate((anchor) => {
      const id = decodeURIComponent(anchor.getAttribute('href').slice(1));
      return (document.getElementById('user-content-' + id) || document.getElementById(id)).id;
    });
    await page.evaluate(() => { window.footnoteScrolls = []; });
    await backref.click();
    assert.deepEqual(await page.evaluate(() => window.footnoteScrolls), [target]);
    assert.equal(readFileSync(file, 'utf8'), original);
  });

  await t.test('脚注のタスクを切り替え、内側の項目では定義全体を開く', async (t) => {
    const source = '[^a]: 定義\n\n    - [ ] 脚注のタスク';
    const original = '本文[^a]\n\n' + source + '\n';
    const { page, file } = await editablePage(t, browser, original);
    const definition = page.locator(definitionSelector);
    const checkbox = definition.locator('input.task-list-item-checkbox');
    assert.equal(await checkbox.isDisabled(), false);
    await checkbox.click();
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('[ ]', '[x]'));
    const item = definition.locator('li');
    await item.hover();
    assert.equal(await definition.evaluate((element) => getComputedStyle(element).outlineStyle), 'dashed');
    assert.equal(await item.evaluate((element) => getComputedStyle(element).outlineStyle), 'none');
    await open(page, item, source.replace('[ ]', '[x]'));
    await page.keyboard.press('Escape');
  });
});

test('ブロックの原文とタスクを画面で編集し、ファイルの内容まで確かめる', { skip, timeout: 120000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const open = async (page, block, original) => {
    await block.dblclick();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    if (original !== undefined) assert.equal(await page.locator('#gp-edit-input').inputValue(), original);
    assert.equal(await page.locator('#gp-edit-source-label').isVisible(), true);
  };

  await t.test('生の HTML に書いた位置では入力欄を開かず、ファイルを変えない', async (t) => {
    const original = 'first\n\nsecond\n\n<p data-sourcepos="1:1-3:6">forged</p>\n\nsafe\n';
    const { page, file } = await editablePage(t, browser, original);
    const responsePromise = page.waitForResponse((response) => response.url().includes('/_/edit/source?'));
    await page.locator('#gp-content p').filter({ hasText: 'forged' }).dblclick();
    const response = await responsePromise;
    assert.equal(response.status(), 422);
    assert.equal((await response.json()).error.code, 'untrusted');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書に書かれた属性と区別できない'));
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    assert.equal(readFileSync(file, 'utf8'), original);
    await open(page, page.locator('#gp-content p').filter({ hasText: 'safe' }), 'safe');
    await page.locator('#gp-edit-input').fill('changed');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('safe\n', 'changed\n'));
  });

  await t.test('生の HTML に書いたチェックボックスの位置では書き込まず、表示を戻す', async (t) => {
    const original = 'plain [ ]\n\n- [ ] task\n\n<ul><li><input type="checkbox" class="task-list-item-checkbox" data-checkbox-sourcepos="1:8-1:8"> forged</li></ul>\n';
    const { page, file } = await editablePage(t, browser, original);
    const forged = page.locator('#gp-content input[data-checkbox-sourcepos="1:8-1:8"]');
    const responsePromise = page.waitForResponse((response) => response.request().method() === 'POST' &&
      new URL(response.url()).pathname.endsWith('/_/edit'));
    await forged.click();
    const response = await responsePromise;
    assert.equal(response.status(), 422);
    assert.equal((await response.json()).error.code, 'untrusted');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書に書かれた属性と区別できない'));
    await editIdle(page);
    assert.equal(await forged.isChecked(), false);
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.locator('#gp-content input[data-checkbox-sourcepos="3:4-3:4"]').click();
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('- [ ] task', '- [x] task'));
  });

  await t.test('表のない段落の原文・改行・確定・取り消し・外のクリック', async (t) => {
    const original = '前の段落。\n\n**太字**と[リンク](#末尾)。\n\n末尾。\n';
    const { page, file } = await editablePage(t, browser, original);
    const block = page.locator('#gp-content p').nth(1);
    const sent = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) sent.push(request.postDataJSON());
    });
    await open(page, block.locator('a'), '**太字**と[リンク](#末尾)。');
    assert.equal(new URL(page.url()).hash, '');
    await page.locator('#gp-edit-input').fill('**変更**');
    await page.keyboard.press('Enter');
    await page.keyboard.insertText('次の行');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '**変更**\n次の行');
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    const changed = original.replace('**太字**と[リンク](#末尾)。', '**変更**\n次の行');
    assert.equal(readFileSync(file, 'utf8'), changed);
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    await open(page, block, '**変更**\n次の行');
    await page.locator('#gp-edit-input').fill('取り消す文字');
    await page.keyboard.press('Escape');
    assert.equal(readFileSync(file, 'utf8'), changed);
    assert.equal(sent.length, 1);
    await open(page, block, '**変更**\n次の行');
    await page.keyboard.press('Control+Enter');
    assert.equal(sent.length, 1, '同じ文字は送らない');
    await open(page, block);
    await page.locator('#gp-edit-input').fill('外で確定\n');
    await page.locator('#gp-content p').first().click();
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), changed.replace('**変更**\n次の行', '外で確定\n'));
    assert.equal(sent.at(-1).text, '外で確定\n', '末尾の改行もそのまま送る');
    await open(page, block);
    await page.locator('#gp-edit-input').fill('Cmd で確定');
    await page.keyboard.press('Meta+Enter');
    await editIdle(page);
    assert.match(readFileSync(file, 'utf8'), /Cmd で確定/);
  });

  await t.test('空行なしの日本語の用語を、補正された範囲で編集する', async (t) => {
    const original = 'りんご\n: 赤\n\n後の段落。\n';
    const { page, file } = await editablePage(t, browser, original);
    const term = page.locator('#gp-content dt');
    assert.equal(await term.getAttribute('data-sourcepos'), '1:1-1:1');
    const sources = [], writes = [];
    page.on('request', (request) => {
      if (request.url().includes('/_/edit/source?')) sources.push(new URL(request.url()));
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) writes.push(request.postDataJSON());
    });
    await open(page, term, 'りんご');
    await page.locator('#gp-edit-input').fill('みかん');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), 'みかん\n: 赤\n\n後の段落。\n');
    assert.equal(sources[0].searchParams.get('tag'), 'dt');
    assert.equal(sources[0].searchParams.get('range'), '1:1-1:1');
    assert.equal(writes[0].range, '1:1-1:9');
    await open(page, page.locator('#gp-content dt'), 'みかん');
    await page.keyboard.press('Escape');
  });

  await t.test('見出し・入れ子のリスト・リスト内の段落・引用・フェンス', async (t) => {
    const original = '## 見出し\n\n- 親\n  - 子\n\n- 長い項目\n\n  項目の段落\n\n> 引用\n\n```text\nコード\n```\n';
    const { page, file } = await editablePage(t, browser, original);
    for (const [selector, source, replacement] of [
      ['h2', '## 見出し', '## 新しい見出し'],
      ['li', '- 親\n  - 子', '- 新しい親\n  - 新しい子'],
      ['li p', '長い項目', '長い項目の変更'],
      ['blockquote', '> 引用', '> 引用の変更'],
      ['pre', '```text\nコード\n```', '```text\n新しいコード\n```'],
    ]) {
      // blockquote の余白と li の直下を選んだ場合も、同じ節点から探索する。
      const target = page.locator('#gp-content ' + selector);
      await (selector === 'li p' ? target.filter({ hasText: '長い項目' }) : target.first()).dispatchEvent('dblclick');
      await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
      assert.equal(await page.locator('#gp-edit-input').inputValue(), source);
      await page.locator('#gp-edit-input').fill(replacement);
      await page.keyboard.press('Control+Enter');
      await editIdle(page);
      assert.ok(readFileSync(file, 'utf8').includes(replacement));
    }
    assert.equal(readFileSync(file, 'utf8'), original.replace('## 見出し', '## 新しい見出し')
      .replace('- 親\n  - 子', '- 新しい親\n  - 新しい子').replace('長い項目', '長い項目の変更')
      .replace('> 引用', '> 引用の変更').replace('コード', '新しいコード'));
  });

  await t.test('複数行の CRLF の原文と、変換中のキーを確定しない合成試験', async (t) => {
    const original = '前。\r\n\r\n**最初**\r\n続き。\r\n\r\n後。\r\n';
    const { page, file } = await editablePage(t, browser, original);
    await open(page, page.locator('#gp-content p').nth(1), '**最初**\n続き。');
    await page.locator('#gp-edit-input').fill('変更\n続き。');
    await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input');
      input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Enter', ctrlKey: true, isComposing: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Escape', keyCode: 229 }));
      input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }));
    });
    assert.equal(readFileSync(file, 'utf8'), original);
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '変更\n続き。');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('**最初**', '変更'));
  });

  await t.test('長い原文の入力欄はブロックの幅を保ち、画面の下端で内側をスクロールする', async (t) => {
    const original = Array.from({ length: 90 }, (_, i) => '長い段落の行 ' + i).join('\n') + '\n';
    const { page, file } = await editablePage(t, browser, original);
    await page.locator('#gp-content p').dispatchEvent('dblclick');
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    const geometry = await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input'), rect = input.getBoundingClientRect();
      return { width: rect.width, bottom: rect.bottom, viewport: innerHeight, scroll: input.scrollHeight,
        height: input.clientHeight, blockWidth: document.querySelector('#gp-content p').getBoundingClientRect().width };
    });
    assert.ok(geometry.width >= geometry.blockWidth);
    assert.ok(geometry.bottom <= geometry.viewport);
    assert.ok(geometry.scroll > geometry.height);
    await page.keyboard.press('Escape');
    assert.equal(readFileSync(file, 'utf8'), original);
  });

  await t.test('見えているブロックを開いてもページを動かさず、外で変わったブロックの入力欄は閉じて下書きに残す', async (t) => {
    const original = '# 題\n\n最初の段落。\n\n二つ目の段落。\n\n' + Array.from({ length: 40 }, (_, i) => '段落 ' + i + '。').join('\n\n') + '\n';
    const { page, file, server } = await editablePage(t, browser, original);
    const block = page.locator('#gp-content p').first();
    const before = await page.evaluate(() => window.scrollY);
    await open(page, block, '最初の段落。');
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const geometry = await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input').getBoundingClientRect();
      const label = document.querySelector('#gp-edit-source-label').getBoundingClientRect();
      return { scrollY: window.scrollY, header: document.querySelector('#gp-header').getBoundingClientRect().bottom,
        input: [input.left, input.top, input.right, input.bottom], label: [label.left, label.top, label.right, label.bottom],
        style: document.documentElement.style.scrollPaddingTop + document.documentElement.style.scrollPaddingBottom };
    });
    assert.equal(geometry.scrollY, before, '見えているブロックでは動かさない');
    assert.ok(geometry.input[1] >= geometry.header - 1, JSON.stringify(geometry));
    // 「原文」の札は入力欄の内側に重ね、前後の本文やヘッダーに掛けない。
    assert.ok(geometry.label[0] >= geometry.input[0] && geometry.label[1] >= geometry.input[1] &&
      geometry.label[2] <= geometry.input[2] && geometry.label[3] <= geometry.input[3], JSON.stringify(geometry));
    assert.equal(geometry.style, '');
    // 編集中の段落が外で見出しに変わると、要素が入れ替わる。見えない入力欄を残さず、書かずに知らせる。
    await page.locator('#gp-edit-input').fill('画面で直した段落。');
    const external = original.replace('最初の段落。', '## 外で見出しにした');
    writeFileSync(file, external);
    await server.setDocument({ path: file, text: external, rev: 40 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 40);
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'off' &&
      document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    assert.equal(readFileSync(file, 'utf8'), external);
    await editIdle(page);
    await open(page, page.locator('#gp-content h2').first(), '## 外で見出しにした');
    await page.keyboard.press('Escape');
  });

  await t.test('include と front matter は原文取得の理由を知らせ、目次は開かない', async (t) => {
    const original = '---\na: b\n---\n\n[TOC]\n\n> [TOC]\n\n# 題\n\n本文[^a]\n\n[^a]: 脚注\n\n$`x`$\n\n```mermaid\ngraph TD; A-->B\n```\n\n::include{file=part.md}\n';
    const fixture = await fixturePage(t, browser, undefined, (root) => {
      writeFileSync(join(root, 'README.md'), original);
      writeFileSync(join(root, 'part.md'), '取り込んだ段落。\n');
    }, { live: true, edit: 'file' });
    const { page, root } = fixture;
    await page.locator('#gp-edit-toggle').click();
    await editIdle(page);
    await page.waitForSelector('.gp-mermaid svg');
    for (const [block, reason] of [
      [page.locator('#gp-content pre').first(), /front matter/],
      [page.locator('#gp-content p').filter({ hasText: '取り込んだ段落。' }), /取り込んだ/],
    ]) {
      await block.dblclick();
      await page.waitForFunction(() => document.querySelector('#gp-edit-status').dataset.kind === 'error');
      assert.match(await page.locator('#gp-edit-status').textContent(), reason);
      assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
      await page.locator('#gp-content h1').click();
    }
    const sources = [];
    page.on('request', (r) => { if (r.url().includes('/_/edit/source?')) sources.push(r.url()); });
    for (const selector of ['ul.section-nav', 'blockquote ul.section-nav a']) {
      const block = page.locator('#gp-content ' + selector).first();
      if (await block.count()) await block.dispatchEvent('dblclick');
    }
    assert.deepEqual(sources, []);
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    assert.equal(readFileSync(join(root, 'README.md'), 'utf8'), original);
    assert.equal(readFileSync(join(root, 'part.md'), 'utf8'), '取り込んだ段落。\n');
  });

  await t.test('セルからブロックへ確定して移り、逆も確定する。表内の段落はセルの編集になる', async (t) => {
    const original = '段落。\n\n| 列 |\n| --- |\n| セル |\n\n<table><tr><td><p>表の段落</p></td></tr></table>\n';
    const { page, file } = await editablePage(t, browser, original);
    await page.locator('#gp-content td').first().dblclick();
    await page.locator('#gp-edit-input').fill('セルの変更');
    await open(page, page.locator('#gp-content > p').first(), '段落。');
    assert.match(readFileSync(file, 'utf8'), /セルの変更/);
    await page.locator('#gp-edit-input').fill('段落の変更');
    await page.locator('#gp-content td').first().dblclick();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    await page.keyboard.press('Escape');
    await editIdle(page);
    assert.match(readFileSync(file, 'utf8'), /段落の変更/);
    await page.locator('#gp-content td p').dblclick();
    assert.equal(await page.locator('#gp-edit-input').getAttribute('aria-label'), '表のセルの編集');
    await page.keyboard.press('Escape');
  });

  await t.test('チェックを一文字だけ変え、Space・差分更新・編集モードの出入りを確かめる', async (t) => {
    const original = '<details><summary>詳細</summary>残す</details>\n\n- [ ] 作業\n- [~] 対象外\n\n| 列 |\n| --- |\n| 保持 |\n';
    const { page, file } = await editablePage(t, browser, original);
    const checkbox = page.locator('#gp-content input[data-checkbox-sourcepos]').first();
    assert.equal(await checkbox.isDisabled(), false);
    await page.locator('#gp-content summary').click();
    await page.evaluate(() => {
      window.keptDetails = document.querySelector('#gp-content details');
      window.keptEditTable = window.gpPreview.tables()[0];
    });
    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    const hold = async (route) => { await gate; await route.continue(); };
    await page.route('**/_/edit', hold);
    await checkbox.click();
    assert.equal(await checkbox.isChecked(), true, '応答前も新しい見た目を残す');
    assert.equal(readFileSync(file, 'utf8'), original);
    release();
    await editIdle(page);
    await page.unroute('**/_/edit', hold);
    assert.equal(readFileSync(file, 'utf8'), original.replace('[ ]', '[x]'));
    assert.equal(await page.evaluate(() => window.keptDetails === document.querySelector('#gp-content details') &&
      window.keptDetails.open && window.keptEditTable === window.gpPreview.tables()[0]), true);
    await checkbox.focus();
    await page.keyboard.press('Space');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original);
    // 切り替えで要素が入れ替わっても、フォーカスは同じチェックボックスへ戻る（キーで続けて操作できる）。
    await page.waitForFunction(() => document.activeElement?.matches('input.task-list-item-checkbox[data-checkbox-sourcepos]'));
    await page.keyboard.press('Space');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('[ ]', '[x]'));
    await page.waitForFunction(() => document.activeElement?.matches('input.task-list-item-checkbox[data-checkbox-sourcepos]'));
    await page.keyboard.press('Space');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original);
    const excluded = page.locator('#gp-content input[data-inapplicable]');
    assert.equal(await excluded.isDisabled(), true);
    await excluded.dispatchEvent('click');
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.locator('#gp-edit-toggle').click();
    assert.equal(await checkbox.isDisabled(), true);
    await checkbox.dispatchEvent('click');
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.locator('#gp-content li').first().dblclick();
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
  });

  await t.test('連続したチェックの切り替えを同じ待ち行列から順に保存する', async (t) => {
    const original = '- [ ] 作業\n';
    const { page, file } = await editablePage(t, browser, original);
    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    const sent = [];
    await page.route('**/_/edit', async (route) => {
      sent.push(route.request().postDataJSON());
      if (sent.length === 1) await gate;
      await route.continue();
    });
    const checkbox = page.locator('#gp-content input');
    await checkbox.click();
    await checkbox.click();
    assert.equal(await checkbox.isChecked(), false);
    assert.equal(sent.length, 1);
    release();
    await editIdle(page);
    assert.deepEqual(sent.map((body) => body.checked), [true, false]);
    assert.ok(sent[1].render > sent[0].render);
    assert.equal(readFileSync(file, 'utf8'), original);
    assert.equal(await checkbox.isChecked(), false);
  });

  await t.test('先行するセルの送信を待って原文を取り、取得中に出た編集モードでは開かない', async (t) => {
    const original = '段落。\n\n| 列 |\n| --- |\n| セル |\n';
    const { page, file } = await editablePage(t, browser, original);
    let releasePost, releaseSource;
    const postGate = new Promise((resolve) => { releasePost = resolve; });
    const sourceGate = new Promise((resolve) => { releaseSource = resolve; });
    t.after(() => { releasePost(); releaseSource(); });
    await page.route('**/_/edit', async (route) => { await postGate; await route.continue(); });
    const sources = [];
    page.on('request', (r) => { if (r.url().includes('/_/edit/source?')) sources.push(r.url()); });
    await page.locator('#gp-content td').dblclick();
    await page.locator('#gp-edit-input').fill('セルの変更');
    await page.locator('#gp-content p').dblclick();
    assert.deepEqual(sources, [], '先行する送信の応答と描画を待つ');
    releasePost();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '段落。');
    assert.equal(readFileSync(file, 'utf8'), original.replace('セル', 'セルの変更'));
    await page.keyboard.press('Escape');
    await page.route('**/_/edit/source?*', async (route) => {
      const response = await route.fetch();
      await sourceGate;
      await route.fulfill({ response });
    });
    const requested = page.waitForRequest((r) => r.url().includes('/_/edit/source?'));
    await page.locator('#gp-content p').dblclick();
    await requested;
    await page.locator('#gp-edit-toggle').click();
    releaseSource();
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
  });

  await t.test('外の変更の描画が届いたら、待ち行列のチェックを送らず元へ戻す', async (t) => {
    const original = '- [ ] 最初\n- [ ] 次\n';
    const { page, file, server } = await editablePage(t, browser, original);
    let release, written;
    const gate = new Promise((resolve) => { release = resolve; });
    const firstWritten = new Promise((resolve) => { written = resolve; });
    t.after(() => release());
    let sent = 0;
    await page.route('**/_/edit', async (route) => {
      sent++;
      const response = await route.fetch();
      written();
      await gate;
      await route.fulfill({ response });
    });
    await page.locator('#gp-content input').first().click();
    await firstWritten;
    await page.locator('#gp-content input').last().click();
    assert.equal(await page.locator('#gp-content input').last().isChecked(), true);
    const external = readFileSync(file, 'utf8').replace('最初', '外の変更');
    writeFileSync(file, external);
    await server.setDocument({ path: file, text: external, rev: 30 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 30);
    release();
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    await editIdle(page);
    assert.equal(sent, 1);
    assert.equal(readFileSync(file, 'utf8'), external);
    assert.equal(await page.locator('#gp-content input').last().isChecked(), false);
  });

  await t.test('409・422・通信失敗でチェックを戻し、編集できない文書では有効にしない', async (t) => {
    const original = '- [ ] 作業\n';
    const { page, file } = await editablePage(t, browser, original);
    const checkbox = page.locator('#gp-content input');
    for (const status of [409, 422, 500]) {
      const message = '切り替えを断りました ' + status;
      await page.route('**/_/edit', (route) => route.fulfill({ status, json: { error: { code: 'test', message } } }));
      await checkbox.click();
      await page.waitForFunction((message) => document.querySelector('#gp-edit-status').textContent.includes(message), message);
      assert.equal(await checkbox.isChecked(), false);
      assert.equal(readFileSync(file, 'utf8'), original);
      await page.unroute('**/_/edit');
      await editIdle(page);
    }
    await page.route('**/_/edit', (route) => route.abort());
    await checkbox.click();
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('書き込めませんでした'));
    assert.equal(await checkbox.isChecked(), false);
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.unroute('**/_/edit');
    await page.locator('#gp-edit-toggle').click();
    await page.route('**/_/edit/tables?*', async (route) => {
      const response = await route.fetch();
      const data = await response.json();
      data.document = { editable: false, reason: 'この文書は編集できません' };
      await route.fulfill({ response, json: data });
    });
    await page.locator('#gp-edit-toggle').click();
    await editIdle(page);
    await page.locator('#gp-content li').dispatchEvent('dblclick');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('この文書は編集できません'));
    assert.equal(await checkbox.isDisabled(), true);
    await page.locator('#gp-content li').dispatchEvent('dblclick');
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
  });

  await t.test('原文取得の409、送信の422と外の変更、HTML の安全な入力', async (t) => {
    const original = '**原文**。\n';
    const { page, file, server } = await editablePage(t, browser, original);
    await page.route('**/_/edit/source?*', (route) => route.fulfill({ status: 409, json: { error: { code: 'stale', message: '古い描画' } } }));
    const response = page.waitForResponse((r) => r.url().includes('/_/edit/source?'));
    await page.locator('#gp-content p').dblclick();
    await response;
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-status').isVisible(), false);
    await page.unroute('**/_/edit/source?*');
    await open(page, page.locator('#gp-content p'), '**原文**。');
    const malicious = '<img src=x onerror=alert(1)>';
    await page.locator('#gp-edit-input').fill(malicious);
    assert.equal(await page.locator('#gp-edit-layer img').count(), 0);
    await page.route('**/_/edit', (route) => route.fulfill({ status: 422, json: { error: { code: 'range', message: malicious } } }));
    await page.keyboard.press('Control+Enter');
    await page.waitForFunction((message) => document.querySelector('#gp-edit-status').textContent === message, malicious);
    assert.equal(await page.locator('#gp-edit-input').inputValue(), malicious);
    assert.equal(await page.locator('#gp-edit-input').isVisible(), true);
    assert.equal(await page.locator('#gp-edit-status img').count(), 0);
    // 断られて開き直した入力を直して確定すると、そのまま送られる。
    await page.unroute('**/_/edit');
    await page.locator('#gp-edit-input').fill('直した入力。');
    await page.keyboard.press('Control+Enter');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), '直した入力。\n');
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    await open(page, page.locator('#gp-content p'));
    await page.locator('#gp-edit-input').fill('画面の変更');
    writeFileSync(file, '外の変更。\n');
    await page.keyboard.press('Control+Enter');
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    assert.equal(readFileSync(file, 'utf8'), '外の変更。\n');
    await server.setDocument({ path: file, text: '外の変更。\n', rev: 20 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 20);
    await editIdle(page);
    await open(page, page.locator('#gp-content p'), '外の変更。');
    await page.locator('#gp-edit-input').fill('再描画より前の入力');
    writeFileSync(file, 'もう一度外の変更。\n');
    await server.setDocument({ path: file, text: 'もう一度外の変更。\n', rev: 21 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 21);
    await page.keyboard.press('Control+Enter');
    assert.equal(readFileSync(file, 'utf8'), 'もう一度外の変更。\n');
  });

  await t.test('ブロックの確定と表内外のチェックを順に送り、修飾キーのリンクは止めない', async (t) => {
    // 開いた入力欄がチェックボックスに重ならないよう、間に段落を置く。
    const original = '段落と[リンク](#x)。\n\n間の段落 1。\n\n間の段落 2。\n\n- [ ] 作業\n\n<table><tr><td>\n\n- [x] 済み\n\n</td></tr></table>\n';
    const { page, file } = await editablePage(t, browser, original);
    const sent = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) sent.push(request.postDataJSON());
    });
    // 開いたブロックの入力は、チェックボックスを押したときに確定し、続けてチェックを送る。
    await open(page, page.locator('#gp-content > p').first(), '段落と[リンク](#x)。');
    await page.locator('#gp-edit-input').fill('直した段落と[リンク](#x)。');
    await page.locator('#gp-content > ul li input').click();
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('段落と', '直した段落と').replace('- [ ]', '- [x]'));
    assert.deepEqual(sent.map((body) => (body.range ? 'range' : body.checkbox ? 'checkbox' : 'table')), ['range', 'checkbox']);
    assert.ok(sent[1].render > sent[0].render);
    // 表のセルの中もチェックだけを切り替え、セルを選ばない。
    const inCell = page.locator('#gp-content td input');
    assert.equal(await inCell.count(), 1, '表のセルにチェックボックスが描かれる');
    assert.equal(await inCell.isDisabled(), false);
    await inCell.click();
    await editIdle(page);
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
    assert.equal(sent.length, 3);
    assert.equal(readFileSync(file, 'utf8'), original.replace('段落と', '直した段落と').replace('- [ ]', '- [x]').replace('- [x] 済み', '- [ ] 済み'));
    await page.keyboard.press('Escape');
    // 編集モードでは段落の中のリンクで移動しないが、Ctrl を押しながらのクリックはブラウザに任せる。
    await page.evaluate(() => {
      window.linkClicks = [];
      window.addEventListener('click', (event) => {
        if (!event.target.closest?.('#gp-content a')) return;
        window.linkClicks.push(event.ctrlKey + ':' + event.defaultPrevented);
        event.preventDefault();
      });
    });
    const link = page.locator('#gp-content > p a').first();
    await link.click();
    await link.click({ modifiers: ['Control'] });
    assert.deepEqual(await page.evaluate(() => window.linkClicks), ['true:false']);
    assert.equal(new URL(page.url()).hash, '');
  });

  await t.test('修飾キー・文字の選択・編集指定なしではブロックの原文を開かない', async (t) => {
    const original = '段落の文字列。\n\n- [ ] 作業\n';
    const { page } = await editablePage(t, browser, original);
    const sourceRequests = [];
    page.on('request', (r) => { if (r.url().includes('/_/edit/source?')) sourceRequests.push(r.url()); });
    for (const modifiers of [['Control'], ['Shift'], ['Alt'], ['Meta']])
      await page.locator('#gp-content p').dblclick({ modifiers });
    const rect = await page.locator('#gp-content p').boundingBox();
    await page.mouse.move(rect.x + 5, rect.y + rect.height / 2);
    await page.mouse.down();
    await page.mouse.move(rect.x + 80, rect.y + rect.height / 2, { steps: 5 });
    await page.mouse.up();
    assert.ok(await page.evaluate(() => getSelection().toString().length > 0));
    assert.deepEqual(sourceRequests, []);
    const other = await editablePage(t, browser, original, { edit: '' });
    await other.page.locator('#gp-content p').dblclick();
    assert.equal(await other.page.locator('#gp-edit-layer').count(), 0);
    assert.equal(await other.page.locator('#gp-content input').isDisabled(), true);
  });
});

test('表と脚注のタスクの切り替え・連続操作・再描画を確かめる', { skip, timeout: 120000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  for (const [name, original, checked] of [
    ['HTML の本文セル', '<table><tr><td>\n\n- [ ] 保持\n- [ ] 作業\n\n</td></tr></table>\n', '- [x] 作業'],
    ['HTML の見出しセル', '<table><thead><tr><th>\n\n- [ ] 作業\n\n</th></tr></thead><tbody><tr><td>本文</td></tr></tbody></table>\n', '- [x] 作業'],
    ['パイプの表', '| 列 |\n| --- |\n| [ ] |\n', '| [x] |'],
    ['脚注', '本文[^a]\n\n[^a]: 定義\n\n    - [ ] 作業\n', '- [x] 作業'],
  ]) {
    await t.test(name, async (t) => {
      const { page, file } = await editablePage(t, browser, original);
      const checkbox = page.locator('#gp-content input.task-list-item-checkbox').last();
      assert.equal(await checkbox.isDisabled(), false);
      let release;
      let gate = new Promise((resolve) => { release = resolve; });
      t.after(() => release());
      const sent = [];
      await page.route('**/_/edit', async (route) => {
        sent.push(route.request().postDataJSON());
        if (sent.length === 1) await gate;
        await route.continue();
      });
      await checkbox.click();
      await checkbox.click();
      assert.equal(sent.length, 1);
      assert.equal(await checkbox.isChecked(), false);
      assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
      release();
      await editIdle(page);
      assert.deepEqual(sent.map((body) => body.checked), [true, false]);
      assert.ok(sent[1].render > sent[0].render);
      assert.equal(readFileSync(file, 'utf8'), original);
      const expected = original.replace(checked.replace('[x]', '[ ]'), checked);
      await checkbox.focus();
      await page.keyboard.press('Space');
      await editIdle(page);
      assert.equal(readFileSync(file, 'utf8'), expected);
      await page.waitForFunction(() => document.activeElement?.matches('input.task-list-item-checkbox'));
      assert.equal(await checkbox.evaluate((element) => element === document.activeElement), true);
      await page.keyboard.press('Space');
      await editIdle(page);
      assert.equal(readFileSync(file, 'utf8'), original);
      if (name === 'HTML の本文セル')
        assert.equal(await page.locator('#gp-content input').first().isChecked(), false, 'セル内の番号で二番目だけを追う');
      sent.length = 0;
      gate = new Promise((resolve) => { release = resolve; });
      await checkbox.dblclick();
      assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
      assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
      release();
      await editIdle(page);
      assert.deepEqual(sent.map((body) => body.checked), [true, false]);
      assert.equal(readFileSync(file, 'utf8'), original);
      await page.locator('#gp-edit-toggle').click();
      assert.equal(await checkbox.isDisabled(), true);
    });
  }
  await t.test('セルの入力を確定してから表内のタスクを送り、セルの選択を解く', async (t) => {
    const original = '<table><tr><td>前のセル</td></tr></table>\n\n' +
      '間の段落。\n\n間の段落。\n\n<table><tr><td>\n\n- [ ] 作業\n\n</td></tr></table>\n';
    const { page, file } = await editablePage(t, browser, original);
    const sent = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) sent.push(request.postDataJSON());
    });
    await page.locator('#gp-content td').first().dblclick();
    await page.locator('#gp-edit-input').fill('直したセル');
    await page.locator('#gp-content input').click();
    await editIdle(page);
    assert.deepEqual(sent.map((body) => body.checkbox ? 'checkbox' : 'table'), ['table', 'checkbox']);
    assert.ok(sent[1].render > sent[0].render);
    assert.equal(readFileSync(file, 'utf8'), original.replace('前のセル', '直したセル').replace('[ ]', '[x]'));
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
  });
  await t.test('全幅表示中のタスクを切り替え、表の情報を失ったら無効に戻す', async (t) => {
    const original = '| 列 |\n| --- |\n| [ ] |\n';
    const { page, file, server } = await editablePage(t, browser, original);
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForSelector('[data-md-table-sticky-bar]');
    await page.locator('[data-md-table-sticky-action="float"]').first().click();
    const checkbox = page.locator('[data-md-table-sticky-float-panel] input.task-list-item-checkbox');
    assert.equal(await checkbox.isDisabled(), false);
    await checkbox.click();
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace('[ ]', '[x]'));
    assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
    await page.route('**/_/edit/tables?*', (route) => route.fulfill({ status: 500, json: { error: { message: '表の情報を取得できません' } } }));
    await server.setDocument({ path: file, text: readFileSync(file, 'utf8'), rev: 30 });
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 30);
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('表の情報を取得できません'));
    assert.equal(await checkbox.isDisabled(), true);
    // 全幅表示の重ねが操作欄の前にあるので、ボタンは要素から押す。
    await page.evaluate(() => document.querySelector('#gp-edit-toggle').click());
    assert.equal(await page.locator('#gp-edit-toggle').getAttribute('aria-pressed'), 'false');
    assert.equal(await checkbox.isDisabled(), true);
  });
  await t.test('入れ子の内外・JSON・対応できない表・取り込んだ表のタスクは無効のまま理由を知らせる', async (t) => {
    const task = '<table><tr><td>\n\n- [ ] 作業\n\n</td></tr></table>\n';
    const original = '<table><tr><td>\n\n- [ ] 外側\n\n' + task + '\n</td></tr></table>\n\n' +
      '```json:table\n{"markdown":true,"items":[{"a":"<input type=\\"checkbox\\" class=\\"task-list-item-checkbox\\" data-checkbox-sourcepos=\\"1:1-1:1\\">"}]}\n```\n\n' +
      '<table><tfoot><tr><td>\n\n- [ ] 脚のセル\n\n</td></tr></tfoot><tbody><tr><td>本文</td></tr></tbody></table>\n\n' +
      '::include{file=part.md}\n';
    const { page, file } = await editablePage(t, browser, original, { files: { 'part.md': '| 列 |\n| --- |\n| [ ] |\n' } });
    const checkboxes = page.locator('#gp-content input.task-list-item-checkbox');
    assert.equal(await checkboxes.count(), 5);
    const sent = [];
    page.on('request', (request) => { if (request.method() === 'POST') sent.push(request.url()); });
    for (let i = 0; i < 5; i++) {
      const checkbox = checkboxes.nth(i);
      assert.equal(await checkbox.isDisabled(), true);
      await checkbox.dispatchEvent('click');
      assert.equal(await page.locator('#gp-edit-status').getAttribute('data-kind'), 'error');
      assert.match(await page.locator('#gp-edit-status').textContent(), /入れ子|JSON|対応づけられない|取り込んだ/);
    }
    assert.deepEqual(sent, []);
    assert.equal(readFileSync(file, 'utf8'), original);
  });
});

async function settledRender(page, rev) {
  await page.waitForFunction((rev) => {
    const current = window.gpPreview?.current();
    return current?.rev === rev && !document.querySelector('#gp-content .js-render-math, #gp-content .js-render-mermaid');
  }, rev);
  await page.evaluate(() => document.fonts.ready);
}

test('表のクリップボード・挿入貼り付け・切り取りを確かめる', { skip }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const pipe = '| 名前 | 数 | 備考 |\n| --- | --- | --- |\n| りんご | 1 | 赤 |\n| みかん | 2 | 橙 |\n| ぶどう | 3 | 紫 |\n';
  const posted = (page) => page.waitForResponse((response) => response.request().method() === 'POST' &&
    new URL(response.url()).pathname.endsWith('/_/edit'));

  await t.test('コピーした行を挿入し、切り取った行は一つの op で移動して一度で戻せる', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    await (await menuItem(page, 'row-select')).click();
    const copy = await copied(page);
    assert.ok(copy.custom, '表情報の origin と写しの材料で独自の形式を作る');
    assert.equal(copy.text, 'りんご\t1\t赤\r\n');
    assert.match(copy.html, /^<table>/);
    assert.equal(JSON.parse(copy.custom).fragment.shape, 'rows');
    await page.locator('#gp-content tr').nth(2).locator('td').first().click();
    let response = posted(page);
    await (await menuItem(page, 'row-insert-after')).click();
    assert.equal((await response).status(), 200);
    await editIdle(page);
    const duplicated = pipe.replace('| みかん | 2 | 橙 |', '| みかん | 2 | 橙 |\n| りんご | 1 | 赤 |');
    assert.equal(readFileSync(file, 'utf8'), duplicated);
    assert.match(await page.locator('#gp-edit-input').getAttribute('aria-label'), /1 行 3 列/);
    await page.keyboard.press('Control+z');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe);
    await page.locator('#gp-content td').first().click();
    await (await menuItem(page, 'row-select')).click();
    assert.ok(JSON.parse((await copied(page, 'Control+x')).custom).cut);
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), true);
    await page.locator('#gp-content tr').last().locator('td').first().click();
    const item = await menuItem(page, 'row-insert-after');
    assert.match(await item.textContent(), /切り取った 1 行/);
    response = posted(page);
    await item.click();
    const moved = await response;
    assert.equal(moved.request().postDataJSON().op.type, 'moveRows');
    assert.equal(moved.status(), 200);
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| りんご | 1 | 赤 |\n', '').trimEnd() + '\n| りんご | 1 | 赤 |\n');
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), false);
    await page.keyboard.press('Control+z');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe);
  });

  await t.test('別の文書のタブへ HTML の原文と属性を写し、切り取りはコピーとして貼る', async (t) => {
    const text = '<table><tr class="行"><td style="color:red" data-x="値"><b>太字</b></td><td>右</td></tr></table>\n';
    const otherText = '<table><tr><td>先</td><td>次</td></tr></table>\n';
    const { page, server, root, file } = await editablePage(t, browser, text, { files: { 'other.md': otherText } });
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowRight');
    const copy = await copied(page);
    assert.ok(copy.custom, '表情報で HTML の原文をコピーする');
    assert.match(JSON.parse(copy.custom).fragment.cells[0].tag, /data-x="値"/);
    assert.doesNotMatch(copy.html, /style="color|data-x|<b>/);
    const other = await page.context().newPage();
    t.after(() => other.close());
    await other.goto(new URL('?doc=other.md', server.url).href);
    await other.locator('#gp-edit-toggle').click();
    await editIdle(other);
    await other.locator('#gp-content td').first().click();
    let response = posted(other);
    await other.keyboard.press('Control+v');
    assert.equal((await response).status(), 200);
    await editIdle(other);
    assert.equal(readFileSync(join(root, 'other.md'), 'utf8'), text.replace('<tr class="行">', '<tr>'));
    assert.equal(readFileSync(file, 'utf8'), text);
    await other.keyboard.press('Control+z');
    await editIdle(other);
    await page.bringToFront();
    await copied(page, 'Control+x');
    await other.bringToFront();
    await other.locator('#gp-content td').first().click();
    response = posted(other);
    await other.keyboard.press('Control+v');
    const pasted = await response;
    assert.equal(pasted.request().postDataJSON().op.type, 'pasteFragment');
    assert.equal(pasted.status(), 200);
    await editIdle(other);
    assert.equal(readFileSync(file, 'utf8'), text);
    assert.match(await other.locator('#gp-edit-status').textContent(), /元を消さずにコピー/);
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), true);
  });

  await t.test('メニューのコピーは本物のクリップボードへ入り、失敗ならキーを案内する', async (t) => {
    const { page } = await editablePage(t, browser, pipe);
    await captureCopy(page);
    await (await menuItemAfterSelect(page, 'row-copy')).click();
    const copy = await page.evaluate(() => window.gpCapturedCopies.at(-1));
    assert.equal(copy.prevented, true);
    assert.equal(copy.text, 'りんご\t1\t赤\r\n');
    // 編集中の textarea に通常の Ctrl+V を行い、OS のクリップボードから読む。
    await page.locator('#gp-content td').last().dblclick();
    await page.locator('#gp-edit-input').fill('');
    await page.keyboard.press('Control+v');
    assert.equal((await page.locator('#gp-edit-input').inputValue()).replace(/\r\n/g, '\n'), 'りんご\t1\t赤\n');
    await page.keyboard.press('Escape');
    await page.evaluate(() => { document.execCommand = () => false; });
    await (await menuItem(page, 'row-copy')).click();
    assert.match(await page.locator('#gp-edit-status').textContent(), /Ctrl\+C を押してください/);
  });

  async function menuItemAfterSelect(page, action) {
    await page.locator('#gp-content td').first().click();
    return menuItem(page, action);
  }

  await t.test('挿入の待機は消えない知らせを出し、次の貼り付けを挿入にする', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    await (await menuItem(page, 'row-paste-after')).click();
    assert.equal(await page.locator('#gp-edit-status').getAttribute('data-kind'), 'wait');
    assert.match(await page.locator('#gp-edit-status').textContent(), /Ctrl\+V/);
    const response = posted(page);
    await pasteText(page, 'x\ty\tz\n');
    const inserted = await response;
    assert.deepEqual(inserted.request().postDataJSON().op, { type: 'insertRowsFrom', row: 1, side: 'after', values: [['x', 'y', 'z']] });
    assert.equal(inserted.status(), 200);
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| りんご | 1 | 赤 |', '| りんご | 1 | 赤 |\n| x | y | z |'));
  });

  await t.test('待機は Esc・ほかのセル・メニュー・外の変更・モード終了・停止で解く', async (t) => {
    const { page, file, server } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    await (await menuItem(page, 'row-paste-after')).click();
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-status').isVisible(), false);
    await pasteText(page, 'x\ty\tz\n');
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), pipe.replace('| りんご | 1 | 赤 |', '| x | y | z |'));
    for (const cancel of ['cell', 'menu', 'external', 'mode', 'stop']) {
      await page.locator('#gp-content td').first().click();
      await (await menuItem(page, 'row-paste-after')).click();
      if (cancel === 'cell') await page.locator('#gp-content td').last().click();
      else if (cancel === 'menu') { await page.locator('#gp-edit-cols').click(); await page.keyboard.press('Escape'); }
      else if (cancel === 'external') {
        const changed = readFileSync(file, 'utf8').replace('みかん', '外のみかん');
        writeFileSync(file, changed);
        await server.setDocument({ path: file, text: changed, rev: 20 });
        await page.waitForFunction(() => window.gpPreview.current().rev === 20);
        await editIdle(page);
      } else if (cancel === 'mode') await page.locator('#gp-edit-toggle').click();
      else await page.evaluate(() => document.dispatchEvent(new Event('gp:stop')));
      assert.equal(await page.locator('#gp-edit-status').isVisible(), false, cancel);
      if (cancel === 'mode') { await page.locator('#gp-edit-toggle').click(); await editIdle(page); }
    }
  });

  await t.test('切り取りの印と待機と範囲は Esc の順で解き、取り消した切り取りを貼らない', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    await page.keyboard.press('Shift+ArrowRight');
    const copy = await copied(page, 'Control+x');
    assert.ok(JSON.parse(copy.custom).cut);
    assert.equal(readFileSync(file, 'utf8'), pipe);
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), true);
    await (await menuItem(page, 'row-paste-after')).click();
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), true);
    assert.equal(await page.locator('#gp-edit-range').isVisible(), true);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-range').isVisible(), true);
    let requests = 0;
    page.on('request', (request) => { if (request.method() === 'POST') requests++; });
    await pasteEnvelope(page, copy.custom);
    assert.match(await page.locator('#gp-edit-status').textContent(), /切り取りは取り消されています/);
    assert.equal(requests, 0);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-edit-range').isVisible(), false);
    await copied(page, 'Control+x');
    await page.locator('#gp-content td').last().click();
    await page.keyboard.insertText('編集で取り消す');
    await page.keyboard.press('Enter');
    await editIdle(page);
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), false);
  });

  await t.test('422 は切り取りと範囲を保ち、409 は範囲を戻し、504 は印だけを消す', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    for (const status of [422, 409, 504]) {
      await page.locator('#gp-content td').first().click();
      await page.keyboard.press('Shift+ArrowRight');
      const copy = await copied(page, 'Control+x');
      await page.route('**/_/edit', (route) => route.fulfill({ status, contentType: 'application/json',
        json: { error: { code: status === 422 ? 'merged' : 'timeout', message: '試験で断る' } } }));
      const response = posted(page);
      await pasteEnvelope(page, copy.custom);
      await response;
      await editIdle(page);
      assert.equal(await page.locator('#gp-edit-cut').isVisible(), status === 422);
      assert.equal(await page.locator('#gp-edit-range').isVisible(), status !== 409);
      if (status === 422) assert.match(await page.locator('#gp-edit-status').textContent(), /Ctrl\+Shift\+V/);
      await page.unroute('**/_/edit');
    }
    assert.equal(readFileSync(file, 'utf8'), pipe);
  });

  await t.test('写せないセルの範囲は値だけの独自形式と TSV にして知らせる', async (t) => {
    const text = '| good | tail\\ |\n| --- | --- |\n';
    const { page, file } = await editablePage(t, browser, text);
    await page.locator('#gp-content th').nth(1).click();
    const copy = await copied(page);
    assert.equal(JSON.parse(copy.custom).fragment.kind, '');
    assert.equal(JSON.parse(copy.custom).fragment.cells[0].source, '');
    assert.equal(copy.text, 'tail\\');
    assert.match(await page.locator('#gp-edit-status').textContent(), /値だけをコピー/);
    await page.locator('#gp-content th').first().click();
    assert.equal(JSON.parse((await copied(page)).custom).fragment.kind, 'pipe');
    assert.equal(readFileSync(file, 'utf8'), text);
  });

  await t.test('列の切り取りと待機は moveColumns と insertColumnsFrom を一回ずつ送る', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    const sent = [];
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/_/edit')) sent.push(request.postDataJSON().op);
    });
    await page.locator('#gp-content td').first().click();
    await captureCopy(page);
    await (await menuItem(page, 'col-cut')).click();
    const copy = JSON.parse(await page.evaluate(() => window.gpCapturedCopies.at(-1).custom));
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), true);
    await page.locator('#gp-content td').nth(1).click();
    await (await menuItem(page, 'col-insert-after')).click();
    await editIdle(page);
    assert.deepEqual(sent[0], { type: 'moveColumns', col: 1, side: 'after',
      from: { table: 0, render: copy.cut.render, hash: copy.cut.hash, origin: copy.origin, col: 0, count: 1 } });
    assert.equal(await page.locator('#gp-edit-cut').isVisible(), false);
    const moved = '| 数 | 名前 | 備考 |\n| --- | --- | --- |\n| 1 | りんご | 赤 |\n| 2 | みかん | 橙 |\n| 3 | ぶどう | 紫 |\n';
    assert.equal(readFileSync(file, 'utf8'), moved);
    await page.locator('#gp-content td').first().click();
    await (await menuItem(page, 'col-paste-before')).click();
    await pasteText(page, '見出し\n一\n二\n三\n');
    await editIdle(page);
    assert.deepEqual(sent[1], { type: 'insertColumnsFrom', col: 0, side: 'before', values: [['見出し'], ['一'], ['二'], ['三']] });
    assert.equal(sent.length, 2);
    assert.equal(readFileSync(file, 'utf8'), '| 見出し | 数 | 名前 | 備考 |\n| --- | --- | --- | --- |\n| 一 | 1 | りんご | 赤 |\n| 二 | 2 | みかん | 橙 |\n| 三 | 3 | ぶどう | 紫 |\n');
  });

  await t.test('大きすぎる原文も表全体の copyLocked も、値だけへ戻す', async (t) => {
    const { page } = await editablePage(t, browser, pipe);
    await changeClipboardMetadata(page, (data) => { data.tables[0].cells[3].source = 'x'.repeat(1572865); });
    await page.locator('#gp-content td').first().click();
    assert.equal(JSON.parse((await copied(page)).custom).fragment.kind, '');
    assert.match(await page.locator('#gp-edit-status').textContent(), /原文を保つ写しは作りません/);
    await page.unroute('**/_/edit/tables?*');
    await changeClipboardMetadata(page, (data) => { data.tables[0].copyLocked = '表全体を写せません'; });
    await page.locator('#gp-content td').nth(1).click();
    assert.equal(JSON.parse((await copied(page)).custom).fragment.kind, '');
    assert.match(await page.locator('#gp-edit-status').textContent(), /値だけをコピー/);
  });

  await t.test('検索の非表示・送信中・古い表情報で断るコピーは、前のクリップボードを保つ', async (t) => {
    const { page, file, server } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    assert.equal((await copied(page)).text, 'りんご');
    await page.locator('#gp-content tr').nth(2).evaluate((row) => { row.style.display = 'none'; });
    await page.locator('#gp-content td').last().click({ modifiers: ['Shift'] });
    assert.equal((await copied(page)).prevented, false);
    assert.match(await page.locator('#gp-edit-status').textContent(), /検索/);
    await page.locator('#gp-content tr').nth(2).evaluate((row) => { row.style.display = ''; });
    await page.locator('#gp-content td').first().dblclick();
    await page.locator('#gp-edit-input').fill('');
    await page.keyboard.press('Control+v');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), 'りんご');
    await page.keyboard.press('Escape');
    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    await page.route('**/_/edit', async (route) => { await gate; await route.fulfill({ json: { unchanged: true } }); });
    await page.keyboard.insertText('送信待ち');
    await page.keyboard.press('Enter');
    assert.equal((await copied(page)).prevented, false);
    assert.match(await page.locator('#gp-edit-status').textContent(), /反映を待っています/);
    release();
    await editIdle(page);
    await page.unroute('**/_/edit');
    let releaseTables;
    const tablesGate = new Promise((resolve) => { releaseTables = resolve; });
    t.after(() => releaseTables());
    await page.route('**/_/edit/tables?*', async (route) => {
      await tablesGate;
      await route.continue();
    });
    const changed = pipe.replace('みかん', '外のみかん');
    writeFileSync(file, changed);
    await server.setDocument({ path: file, text: changed, rev: 21 });
    await page.waitForFunction(() => window.gpPreview.current().rev === 21);
    assert.equal((await copied(page)).prevented, false);
    assert.match(await page.locator('#gp-edit-status').textContent(), /表の情報を読み込んでいます/);
    releaseTables();
    await editIdle(page);
  });

  await t.test('結合を切る範囲のコピーは既定の動きを止めずに知らせる', async (t) => {
    const text = '<table><tr><td>左</td><td rowspan="2">結合</td><td>右</td></tr><tr><td>下</td><td>右下</td></tr></table>\n';
    const { page } = await editablePage(t, browser, text);
    await page.locator('#gp-content td').first().click();
    // 一行だけを選ぶと、右隣の縦結合の途中を切る。
    await (await menuItem(page, 'row-select')).click();
    const result = await copied(page);
    assert.equal(result.prevented, false);
    assert.match(await page.locator('#gp-edit-status').textContent(), /結合/);
  });

  await t.test('偽の独自形式は文字へ戻し、HTML を層へ入れず、値の写しの参照にも注意を出す', async (t) => {
    const { page, file } = await editablePage(t, browser, pipe);
    await page.locator('#gp-content td').first().click();
    const invalid = [JSON.stringify({ format: 'md-preview-table', version: 1, origin: 'a'.repeat(32),
      fragment: { tag: '<img src=x onerror=alert(1)>' }, cut: null }), 'x'.repeat(1572865), '{"__proto__":{}}'];
    for (const custom of invalid) {
      await pasteEnvelope(page, custom, '文字として貼る');
      assert.equal(await page.locator('#gp-edit-input').inputValue(), '文字として貼る');
      assert.match(await page.locator('#gp-edit-status').textContent(), /写しの形が正しくない/);
      assert.equal(await page.locator('#gp-edit-layer img').count(), 0);
      await page.keyboard.press('Escape');
    }
    assert.equal(readFileSync(file, 'utf8'), pipe);
    const custom = await page.evaluate(() => JSON.stringify(window.gpEditGrid.envelope({ origin: 'c'.repeat(32),
      fragment: window.gpEditGrid.valuesFragment([['[参照](../other.md)']]) })));
    await page.route('**/_/edit', (route) => route.fulfill({ json: { unchanged: true } }));
    const response = posted(page);
    await pasteEnvelope(page, custom);
    await response;
    await editIdle(page);
    assert.match(await page.locator('#gp-edit-status').textContent(), /参照リンク・脚注・相対パス/);
    assert.equal(await page.locator('#gp-edit-layer img').count(), 0);
  });
});

test('Neovim の橋渡しで表の編集をバッファへ頼み、拒否の知らせと古い相手との互換性を確かめる', { skip }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const original = '| 見出し |\n| --- |\n| 本文 |\n';
  await t.test('行の複製と表をまたぐ移動は、それぞれ edit 一個と行の置き換え一個で頼む', async (t) => {
    const bridge = await startGoStdio(t);
    const context = await browser.newContext();
    t.after(() => context.close());
    const page = await context.newPage();
    let text = original + '\n| 先 |\n| --- |\n| 宛先 |\n';
    const document = (rev) => bridge.send({ type: 'doc', path: null, name: '二つの表.md', text, buf: 3, rev });
    const receiveEdit = async (action) => {
      const response = page.waitForResponse((value) => value.request().method() === 'POST' &&
        new URL(value.url()).pathname.endsWith('/_/edit'));
      await action();
      return Promise.race([bridge.waitFor('edit', 10000), response.then((value) => {
        assert.fail(`新しい op で edit を頼む前に HTTP ${value.status()} が返りました`);
      })]);
    };
    const apply = async (edit, rev) => {
      assert.equal(edit.edits.length, 1);
      const lines = text.replace(/\n$/, '').split('\n');
      for (const replacement of edit.edits) lines.splice(replacement.start, replacement.end - replacement.start, ...replacement.lines);
      text = lines.join('\n') + '\n';
      await bridge.send({ type: 'edit_result', id: edit.id, ok: true });
      await document(rev);
      await page.waitForFunction((rev) => window.gpPreview.current()?.rev === rev, rev);
      await editIdle(page);
    };
    await page.goto(bridge.url);
    await document(1);
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 1);
    await page.locator('#gp-edit-toggle').click();
    await editIdle(page);
    await page.locator('#gp-content td').first().click();
    const duplicate = await receiveEdit(async () => (await menuItem(page, 'row-duplicate')).click());
    assert.deepEqual(duplicate, { type: 'edit', id: 1, path: null, buf: 3, rev: 1,
      edits: [{ start: 3, end: 3, lines: ['| 本文 |'] }] });
    await apply(duplicate, 2);
    await page.locator('#gp-content table').first().locator('td').first().click();
    await (await menuItem(page, 'row-select')).click();
    assert.ok(JSON.parse((await copied(page, 'Control+x')).custom).cut);
    await page.locator('#gp-content table').last().locator('td').first().click();
    const move = await receiveEdit(async () => (await menuItem(page, 'row-insert-after')).click());
    assert.deepEqual(move, { type: 'edit', id: 2, path: null, buf: 3, rev: 2,
      edits: [{ start: 3, end: 8, lines: ['', '| 先 |', '| --- |', '| 宛先 |', '| 本文 |'] }] });
    await apply(move, 3);
    assert.equal(text, original + '\n| 先 |\n| --- |\n| 宛先 |\n| 本文 |\n');
  });
  await t.test('行の置き換えを受け、新しい doc で表示を更新する', async (t) => {
    const bridge = await startGoStdio(t);
    assert.equal(bridge.ready.edit, 1);
    const context = await browser.newContext();
    t.after(() => context.close());
    const outside = [];
    await context.route('**/*', (route) => {
      if (route.request().url().startsWith(bridge.url)) return route.continue();
      outside.push(route.request().url());
      return route.abort();
    });
    const page = await context.newPage();
    await page.goto(bridge.url);
    let text = original;
    const document = (rev) => bridge.send({ type: 'doc', path: null, name: 'バッファ.md', text, buf: 1, rev });
    await document(1);
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 1);
    await page.locator('#gp-edit-toggle').click();
    await editIdle(page);
    assert.equal(await page.locator('#gp-edit-undo, #gp-edit-redo').count(), 0);
    const cell = page.locator('#gp-content td').first();
    await cell.click();
    await page.keyboard.insertText('変更');
    await page.keyboard.press('Enter');
    const edit = await bridge.waitFor('edit');
    assert.deepEqual(edit, { type: 'edit', id: 1, path: null, buf: 1, rev: 1,
      edits: [{ start: 2, end: 3, lines: ['| 変更 |'] }] });
    const lines = text.replace(/\n$/, '').split('\n');
    for (const replacement of edit.edits) lines.splice(replacement.start, replacement.end - replacement.start, ...replacement.lines);
    text = lines.join('\n') + '\n';
    await bridge.send({ type: 'edit_result', id: edit.id, ok: true });
    await document(2);
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 2);
    await editIdle(page);
    assert.equal(await cell.textContent(), '変更');
    await cell.click();
    await page.keyboard.insertText('断られる入力');
    await page.keyboard.press('Enter');
    const rejected = await bridge.waitFor('edit');
    assert.equal(rejected.id, 2);
    assert.equal(rejected.rev, 2);
    const message = 'Neovim で拒否しました <img src=x onerror=alert(1)>';
    await bridge.send({ type: 'edit_result', id: rejected.id, ok: false, message });
    await page.waitForFunction((message) => document.querySelector('#gp-edit-status').textContent === message, message);
    assert.equal(await cell.textContent(), '変更');
    assert.equal(await page.locator('#gp-edit-status img').count(), 0);
    // 拒否された入力は下書きに残り、同じセルを開いたときに戻る。
    await cell.dblclick();
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '断られる入力');
    await page.keyboard.press('Escape');
    for (const [status, code] of [[409, 'paused'], [503, 'closed'], [503, 'busy'], [504, 'timeout']]) {
      const message = `サーバーからの知らせ ${code}`;
      const route = (route) => route.fulfill({ status, contentType: 'application/json',
        body: JSON.stringify({ error: { code, message } }) });
      await page.route('**/_/edit', route);
      await cell.click();
      await page.keyboard.insertText('残す入力 ' + code);
      await page.keyboard.press('Enter');
      await page.waitForFunction((message) => document.querySelector('#gp-edit-status').textContent.includes(message), message);
      assert.equal(await cell.textContent(), '変更');
      if (status === 409) await cell.dblclick();
      assert.equal(await page.locator('#gp-edit-input').inputValue(), '残す入力 ' + code);
      await page.keyboard.press('Escape');
      await page.unroute('**/_/edit', route);
    }
    assert.deepEqual(outside, []);
  });
  await t.test('段落とタスクを行の置き換えで頼み、編集モード中は行へジャンプしない', async (t) => {
    const bridge = await startGoStdio(t);
    const context = await browser.newContext();
    t.after(() => context.close());
    const outside = [], jumps = [], errors = [], violations = [];
    await context.route('**/*', (route) => {
      if (route.request().url().startsWith(bridge.url)) {
        if (new URL(route.request().url()).pathname.endsWith('/_/jump')) jumps.push(route.request().url());
        return route.continue();
      }
      outside.push(route.request().url());
      return route.abort();
    });
    await context.exposeBinding('recordBlockViolation', (_source, value) => violations.push(value));
    await context.addInitScript(() => document.addEventListener('securitypolicyviolation',
      (event) => void window.recordBlockViolation(event.violatedDirective)));
    const page = await context.newPage();
    page.on('pageerror', (error) => errors.push(error.message));
    await page.goto(bridge.url);
    let text = '**段落**。\n\n- [ ] 作業\n';
    const document = (rev) => bridge.send({ type: 'doc', path: null, name: 'バッファ.md', text, buf: 7, rev });
    await document(1);
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 1);
    await page.locator('#gp-edit-toggle').click();
    await editIdle(page);
    await page.locator('#gp-content p').dblclick();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '**段落**。');
    await page.locator('#gp-edit-input').fill('**新しい段落**。');
    await page.keyboard.press('Control+Enter');
    const paragraph = await bridge.waitFor('edit');
    assert.deepEqual(paragraph, { type: 'edit', id: 1, path: null, buf: 7, rev: 1,
      edits: [{ start: 0, end: 1, lines: ['**新しい段落**。'] }] });
    text = text.replace('**段落**。', '**新しい段落**。');
    await bridge.send({ type: 'edit_result', id: paragraph.id, ok: true });
    await document(2);
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 2);
    await editIdle(page);
    await page.locator('#gp-content input').click();
    const task = await bridge.waitFor('edit');
    assert.deepEqual(task, { type: 'edit', id: 2, path: null, buf: 7, rev: 2,
      edits: [{ start: 2, end: 3, lines: ['- [x] 作業'] }] });
    text = text.replace('[ ]', '[x]');
    await bridge.send({ type: 'edit_result', id: task.id, ok: true });
    await document(3);
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 3);
    await editIdle(page);
    assert.equal(await page.locator('#gp-content input').isChecked(), true);
    assert.deepEqual(jumps, []);
    await page.locator('#gp-edit-toggle').click();
    const jumped = page.waitForResponse((r) => new URL(r.url()).pathname.endsWith('/_/jump'));
    await page.locator('#gp-content p').dblclick();
    await jumped;
    assert.deepEqual(await bridge.waitFor('jump'), { type: 'jump', path: null, line: 1 });
    assert.deepEqual(outside, []);
    assert.deepEqual(violations, []);
    assert.deepEqual(errors, []);
  });
  await t.test('表と脚注のタスクも Neovim に一行の置き換えで送る', async (t) => {
    const bridge = await startGoStdio(t);
    const context = await browser.newContext();
    t.after(() => context.close());
    const page = await context.newPage();
    await page.goto(bridge.url);
    let rev = 0;
    for (const [original, start, line] of [
      ['<table><tr><td>\n\n- [ ] 作業\n\n</td></tr></table>\n', 2, '- [x] 作業'],
      ['| 列 |\n| --- |\n| [ ] |\n', 2, '| [x] |'],
      ['本文[^a]\n\n[^a]: 定義\n\n    - [ ] 作業\n', 4, '    - [x] 作業'],
    ]) {
      const document = (text) => bridge.send({ type: 'doc', path: null, name: 'バッファ.md', text, buf: 7, rev: ++rev });
      await document(original);
      await page.waitForFunction((rev) => window.gpPreview.current()?.rev === rev, rev);
      if (await page.locator('#gp-edit-toggle').getAttribute('aria-pressed') !== 'true') await page.locator('#gp-edit-toggle').click();
      await editIdle(page);
      const checkbox = page.locator('#gp-content input.task-list-item-checkbox');
      assert.equal(await checkbox.isDisabled(), false);
      await checkbox.click();
      const edit = await bridge.waitFor('edit');
      assert.equal(edit.rev, rev);
      assert.equal(edit.buf, 7);
      assert.deepEqual(edit.edits, [{ start, end: start + 1, lines: [line] }]);
      await bridge.send({ type: 'edit_result', id: edit.id, ok: true });
      await document(original.replace('[ ]', '[x]'));
      await page.waitForFunction((rev) => window.gpPreview.current()?.rev === rev, rev);
      await editIdle(page);
      assert.equal(await checkbox.isChecked(), true);
      assert.equal(await page.locator('#gp-edit-selection').isVisible(), false);
    }
  });
  await t.test('edit を知らせない古い Lua には編集の入口を出さない', async (t) => {
    const bridge = await startGoStdio(t, { edit: false });
    assert.equal(Object.hasOwn(bridge.ready, 'edit'), false);
    const context = await browser.newContext();
    t.after(() => context.close());
    const page = await context.newPage();
    await page.goto(bridge.url);
    await bridge.send({ type: 'doc', path: null, name: '文書.md', text: original, buf: 1, rev: 1 });
    await page.waitForFunction(() => window.gpPreview.current()?.rev === 1);
    assert.equal(await page.locator('#gp-edit-toggle').isVisible(), false);
    assert.equal(await page.locator('#gp-edit-layer').count(), 0);
  });
});

test('本文の差分更新と編集用の接点', { skip, timeout: 120000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());

  await t.test('全見本の行を削除・挿入・入れ替えた結果が新しく開いたページと一致する', async (t) => {
    const fixture = await fixturePage(t, browser, undefined,
      (root) => writeFileSync(join(root, 'README.md'), '最初の本文\n'), { live: true });
    const { page, server } = fixture;
    let rev = 1;
    const fixtures = new URL('./fixtures/', import.meta.url);
    for (const name of readdirSync(fixtures).filter((name) => name.endsWith('.md')).sort()) {
      // 外部メディアを含む見本も、本文の形を保って手元の参照にする。
      const source = readFileSync(new URL(name, fixtures), 'utf8')
        .replaceAll('https://example.com/b.png', 'images/b.png')
        .replaceAll('https://example.com/x.png', 'images/x.png')
        .replaceAll('https://example.com/v.mov', 'videos/v.mov');
      await server.setDocument({ name: 'README.md', text: source, rev: ++rev });
      await settledRender(page, rev);
      const lines = source.split('\n');
      const middle = Math.floor(lines.length / 2);
      lines.splice(middle, 1, '挿入した行', '', '書き換えた段落');
      if (lines.length > 3) [lines[1], lines[lines.length - 2]] = [lines[lines.length - 2], lines[1]];
      await server.setDocument({ name: 'README.md', text: lines.join('\n'), rev: ++rev });
      await settledRender(page, rev);
      const fresh = await page.context().newPage();
      try {
        await fresh.goto(server.url);
        await settledRender(fresh, rev);
        assert.deepEqual(await contentTree(page), await contentTree(fresh), name + ' の差分更新が一致する');
      } finally {
        await fresh.close();
      }
    }
  });

  await t.test('表のセルと行を更新し、変わらない段落・details・数式・図を残す', async (t) => {
    const markdown = (cell, rows, heading = '見出し') => '# ' + heading + '\n\n残す段落\n\n' +
      '<details><summary>詳細</summary>内容</details>\n\n$`x^2`$\n\n```mermaid\ngraph TD; A-->B\n```\n\n' +
      '| 列 | 値 |\n| --- | --- |\n| a | ' + cell + ' |\n' + rows;
    const fixture = await fixturePage(t, browser, undefined,
      (root) => writeFileSync(join(root, 'README.md'), markdown('最初', '| b | 二行目 |\n')), { live: true });
    const { page, server } = fixture;
    await settledRender(page, 1);
    await page.evaluate(() => {
      window.kept = ['#gp-content > p', 'details', '.gp-math-inline', '.gp-mermaid', 'table', 'tbody tr', 'tbody td']
        .map((selector) => document.querySelector(selector));
      document.querySelector('details').open = true;
    });
    for (const [rev, rows] of [[2, '| b | 二行目 |\n'], [3, '| b | 二行目 |\n| c | 追加 |\n'], [4, '']]) {
      await server.setDocument({ name: 'README.md', text: markdown('変更後', rows), rev });
      await settledRender(page, rev);
      assert.deepEqual(await page.evaluate(() => ['#gp-content > p', 'details', '.gp-math-inline', '.gp-mermaid', 'table', 'tbody tr', 'tbody td']
        .map((selector, i) => window.kept[i] === document.querySelector(selector))), Array(7).fill(true));
      assert.equal(await page.locator('details').evaluate((element) => element.open), true);
      assert.equal(await page.locator('tbody td').nth(1).textContent(), '変更後');
      const fresh = await page.context().newPage();
      try {
        await fresh.goto(server.url);
        await settledRender(fresh, rev);
        // 利用者が開いた状態だけをそろえる。
        await fresh.locator('details').evaluate((element) => { element.open = true; });
        assert.deepEqual(await contentTree(page), await contentTree(fresh));
      } finally {
        await fresh.close();
      }
    }
    const before = await page.evaluate(() => window.gpPreview.tables().map((table) => table.textContent));
    assert.equal(before.length, 1);
    await page.evaluate(() => { window.renderEvents = []; document.addEventListener('gp:render', (event) => window.renderEvents.push(event.detail)); });
    await server.setDocument({ name: 'README.md', text: markdown('変更後', '') + '\n| 次 | 表 |\n| --- | --- |\n| d | e |\n', rev: 5 });
    await settledRender(page, 5);
    assert.deepEqual(await page.evaluate(() => window.gpPreview.tables().map((table) => table.rows[0].cells[0].textContent)), ['列', '次']);
    assert.deepEqual(await page.evaluate(() => window.renderEvents), [await page.evaluate(() => ({ render: window.gpPreview.current().render }))]);
    assert.equal(await page.evaluate(() => window.gpPreview.doc), null);
    assert.equal(await page.evaluate(() => window.gpPreview.base.href === new URL('./', location.href).href), true);
    assert.equal(await page.evaluate(() => typeof window.gpPreview.config === 'object'), true);
  });

  await t.test('位置属性だけを更新し、表と行の属性差分でも操作用の style を残す', async (t) => {
    const html = (pos, kind, checked) => '<p' + (pos ? ' data-sourcepos="' + pos + '"' : '') + '>本文</p>' +
      '<input type="checkbox"' + (checked ? ' data-checkbox-sourcepos="' + checked + '"' : '') + '>' +
      '<table class="' + kind + '"><tbody><tr class="' + kind + '"><td style="text-align: right">値</td></tr></tbody></table>';
    const fixture = await fixturePage(t, browser, payload(html('1:1-1:2', '最初', '2:1-2:3')));
    const { page } = fixture;
    await page.evaluate(() => {
      window.originalNodes = Array.from(document.querySelector('#gp-content').children);
      document.querySelector('table').style.width = '123px';
      document.querySelector('tr').style.height = '42px';
    });
    for (const [rev, pos, checked] of [[2, '3:1-3:2', '4:1-4:3'], [3, '', '']]) {
      await fixture.send('render', payload(html(pos, '変更後', checked), { rev }));
      await settledRender(page, rev);
      assert.equal(await page.evaluate(() => window.originalNodes.every((node, i) => node === document.querySelector('#gp-content').children[i])), true);
      assert.equal(await page.locator('#gp-content p').getAttribute('data-sourcepos'), pos || null);
      assert.equal(await page.locator('#gp-content input').getAttribute('data-checkbox-sourcepos'), checked || null);
      assert.equal(await page.locator('table').getAttribute('class'), '変更後');
      assert.equal(await page.locator('tr').getAttribute('class'), '変更後');
      assert.equal(await page.locator('table').evaluate((node) => node.style.width), '123px');
      assert.equal(await page.locator('tr').evaluate((node) => node.style.height), '42px');
    }
  });

  await t.test('表と段落の順序が入れ替わっても表を残し、セルの配置の変更は表と行を残す', async (t) => {
    const table = (align, value) => '<table><thead><tr><th align="' + align + '">列</th></tr></thead>' +
      '<tbody><tr><td align="' + align + '">' + value + '</td></tr></tbody></table>';
    const fixture = await fixturePage(t, browser, payload(table('left', '値') + '<p>段落</p>'));
    const { page } = fixture;
    await page.evaluate(() => {
      window.keptTable = document.querySelector('#gp-content table');
      window.keptRow = document.querySelector('#gp-content tbody tr');
    });
    // 節点の数は同じで、表の位置だけが変わる
    await fixture.send('render', payload('<p>段落</p>' + table('left', '値'), { rev: 2 }));
    await settledRender(page, 2);
    assert.equal(await page.evaluate(() => window.keptTable === document.querySelector('#gp-content table') &&
      window.keptTable.previousElementSibling.localName === 'p'), true);
    // 表の前の節点が減り、後ろの節点が増える
    await fixture.send('render', payload(table('left', '値') + '<p>後ろ</p>', { rev: 3 }));
    await settledRender(page, 3);
    assert.equal(await page.evaluate(() => window.keptTable === document.querySelector('#gp-content table') &&
      !window.keptTable.previousElementSibling && window.keptTable.nextElementSibling.textContent === '後ろ'), true);
    // セルの配置が変わると、セルは替わるが、表と行は残る
    await fixture.send('render', payload(table('center', '変更後') + '<p>後ろ</p>', { rev: 4 }));
    await settledRender(page, 4);
    assert.equal(await page.evaluate(() => window.keptTable === document.querySelector('#gp-content table') &&
      window.keptRow === document.querySelector('#gp-content tbody tr')), true);
    assert.equal(await page.locator('#gp-content tbody td').getAttribute('align'), 'center');
    assert.equal(await page.locator('#gp-content tbody td').textContent(), '変更後');
  });

  await t.test('表の固定を外した直後の描画でも、表と段落を残して差分更新する', async (t) => {
    const html = (value) => '<p>段落</p><table><thead><tr><th>列</th></tr></thead>' +
      '<tbody><tr><td>' + value + '</td></tr></tbody></table>';
    const fixture = await fixturePage(t, browser, payload(html('最初')));
    const { page } = fixture;
    await page.evaluate(() => {
      window.keptTable = document.querySelector('#gp-content table');
      window.keptParagraph = document.querySelector('#gp-content p');
    });
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-md-table-sticky-bar]').length === 1);
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForFunction(() => !window.__mdTableSticky &&
      !document.querySelector('[data-md-table-sticky-wrap], [data-md-table-sticky-bar]'));
    await fixture.send('render', payload(html('変更後'), { rev: 2 }));
    await settledRender(page, 2);
    assert.equal(await page.evaluate(() => window.keptTable === document.querySelector('#gp-content table') &&
      window.keptParagraph === document.querySelector('#gp-content p')), true);
    assert.equal(await page.locator('#gp-content tbody td').textContent(), '変更後');
  });

  await t.test('誤りの前後・文書の変更・対応の食い違いは全置換する', async (t) => {
    const html = '<details><summary>詳細</summary>本文</details><p>残す本文</p>';
    const fixture = await fixturePage(t, browser, payload(html));
    const { page } = fixture;
    await page.locator('details').evaluate((element) => { element.open = true; });
    const cases = [
      payload('', { error: '試験の誤り', markdown: '原文', rev: 2 }),
      payload(html, { rev: 3 }),
      payload(html, { file: '別.md', rev: 4 }),
      payload(html, { file: '別.md', rev: 5 }),
    ];
    for (const message of cases) {
      if (await page.locator('details').count()) await page.locator('details').evaluate((element) => { element.open = true; });
      await page.evaluate(() => { window.previousNode = document.querySelector('#gp-content').firstChild; });
      if (message.rev === 5) await page.locator('#gp-content p').evaluate((element) => element.remove());
      await fixture.send('render', message);
      await settledRender(page, message.rev);
      assert.equal(await page.evaluate(() => !window.previousNode.isConnected), true);
      if (message.rev === 4) assert.equal(await page.locator('details').evaluate((element) => element.open), true);
    }
  });

  await t.test('編集中のスクロール保持とジャンプの停止を接点から切り替える', async (t) => {
    const html = Array.from({ length: 100 }, (_, i) => '<p data-sourcepos="' + (i + 1) + ':1-' + (i + 1) + ':10">段落 ' + i + '</p>').join('');
    const fixture = await fixturePage(t, browser, payload(html), undefined, { jump: true });
    const { page, server } = fixture;
    await fixture.send('scroll', { line: 70, ratio: 0.5, lines: 100 });
    await page.waitForFunction(() => window.scrollY > 100);
    await page.evaluate(() => { window.gpPreview.holdScroll(true); window.gpPreview.setEditing(true); window.scrollTo(0, 100); });
    await fixture.send('render', payload(html.replace('段落 0', '変更 0'), { rev: 2 }));
    await settledRender(page, 2);
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert.equal(await page.evaluate(() => window.scrollY), 100);
    await page.locator('#gp-content p').first().dblclick();
    await server.sync();
    assert.deepEqual(fixture.jumpRequests, []);
    await fixture.send('scroll', { line: 70, ratio: 0.5, lines: 100 });
    await page.waitForFunction(() => window.scrollY > 100);
    await page.evaluate(() => { window.gpPreview.holdScroll(false); window.gpPreview.setEditing(false); });
    const jumped = page.waitForResponse((response) => response.url().endsWith('/_/jump'));
    await page.locator('#gp-content p').first().dblclick();
    await jumped;
    await server.sync();
    assert.deepEqual(fixture.jumps, [{ line: 1 }]);
  });

  await t.test('固定・コンパクト・検索・全幅表示中も表と記録と結合セルの内側を残す', async (t) => {
    const table = (value, rows = '') => '<table><thead><tr><th>列1</th><th>列2</th><th>列3</th></tr></thead>' +
      '<tbody><tr><td rowspan="2">' + value + '</td><td>名前</td><td>検索語 <span class="js-render-math">x</span></td></tr>' +
      '<tr><td>二行目</td><td>値</td></tr>' + rows + '</tbody></table>';
    const fixture = await fixturePage(t, browser, payload('<p>前</p>' + table('最初') + '<p>後</p>'));
    const { page } = fixture;
    await settledRender(page, 1);
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForSelector('[data-md-table-sticky-bar]');
    let cols = await page.evaluate(() => window.__mdTableSticky.records.get(window.gpPreview.tables()[0]).layout.cols);
    if (cols === 2) {
      await page.locator('[data-md-table-sticky-action="cols-minus"]').click();
      cols--;
    }
    while (cols !== 2) {
      await page.locator('[data-md-table-sticky-action="cols-' + (cols < 2 ? 'plus' : 'minus') + '"]').click();
      cols = await page.evaluate(() => window.__mdTableSticky.records.get(window.gpPreview.tables()[0]).layout.cols);
    }
    await page.locator('[data-md-table-sticky-action="compact"]').check();
    await page.locator('[data-md-table-sticky-action="search"]').click();
    await page.locator('[data-md-table-sticky-search-input]').fill('検索語');
    await page.waitForFunction(() => window.__mdTableSticky.records.get(window.gpPreview.tables()[0]).search.query === '検索語');
    await page.locator('[data-md-table-sticky-action="float"]').click();
    await page.waitForSelector('[data-md-table-sticky-float]');
    await page.evaluate(() => {
      window.keptTable = window.gpPreview.tables()[0];
      window.keptRecord = window.__mdTableSticky.records.get(window.keptTable);
      window.keptInner = window.keptTable.querySelector('td > span[data-md-table-sticky-inner]');
      window.keptState = JSON.stringify({ override: window.keptRecord.override, compact: window.keptRecord.compact, query: window.keptRecord.search.query });
    });
    assert.equal(await page.evaluate(() => !!window.keptInner), true);
    assert.equal(await page.evaluate(() => window.keptRecord.override.cols), 2);
    await fixture.send('render', payload('<p>前</p>' + table('セルだけ変更') + '<p>後</p>', { rev: 2 }));
    await settledRender(page, 2);
    assert.deepEqual(await page.evaluate(() => ({
      connected: window.keptInner.isConnected, text: window.keptInner.textContent,
      sameTable: window.keptTable === window.gpPreview.tables()[0],
    })), { connected: true, text: 'セルだけ変更', sameTable: true });
    // 段落の数も変え、表を目印に分ける経路と、placeholder の前への挿入も通す。
    await fixture.send('render', payload('<p>前の変更</p><p>挿入</p>' +
      table('変更後', '<tr><td>追加</td><td>行</td><td>検索語</td></tr>') + '<p>後の変更</p>', { rev: 3 }));
    await settledRender(page, 3);
    await page.waitForFunction(() => window.keptTable.rows.length === 4 && window.keptTable.querySelector('td > span[data-md-table-sticky-inner]')?.textContent === '変更後');
    await page.waitForFunction(() => window.keptRecord.search.query === '検索語');
    assert.equal(await page.evaluate(() => {
      const table = window.gpPreview.tables()[0];
      return table === window.keptTable && window.__mdTableSticky.records.get(table) === window.keptRecord &&
        window.__mdTableSticky.floating?.record === window.keptRecord &&
        JSON.stringify({ override: window.keptRecord.override, compact: window.keptRecord.compact, query: window.keptRecord.search.query }) === window.keptState;
    }), true);
    assert.deepEqual(await page.locator('#gp-content').evaluate((element) => Array.from(element.children, (child) => child.textContent)),
      ['前の変更', '挿入', 'この表は全幅で表示中（Esc で元に戻す）', '後の変更']);
    // 全幅パネルへ移されたセルの数式も描く。
    await fixture.send('render', payload('<p>前の変更</p><p>挿入</p>' + table('変更後', '<tr><td>追加</td><td>行</td><td><span class="js-render-math">y</span>' +
      '<pre><code class="js-render-mermaid">graph TD; X-->Y</code></pre></td></tr>') + '<p>後の変更</p>', { rev: 4 }));
    await page.waitForSelector('[data-md-table-sticky-float] .gp-mermaid svg');
    await page.waitForFunction(() => window.gpPreview.current().rev === 4 && !window.keptTable.querySelector('.js-render-math, .js-render-mermaid'));
    assert.equal(await page.locator('[data-md-table-sticky-float] .katex').count(), 2);
    await page.locator('[data-md-table-sticky-action="float"]').click();
    await page.waitForSelector('[data-md-table-sticky-float]', { state: 'detached' });
    assert.equal(await page.evaluate(() => document.querySelector('#gp-content table') === window.keptTable), true);
    // 全幅表示のまま表を削除した場合も、表自身と残った操作欄を片付ける。
    await page.locator('[data-md-table-sticky-action="float"]').click();
    await fixture.send('render', payload('<p>表を削除</p>', { rev: 5 }));
    await settledRender(page, 5);
    await page.waitForSelector('[data-md-table-sticky-float], [data-md-table-sticky-bar], [data-md-table-sticky-wrap]', { state: 'detached' });
    assert.equal(await page.evaluate(() => window.keptTable.isConnected), false);
    assert.deepEqual(await page.evaluate(() => window.gpPreview.tables()), []);
  });
});

test('ローカルのプレビューページ', { skip, timeout: 180000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());

  const tableHtml = '<table><thead><tr><th>名前</th><th>値</th></tr></thead>' +
    '<tbody><tr><td>項目</td><td>内容</td></tr></tbody></table>';
  const stickySource = readFileSync(new URL('../sticky/src/sticky-table.js', import.meta.url), 'utf8');
  const waitSticky = (page, want) => page.waitForFunction(
    (want) => Boolean(window.__mdTableSticky) === want &&
      document.querySelector('#gp-sticky-toggle').getAttribute('aria-pressed') === String(want) &&
      document.querySelectorAll('[data-md-table-sticky-bar]').length === Number(want),
    want,
  );

  await t.test('表の固定を切り替え、script を外し、再読込でも固定の指定を保持する', async (t) => {
    const { page } = await fixturePage(t, browser, payload(tableHtml));
    const toggle = page.locator('#gp-sticky-toggle');
    await waitSticky(page, false);
    await toggle.click();
    await waitSticky(page, true);
    assert.equal(await toggle.textContent(), '表を固定');
    assert.equal(await page.locator('script[src$="/md-table-sticky/sticky-table.js"]').count(), 0);
    assert.equal(await page.evaluate(() => JSON.parse(sessionStorage.getItem('gitlab-preview-appearance')).sticky), true);
    await page.reload();
    await waitSticky(page, true);
    await toggle.click();
    await waitSticky(page, false);
    assert.equal(await toggle.textContent(), '表を固定');
    assert.equal(await page.evaluate(() => JSON.parse(sessionStorage.getItem('gitlab-preview-appearance')).sticky), false);
    await page.reload();
    await waitSticky(page, false);
  });

  await t.test('ブックマークレットの状態を描画後に反映し、同じ解除処理で戻す', async (t) => {
    const fixture = await fixturePage(t, browser, payload(tableHtml));
    const { page } = fixture;
    await page.evaluate(stickySource);
    await page.evaluate(() => { window.gpStickyState = window.__mdTableSticky; });
    await fixture.send('render', payload('<p>更新</p>' + tableHtml, { rev: 2 }));
    await waitSticky(page, true);
    assert.equal(await page.evaluate(() => window.gpStickyState === window.__mdTableSticky), true);
    await page.locator('#gp-sticky-toggle').click();
    await waitSticky(page, false);
    assert.equal(await page.evaluate(() => window.gpStickyState.active), false);
    // ボタンで有効にした後にブックマークレットで外しても、次の描画で勝手に戻さない。
    await page.locator('#gp-sticky-toggle').click();
    await waitSticky(page, true);
    await page.evaluate(stickySource);
    await fixture.send('render', payload('<p>解除後</p>' + tableHtml, { rev: 3 }));
    await waitSticky(page, false);
  });

  await t.test('読み込み中の連打で表の固定を二重に実行しない', async (t) => {
    const { page } = await fixturePage(t, browser, payload(tableHtml));
    let release;
    const ready = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    let requests = 0;
    await page.route('**/_/lib/md-table-sticky/sticky-table.js', async (route) => {
      requests++;
      await ready;
      await route.continue();
    });
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForSelector('script[src$="/md-table-sticky/sticky-table.js"]', { state: 'attached' });
    await page.locator('#gp-sticky-toggle').click();
    assert.equal(await page.locator('script[src$="/md-table-sticky/sticky-table.js"]').count(), 1);
    release();
    await waitSticky(page, true);
    assert.equal(requests, 1);
  });

  await t.test('表の固定の読み込みに失敗しても script を外し、押し直して再試行できる', async (t) => {
    const { page } = await fixturePage(t, browser, payload(tableHtml));
    const routePattern = '**/_/lib/md-table-sticky/sticky-table.js';
    await page.route(routePattern, (route) => route.abort());
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForSelector('script[src$="/md-table-sticky/sticky-table.js"]', { state: 'detached' });
    await waitSticky(page, false);
    await page.unroute(routePattern);
    await page.locator('#gp-sticky-toggle').click();
    await waitSticky(page, true);
  });

  await t.test('表の無い文書でも表の固定を切り替えられ、後から描いた表に当たる', async (t) => {
    const fixture = await fixturePage(t, browser, payload('<p>本文だけ</p>'));
    const { page } = fixture;
    const toggle = page.locator('#gp-sticky-toggle');
    await toggle.click();
    await page.waitForFunction(() => Boolean(window.__mdTableSticky) &&
      document.querySelector('#gp-sticky-toggle').getAttribute('aria-pressed') === 'true');
    assert.equal(await page.locator('[data-md-table-sticky-bar]').count(), 0);
    await fixture.send('render', payload('<p>本文</p>' + tableHtml, { rev: 2 }));
    await waitSticky(page, true);
    await toggle.click();
    await waitSticky(page, false);
  });

  await t.test('本文・コード配色の目印・details・ページ内リンクを保つ', async (t) => {
    const html =
      '<h1 id="user-content-heading">見出し</h1><a href="#heading">移動</a>' +
      '<details><summary>詳細</summary><p>本文</p></details><pre class="highlight"><code>const a = 1</code></pre>';
    const fixture = await fixturePage(t, browser, payload(html));
    const { page } = fixture;
    assert.equal(await page.locator('#gp-content h1').textContent(), '見出し');
    assert.equal(
      await page.locator('.highlight').evaluate((el) => el.classList.contains('code-syntax-highlight-theme')),
      true,
    );
    assert.equal(
      await page.locator('body').evaluate((el) => el.classList.contains('code-syntax-highlight-theme')),
      false,
    );
    await page.locator('summary').click();
    await fixture.send('render', payload(html, { rev: 2 }));
    await page.waitForFunction(() => document.querySelector('details').open);
    await page.locator('a[href="#heading"]').click();
    assert.equal(await page.locator('details').evaluate((el) => el.open), true);
  });

  await t.test('配色と本文幅を切り替え、再読込でも保持する', async (t) => {
    const fixture = await fixturePage(t, browser);
    const { page } = fixture;
    await page.waitForFunction(() => document.documentElement.dataset.style === 'fallback');
    assert.ok((await page.locator('#gp-page').evaluate((el) => el.getBoundingClientRect().width)) > 1600);
    await page.selectOption('#gp-width', 'fixed');
    assert.equal(await page.locator('#gp-page').evaluate((el) => el.getBoundingClientRect().width), 1600);
    await page.selectOption('#gp-theme', 'dark');
    await page.waitForFunction(() => getComputedStyle(document.body).backgroundColor === 'rgb(31, 30, 36)');
    await page.selectOption('#gp-theme', 'light');
    await page.waitForFunction(() => getComputedStyle(document.body).backgroundColor === 'rgb(255, 255, 255)');
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.selectOption('#gp-theme', 'system');
    await page.waitForFunction(() => document.documentElement.classList.contains('gl-dark'));
    await Promise.all([
      page.waitForEvent('domcontentloaded'),
      Promise.resolve().then(() => fixture.send('reload', {})),
    ]);
    await page.waitForFunction(() => document.documentElement.dataset.style === 'fallback');
    assert.equal(await page.locator('#gp-width').inputValue(), 'fixed');
    assert.equal(await page.locator('#gp-theme').inputValue(), 'system');
  });

  await t.test('本物の KaTeX と mermaid をローカルから読み、テーマ変更は図だけを更新する', async (t) => {
    const fixture = await fixturePage(
      t,
      browser,
      payload(
        '<details open><summary>詳細</summary><p>本文</p></details>' +
          '<code class="js-render-math" data-math-style="inline">x^2 + y^2</code>' +
          '<pre data-sourcepos="2:1-5:1"><code class="js-render-mermaid">graph TD; A--&gt;B</code></pre>',
      ),
    );
    const { page } = fixture;
    await page.waitForSelector('.katex');
    await page.waitForSelector('.gp-mermaid svg');
    assert.ok((await page.locator('.gp-mermaid svg').evaluate((el) => el.getBoundingClientRect().width)) > 0);
    await page.evaluate(() => {
      window.gpOriginalDetails = document.querySelector('details');
      window.gpOldSvg = document.querySelector('.gp-mermaid svg');
    });
    await page.selectOption('#gp-theme', 'dark');
    await page.waitForFunction(() => document.querySelector('.gp-mermaid svg') !== window.gpOldSvg);
    assert.equal(
      await page.evaluate(
        () => window.gpOriginalDetails === document.querySelector('details') && window.gpOriginalDetails.open,
      ),
      true,
    );
    assert.equal(await page.locator('script[src*="/_/lib/"]').count(), 2);
  });

  await t.test('誤りはバナーとエスケープした原文で表示する', async (t) => {
    const { page } = await fixturePage(t, browser, {
      file: 'error.md',
      dir: '',
      rev: 1,
      error: '描画が時間切れになりました',
      markdown: '<img src="https://example.invalid/leak">\n原文',
    });
    assert.match(await page.locator('#gp-banner').textContent(), /時間切れ/);
    assert.equal(await page.locator('.gp-raw').textContent(), '<img src="https://example.invalid/leak">\n原文');
    assert.equal(await page.locator('#gp-content img').count(), 0);
  });

  await t.test('幅・フォント・画面の高さの変更後もスクロールを合わせ直す', async (t) => {
    const html = Array.from(
      { length: 120 },
      (_, i) =>
        '<p id="line-' +
        (i + 1) +
        '" data-sourcepos="' +
        (i + 1) +
        ':1-' +
        (i + 1) +
        ':1">' +
        '日本語の長い文章で折り返し位置を確かめる。'.repeat(12) +
        '</p>',
    ).join('');
    const fixture = await fixturePage(t, browser, payload(html));
    const { page } = fixture;
    const aligned = () =>
      page.waitForFunction(() => {
        const header = document.querySelector('#gp-header').offsetHeight;
        const banner = document.querySelector('#gp-banner');
        const top = header + (banner.hidden ? 0 : banner.offsetHeight);
        return (
          Math.abs(document.querySelector('#line-70').getBoundingClientRect().top - (top + (innerHeight - top) * 0.5)) <
          2
        );
      });
    await fixture.send('scroll', { line: 70, ratio: 0.5, lines: 120 });
    await aligned();
    await page.selectOption('#gp-width', 'fixed');
    await aligned();
    await page.evaluate(() => {
      document.querySelector('#gp-content').style.fontSize = '24px';
      document.fonts.dispatchEvent(new Event('loadingdone'));
    });
    await aligned();
    await page.setViewportSize({ width: 500, height: 700 });
    await aligned();
  });

  await t.test('相対画像とルートからのリンクを token 内へ戻す', async (t) => {
    const fixture = await fixturePage(
      t,
      browser,
      payload(
        '<p><img id="relative" src="画像.svg"></p><a id="local" href="/readme.md">文書</a><a id="fragment" href="#heading">見出し</a><a id="external" href="https://example.invalid/">外部</a><a id="query" href="?list">クエリだけ</a><a id="self" href="試験.md#heading">今の文書の見出し</a><a id="self-root" href="/docs/%E8%A9%A6%E9%A8%93.md">今の文書</a>',
      ),
      (root) => {
        mkdirSync(join(root, 'docs'), { recursive: true });
        writeFileSync(
          join(root, 'docs', '画像.svg'),
          '<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1"/></svg>',
        );
      },
    );
    const { page, server } = fixture;
    await page.waitForFunction(() => document.querySelector('#relative').naturalWidth === 1);
    assert.equal(
      await page.locator('#relative').getAttribute('src'),
      server.url + 'docs/' + encodeURIComponent('画像.svg'),
    );
    assert.equal(await page.locator('#local').getAttribute('href'), new URL('?doc=readme.md', server.url).href);
    assert.equal(await page.locator('#fragment').getAttribute('href'), '#heading');
    assert.equal(await page.locator('#external').getAttribute('href'), 'https://example.invalid/');
    // クエリだけのリンクは、ファイルへのリンクとして書き換えない
    assert.equal(await page.locator('#query').getAttribute('href'), '?list');
    // 今の文書そのものへのリンクは、ページ内の移動にする (?doc= へ移らない)
    assert.equal(await page.locator('#self').getAttribute('href'), '#heading');
    assert.equal(await page.locator('#self-root').getAttribute('href'), '#');
    assert.equal(await page.locator('#self').getAttribute('target'), null);
  });

  await t.test('終了通知を単体アプリの言葉で表示する', async (t) => {
    const fixture = await fixturePage(t, browser);
    await fixture.server.close('exit');
    await fixture.page.waitForFunction(
      () => document.querySelector('#gp-banner').textContent === 'プレビューが終了した',
    );
  });
  await t.test('ダブルクリックで最も近い開始行へジャンプし、文字の選択を妨げない', async (t) => {
    const fixture = await fixturePage(
      t,
      browser,
      payload(
        '<div data-sourcepos="4:1-12:1"><p data-sourcepos="8:1-9:5"><span id="target">選択できる文字</span></p></div>',
      ),
      () => {},
      { jump: true },
    );
    const { page } = fixture;
    await page.evaluate(() => {
      window.gpDefaultPrevented = null;
      document.querySelector('#gp-content').addEventListener('dblclick', (event) => {
        window.gpDefaultPrevented = event.defaultPrevented;
      });
    });
    const response = page.waitForResponse(
      (value) => value.url().endsWith('/_/jump') && value.request().method() === 'POST',
    );
    await page.locator('#target').dblclick();
    assert.equal((await response).status(), 204);
    await fixture.server.sync();
    assert.deepEqual(fixture.jumps, [{ line: 8 }]);
    assert.deepEqual(fixture.jumpRequests, [{ method: 'POST', body: { line: 8 } }]);
    assert.equal(await page.evaluate(() => window.gpDefaultPrevented), false);
  });

  await t.test('ジャンプが無効なら送信せず、操作する要素の上でも送信しない', async (t) => {
    const disabled = await fixturePage(t, browser, payload('<p id="disabled" data-sourcepos="5:1-5:5">本文</p>'));
    await disabled.page.locator('#disabled').dblclick();
    await disabled.page.waitForTimeout(100);
    assert.deepEqual(disabled.jumpRequests, []);
    const enabled = await fixturePage(
      t,
      browser,
      payload(
        '<div data-sourcepos="10:1-20:1"><a id="link" href="#link">リンク</a><details><summary id="summary">開閉</summary></details><input id="input"><button id="button">ボタン</button><video id="video"></video><audio id="audio"></audio></div><p id="plain">行番号のない本文</p>',
      ),
      () => {},
      { jump: true },
    );
    for (const id of ['link', 'summary', 'input', 'button', 'video', 'audio', 'plain']) {
      await enabled.page.locator('#' + id).dispatchEvent('dblclick');
    }
    await enabled.page.waitForTimeout(100);
    assert.deepEqual(enabled.jumpRequests, []);
    assert.deepEqual(enabled.jumps, []);
  });

  await t.test('休止中の表示は、新しい render が届いたら接続を開き直さずに消える', async (t) => {
    const fixture = await fixturePage(t, browser);
    await fixture.send('stop', { reason: 'stop' });
    await fixture.page.waitForFunction(() => document.querySelector('#gp-banner').textContent === 'プレビューを止めた');
    await fixture.send('render', payload('<p>再開した本文</p>', { rev: 2 }));
    await fixture.page.waitForFunction(
      () =>
        document.querySelector('#gp-banner').hidden &&
        document.querySelector('#gp-content').textContent === '再開した本文',
    );
  });

  await t.test('Markdown のリンクを同じタブで開き、戻る・進むで文書を移動する', async (t) => {
    const fixture = await fixturePage(
      t,
      browser,
      undefined,
      (root) => {
        mkdirSync(join(root, 'docs'));
        writeFileSync(
          join(root, 'README.md'),
          '# Home\n\n[次の文書](docs/next.MARKDOWN#target)\n\n[ルート外](../outside.md)\n\n[PDF](manual.pdf)\n\n[百分率](100%.md)',
        );
        writeFileSync(join(root, '100%.md'), '# 百分率');
        writeFileSync(join(root, 'docs', 'next.MARKDOWN'), '# Target\n\n次の本文');
      },
      { live: true },
    );
    const { page, server } = fixture;
    const next = page.locator('#gp-content a').filter({ hasText: '次の文書' });
    assert.equal(await next.getAttribute('target'), null);
    assert.equal(
      new URL(await page.locator('#gp-content a').filter({ hasText: '百分率' }).getAttribute('href')).searchParams.get(
        'doc',
      ),
      '100%.md',
    );
    assert.equal(new URL(await next.getAttribute('href')).searchParams.get('doc'), 'docs/next.MARKDOWN');
    assert.equal(await page.locator('#gp-content a').filter({ hasText: 'ルート外' }).getAttribute('href'), null);
    assert.equal(await page.locator('#gp-content a').filter({ hasText: 'PDF' }).getAttribute('target'), '_blank');
    await next.click();
    await page.waitForFunction(() => document.querySelector('#gp-file').textContent === 'docs/next.MARKDOWN');
    assert.equal(new URL(page.url()).hash, '#target');
    assert.equal(await page.context().pages().length, 1);
    assert.equal(await page.locator('#gp-back').isVisible(), true);
    await page.goBack();
    await page.waitForFunction(() => document.querySelector('#gp-content h1')?.textContent.includes('Home'));
    assert.equal(page.url(), server.url);
    await page.goForward();
    await page.waitForFunction(() => document.querySelector('#gp-content h1')?.textContent.includes('Target'));
  });

  await t.test('フォルダーの一覧は SSE を使わず、入れ子のファイル名から文書を開く', async (t) => {
    const fixture = await fixturePage(
      t,
      browser,
      undefined,
      (root) => {
        mkdirSync(join(root, 'docs'));
        writeFileSync(join(root, 'README.md'), '# 最初の文書');
        writeFileSync(join(root, 'docs', 'a.md'), '# 一覧から開いた文書');
      },
      { live: true, directory: true },
    );
    const { page } = fixture;
    await page.waitForSelector('.gp-document-list a');
    assert.equal(fixture.eventRequests.length, 0);
    assert.equal(await page.locator('#gp-sticky-toggle').isVisible(), false);
    assert.equal(await page.locator('#gp-back').isVisible(), false);
    assert.ok((await page.locator('.gp-document-list ul a').count()) > 0);
    await page.locator('.gp-document-list a').filter({ hasText: 'a.md' }).click();
    await page.waitForFunction(() => document.querySelector('#gp-file').textContent === 'docs/a.md');
    assert.match(await page.locator('#gp-content').textContent(), /一覧から開いた文書/);
    assert.equal(await page.locator('#gp-back').isVisible(), false);
  });

  await t.test('見出しを安全な文字で表示し、開閉を保存し、押した見出しを目印にする', async (t) => {
    const headings = [
      { level: 1, id: 'first', text: '<img src="https://example.invalid/">' },
      { level: 2, id: 'second', text: '二番目' },
      { level: 3, id: 'hidden', text: '折りたたんだ見出し' },
    ];
    const html =
      '<h1 id="user-content-first">最初</h1><p>' +
      '本文。'.repeat(10000) +
      '</p><h2 id="user-content-second">二番目</h2><details><summary>開閉</summary><h3 id="user-content-hidden">折りたたんだ見出し</h3></details>';
    const { page } = await fixturePage(t, browser, payload(html, { headings }));
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    assert.equal(await page.locator('#gp-outline img').count(), 0);
    assert.equal(await page.locator('#gp-outline a').first().textContent(), headings[0].text);
    assert.equal(
      await page
        .locator('#gp-outline-list li')
        .nth(1)
        .evaluate((el) => el.style.paddingInlineStart),
      '12px',
    );
    await page.locator('#gp-outline-toggle').click();
    assert.equal(await page.locator('#gp-outline').isVisible(), false);
    await page.reload();
    await page.waitForFunction(() => document.querySelector('#gp-outline-toggle').disabled === false);
    assert.equal(await page.locator('#gp-outline').isVisible(), false);
    await page.locator('#gp-outline-toggle').click();
    await page.locator('#gp-outline a').nth(1).click();
    await page.waitForFunction(() => document.querySelectorAll('#gp-outline a')[1].hasAttribute('aria-current'));
    assert.equal(await page.locator('#gp-outline a[aria-current]').count(), 1);
    assert.equal(await page.locator('#gp-outline').evaluate((el) => getComputedStyle(el).position), 'fixed');
    assert.equal(await page.locator('#gp-outline').evaluate((el) => getComputedStyle(el).overflowY), 'auto');
  });

  const outlineRender = () => payload('<h1 id="user-content-title">題</h1><p>本文</p>', {
    headings: [{ level: 1, id: 'title', text: '題' }],
  });
  const waitOutlineWidth = (page, width) => page.waitForFunction((width) =>
    document.querySelector('#gp-outline').getBoundingClientRect().width === width &&
    parseFloat(getComputedStyle(document.querySelector('#gp-layout')).marginLeft) === width &&
    document.querySelector('#gp-outline-resizer').getAttribute('aria-valuenow') === String(width), width);
  const savedOutlineWidth = (page) => page.evaluate(() =>
    JSON.parse(sessionStorage.getItem('gitlab-preview-appearance') || '{}').outlineWidth);
  const startOutlineDrag = async (page, delta) => {
    const resizer = page.locator('#gp-outline-resizer');
    const box = await resizer.boundingBox();
    assert.ok(box, 'つまみが見える');
    await resizer.evaluate((element) => element.addEventListener('pointerdown', (event) => {
      element.dataset.testPointer = String(event.pointerId);
    }, { once: true }));
    const x = box.x + box.width / 2, y = box.y + 30;
    await page.mouse.move(x, y);
    await page.mouse.down();
    await page.mouse.move(x + delta, y, { steps: 5 });
  };
  const dragOutline = async (page, delta) => {
    await startOutlineDrag(page, delta);
    await page.mouse.up();
  };

  await t.test('見出し欄のつまみの属性・ドラッグ・再読み込み・キーと初期化', async (t) => {
    const { page } = await fixturePage(t, browser, outlineRender());
    const resizer = page.locator('#gp-outline-resizer');
    await waitOutlineWidth(page, 260);
    for (const [name, value] of Object.entries({ role: 'separator', 'aria-orientation': 'vertical',
      'aria-controls': 'gp-outline', 'aria-label': '見出し欄の幅', tabindex: '0',
      'aria-valuemin': '180', 'aria-valuemax': '480', 'aria-valuetext': '260 ピクセル' }))
      assert.equal(await resizer.getAttribute(name), value);
    await page.selectOption('#gp-width', 'fixed');
    await dragOutline(page, 100);
    await waitOutlineWidth(page, 360);
    assert.equal(await savedOutlineWidth(page), 360);
    assert.equal(await resizer.getAttribute('aria-valuetext'), '360 ピクセル');
    await page.reload();
    await waitOutlineWidth(page, 360);
    await resizer.focus();
    for (const [key, width] of [['ArrowLeft', 350], ['Shift+ArrowLeft', 300], ['ArrowRight', 310],
      ['Shift+ArrowRight', 360], ['Home', 180], ['End', 480]]) {
      await page.keyboard.press(key);
      await waitOutlineWidth(page, width);
      assert.equal(await savedOutlineWidth(page), width);
    }
    // 修飾キー付きの矢印・Home は、ブラウザの操作に任せて幅を変えない。
    // Alt+← は試験のページを戻してしまうので、同じ判定を通る Ctrl・Meta で確かめる。
    for (const key of ['Control+ArrowLeft', 'Control+Home', 'Control+Shift+ArrowLeft', 'Meta+ArrowLeft']) {
      await resizer.evaluate((element) => element.addEventListener('keydown', (event) => {
        setTimeout(() => { element.dataset.testPrevented = String(event.defaultPrevented); });
      }, { once: true }));
      await page.keyboard.press(key);
      await page.waitForFunction(() => document.querySelector('#gp-outline-resizer').dataset.testPrevented !== undefined);
      assert.equal(await resizer.evaluate((element) => {
        const value = element.dataset.testPrevented;
        delete element.dataset.testPrevented;
        return value;
      }), 'false', key);
      assert.equal(await resizer.getAttribute('aria-valuenow'), '480', key);
      assert.equal(await savedOutlineWidth(page), 480, key);
    }
    await page.keyboard.press('Enter');
    await waitOutlineWidth(page, 260);
    assert.equal(await savedOutlineWidth(page), undefined);
    assert.equal(await page.evaluate(() => JSON.parse(sessionStorage.getItem('gitlab-preview-appearance')).width), 'fixed');
    await dragOutline(page, 1200);
    await waitOutlineWidth(page, 480);
    assert.equal(await savedOutlineWidth(page), 480);
    await dragOutline(page, -1200);
    await waitOutlineWidth(page, 180);
    assert.equal(await savedOutlineWidth(page), 180);
    await resizer.dblclick();
    await waitOutlineWidth(page, 260);
    assert.equal(await savedOutlineWidth(page), undefined);
    await resizer.focus();
    await page.keyboard.press('ArrowRight');
    assert.equal(await savedOutlineWidth(page), 270);
    await resizer.dblclick();
    assert.equal(await savedOutlineWidth(page), undefined);
    await dragOutline(page, 40);
    await waitOutlineWidth(page, 300);
    await page.locator('#gp-outline-toggle').click();
    assert.equal(await resizer.isVisible(), false);
    // 開き直すと、つまみと変えた幅が戻る。
    await page.locator('#gp-outline-toggle').click();
    await waitOutlineWidth(page, 300);
    assert.equal(await resizer.isVisible(), true);
    assert.equal(await savedOutlineWidth(page), 300);
  });

  await t.test('見出し欄のドラッグを Esc・pointercancel・捕捉の喪失で取り消し、変更なしでは保存しない', async (t) => {
    const { page } = await fixturePage(t, browser, outlineRender());
    await waitOutlineWidth(page, 260);
    await dragOutline(page, 0);
    assert.equal(await savedOutlineWidth(page), undefined);
    await dragOutline(page, 100);
    for (const cancel of ['Escape', 'pointercancel', 'lostpointercapture']) {
      await startOutlineDrag(page, 80);
      await waitOutlineWidth(page, 440);
      assert.equal(await savedOutlineWidth(page), 360);
      // ドラッグ中は文字の選択を止める印が付く。
      assert.equal(await page.locator('html').getAttribute('data-outline-resizing'), '');
      if (cancel === 'Escape') await page.keyboard.press('Escape');
      else await page.locator('#gp-outline-resizer').evaluate((element, cancel) => {
        const id = Number(element.dataset.testPointer);
        if (cancel === 'lostpointercapture') element.releasePointerCapture(id);
        else element.dispatchEvent(new PointerEvent(cancel, { pointerId: id, bubbles: true }));
      }, cancel);
      await waitOutlineWidth(page, 360);
      assert.equal(await savedOutlineWidth(page), 360);
      assert.equal(await page.locator('html').getAttribute('data-outline-resizing'), null);
      await page.mouse.up();
      assert.equal(await savedOutlineWidth(page), 360);
    }
    // ドラッグ中に画面が狭くなってつまみが隠れたら、開始時の幅へ戻して保存しない。
    await startOutlineDrag(page, 80);
    await waitOutlineWidth(page, 440);
    await page.setViewportSize({ width: 650, height: 900 });
    await page.waitForFunction(() => document.querySelector('#gp-outline-resizer').hidden);
    assert.equal(await page.locator('html').getAttribute('data-outline-resizing'), null);
    await page.mouse.up();
    assert.equal(await savedOutlineWidth(page), 360);
    await page.setViewportSize({ width: 2000, height: 900 });
    await waitOutlineWidth(page, 360);
  });

  await t.test('見出し欄の保存値を検査し、狭い画面の上限と 42vw を保つ', async (t) => {
    const { page } = await fixturePage(t, browser, outlineRender());
    for (const raw of ['{"outlineWidth":"300"}', '{"outlineWidth":1e999}', '{"outlineWidth":-1}',
      '{"outlineWidth":179}', '{"outlineWidth":481}', '{"outlineWidth":null}', '{壊れた JSON']) {
      await page.evaluate((raw) => sessionStorage.setItem('gitlab-preview-appearance', raw), raw);
      await page.reload();
      await waitOutlineWidth(page, 260);
    }
    await page.evaluate(() => sessionStorage.setItem('gitlab-preview-appearance', '{"outlineWidth":300.4}'));
    await page.reload();
    await waitOutlineWidth(page, 300);
    await page.evaluate(() => sessionStorage.setItem('gitlab-preview-appearance', '{"outline":true,"outlineWidth":450}'));
    await page.reload();
    await waitOutlineWidth(page, 450);
    await page.setViewportSize({ width: 720, height: 900 });
    await waitOutlineWidth(page, 400);
    assert.equal(await page.locator('#gp-outline-resizer').getAttribute('aria-valuemax'), '400');
    assert.equal(await savedOutlineWidth(page), 450);
    // 上限に張り付いて見た目が変わらないキーでは、広い画面のための保存値を変えない。
    await page.locator('#gp-outline-resizer').focus();
    for (const key of ['ArrowRight', 'Shift+ArrowRight', 'End']) {
      await page.keyboard.press(key);
      assert.equal(await page.locator('#gp-outline-resizer').getAttribute('aria-valuenow'), '400', key);
      assert.equal(await savedOutlineWidth(page), 450, key);
    }
    await page.setViewportSize({ width: 650, height: 900 });
    await page.waitForFunction(() => document.querySelector('#gp-outline-resizer').hidden);
    assert.equal(await page.locator('#gp-outline-resizer').isVisible(), false);
    assert.equal(await page.locator('#gp-outline').evaluate((el) => el.getBoundingClientRect().width), 273);
    assert.equal(await page.locator('#gp-layout').evaluate((el) => getComputedStyle(el).marginLeft), '273px');
    await page.locator('#gp-outline-resizer').dispatchEvent('keydown', { key: 'End' });
    assert.equal(await savedOutlineWidth(page), 450);
    await page.setViewportSize({ width: 1200, height: 900 });
    await waitOutlineWidth(page, 450);
  });

  await t.test('段落とセルの入力中に見出し欄を操作しても確定せず、ドラッグの Esc で入力を消さない', async (t) => {
    const original = '# 題\n\n編集する段落。\n\n| 列 |\n| --- |\n| 値 |\n';
    for (const [name, selector] of [['段落', '#gp-content p'], ['セル', '#gp-content td']]) await t.test(name, async (t) => {
      const { page, file } = await editablePage(t, browser, original);
      await waitOutlineWidth(page, 260);
      const sent = [];
      page.on('request', (request) => {
        if (request.method() === 'POST') sent.push(request.url());
      });
      await page.locator(selector).dblclick();
      const input = page.locator('#gp-edit-input');
      await input.fill('入力中の値');
      await page.locator('#gp-outline-resizer').click();
      assert.equal(await input.isVisible(), true);
      assert.equal(await input.evaluate((el) => el === document.activeElement), true);
      await dragOutline(page, 100);
      await waitOutlineWidth(page, 360);
      assert.equal(await input.inputValue(), '入力中の値');
      assert.equal(await input.evaluate((el) => el === document.activeElement), true);
      await startOutlineDrag(page, 60);
      await waitOutlineWidth(page, 420);
      await page.keyboard.press('Escape');
      await page.mouse.up();
      await waitOutlineWidth(page, 360);
      assert.equal(await input.isVisible(), true);
      assert.equal(await input.inputValue(), '入力中の値');
      assert.equal(await input.evaluate((el) => el === document.activeElement), true);
      assert.equal(await savedOutlineWidth(page), 360);
      // 次のフレームまで進め、遅れて編集要求が出ていないことも確かめる。
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.deepEqual(sent, []);
      assert.equal(readFileSync(file, 'utf8'), original);
    });
  });

  await t.test('コメントモードで見出し欄のつまみを押しても対象を選ばない', async (t) => {
    const original = '# 題\n\nコメントする段落。\n';
    const { page } = await fixturePage(t, browser, undefined,
      (root) => writeFileSync(join(root, 'README.md'), original), { live: true, review: true });
    await page.locator('#gp-review-toggle').click();
    await page.waitForFunction(() => document.querySelector('#gp-review-layer').dataset.render === String(window.gpPreview.current().render));
    const label = await page.locator('#gp-review-target').textContent();
    await page.locator('#gp-outline-resizer').click();
    await dragOutline(page, 100);
    await waitOutlineWidth(page, 360);
    assert.equal(await page.locator('#gp-review-target').textContent(), label);
    assert.equal(await page.locator('#gp-review-add').isDisabled(), true);
    await page.locator('#gp-content p').click();
    const selected = await page.locator('#gp-review-target').textContent();
    await page.locator('#gp-outline-resizer').click();
    assert.equal(await page.locator('#gp-review-target').textContent(), selected);
  });

  await t.test('コメント欄を開いている間は、その幅も本文に残し、つまみを欄の下に入れない', async (t) => {
    const original = '# 題\n\nコメントする段落。\n';
    const { page } = await fixturePage(t, browser, undefined,
      (root) => writeFileSync(join(root, 'README.md'), original), { live: true, review: true });
    await page.setViewportSize({ width: 1000, height: 900 });
    await page.evaluate(() => sessionStorage.setItem('gitlab-preview-appearance', '{"outline":true,"outlineWidth":400}'));
    await page.reload();
    await waitOutlineWidth(page, 400);
    const resizer = page.locator('#gp-outline-resizer');
    const apart = async () => {
      const handle = await resizer.boundingBox(), panel = await page.locator('#gp-review-panel').boundingBox();
      return handle.x + handle.width <= panel.x;
    };
    await page.locator('#gp-review-toggle').click();
    // 1000 − 320 − 360 = 320 px が上限になる。保存値は広い画面のために残す。
    await waitOutlineWidth(page, 320);
    assert.equal(await resizer.getAttribute('aria-valuemax'), '320');
    assert.equal(await apart(), true);
    await dragOutline(page, 200);
    await waitOutlineWidth(page, 320);
    await resizer.focus();
    await page.keyboard.press('ArrowRight');
    assert.equal(await savedOutlineWidth(page), 400);
    await page.keyboard.press('ArrowLeft');
    await waitOutlineWidth(page, 310);
    assert.equal(await savedOutlineWidth(page), 310);
    await page.locator('#gp-review-toggle').click();
    await waitOutlineWidth(page, 310);
    assert.equal(await resizer.getAttribute('aria-valuemax'), '480');
    await dragOutline(page, 170);
    await waitOutlineWidth(page, 480);
    // 狭い画面では最小の 180 px にとどめ、つまみは欄の左に残す。
    await page.setViewportSize({ width: 760, height: 900 });
    await page.locator('#gp-review-toggle').click();
    await waitOutlineWidth(page, 180);
    assert.equal(await apart(), true);
    assert.equal(await page.locator('#gp-layout').evaluate((el) => el.getBoundingClientRect().width), 760 - 180 - 360);
    assert.equal(await savedOutlineWidth(page), 480);
  });

  await t.test('表の全幅表示はつまみを覆い、全幅表示中にドラッグを始められない', async (t) => {
    const { page } = await editablePage(t, browser, '# 題\n\n| 列 |\n| --- |\n| 値 |\n');
    await waitOutlineWidth(page, 260);
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForSelector('[data-md-table-sticky-bar]');
    await page.locator('[data-md-table-sticky-action="float"]').click();
    await page.waitForSelector('[data-md-table-sticky-float-panel]');
    const box = await page.locator('#gp-outline-resizer').boundingBox();
    assert.equal(await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.id,
      { x: box.x + box.width / 2, y: box.y + 30 }) === 'gp-outline-resizer', false);
  });

  await t.test('見出しの既定は画面幅に従い、見出しのない文書では欄を隠す', async (t) => {
    const fixture = await fixturePage(
      t,
      browser,
      payload('<h1 id="user-content-title">題</h1>', { headings: [{ level: 1, id: 'title', text: '題' }] }),
    );
    const { page } = fixture;
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    await page.setViewportSize({ width: 800, height: 700 });
    await page.waitForFunction(() => document.querySelector('#gp-outline').hidden);
    await page.setViewportSize({ width: 1200, height: 700 });
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    await fixture.send('render', payload('<p>見出しのない本文</p>', { headings: [], rev: 2 }));
    await page.waitForFunction(
      () => document.querySelector('#gp-outline').hidden && document.querySelector('#gp-outline-toggle').disabled,
    );
    assert.equal(await page.locator('#gp-outline-resizer').isVisible(), false);
    await page.goto(fixture.server.url + '?list');
    assert.equal(await page.locator('#gp-outline-resizer').isVisible(), false);
  });

  await t.test('見出し欄の出し入れの後も、本文幅・配色とスクロールの同期を保つ', async (t) => {
    const html =
      '<h1 id="user-content-title" data-sourcepos="1:1-1:1">題</h1>' +
      Array.from(
        { length: 120 },
        (_, i) =>
          '<p id="line-' +
          (i + 2) +
          '" data-sourcepos="' +
          (i + 2) +
          ':1-' +
          (i + 2) +
          ':1">' +
          '折り返しのある本文。'.repeat(50) +
          '</p>',
      ).join('');
    const fixture = await fixturePage(t, browser, payload(html, { headings: [{ level: 1, id: 'title', text: '題' }] }));
    const { page } = fixture;
    const aligned = () =>
      page.waitForFunction(() => {
        const header = document.querySelector('#gp-header').offsetHeight;
        const banner = document.querySelector('#gp-banner');
        const top = header + (banner.hidden ? 0 : banner.offsetHeight);
        return (
          Math.abs(document.querySelector('#line-70').getBoundingClientRect().top - (top + (innerHeight - top) * 0.5)) <
          2
        );
      });
    await fixture.send('scroll', { line: 70, ratio: 0.5, lines: 121 });
    await aligned();
    await page.locator('#gp-outline-toggle').click();
    await aligned();
    await page.locator('#gp-outline-toggle').click();
    await aligned();
    await dragOutline(page, 100);
    await waitOutlineWidth(page, 360);
    await aligned();
    await page.selectOption('#gp-width', 'fixed');
    await aligned();
    await page.selectOption('#gp-theme', 'dark');
    await aligned();
  });

  await t.test('印刷では操作欄と見出し欄を隠し、本文を全幅にする', async (t) => {
    const { page } = await fixturePage(
      t,
      browser,
      payload('<h1 id="user-content-title">題</h1><pre>code</pre><table><tr><td>表</td></tr></table>', {
        headings: [{ level: 1, id: 'title', text: '題' }],
      }),
    );
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    await page.emulateMedia({ media: 'print' });
    assert.equal(await page.locator('#gp-header').isVisible(), false);
    assert.equal(await page.locator('#gp-outline').isVisible(), false);
    assert.equal(await page.locator('#gp-outline-resizer').isVisible(), false);
    assert.equal(await page.locator('#gp-layout').evaluate((el) => getComputedStyle(el).marginLeft), '0px');
    assert.equal(
      await page.locator('#gp-page').evaluate((el) => el.getBoundingClientRect().width),
      await page.evaluate(() => innerWidth),
    );
    assert.equal(await page.locator('pre').evaluate((el) => getComputedStyle(el).breakInside), 'avoid');
    assert.equal(await page.locator('table').evaluate((el) => getComputedStyle(el).breakInside), 'avoid');
  });

  await t.test('違いを種類ごとに安全な文字で表示し、開閉を保存して行へ移る', async (t) => {
    const notices = [
      { kind: 'reference', line: 12, text: '<img src="https://example.invalid/leak">' },
      { kind: 'diagram', line: 20, text: 'plantuml' },
      { kind: 'suggestion', line: 30, text: '提案の本文' },
      { kind: 'include', line: 40, text: '::include{file=missing.md}' },
      { kind: 'emoji', line: 50, text: ':not-an-emoji:' },
      { kind: 'missing', line: null, text: '行のない知らせ' },
      { kind: 'more', line: null, text: '15' },
    ];
    const html =
      '<h1 id="user-content-title">題</h1>' +
      Array.from(
        { length: 80 },
        (_, i) =>
          '<p id="notice-line-' +
          (i + 1) +
          '" data-sourcepos="' +
          (i + 1) +
          ':1-' +
          (i + 1) +
          ':1">' +
          '行の位置を確かめる本文。'.repeat(20) +
          '</p>',
      ).join('');
    const fixture = await fixturePage(
      t,
      browser,
      payload(html, { notices, headings: [{ level: 1, id: 'title', text: '題' }] }),
    );
    const { page } = fixture;
    assert.equal(await page.locator('#gp-notices-toggle').textContent(), 'GitLab との違い 7');
    assert.equal(await page.locator('#gp-notices').isVisible(), false);
    await page.locator('#gp-notices-toggle').click();
    assert.equal(await page.locator('#gp-notices img, #gp-notices script').count(), 0);
    assert.equal(
      await page.locator('#gp-notices [data-kind="reference"] button').textContent(),
      '12 行: ' + notices[0].text,
    );
    assert.match(
      await page.locator('#gp-notices [data-kind="reference"] h3').textContent(),
      /GitLab では参照のリンクになる書き方（手元ではリンクにしない）.*1件/,
    );
    assert.equal(await page.locator('#gp-notices [data-kind="missing"] button').count(), 0);
    assert.equal(await page.locator('#gp-notices [data-kind="missing"] li > span').textContent(), '行のない知らせ');
    assert.equal(await page.locator('#gp-notices [data-kind="more"] h3').textContent(), 'ほかに 15 件');
    assert.equal(await page.locator('#gp-notices [data-kind="more"] li').count(), 0);
    await page.reload();
    await page.waitForFunction(() => !document.querySelector('#gp-notices').hidden);
    await page.locator('#gp-notices [data-kind="reference"] button').click();
    await page.waitForFunction(() => {
      const top =
        document.querySelector('#gp-header').offsetHeight +
        document.querySelector('#gp-notices').offsetHeight +
        (document.querySelector('#gp-banner').hidden ? 0 : document.querySelector('#gp-banner').offsetHeight);
      return Math.abs(document.querySelector('#notice-line-12').getBoundingClientRect().top - top) < 2;
    });
  });

  await t.test('違いが0件・誤り・一覧の場合はボタンと欄を出さない', async (t) => {
    const fixture = await fixturePage(t, browser, payload('<p>本文</p>', { notices: [] }));
    const { page } = fixture;
    assert.equal(await page.locator('#gp-notices-toggle').isVisible(), false);
    await fixture.send(
      'render',
      payload('<p>本文</p>', { rev: 2, notices: [{ kind: 'emoji', line: null, text: ':none:' }] }),
    );
    await page.waitForFunction(() => !document.querySelector('#gp-notices-toggle').hidden);
    await page.locator('#gp-notices-toggle').click();
    await fixture.send('render', {
      file: 'error.md',
      dir: '',
      rev: 3,
      error: '試験の誤り',
      markdown: '原文',
      notices: [{ kind: 'reference', line: 1, text: '<script>本文</script>' }],
    });
    await page.waitForFunction(
      () => document.querySelector('#gp-notices-toggle').hidden && document.querySelector('#gp-notices').hidden,
    );
    await page.locator('#gp-list').click();
    await page.waitForFunction(() => document.querySelector('#gp-file').textContent === '一覧');
    assert.equal(await page.locator('#gp-notices-toggle').isVisible(), false);
    assert.equal(await page.locator('#gp-notices').isVisible(), false);
  });

  await t.test('違いの欄は40%までで独立してスクロールし、見出し・配色・幅・印刷と共存する', async (t) => {
    const notices = Array.from({ length: 100 }, (_, i) => ({
      kind: 'reference',
      line: i + 1,
      text: '参照の知らせ ' + i,
    }));
    const html =
      '<h1 id="user-content-title">題</h1>' +
      Array.from(
        { length: 120 },
        (_, i) =>
          '<p id="sync-line-' +
          (i + 1) +
          '" data-sourcepos="' +
          (i + 1) +
          ':1-' +
          (i + 1) +
          ':1">' +
          '長い本文で位置を確かめる。'.repeat(30) +
          '</p>',
      ).join('');
    const fixture = await fixturePage(
      t,
      browser,
      payload(html, { notices, headings: [{ level: 1, id: 'title', text: '題' }] }),
    );
    const { page } = fixture;
    const aligned = () =>
      page.waitForFunction(() => {
        const panel = document.querySelector('#gp-notices'),
          banner = document.querySelector('#gp-banner');
        const top =
          document.querySelector('#gp-header').offsetHeight +
          (panel.hidden ? 0 : panel.offsetHeight) +
          (banner.hidden ? 0 : banner.offsetHeight);
        return (
          Math.abs(
            document.querySelector('#sync-line-70').getBoundingClientRect().top - (top + (innerHeight - top) * 0.5),
          ) < 2
        );
      });
    await fixture.send('scroll', { line: 70, ratio: 0.5, lines: 120 });
    await aligned();
    await page.locator('#gp-notices-toggle').click();
    await aligned();
    assert.ok(
      await page.locator('#gp-notices').evaluate((el) => el.getBoundingClientRect().height <= innerHeight * 0.4 + 1),
    );
    assert.equal(await page.locator('#gp-notices').evaluate((el) => getComputedStyle(el).overflowY), 'auto');
    await page.locator('#gp-outline-toggle').click();
    await aligned();
    await page.selectOption('#gp-width', 'fixed');
    await aligned();
    await page.selectOption('#gp-theme', 'dark');
    await aligned();
    await page.locator('#gp-notices-toggle').click();
    await aligned();
    await page.locator('#gp-notices-toggle').click();
    await aligned();
    await page.emulateMedia({ media: 'print' });
    assert.equal(await page.locator('#gp-notices').isVisible(), false);
    assert.equal(await page.locator('#gp-header').isVisible(), false);
    assert.equal(await page.locator('#gp-outline').isVisible(), false);
  });

  await t.test('取り込めなかったファイルが現れ、変わると、本文と違いの知らせも更新される', async (t) => {
    const fixture = await fixturePage(
      t,
      browser,
      undefined,
      (root) => {
        writeFileSync(join(root, 'README.md'), '# 本文\n\n::include{file=part.md}');
      },
      { live: true },
    );
    const { page, root } = fixture;
    await page.waitForFunction(() => document.querySelector('#gp-notices-toggle').textContent === 'GitLab との違い 1');
    writeFileSync(join(root, 'part.md'), '現れた取り込み');
    await page.waitForFunction(
      () =>
        document.querySelector('#gp-content').textContent.includes('現れた取り込み') &&
        document.querySelector('#gp-notices-toggle').hidden,
    );
    writeFileSync(join(root, 'part.md'), '保存し直した取り込み');
    await page.waitForFunction(() =>
      document.querySelector('#gp-content').textContent.includes('保存し直した取り込み'),
    );
  });
});


test('水平線とブロック追加を本物のファイルで確かめる', { skip }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const open = async (page, selector) => {
    await page.locator(selector).dblclick();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
  };
  const commit = async (page) => {
    const response = page.waitForResponse((r) => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/_/edit'));
    await page.keyboard.press('Control+Enter');
    const result = await response;
    return result.status();
  };
  const separated = async (page) => {
    assert.equal(await page.evaluate(() => {
      const input = document.querySelector('#gp-edit-input').getBoundingClientRect();
      const toolbar = document.querySelector('#gp-edit-block-actions').getBoundingClientRect();
      const header = document.querySelector('#gp-header').getBoundingClientRect();
      return toolbar.top >= header.bottom && (toolbar.bottom <= input.top || toolbar.top >= input.bottom);
    }), true, '操作欄は入力欄とヘッダーに重ならない');
  };

  await t.test('水平線の余白を押して編集し、本文の木と配置を保つ', async (t) => {
    const { page, file } = await editablePage(t, browser, '前\n\n---\n\n後\n');
    await page.locator('#gp-edit-toggle').click();
    const before = await page.locator('#gp-content').evaluate((root) => ({ html: root.innerHTML,
      positions: [...root.children].map((node) => ({ top: node.offsetTop, height: node.offsetHeight })) }));
    await page.locator('#gp-edit-toggle').click();
    await editIdle(page);
    assert.deepEqual(await page.locator('#gp-content').evaluate((root) => ({ html: root.innerHTML,
      positions: [...root.children].map((node) => ({ top: node.offsetTop, height: node.offsetHeight })) })), before);
    await page.locator('#gp-content hr').hover();
    assert.equal(await page.evaluate(() => {
      const rule = document.querySelector('#gp-edit-hover').sheet.cssRules[0];
      return rule && /^#gp-content(?: > :nth-child\([1-9][0-9]*\))+$/.test(rule.selectorText) &&
        document.querySelector(rule.selectorText).localName === 'hr';
    }), true);
    const line = await page.locator('#gp-content hr').boundingBox();
    await page.mouse.dblclick(line.x + line.width / 2, line.y + 5);
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '---');
    await separated(page);
    await page.locator('#gp-edit-input').fill('***');
    assert.equal(await commit(page), 200);
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), '前\n\n***\n\n後\n');
  });

  for (const side of ['before', 'after']) for (const ending of ['\n', '\r\n']) await t.test(side + JSON.stringify(ending), async (t) => {
    const original = '前\n\n本文\n\n後\n'.replaceAll('\n', ending);
    const { page, file } = await editablePage(t, browser, original);
    await open(page, '#gp-content > p:nth-child(2)');
    await separated(page);
    await page.locator('#gp-edit-input').fill('本文の下書き');
    await page.locator('#gp-edit-' + side).click();
    assert.equal(await page.locator('#gp-edit-source-label').textContent(), '追加');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '');
    assert.equal(await page.locator('#gp-edit-block-actions').isVisible(), false);
    assert.equal(readFileSync(file, 'utf8'), original, '元の入力は確定しない');
    await page.locator('#gp-edit-input').fill('追加した段落');
    await page.keyboard.press('Alt+ArrowUp');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '追加した段落');
    assert.equal(await commit(page), 200);
    await editIdle(page);
    const want = (side === 'before' ? '前\n\n追加した段落\n\n本文\n\n後\n' : '前\n\n本文\n\n追加した段落\n\n後\n').replaceAll('\n', ending);
    assert.equal(readFileSync(file, 'utf8'), want);
    await open(page, '#gp-content > p:nth-child(' + (side === 'after' ? 2 : 3) + ')');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), '本文の下書き');
  });

  await t.test('追加の取り消しと空の外側クリックでは書かず、拒否した入力を残す', async (t) => {
    const original = '前\n\n後\n';
    const { page, file } = await editablePage(t, browser, original);
    await open(page, '#gp-content > p:first-child');
    await page.locator('#gp-edit-after').click();
    await page.locator('#gp-edit-input').fill('取り消す追加');
    await page.keyboard.press('Escape');
    assert.equal(readFileSync(file, 'utf8'), original);
    await open(page, '#gp-content > p:first-child');
    await page.locator('#gp-edit-after').click();
    await page.mouse.click(8, page.viewportSize().height - 8);
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    assert.equal(readFileSync(file, 'utf8'), original);
    await open(page, '#gp-content > p:first-child');
    await page.locator('#gp-edit-after').click();
    await page.locator('#gp-edit-input').fill(' \t');
    assert.equal(await commit(page), 422);
    assert.equal(await page.locator('#gp-edit-input').inputValue(), ' \t');
    assert.match(await page.locator('#gp-edit-status').textContent(), /追加する文を入力/);
    await page.locator('#gp-edit-input').fill('```');
    assert.equal(await commit(page), 422);
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').value === '```' && !document.querySelector('#gp-edit-input').hidden);
    assert.equal(await page.locator('#gp-edit-source-label').textContent(), '追加');
    assert.match(await page.locator('#gp-edit-status').textContent(), /前後のブロックに取り込まれます/);
    assert.equal(readFileSync(file, 'utf8'), original);
    await page.locator('#gp-edit-input').fill('外側クリックで追加');
    const response = page.waitForResponse((r) => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/_/edit'));
    await page.mouse.click(8, page.viewportSize().height - 8);
    assert.equal((await response).status(), 200);
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), '前\n\n外側クリックで追加\n\n後\n');
  });

  for (const side of ['before', 'after']) await t.test('項目からリスト全体の' + side + 'へ追加する', async (t) => {
    const original = '# 題\n\n- a\n- b\n\n後\n';
    const { page, file } = await editablePage(t, browser, original);
    await open(page, '#gp-content li:first-child');
    await separated(page);
    await page.locator('#gp-edit-' + side).click();
    const boxes = await page.evaluate(() => ({
      input: document.querySelector('#gp-edit-input').getBoundingClientRect().toJSON(),
      list: document.querySelector('#gp-content ul').getBoundingClientRect().toJSON(),
    }));
    if (side === 'after') assert.ok(boxes.input.top >= boxes.list.bottom);
    else assert.ok(boxes.input.bottom <= boxes.list.top);
    await page.locator('#gp-edit-input').fill('追加');
    assert.equal(await commit(page), 200);
    await editIdle(page);
    const want = side === 'before' ? '# 題\n\n追加\n\n- a\n- b\n\n後\n' : '# 題\n\n- a\n- b\n\n追加\n\n後\n';
    assert.equal(readFileSync(file, 'utf8'), want);
  });
});

test('数式と図の原文を編集し、描き直しと編集の枠を確かめる', { skip }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());
  const open = async (page, selector, source, tag) => {
    const response = page.waitForResponse((r) => new URL(r.url()).pathname.endsWith('/_/edit/source'));
    await page.locator(selector).dblclick();
    const result = await response;
    assert.equal(result.status(), 200);
    const target = new URL(result.url()).searchParams;
    assert.equal(target.get('tag'), tag);
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), source);
    return { tag, range: target.get('range') };
  };
  const commit = async (page, source) => {
    await page.locator('#gp-edit-input').fill(source);
    const response = page.waitForResponse((r) => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/_/edit'));
    await page.keyboard.press('Control+Enter');
    assert.equal((await response).status(), 200);
    await editIdle(page);
  };
  const hover = async (page, selector, expected, direct = false) => {
    if (direct) await page.locator(selector).dispatchEvent('pointerover');
    else await page.locator(selector).hover();
    assert.equal(await page.evaluate((expected) => {
      const style = document.querySelector('#gp-edit-hover');
      const rule = style.sheet.cssRules[0];
      return style.sheet.cssRules.length === 1 && /^#gp-content(?: > :nth-child\([1-9][0-9]*\))+$/.test(rule.selectorText) &&
        document.querySelector(rule.selectorText) === document.querySelector(expected) &&
        getComputedStyle(document.querySelector(expected)).outlineStyle === 'dashed';
    }, expected), true, '載せた位置から開く対象にだけ枠を出す');
  };

  for (const c of [
    { name: '囲みの数式', source: '```math\nb^2\n```', changed: '```math\nc^3\n```', selector: '.gp-math-display', tag: 'pre', math: 'c^3' },
    { name: 'ドルの数式', source: '$$\na^2\n$$', changed: '$$\nc^3\n$$', selector: '.gp-math-display', tag: 'p', math: 'c^3' },
    { name: 'mermaid', source: '```mermaid\ngraph TD; A-->B\n```', changed: '```mermaid\ngraph TD; A-->C\n```', selector: '.gp-mermaid', tag: 'pre' },
  ]) {
    for (const ending of ['\n', '\r\n']) await t.test(c.name + JSON.stringify(ending), async (t) => {
      const original = ('前\n\n' + c.source + '\n\n後\n').replaceAll('\n', ending);
      const { page, file, jumps } = await editablePage(t, browser, original, { jump: true });
      await page.waitForSelector(c.math ? '.gp-math-display .katex' : '.gp-mermaid svg');
      const before = await page.locator('#gp-content').innerHTML();
      await hover(page, c.selector, c.tag === 'p' ? '#gp-content > p:nth-child(2)' : c.selector);
      const target = await open(page, c.selector, c.source, c.tag);
      assert.deepEqual(target, { tag: c.tag, range: c.math && c.tag === 'p' ? '3:1-5:2' : '3:1-5:3' });
      assert.deepEqual(await page.evaluate((target) => {
        const live = window.gpPreview.findSource(target.tag, target.range);
        const source = window.gpPreview.sourceOf(live);
        source.tag = '書き換えても写しに届かない';
        return window.gpPreview.sourceOf(live);
      }, target), target);
      await page.keyboard.press('Escape');
      assert.equal(await page.locator('#gp-content').innerHTML(), before, '本文の DOM に編集用の属性やクラスを足さない');
      await open(page, c.selector, c.source, c.tag);
      await commit(page, c.changed);
      // 置き換える文字に $$ があるので、関数で渡す（文字列のままだと $$ が $ 1 個になる）。
      assert.equal(readFileSync(file, 'utf8'), original.replace(c.source.replaceAll('\n', ending), () => c.changed.replaceAll('\n', ending)));
      if (c.math) await page.waitForFunction((math) => document.querySelector('.gp-math-display annotation')?.textContent.trim() === math, c.math);
      else await page.waitForFunction(() => document.querySelector('.gp-mermaid svg')?.textContent.includes('C'));
      await page.locator('#gp-edit-toggle').click();
      assert.equal(await page.locator('#gp-edit-hover').textContent(), '');
      assert.deepEqual(jumps, [], '編集モードから Neovim の行へ移らない');
    });
  }

  await t.test('ライブラリを読む前の数式と図も囲みから編集し、読み込み後も入力欄を保つ', async (t) => {
    let release;
    const held = new Promise((resolve) => { release = resolve; });
    t.after(() => release());
    const sources = ['```math\nb^2\n```', '$$\na^2\n$$', '```mermaid\ngraph TD; A-->B\n```'];
    const original = sources.join('\n\n') + '\n';
    const { page, file } = await editablePage(t, browser, original, {
      waitUntil: 'domcontentloaded',
      beforeNavigate: (page) => page.route(/\/_\/lib\/(?:katex|mermaid)\/.*\.js$/, async (route) => { await held; await route.continue(); }),
    });
    assert.equal(await page.locator('.gp-math-display, .gp-mermaid').count(), 0);
    for (const [i, selector, tag] of [[0, 'pre.js-render-math', 'pre'], [1, 'p:has(.js-render-math)', 'p'], [2, 'pre[data-canonical-lang="mermaid"]', 'pre']]) {
      await open(page, '#gp-content ' + selector, sources[i], tag);
      if (i < 2) await page.keyboard.press('Escape');
    }
    const changed = sources[2].replace('B', 'C');
    await page.locator('#gp-edit-input').fill(changed);
    release();
    await page.waitForSelector('.gp-math-display .katex');
    await page.waitForSelector('.gp-mermaid svg');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), changed);
    await commit(page, changed);
    assert.equal(readFileSync(file, 'utf8'), original.replace(sources[2], changed));
  });

  await t.test('行の中の数式は段落を開く', async (t) => {
    const original = '前 $x$ と $`y`$ の続き\n';
    const { page } = await editablePage(t, browser, original);
    await page.waitForSelector('.gp-math-inline .katex');
    await hover(page, '.gp-math-inline >> nth=0', '#gp-content > p');
    await open(page, '.gp-math-inline >> nth=0', original.trimEnd(), 'p');
    await page.keyboard.press('Escape');
  });

  await t.test('位置を持たない題は HTML 全体を開き、内側の下書きへ戻れる', async (t) => {
    const raw = '<details>\n<summary>題</summary>\n\n中の段落\n\n</details>';
    const original = '前\n\n' + raw + '\n\n後\n';
    const { page, file } = await editablePage(t, browser, original);
    const before = await page.locator('#gp-content').innerHTML();
    await hover(page, '#gp-content summary', '#gp-content details');
    await open(page, '#gp-content summary', raw, 'html');
    await page.keyboard.press('Escape');
    await page.evaluate(() => { document.querySelector('#gp-content details').open = true; });
    await open(page, '#gp-content details p', '中の段落', 'p');
    await page.locator('#gp-edit-input').fill('内側の下書き');
    assert.equal(await page.locator('#gp-edit-outer').textContent(), 'HTML のブロック全体を編集');
    await page.locator('#gp-edit-outer').click();
    await page.waitForFunction((raw) => document.querySelector('#gp-edit-input').value === raw, raw);
    await page.locator('#gp-edit-input').fill(raw.replace('題', '外側の下書き'));
    await page.locator('#gp-edit-inner').click();
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').value === '内側の下書き');
    await page.keyboard.press('Alt+ArrowUp');
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').value.includes('外側の下書き'));
    await page.keyboard.press('Escape');
    // details の開閉だけは利用者の操作で変わっている。
    await page.evaluate(() => { document.querySelector('#gp-content details').open = false; });
    assert.equal(await page.locator('#gp-content').innerHTML(), before);
    // Esc は入力と一緒に下書きも捨てるので、開き直すと原文に戻る。
    await open(page, '#gp-content summary', raw, 'html');
    await commit(page, raw.replace('題', '直した題'));
    assert.equal(readFileSync(file, 'utf8'), original.replace('題', '直した題'));
  });

  await t.test('連なりの全部に枠を出し、表の固定後も開いて後ろへ追加する', async (t) => {
    const raw = '<div>a</div>\n\n<div>b</div>';
    const original = '| 列 |\n| --- |\n| 値 |\n\n' + raw + '\n\n後\n';
    const { page, file } = await editablePage(t, browser, original);
    // 表の固定で、表は画面の DOM の中で包みへ移る。連なりは本文の写しの並びで決めるので変わらない。
    await page.locator('#gp-sticky-toggle').click();
    await page.waitForFunction(() => document.querySelector('#gp-content table').parentElement.id !== 'gp-content');
    const target = await page.locator('#gp-content > div').last().evaluate((node) => window.gpPreview.htmlOf(node));
    assert.equal(target.prev.tag, 'table');
    await page.locator('#gp-content > div').last().hover();
    assert.equal(await page.evaluate(() => {
      const rule = document.querySelector('#gp-edit-hover').sheet.cssRules[0];
      return rule && rule.selectorText.split(',').length === 2 && document.querySelectorAll(rule.selectorText).length === 2 &&
        [...document.querySelectorAll(rule.selectorText)].every((node) => node.localName === 'div');
    }), true);
    await open(page, '#gp-content > div:last-of-type', raw, 'html');
    await page.locator('#gp-edit-after').click();
    await page.locator('#gp-edit-input').fill('追加した段落');
    const response = page.waitForResponse((r) => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/_/edit'));
    await page.keyboard.press('Control+Enter');
    assert.equal((await response).status(), 200);
    await editIdle(page);
    assert.equal(readFileSync(file, 'utf8'), original.replace(raw, raw + '\n\n追加した段落'));
  });

  for (const side of ['before', 'after']) await t.test('連なりの ' + side + ' に追加しても原文の下書きを戻す', async (t) => {
    const raw = '<div>原文</div>';
    const { page, file } = await editablePage(t, browser, '前\n\n' + raw + '\n\n後\n');
    await open(page, '#gp-content > div', raw, 'html');
    await page.locator('#gp-edit-input').fill('<div>下書き</div>');
    await page.locator('#gp-edit-' + side).click();
    await page.locator('#gp-edit-input').fill('追加');
    const response = page.waitForResponse((r) => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/_/edit'));
    await page.keyboard.press('Control+Enter');
    assert.equal((await response).status(), 200);
    await editIdle(page);
    await open(page, '#gp-content > div', '<div>下書き</div>', 'html');
    await page.keyboard.press('Escape');
    assert.equal(readFileSync(file, 'utf8'), side === 'before' ? '前\n\n追加\n\n' + raw + '\n\n後\n' : '前\n\n' + raw + '\n\n追加\n\n後\n');
  });

  await t.test('連なりの範囲を断られたら理由を知らせて開かない', async (t) => {
    // 続きの行のある脚注の定義は、定義だけの段と見分けられず候補に残るので、試しの描画で脚注が消えて断られる。
    const original = '本文[^1]\n\n[^1]: 注\n    続き\n\n<details><summary>題</summary>本文</details>\n';
    const { page, file } = await editablePage(t, browser, original);
    const response = page.waitForResponse((r) => new URL(r.url()).pathname.endsWith('/_/edit/source'));
    await page.locator('#gp-content summary').dblclick();
    assert.equal((await response).status(), 422);
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').dataset.kind === 'error');
    assert.match(await page.locator('#gp-edit-status').textContent(), /範囲を確かめられません/);
    assert.equal(await page.locator('#gp-edit-input').isVisible(), false);
    assert.equal(readFileSync(file, 'utf8'), original);
  });

  await t.test('mermaid の失敗表示から原文を直して図へ戻す', async (t) => {
    const source = '```mermaid\n図ではない原文\n```';
    const { page, file } = await editablePage(t, browser, source + '\n');
    await page.waitForSelector('#gp-content > .gp-error');
    await hover(page, '#gp-content > .gp-error', '#gp-content > .gp-error');
    await open(page, '#gp-content > .gp-error', source, 'pre');
    const changed = '```mermaid\ngraph TD; A-->B\n```';
    await commit(page, changed);
    await page.waitForSelector('.gp-mermaid svg');
    assert.equal(readFileSync(file, 'utf8'), changed + '\n');
  });

  for (const failed of [false, true]) await t.test('PlantUML の' + (failed ? '失敗表示' : '画像') + 'から囲みを直す', async (t) => {
    const source = '```plantuml\n' + (failed ? 'plain 日本語' : 'A -> B') + '\n```';
    const { page, file, jumps } = await editablePage(t, browser, source + '\n', { plantuml: true, jump: true });
    const selector = failed ? '.gp-plantuml-error' : 'img.gp-plantuml';
    await page.waitForSelector(selector);
    if (!failed) await page.waitForFunction(() => document.querySelector('img.gp-plantuml')?.naturalWidth > 0);
    const opened = [];
    page.context().on('page', (other) => opened.push(other));
    await hover(page, failed ? '.gp-plantuml-error pre' : selector, selector);
    await open(page, selector, source, 'img');
    assert.deepEqual(opened, [], '編集モードでは SVG のリンクを開かない');
    const changed = '```plantuml\nA -> C\n```';
    await commit(page, changed);
    await page.waitForFunction(() => document.querySelector('img.gp-plantuml')?.naturalWidth > 0);
    assert.equal(await page.locator('#gp-content svg').count(), 0);
    assert.equal(readFileSync(file, 'utf8'), changed + '\n');
    assert.deepEqual(jumps, []);
  });

  await t.test('mermaid の編集中に配色を切り替えても入力と位置を保ち、消えた図は下書きへ戻す', async (t) => {
    const source = '```mermaid\ngraph TD; A-->B\n```';
    const original = '前\n\n' + source + '\n';
    const { page, file, server } = await editablePage(t, browser, original);
    await page.waitForSelector('.gp-mermaid svg');
    await open(page, '.gp-mermaid', source, 'pre');
    const draft = source.replace('B', 'C');
    await page.locator('#gp-edit-input').fill(draft);
    await page.evaluate(() => { window.oldEditDiagram = document.querySelector('.gp-mermaid'); });
    await page.locator('#gp-theme').click();
    await page.selectOption('#gp-theme', 'dark');
    await page.waitForFunction(() => window.oldEditDiagram !== document.querySelector('.gp-mermaid'));
    await page.waitForFunction(() => {
      const input = document.querySelector('#gp-edit-input').getBoundingClientRect();
      const figure = document.querySelector('.gp-mermaid').getBoundingClientRect();
      return Math.abs(input.top - figure.top) < 1 && Math.abs(input.left - figure.left) < 1;
    });
    assert.equal(await page.locator('#gp-edit-layer').getAttribute('data-typing'), 'on');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), draft);
    assert.equal(readFileSync(file, 'utf8'), original);
    writeFileSync(file, '図を外した文書\n');
    await server.setDocument({ path: file, text: '図を外した文書\n', rev: 20 });
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').hidden);
    writeFileSync(file, original);
    await server.setDocument({ path: file, text: original, rev: 21 });
    await page.waitForSelector('.gp-mermaid svg');
    await editIdle(page);
    await page.locator('.gp-mermaid').dblclick();
    await page.waitForFunction(() => document.querySelector('#gp-edit-layer').dataset.typing === 'on');
    assert.equal(await page.locator('#gp-edit-input').inputValue(), draft);
    await page.keyboard.press('Escape');
  });

  await t.test('図の編集中に、同じ位置の図が外で変わったら、入力欄を付け替えずに閉じる', async (t) => {
    const source = '```mermaid\ngraph TD; A-->B\n```';
    const original = '前\n\n' + source + '\n';
    const { page, file, server } = await editablePage(t, browser, original);
    await page.waitForSelector('.gp-mermaid svg');
    await open(page, '.gp-mermaid', source, 'pre');
    await page.locator('#gp-edit-input').fill(source.replace('B', 'C'));
    // 要素名と位置は同じ（pre の 3:1-5:3）で、中身だけが外で変わる。
    const external = original.replace('A-->B', 'A-->Z');
    writeFileSync(file, external);
    await server.setDocument({ path: file, text: external, rev: 30 });
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').hidden);
    await page.waitForFunction(() => document.querySelector('#gp-edit-status').textContent.includes('文書が変わった'));
    await page.waitForFunction(() => document.querySelector('.gp-mermaid svg')?.textContent.includes('Z'));
    assert.equal(readFileSync(file, 'utf8'), external);
    await editIdle(page);
    // 開き直すと、外で変わった後の原文が出る（前の原文に対する下書きは出さない）。
    await open(page, '.gp-mermaid', source.replace('A-->B', 'A-->Z'), 'pre');
    await page.keyboard.press('Escape');
  });

  await t.test('図から外側へ移った入力欄と別に、項目と注記の題名には開く対象の枠を出す', async (t) => {
    const original = '- item\n\n  ```mermaid\n  graph TD; A-->B\n  ```\n- next\n\n>>> [!note] 題名\n本文\n>>>\n';
    const { page } = await editablePage(t, browser, original);
    await page.waitForSelector('.gp-mermaid svg');
    const before = await page.locator('#gp-content').innerHTML();
    // 空行のあるリストなので、項目の文字は段落に入る。載せた所のいちばん内側（段落）に枠が出る。
    await hover(page, '#gp-content > ul > li:last-child > p', '#gp-content > ul > li:last-child > p');
    // 項目自身に届いたイベントの枠を、段落への載せ替えと分けて確かめる。
    await hover(page, '#gp-content > ul > li:last-child', '#gp-content > ul > li:last-child', true);
    await hover(page, '.markdown-alert-title', '.markdown-alert');
    await hover(page, '.gp-mermaid svg', '.gp-mermaid');
    await open(page, '.gp-mermaid', '```mermaid\n  graph TD; A-->B\n  ```', 'pre');
    await page.keyboard.press('Alt+ArrowUp');
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').value.startsWith('- item'));
    // 入力欄は層にある。本文の図へ pointerover を送っても、外側でなく内側を選ぶ。
    await page.locator('.gp-mermaid svg').dispatchEvent('pointerover');
    assert.equal(await page.evaluate(() => {
      const rule = document.querySelector('#gp-edit-hover').sheet.cssRules[0];
      return document.querySelector(rule.selectorText) === document.querySelector('.gp-mermaid');
    }), true);
    await page.keyboard.press('Alt+ArrowDown');
    await page.waitForFunction(() => document.querySelector('#gp-edit-input').value.startsWith('```mermaid'));
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#gp-content').innerHTML(), before);
    await page.locator('#gp-header').hover();
    assert.equal(await page.locator('#gp-edit-hover').textContent(), '');
    // 窓からフォーカスが外れたとき・プレビューが止まったときも、枠を残さない。
    await page.locator('.markdown-alert-title').hover();
    assert.notEqual(await page.locator('#gp-edit-hover').textContent(), '');
    await page.evaluate(() => window.dispatchEvent(new Event('blur')));
    assert.equal(await page.locator('#gp-edit-hover').textContent(), '');
    await page.locator('#gp-header').hover();
    await page.locator('.markdown-alert-title').hover();
    assert.notEqual(await page.locator('#gp-edit-hover').textContent(), '');
    await page.evaluate(() => document.dispatchEvent(new CustomEvent('gp:stop', { detail: { reason: 'exit' } })));
    assert.equal(await page.locator('#gp-edit-hover').textContent(), '');
  });
});
