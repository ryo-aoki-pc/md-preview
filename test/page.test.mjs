// ページの配線を、本物の HTTP / SSE・KaTeX・mermaid・ブラウザで確かめる。
import assert from 'node:assert/strict';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import test from 'node:test';
import { createServer } from '../lib/server.mjs';
import { loadPage } from '../lib/page.mjs';
import { createSession } from '../lib/session.mjs';
import { temp } from './helpers.mjs';

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
const skip = !chromium ? 'Playwright が見つからない（PLAYWRIGHT_MODULE または playwright が必要）'
  : !browserPath ? 'Edge / Chrome / Chromium が見つからない（EDGE_BIN または CHROME_BIN を指定できる）' : false;

const payload = (html, extra = {}) => ({
  file: 'docs/試験.md', dir: 'docs/', rev: 1, html, headings: [], ...extra,
});

async function fixturePage(t, browser, render = payload('<h1>見出し</h1><p>本文</p>'), prepareRoot = () => {}, { jump = false, live = false, directory = false } = {}) {
  const root = temp(t);
  prepareRoot(root);
  const jumps = [];
  const jumpRequests = [];
  let lastRender = render;
  let lastScroll = null;
  let server;
  const session = live ? createSession({ root, onRender: (data) => server?.broadcast('render', data), onScroll: (data) => server?.broadcast('scroll', data) })
    : { root: () => root, last: () => ({ render: lastRender, scroll: lastScroll }) };
  server = await createServer({
    session,
    page: loadPage({ jump, directory }),
    onJump: jump ? (value) => jumps.push(value) : undefined,
  });
  if (live && !directory) session.setDocument({ path: join(root, 'README.md'), name: 'README.md', text: readFileSync(join(root, 'README.md'), 'utf8'), rev: 1 });
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
  // 通信開始前に CSP で拒否されたものも、外への試みとして数える。
  await context.addInitScript(() => {
    window.gpBlocked = [];
    document.addEventListener('securitypolicyviolation', (event) => {
      const value = event.blockedURI;
      if (/^https?:/.test(value) && new URL(value).hostname !== '127.0.0.1') {
        const blocked = { url: value, type: event.violatedDirective };
        window.gpBlocked.push(blocked);
        void window.gpRecordViolation(blocked).catch(() => {});
      }
    });
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on('pageerror', (error) => errors.push(error.message));
  t.after(async () => {
    const blocked = page.isClosed() ? [] : await page.evaluate(() => window.gpBlocked);
    await context.close();
    await server.close();
    await session.close?.();
    assert.deepEqual(errors, [], 'ページの例外がない');
    assert.deepEqual(attempted, [], '本文の外部画像を含まない試験では、外への要求を試みない');
    assert.deepEqual(blocked, [], 'CSP で拒否された外部への試みもない');
    assert.deepEqual(blockedAcrossPages, [], '文書を移動する前の CSP 違反もない');
  });
  await page.goto(server.url);
  await page.waitForFunction(() => document.querySelector('#gp-content').textContent !== '読み込み中…');
  await page.waitForSelector('#gp-fallback-style', { state: 'attached' });
  return {
    page, server, root, jumps, jumpRequests, eventRequests,
    send(name, data) {
      if (name === 'render') lastRender = data;
      if (name === 'scroll') lastScroll = data;
      server.broadcast(name, data);
    },
  };
}

test('ローカルのプレビューページ', { skip, timeout: 90000 }, async (t) => {
  const browser = await chromium.launch({ executablePath: browserPath, headless: true });
  t.after(() => browser.close());

  await t.test('本文・コード配色の目印・details・ページ内リンクを保つ', async (t) => {
    const html = '<h1 id="user-content-heading">見出し</h1><a href="#heading">移動</a>' +
      '<details><summary>詳細</summary><p>本文</p></details><pre class="highlight"><code>const a = 1</code></pre>';
    const fixture = await fixturePage(t, browser, payload(html));
    const { page } = fixture;
    assert.equal(await page.locator('#gp-content h1').textContent(), '見出し');
    assert.equal(await page.locator('.highlight').evaluate((el) => el.classList.contains('code-syntax-highlight-theme')), true);
    assert.equal(await page.locator('body').evaluate((el) => el.classList.contains('code-syntax-highlight-theme')), false);
    await page.locator('summary').click();
    fixture.send('render', payload(html, { rev: 2 }));
    await page.waitForFunction(() => document.querySelector('details').open);
    await page.locator('a[href="#heading"]').click();
    assert.equal(await page.locator('details').evaluate((el) => el.open), true);
  });

  await t.test('配色と本文幅を切り替え、再読込でも保持する', async (t) => {
    const fixture = await fixturePage(t, browser);
    const { page } = fixture;
    await page.waitForFunction(() => document.documentElement.dataset.style === 'fallback');
    assert.ok(await page.locator('#gp-page').evaluate((el) => el.getBoundingClientRect().width) > 1600);
    await page.selectOption('#gp-width', 'fixed');
    assert.equal(await page.locator('#gp-page').evaluate((el) => el.getBoundingClientRect().width), 1600);
    await page.selectOption('#gp-theme', 'dark');
    await page.waitForFunction(() => getComputedStyle(document.body).backgroundColor === 'rgb(31, 30, 36)');
    await page.selectOption('#gp-theme', 'light');
    await page.waitForFunction(() => getComputedStyle(document.body).backgroundColor === 'rgb(255, 255, 255)');
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.selectOption('#gp-theme', 'system');
    await page.waitForFunction(() => document.documentElement.classList.contains('gl-dark'));
    await Promise.all([page.waitForEvent('domcontentloaded'), Promise.resolve().then(() => fixture.send('reload', {}))]);
    await page.waitForFunction(() => document.documentElement.dataset.style === 'fallback');
    assert.equal(await page.locator('#gp-width').inputValue(), 'fixed');
    assert.equal(await page.locator('#gp-theme').inputValue(), 'system');
  });

  await t.test('本物の KaTeX と mermaid をローカルから読み、テーマ変更は図だけを更新する', async (t) => {
    const fixture = await fixturePage(t, browser, payload('<details open><summary>詳細</summary><p>本文</p></details>' +
      '<code class="js-render-math" data-math-style="inline">x^2 + y^2</code>' +
      '<pre data-sourcepos="2:1-5:1"><code class="js-render-mermaid">graph TD; A--&gt;B</code></pre>'));
    const { page } = fixture;
    await page.waitForSelector('.katex');
    await page.waitForSelector('.gp-mermaid svg');
    assert.ok(await page.locator('.gp-mermaid svg').evaluate((el) => el.getBoundingClientRect().width) > 0);
    await page.evaluate(() => { window.gpOriginalDetails = document.querySelector('details'); window.gpOldSvg = document.querySelector('.gp-mermaid svg'); });
    await page.selectOption('#gp-theme', 'dark');
    await page.waitForFunction(() => document.querySelector('.gp-mermaid svg') !== window.gpOldSvg);
    assert.equal(await page.evaluate(() => window.gpOriginalDetails === document.querySelector('details') && window.gpOriginalDetails.open), true);
    assert.equal(await page.locator('script[src*="/_/lib/"]').count(), 2);
  });

  await t.test('誤りはバナーとエスケープした原文で表示する', async (t) => {
    const { page } = await fixturePage(t, browser, { file: 'error.md', dir: '', rev: 1, error: '描画が時間切れになりました', markdown: '<img src="https://example.invalid/leak">\n原文' });
    assert.match(await page.locator('#gp-banner').textContent(), /時間切れ/);
    assert.equal(await page.locator('.gp-raw').textContent(), '<img src="https://example.invalid/leak">\n原文');
    assert.equal(await page.locator('#gp-content img').count(), 0);
  });

  await t.test('幅・フォント・画面の高さの変更後もスクロールを合わせ直す', async (t) => {
    const html = Array.from({ length: 120 }, (_, i) => '<p id="line-' + (i + 1) + '" data-sourcepos="' + (i + 1) + ':1-' + (i + 1) + ':1">' + '日本語の長い文章で折り返し位置を確かめる。'.repeat(12) + '</p>').join('');
    const fixture = await fixturePage(t, browser, payload(html));
    const { page } = fixture;
    const aligned = () => page.waitForFunction(() => {
      const header = document.querySelector('#gp-header').offsetHeight;
      const banner = document.querySelector('#gp-banner');
      const top = header + (banner.hidden ? 0 : banner.offsetHeight);
      return Math.abs(document.querySelector('#line-70').getBoundingClientRect().top - (top + (innerHeight - top) * 0.5)) < 2;
    });
    fixture.send('scroll', { line: 70, ratio: 0.5, lines: 120 });
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
    const fixture = await fixturePage(t, browser, payload('<p><img id="relative" src="画像.svg"></p><a id="local" href="/readme.md">文書</a><a id="fragment" href="#heading">見出し</a><a id="external" href="https://example.invalid/">外部</a>'), (root) => {
      mkdirSync(join(root, 'docs'), { recursive: true });
      writeFileSync(join(root, 'docs', '画像.svg'), '<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1"/></svg>');
    });
    const { page, server } = fixture;
    await page.waitForFunction(() => document.querySelector('#relative').naturalWidth === 1);
    assert.equal(await page.locator('#relative').getAttribute('src'), server.url + 'docs/' + encodeURIComponent('画像.svg'));
    assert.equal(await page.locator('#local').getAttribute('href'), new URL('?doc=readme.md', server.url).href);
    assert.equal(await page.locator('#fragment').getAttribute('href'), '#heading');
    assert.equal(await page.locator('#external').getAttribute('href'), 'https://example.invalid/');
  });

  await t.test('終了通知を単体アプリの言葉で表示する', async (t) => {
    const fixture = await fixturePage(t, browser);
    await fixture.server.close('exit');
    await fixture.page.waitForFunction(() => document.querySelector('#gp-banner').textContent === 'プレビューが終了した');
  });
  await t.test('ダブルクリックで最も近い開始行へジャンプし、文字の選択を妨げない', async (t) => {
    const fixture = await fixturePage(t, browser, payload('<div data-sourcepos="4:1-12:1"><p data-sourcepos="8:1-9:5"><span id="target">選択できる文字</span></p></div>'), () => {}, { jump: true });
    const { page } = fixture;
    await page.evaluate(() => {
      window.gpDefaultPrevented = null;
      document.querySelector('#gp-content').addEventListener('dblclick', (event) => { window.gpDefaultPrevented = event.defaultPrevented; });
    });
    const response = page.waitForResponse((value) => value.url().endsWith('/_/jump') && value.request().method() === 'POST');
    await page.locator('#target').dblclick();
    assert.equal((await response).status(), 204);
    assert.deepEqual(fixture.jumps, [{ line: 8 }]);
    assert.deepEqual(fixture.jumpRequests, [{ method: 'POST', body: { line: 8 } }]);
    assert.equal(await page.evaluate(() => window.gpDefaultPrevented), false);
  });

  await t.test('ジャンプが無効なら送信せず、操作する要素の上でも送信しない', async (t) => {
    const disabled = await fixturePage(t, browser, payload('<p id="disabled" data-sourcepos="5:1-5:5">本文</p>'));
    await disabled.page.locator('#disabled').dblclick();
    await disabled.page.waitForTimeout(100);
    assert.deepEqual(disabled.jumpRequests, []);
    const enabled = await fixturePage(t, browser, payload('<div data-sourcepos="10:1-20:1"><a id="link" href="#link">リンク</a><details><summary id="summary">開閉</summary></details><input id="input"><button id="button">ボタン</button><video id="video"></video><audio id="audio"></audio></div><p id="plain">行番号のない本文</p>'), () => {}, { jump: true });
    for (const id of ['link', 'summary', 'input', 'button', 'video', 'audio', 'plain']) {
      await enabled.page.locator('#' + id).dispatchEvent('dblclick');
    }
    await enabled.page.waitForTimeout(100);
    assert.deepEqual(enabled.jumpRequests, []);
    assert.deepEqual(enabled.jumps, []);
  });

  await t.test('休止中の表示は、新しい render が届いたら接続を開き直さずに消える', async (t) => {
    const fixture = await fixturePage(t, browser);
    fixture.send('stop', { reason: 'stop' });
    await fixture.page.waitForFunction(() => document.querySelector('#gp-banner').textContent === 'プレビューを止めた');
    fixture.send('render', payload('<p>再開した本文</p>', { rev: 2 }));
    await fixture.page.waitForFunction(() => document.querySelector('#gp-banner').hidden && document.querySelector('#gp-content').textContent === '再開した本文');
  });

  await t.test('Markdown のリンクを同じタブで開き、戻る・進むで文書を移動する', async (t) => {
    const fixture = await fixturePage(t, browser, undefined, (root) => {
      mkdirSync(join(root, 'docs'));
      writeFileSync(join(root, 'README.md'), '# Home\n\n[次の文書](docs/next.MARKDOWN#target)\n\n[ルート外](../outside.md)\n\n[PDF](manual.pdf)\n\n[百分率](100%.md)');
      writeFileSync(join(root, '100%.md'), '# 百分率');
      writeFileSync(join(root, 'docs', 'next.MARKDOWN'), '# Target\n\n次の本文');
    }, { live: true });
    const { page, server } = fixture;
    const next = page.locator('#gp-content a').filter({ hasText: '次の文書' });
    assert.equal(await next.getAttribute('target'), null);
    assert.equal(new URL(await page.locator('#gp-content a').filter({ hasText: '百分率' }).getAttribute('href')).searchParams.get('doc'), '100%.md');
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
    const fixture = await fixturePage(t, browser, undefined, (root) => {
      mkdirSync(join(root, 'docs'));
      writeFileSync(join(root, 'README.md'), '# 最初の文書');
      writeFileSync(join(root, 'docs', 'a.md'), '# 一覧から開いた文書');
    }, { live: true, directory: true });
    const { page } = fixture;
    await page.waitForSelector('.gp-document-list a');
    assert.equal(fixture.eventRequests.length, 0);
    assert.equal(await page.locator('#gp-back').isVisible(), false);
    assert.ok(await page.locator('.gp-document-list ul a').count() > 0);
    await page.locator('.gp-document-list a').filter({ hasText: 'a.md' }).click();
    await page.waitForFunction(() => document.querySelector('#gp-file').textContent === 'docs/a.md');
    assert.match(await page.locator('#gp-content').textContent(), /一覧から開いた文書/);
    assert.equal(await page.locator('#gp-back').isVisible(), false);
  });

  await t.test('見出しを安全な文字で表示し、開閉を保存し、押した見出しを目印にする', async (t) => {
    const headings = [{ level: 1, id: 'first', text: '<img src="https://example.invalid/">' }, { level: 2, id: 'second', text: '二番目' }, { level: 3, id: 'hidden', text: '折りたたんだ見出し' }];
    const html = '<h1 id="user-content-first">最初</h1><p>' + '本文。'.repeat(10000) + '</p><h2 id="user-content-second">二番目</h2><details><summary>開閉</summary><h3 id="user-content-hidden">折りたたんだ見出し</h3></details>';
    const { page } = await fixturePage(t, browser, payload(html, { headings }));
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    assert.equal(await page.locator('#gp-outline img').count(), 0);
    assert.equal(await page.locator('#gp-outline a').first().textContent(), headings[0].text);
    assert.equal(await page.locator('#gp-outline-list li').nth(1).evaluate((el) => el.style.paddingInlineStart), '12px');
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
    const fixture = await fixturePage(t, browser, payload('<h1 id="user-content-title">題</h1>', { headings: [{ level: 1, id: 'title', text: '題' }] }));
    const { page } = fixture;
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    await page.setViewportSize({ width: 800, height: 700 });
    await page.waitForFunction(() => document.querySelector('#gp-outline').hidden);
    await page.setViewportSize({ width: 1200, height: 700 });
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    fixture.send('render', payload('<p>見出しのない本文</p>', { headings: [], rev: 2 }));
    await page.waitForFunction(() => document.querySelector('#gp-outline').hidden && document.querySelector('#gp-outline-toggle').disabled);
  });

  await t.test('見出し欄の出し入れの後も、本文幅・配色とスクロールの同期を保つ', async (t) => {
    const html = '<h1 id="user-content-title" data-sourcepos="1:1-1:1">題</h1>' + Array.from({ length: 120 }, (_, i) => '<p id="line-' + (i + 2) + '" data-sourcepos="' + (i + 2) + ':1-' + (i + 2) + ':1">' + '折り返しのある本文。'.repeat(50) + '</p>').join('');
    const fixture = await fixturePage(t, browser, payload(html, { headings: [{ level: 1, id: 'title', text: '題' }] }));
    const { page } = fixture;
    const aligned = () => page.waitForFunction(() => {
      const header = document.querySelector('#gp-header').offsetHeight;
      const banner = document.querySelector('#gp-banner');
      const top = header + (banner.hidden ? 0 : banner.offsetHeight);
      return Math.abs(document.querySelector('#line-70').getBoundingClientRect().top - (top + (innerHeight - top) * 0.5)) < 2;
    });
    fixture.send('scroll', { line: 70, ratio: 0.5, lines: 121 });
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
    const { page } = await fixturePage(t, browser, payload('<h1 id="user-content-title">題</h1><pre>code</pre><table><tr><td>表</td></tr></table>', { headings: [{ level: 1, id: 'title', text: '題' }] }));
    await page.waitForFunction(() => !document.querySelector('#gp-outline').hidden);
    await page.emulateMedia({ media: 'print' });
    assert.equal(await page.locator('#gp-header').isVisible(), false);
    assert.equal(await page.locator('#gp-outline').isVisible(), false);
    assert.equal(await page.locator('#gp-layout').evaluate((el) => getComputedStyle(el).marginLeft), '0px');
    assert.equal(await page.locator('#gp-page').evaluate((el) => el.getBoundingClientRect().width), await page.evaluate(() => innerWidth));
    assert.equal(await page.locator('pre').evaluate((el) => getComputedStyle(el).breakInside), 'avoid');
    assert.equal(await page.locator('table').evaluate((el) => getComputedStyle(el).breakInside), 'avoid');
  });

});
