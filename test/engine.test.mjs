// 描画エンジン (engine/glfm_markdown.wasm と lib/engine.mjs) の試験。
// GitLab の整形器 (engine/src/formatter.rs) が上書きしている形を、ここで固定する。
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { createEngine, GITLAB_OPTIONS, renderMarkdown } from '../lib/engine.mjs';

const engineDir = new URL('../engine/', import.meta.url);
const lock = JSON.parse(readFileSync(new URL('engine.lock.json', engineDir), 'utf8'));
const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex');

test('.wasm と gem の写しが engine.lock.json の固定値と同じ', () => {
  const wasm = readFileSync(new URL(lock.wasm.path, engineDir));
  assert.equal(wasm.length, lock.wasm.size);
  assert.equal(sha256(wasm), lock.wasm.sha256);
  for (const [path, record] of [...Object.entries(lock.gem.files), [lock.gitlab.options.path, lock.gitlab.options]]) {
    assert.equal(sha256(readFileSync(new URL(path, engineDir))), record.sha256, path);
  }
});

test('.wasm は外の機能を何も取り込まない', () => {
  const module = new WebAssembly.Module(readFileSync(new URL(lock.wasm.path, engineDir)));
  assert.deepEqual(WebAssembly.Module.imports(module), []);
  assert.deepEqual(
    WebAssembly.Module.exports(module).map((entry) => entry.name).sort(),
    ['glfm_alloc', 'glfm_free', 'glfm_render', 'memory'],
  );
});

test('Cargo.toml の comrak の版が固定値と同じ', () => {
  const cargo = readFileSync(new URL('Cargo.toml', engineDir), 'utf8');
  assert.match(cargo, new RegExp(`comrak = \\{ version = "=${lock.comrak.replaceAll('.', '\\.')}"`));
  const cargoLock = readFileSync(new URL('Cargo.lock', engineDir), 'utf8');
  assert.match(cargoLock, new RegExp(`name = "comrak"\\nversion = "${lock.comrak.replaceAll('.', '\\.')}"`));
});

test('オプションは GitLab の OPTIONS と同じ', () => {
  // engine/gitlab/glfm_markdown.rb は GitLab 19.5.1 の lib/banzai/filter/markdown_engines/glfm_markdown.rb の写し。
  // その OPTIONS のハッシュを読み、lib/engine.mjs の値と比べる。
  const ruby = readFileSync(new URL(lock.gitlab.options.path, engineDir), 'utf8');
  const referenceChars = /REFERENCE_CHARS = %w\[([^\]]+)\]\.freeze/.exec(ruby)[1].split(' ');
  const body = /^ {8}OPTIONS = \{\n([\s\S]*?)\n {8}\}\.freeze$/m.exec(ruby)[1];
  const expected = {};
  for (const line of body.split('\n')) {
    const [, key, value] = /^\s*(\w+): (.+?),?$/.exec(line);
    if (value === 'true' || value === 'false') expected[key] = value === 'true';
    else if (value === 'REFERENCE_CHARS') expected[key] = referenceChars;
    // Banzai::Renderer::USER_CONTENT_ID_PREFIX の値 (GitLab の lib/banzai/renderer.rb)
    else if (value === 'Banzai::Renderer::USER_CONTENT_ID_PREFIX') expected[key] = 'user-content-';
    else assert.fail(`OPTIONS の読めない値: ${line}`);
  }
  assert.ok(Object.keys(expected).length >= 25, 'OPTIONS を読めていない');
  // github_pre_lang は OPTIONS ではなく render_options が足す (機能フラグで決まる)。ここでは <pre lang> の形に固定している。
  assert.match(ruby, /github_pre_lang: Feature\.disabled\?\(:use_css_language_classes, resolve_project\)/);
  const { github_pre_lang: preLang, ...ours } = GITLAB_OPTIONS;
  assert.equal(preLang, true);
  assert.deepEqual({ ...ours, only_escape_chars: [...ours.only_escape_chars] }, expected);
});

test('見出しは id を持ち、アンカーを末尾に置く', () => {
  assert.equal(
    renderMarkdown('# 見出し A & B\n'),
    '<h1 id="user-content-見出し-a--b" data-sourcepos="1:1-1:17">見出し A &amp; B' +
      '<a href="#見出し-a--b" aria-label="Link to heading \'見出し A &amp; B\'" data-heading-content="見出し A &amp; B" class="anchor"></a></h1>\n',
  );
  const twice = renderMarkdown('## 同じ\n\n## 同じ\n');
  assert.match(twice, /<h2 id="user-content-同じ" /);
  assert.match(twice, /<h2 id="user-content-同じ-1" /);
});

test('タスクのリストは task-list で、[~] は inapplicable になる', () => {
  const html = renderMarkdown('- [ ] todo\n- [x] done\n- [~] n/a\n');
  assert.match(html, /^<ul class="task-list" data-sourcepos="1:1-3:9">/);
  assert.match(html, /<li class="task-list-item" data-sourcepos="1:1-1:10"><input type="checkbox" data-checkbox-sourcepos="1:4-1:4" class="task-list-item-checkbox" disabled="" \/> todo<\/li>/);
  assert.match(html, /class="task-list-item-checkbox" checked="" disabled="" \/> done/);
  assert.match(html, /<li class="inapplicable task-list-item" [^>]*><input [^>]*class="task-list-item-checkbox" data-inapplicable disabled="" \/> n\/a/);
});

test('表の中のタスクは task-table-item になる', () => {
  const html = renderMarkdown('| t | u |\n|---|---|\n| [x] | [~] |\n');
  assert.equal((html.match(/<td [^>]*class="task-table-item">/g) || []).length, 2);
  assert.match(html, /data-inapplicable disabled=""/);
});

test('参照の記号のエスケープと、プレースホルダーに目印が付く', () => {
  const html = renderMarkdown('a \\#123 \\@user \\*x\\* %{PLACEHOLDER}\n');
  assert.match(html, /<span data-escaped-char data-sourcepos="1:3-1:4">#<\/span>123/);
  assert.match(html, /<span data-escaped-char data-sourcepos="1:9-1:10">@<\/span>user/);
  assert.match(html, /user \*x\* <span data-placeholder>%\{PLACEHOLDER\}<\/span>/); // * は参照の記号ではないので span にしない
});

test('GLFM の記法 (アラート・数式・複数行の引用・説明リスト・脚注・wikilink・autolink)', () => {
  const html = renderMarkdown([
    '> [!note] 題', '> 本文', '', '$`a^2`$ と $x$', '', '```math', 'a=b', '```', '', '```mermaid', 'graph TD;A-->B', '```', '',
    '>>>', '複数行', '>>>', '', '用語', ': 説明', '', '脚注[^1]', '', '[^1]: 注', '', '[[_TOC_]]', '', 'https://example.com', '',
    '```ruby:app.rb', 'puts 1', '```', '',
  ].join('\n'));
  assert.match(html, /<div class="markdown-alert markdown-alert-note" data-sourcepos="1:1-2:8">\n<p class="markdown-alert-title">題<\/p>/);
  assert.match(html, /<code data-math-style="inline" data-sourcepos="4:1-4:7">a\^2<\/code>/);
  // 位置の桁は UTF-8 のバイト数 (「と」は 3 バイト)
  assert.match(html, /<span data-math-style="inline" data-sourcepos="4:13-4:15">x<\/span>/);
  assert.match(html, /<pre lang="math" data-math-style="display" data-sourcepos="6:1-8:3"><code>a=b\n<\/code><\/pre>/);
  assert.match(html, /<pre data-sourcepos="10:1-12:3" lang="mermaid"><code>graph TD;A--&gt;B\n<\/code><\/pre>/);
  assert.match(html, /<blockquote data-sourcepos="14:1-16:3">\n<p data-sourcepos="15:1-15:9">複数行<\/p>\n<\/blockquote>/);
  assert.match(html, /<dl [^>]*>\n<dt [^>]*>用語<\/dt>\n<dd [^>]*>説明<\/dd>\n<\/dl>/);
  assert.match(html, /<sup [^>]*class="footnote-ref"><a href="#fn-1" id="fnref-1" data-footnote-ref>1<\/a><\/sup>/);
  assert.match(html, /<section [^>]*class="footnotes" data-footnotes>/);
  assert.match(html, /<a data-sourcepos="[^"]+" href="_TOC_" data-wikilink="true">_TOC_<\/a>/);
  assert.match(html, /<a data-sourcepos="[^"]+" href="https:\/\/example\.com">https:\/\/example\.com<\/a>/);
  assert.match(html, /<pre lang="ruby:app\.rb" data-sourcepos="[^"]+"><code>puts 1\n<\/code><\/pre>/);
});

test('生の HTML は通す (消毒は後ろの段で行う)', () => {
  assert.match(renderMarkdown('<details><summary>s</summary>x</details>\n\n<script>alert(1)</script>\n'), /<script>alert\(1\)<\/script>/);
});

test('オプションを重ねられる', () => {
  assert.equal(renderMarkdown('text\n', { sourcepos: false }), '<p>text</p>\n');
  assert.match(renderMarkdown('# h\n', { header_slug_prefix: 'file-' }), /<h1 id="user-content-file-h"/);
});

test('誤りは日本語で知らせ、その後も描ける', () => {
  assert.throws(() => renderMarkdown('x', { sourcepos: 'yes' }), /^Error: オプションを読めません: /);
  assert.throws(() => renderMarkdown(null), { name: 'TypeError', message: 'Markdown は文字列で渡してください。' });
  assert.equal(renderMarkdown(''), '');
  assert.equal(renderMarkdown('ok\n', { sourcepos: false }), '<p>ok</p>\n');
});

test('日本語・絵文字・対になっていないサロゲートを扱える', () => {
  assert.equal(renderMarkdown('日本語 😀 と **強調**\n', { sourcepos: false }), '<p>日本語 😀 と <strong>強調</strong></p>\n');
  assert.equal(renderMarkdown('a\ud800b\n', { sourcepos: false }), '<p>a�b</p>\n');
});

test('大きい文書を続けて描ける', () => {
  const big = '# h\n\ntext **b** `c` [l](u)\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- [ ] t\n\n'.repeat(2000);
  const html = renderMarkdown(big);
  assert.equal((html.match(/<table /g) || []).length, 2000);
  for (let i = 0; i < 50; i++) assert.equal(renderMarkdown('x\n', { sourcepos: false }), '<p>x</p>\n');
});

// ---- 入口 (engine/src/lib.rs) を、JavaScript の入口を通さずに呼ぶ ----

function rawWasm() {
  return new WebAssembly.Instance(new WebAssembly.Module(readFileSync(new URL(lock.wasm.path, engineDir))), {}).exports;
}

function readResult(wasm, pointer) {
  const view = new DataView(wasm.memory.buffer);
  const length = view.getUint32(pointer + 1, true);
  const body = new TextDecoder().decode(new Uint8Array(wasm.memory.buffer, pointer + 5, length));
  const status = view.getUint8(pointer);
  wasm.glfm_free(pointer, 5 + length);
  return { status, body };
}

test('入口はヌルの領域と UTF-8 でない入力を、誤りとして返す', () => {
  const wasm = rawWasm();
  assert.deepEqual(readResult(wasm, wasm.glfm_render(0, 0, 0, 0)), { status: 1, body: '入力の領域がありません。' });
  const options = new TextEncoder().encode('{}');
  const optionsPointer = wasm.glfm_alloc(options.length);
  new Uint8Array(wasm.memory.buffer, optionsPointer, options.length).set(options);
  const textPointer = wasm.glfm_alloc(2);
  new Uint8Array(wasm.memory.buffer, textPointer, 2).set([0xff, 0xfe]);
  assert.deepEqual(readResult(wasm, wasm.glfm_render(textPointer, 2, optionsPointer, options.length)), { status: 1, body: 'Markdown が UTF-8 ではありません。' });
  // 長さ 0 の領域 (番兵) も受け付ける
  assert.deepEqual(readResult(wasm, wasm.glfm_render(wasm.glfm_alloc(0), 0, optionsPointer, options.length)), { status: 0, body: '' });
  wasm.glfm_free(textPointer, 2);
  wasm.glfm_free(optionsPointer, options.length);
});

// ---- JavaScript の入口の、誤りからの立ち直り (本物の .wasm を包んだ偽物で確かめる) ----

function wrapped(overrides) {
  const calls = { instantiate: 0, alloc: 0, free: 0 };
  const engine = createEngine({
    memoryLimit: overrides.memoryLimit,
    instantiate() {
      calls.instantiate += 1;
      const real = rawWasm();
      const instance = calls.instantiate;
      return {
        memory: overrides.memory ? overrides.memory(real.memory, instance) : real.memory,
        glfm_alloc(length) {
          calls.alloc += 1;
          return overrides.alloc ? overrides.alloc(real, length, instance, calls) : real.glfm_alloc(length);
        },
        glfm_free(pointer, length) {
          calls.free += 1;
          return real.glfm_free(pointer, length);
        },
        glfm_render(...args) {
          return overrides.render ? overrides.render(real, args, instance) : real.glfm_render(...args);
        },
      };
    },
  });
  return { engine, calls };
}

test('描画の中で例外が出たら、エンジンを作り直して次を描く', () => {
  for (const thrown of [new WebAssembly.RuntimeError('unreachable'), new RangeError('Maximum call stack size exceeded')]) {
    const { engine, calls } = wrapped({
      render(real, args, instance) {
        if (instance === 1) throw thrown;
        return real.glfm_render(...args);
      },
    });
    assert.throws(() => engine('x\n'), (error) => error.message === '描画エンジンが異常終了しました。' && error.cause === thrown);
    assert.equal(calls.free, 0, '捨てたエンジンの領域は返さない');
    assert.equal(engine('ok\n', { sourcepos: false }), '<p>ok</p>\n');
    assert.equal(calls.instantiate, 2);
    assert.equal(calls.free, 3, '入力・オプション・結果を 1 回ずつ返す');
  }
});

test('領域を取れなかったら、エンジンを作り直して次を描く', () => {
  const { engine, calls } = wrapped({
    alloc(real, length, instance, counts) {
      return instance === 1 && counts.alloc === 2 ? 0 : real.glfm_alloc(length);
    },
  });
  assert.throws(() => engine('x\n'), /^Error: 描画エンジンのメモリーを確保できません。$/);
  assert.equal(calls.free, 0);
  assert.equal(engine('ok\n', { sourcepos: false }), '<p>ok</p>\n');
  assert.equal(calls.instantiate, 2);
});

test('オプションの誤りでは作り直さず、取った領域をすべて返す', () => {
  const { engine, calls } = wrapped({});
  assert.throws(() => engine('x\n', { sourcepos: 'yes' }), /^Error: オプションを読めません: /);
  assert.equal(calls.free, 3);
  assert.equal(engine('ok\n', { sourcepos: false }), '<p>ok</p>\n');
  assert.equal(calls.instantiate, 1);
  assert.equal(calls.alloc, 4);
  assert.equal(calls.free, 6);
});

test('メモリーが上限を超えたら、次の描画の前に作り直す', () => {
  const { engine, calls } = wrapped({ memoryLimit: 1 });
  assert.equal(engine('a\n', { sourcepos: false }), '<p>a</p>\n');
  assert.equal(engine('b\n', { sourcepos: false }), '<p>b</p>\n');
  assert.equal(calls.instantiate, 2);
  const roomy = wrapped({});
  roomy.engine('a\n');
  roomy.engine('b\n');
  assert.equal(roomy.calls.instantiate, 1);
});
