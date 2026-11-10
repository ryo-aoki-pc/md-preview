import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { EventEmitter } from 'node:events';
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { join, dirname, relative } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';
import { fromHtml } from 'hast-util-from-html';
import { toHtml } from 'hast-util-to-html';
import { visit } from 'unist-util-visit';
import { exportDocument, renderForExport } from '../lib/export.mjs';
import { main } from '../cli.mjs';
import { temp } from './helpers.mjs';
import { payloads, problems } from './export-xss-helper.mjs';

function fixture(t, text = '# 文書\n\n本文') {
  const root = temp(t),
    file = join(root, 'docs', 'sample.md');
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, text);
  return { root, file };
}
function inspect(html) {
  const tree = fromHtml(html),
    scripts = [];
  let csp, content;
  visit(tree, 'element', (node) => {
    if (node.tagName === 'script') scripts.push(node.children.map((child) => child.value || '').join(''));
    if (node.tagName === 'meta' && node.properties.httpEquiv?.[0] === 'Content-Security-Policy')
      csp = node.properties.content;
    if (node.properties.id === 'gp-content') content = node;
  });
  return { tree, scripts, csp, content };
}

test('通常の書き出しはスクリプト無しで、CSP・本文・配色・幅を保つ', async (t) => {
  const f = fixture(t);
  for (const theme of ['system', 'light', 'dark']) {
    const { html } = await exportDocument({ ...f, theme, width: 'fixed' });
    const parsed = inspect(html);
    assert.equal(parsed.scripts.length, 0);
    assert.match(parsed.csp, /script-src 'none'/);
    for (const directive of [
      "default-src 'none'",
      "style-src 'unsafe-inline'",
      'font-src data:',
      "base-uri 'none'",
      "form-action 'none'",
      'img-src * data: blob:',
      'media-src * data:',
    ])
      assert.ok(parsed.csp.includes(directive), directive);
    assert.match(html, /name="referrer" content="no-referrer"/);
    assert.match(html, /data-width="fixed"/);
    assert.ok(html.includes('data-theme="' + theme + '"'));
    assert.doesNotMatch(toHtml(parsed.tree), /<header|<aside|id="gp-(?:notices|controls|banner)"/);
    assert.match(toHtml(parsed.content), /本文/);
    assert.doesNotMatch(html, /127\.0\.0\.1|\/_[/]assets\/|\?token=/);
    assert.equal(html.includes(f.root), false);
    assert.equal(html.includes(f.root.replaceAll('\\', '/')), false);
    if (theme === 'system') assert.match(html, /@media\(prefers-color-scheme:dark\)/);
  }
});

test('数式だけ・図だけ・両方で必要なライブラリだけを埋め、CSP のハッシュを実バイトに合わせる', async (t) => {
  const f = fixture(t);
  const math = '$`x^2`$\n',
    diagram = '```mermaid\ngraph TD; A-->B\n```\n';
  for (const [source, mathPresent, diagramPresent] of [
    [math, true, false],
    [diagram, false, true],
    [math + '\n' + diagram, true, true],
  ]) {
    writeFileSync(f.file, source);
    const { html } = await exportDocument(f);
    const { scripts, csp } = inspect(html);
    assert.equal(scripts.length, Number(mathPresent) + Number(diagramPresent) + 1);
    for (const script of scripts)
      assert.ok(csp.includes("'sha256-" + createHash('sha256').update(script).digest('base64') + "'"));
    assert.equal((csp.match(/sha256-/g) || []).length, scripts.length);
    assert.equal(html.includes('data:font/woff2;base64,'), mathPresent);
    assert.doesNotMatch(html, /url\(fonts\//);
    assert.equal(html.includes('var __esbuild_esm_mermaid_nm'), diagramPresent);
    assert.doesNotMatch(html, /127\.0\.0\.1|\?token=|\/_\/assets\//);
    assert.equal(html.includes(f.root), false);
    assert.equal(html.includes(f.root.replaceAll('\\', '/')), false);
    assert.doesNotMatch(html, /<script[^>]+src=|<link[^>]+stylesheet/);
  }
});

test('全 XSS 入力と include の出力を HTML として読み直して同じ検査を通す', async (t) => {
  const f = fixture(t);
  const all = payloads.filter((text) => !text.includes('\0')).join('\n\n---\n\n');
  for (const included of [false, true]) {
    if (included) {
      writeFileSync(join(dirname(f.file), 'input.md'), all);
      writeFileSync(f.file, '::include{file=input.md}\n');
    } else writeFileSync(f.file, all);
    const { html } = await exportDocument(f);
    const { content } = inspect(html);
    assert.deepEqual(problems(toHtml({ type: 'root', children: content.children })), []);
    visit(content, 'element', (node) => {
      for (const key of Object.keys(node.properties)) assert.equal(/^on/i.test(key), false, key);
    });
  }
});

test('include の読込と欠落の知らせをプレビューと同じルートで扱う', async (t) => {
  const f = fixture(t, '::include{file=part.md}\n\n::include{file=/missing.md}\n');
  writeFileSync(join(dirname(f.file), 'part.md'), '## 取り込んだ本文\n');
  const { html, notices } = await exportDocument(f);
  assert.match(toHtml(inspect(html).content), /取り込んだ本文/);
  assert.equal(notices.get('include'), 1);
  assert.doesNotMatch(html, /id="gp-notices"/);
});

test('相対とルートからの URL を書き出し先からの相対にし、外へのパスは直さない', async (t) => {
  const f = fixture(
    t,
    '[文書](other.md#heading)\n\n![絵](../images/a%20b.png)\n\n[ルート](/other.md?x=1#part)\n\n[外](../../outside.md)\n\n[未作成](new.md)\n',
  );
  mkdirSync(join(f.root, 'images'));
  writeFileSync(join(f.root, 'images', 'a b.png'), '画像');
  writeFileSync(join(f.root, 'other.md'), '本文');
  writeFileSync(join(dirname(f.file), 'other.md'), '本文');
  mkdirSync(join(f.root, 'output'));
  const output = join(f.root, 'output', 'out.html');
  writeFileSync(output, '前の文書');
  const { html } = await exportDocument({ ...f, output });
  const content = toHtml(inspect(html).content);
  assert.match(content, /href="\.\.\/docs\/other.md#heading"/);
  assert.match(content, /src="\.\.\/images\/a%20b.png"/);
  assert.match(content, /href="\.\.\/other.md\?x=1#part"/);
  assert.match(content, /href="\.\.\/\.\.\/outside.md"/);
  assert.match(content, /href="\.\.\/docs\/new.md"/);
  assert.equal(readFileSync(output, 'utf8'), html);
  const stdout = toHtml(inspect((await exportDocument(f)).html).content);
  assert.match(stdout, /href="\.\/other.md#heading"/);
  assert.match(stdout, /href="\.\.\/other.md\?x=1#part"/);
});

test('git のトップを既定のルートにし、無ければ文書のフォルダーを使う', async (t) => {
  const f = fixture(t, '::include{file=/part.md}\n');
  mkdirSync(join(f.root, '.git'));
  writeFileSync(join(f.root, 'part.md'), 'ルートの本文');
  assert.match((await exportDocument({ file: f.file })).html, /ルートの本文/);
  const plain = fixture(t, '::include{file=/part.md}\n');
  writeFileSync(join(dirname(plain.file), 'part.md'), 'フォルダーの本文');
  // 作業ツリーの .git の影響を避けるため明示ルートでも同じ fileContext を確かめる。
  assert.match((await exportDocument({ file: plain.file, root: dirname(plain.file) })).html, /フォルダーの本文/);
});

test('ルート外の文書と include・リンクによる脱出を拒む', async (t) => {
  const f = fixture(t),
    outside = temp(t);
  writeFileSync(join(outside, 'secret.md'), '外側の本文');
  await assert.rejects(exportDocument({ ...f, root: outside }), /ルート/);
  writeFileSync(f.file, '::include{file=../../secret.md}\n');
  assert.equal((await exportDocument(f)).html.includes('外側の本文'), false);
  try {
    symlinkSync(join(outside, 'secret.md'), join(dirname(f.file), 'link.md'));
  } catch (e) {
    if (e.code === 'EPERM') {
      t.skip('シンボリックリンクを作る権限がない');
      return;
    }
    throw e;
  }
  writeFileSync(f.file, '::include{file=link.md}\n');
  assert.equal((await exportDocument(f)).html.includes('外側の本文'), false);
});

test('読込・容量・文字コード・描画・書込の失敗では新規出力も途中の HTML も残さない', async (t) => {
  const f = fixture(t),
    output = join(f.root, 'out.html');
  for (const source of [
    Buffer.from([255]),
    Buffer.from([0]),
    Buffer.alloc(16 * 1024 * 1024 + 1, 97),
    '>'.repeat(20000) + ' x',
  ]) {
    writeFileSync(f.file, source);
    for (const previous of [false, true]) {
      if (!previous) rmSync(output, { force: true });
      if (previous) writeFileSync(output, '前の文書');
      await assert.rejects(exportDocument({ ...f, output }));
      if (previous) assert.equal(readFileSync(output, 'utf8'), '前の文書');
      else assert.equal(existsSync(output), false);
    }
  }
  await assert.rejects(exportDocument({ ...f, file: join(f.root, 'missing.md'), output }));
  assert.equal(readFileSync(output, 'utf8'), '前の文書');
  writeFileSync(f.file, '# 文書');
  await assert.rejects(exportDocument({ ...f, output: join(f.root, 'missing', 'out.html') }));
  mkdirSync(join(f.root, 'directory.html'));
  await assert.rejects(exportDocument({ ...f, output: join(f.root, 'directory.html') }));
  assert.equal(
    readdirSync(f.root).some((name) => name.endsWith('.tmp')),
    false,
  );
});

test('worker を時間で打ち切り、誤りと異常終了でも必ず終了する', async () => {
  for (const mode of ['timeout', 'error', 'exit', 'message']) {
    const worker = new EventEmitter();
    let stopped = false;
    worker.terminate = async () => {
      stopped = true;
    };
    worker.postMessage = () => {
      if (mode === 'timeout') return;
      queueMicrotask(() => worker.emit(mode, mode === 'message' ? { id: 1, ok: false } : undefined));
    };
    await assert.rejects(
      renderForExport('', {}, { timeoutMs: 5, workerFactory: () => worker }),
      mode === 'timeout' ? /時間切れ/ : /描画/,
    );
    assert.equal(stopped, true);
  }
});

test('render の標準出力は HTML だけ、知らせと実行時の誤りは標準エラーに日本語1行ずつ出す', async (t) => {
  const f = fixture(t, '# 文書\n\n![欠落](missing.png)\n\n#123\n');
  const cli = new URL('../cli.mjs', import.meta.url);
  const result = spawnSync(
    process.execPath,
    [cli.pathname.replace(/^\/(\w:)/, '$1'), 'render', '--root', f.root, f.file],
    { encoding: 'utf8', timeout: 20000 },
  );
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /^<!doctype html>/);
  assert.match(result.stderr, /見つからないメディア: 1 件/);
  assert.match(result.stderr, /参照: 1 件/);
  for (const argv of [
    ['render'],
    ['render', '--theme', 'bad', f.file],
    ['render', '--width', 'bad', f.file],
    ['render', '--port', '1', f.file],
    ['render', '-o'],
  ]) {
    let out = '',
      err = '';
    assert.equal(await main(argv, { stdout: { write: (s) => (out += s) }, stderr: { write: (s) => (err += s) } }), 2);
    assert.equal(out, '');
    assert.match(err, /^mdpreview: [^\n]+\n$/);
  }
  let out = '',
    err = '';
  assert.equal(
    await main(['render', join(f.root, 'missing.md')], {
      stdout: { write: (s) => (out += s) },
      stderr: { write: (s) => (err += s) },
    }),
    1,
  );
  assert.equal(out, '');
  assert.match(err, /^mdpreview: [^\n]+\n$/);
});
