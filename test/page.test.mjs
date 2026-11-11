// ページの配線を、本物の HTTP / SSE・KaTeX・mermaid・ブラウザで確かめる。
import assert from 'node:assert/strict';
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import test from 'node:test';
import { prepareGoPageTests, renderGoPageDocument, startGoPageServer, temp } from './helpers.mjs';
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
  { jump = false, live = false, directory = false } = {},
) {
  const root = temp(t);
  prepareRoot(root);
  const jumpRequests = [];
  const server = await startGoPageServer(t, { root, live, jump, directory, render });
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
  await page.goto(server.url);
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

async function settledRender(page, rev) {
  await page.waitForFunction((rev) => {
    const current = window.gpPreview?.current();
    return current?.rev === rev && !document.querySelector('#gp-content .js-render-math, #gp-content .js-render-mermaid');
  }, rev);
  await page.evaluate(() => document.fonts.ready);
}

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

test('ローカルのプレビューページ', { skip, timeout: 90000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());

  const tableHtml = '<table><thead><tr><th>名前</th><th>値</th></tr></thead>' +
    '<tbody><tr><td>項目</td><td>内容</td></tr></tbody></table>';
  const stickySource = readFileSync(new URL('../page/lib/md-table-sticky/sticky-table.js', import.meta.url), 'utf8');
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
