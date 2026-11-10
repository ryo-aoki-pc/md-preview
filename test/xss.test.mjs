// 悪意のある入力を renderDocument に通し、出力にスクリプトが動く経路が残らないことを確かめる。
// 文字列の検査だけでなく、出力を HTML として読み直した木 (ブラウザが innerHTML で作る木と同じ規則) を調べる。
// 読み直したときに要素の境界が変わる入力 (mutation XSS) も、ここで捕まえる。
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';

import { fromHtml } from 'hast-util-from-html';
import { visit } from 'unist-util-visit';

import { DocumentError, renderDocument } from '../lib/pipeline.mjs';
import { gitlabSchema } from '../lib/sanitize.mjs';

// 消毒が通す要素と、消毒の後ろのフィルターが作る要素。これ以外が出力にあれば誤り。
const allowedTags = new Set([
  ...gitlabSchema.tagNames,
  'video', 'audio', // VideoLink・AudioLink が画像から作る
  'copy-code', 'insert-code-snippet', // SyntaxHighlight が置く操作用の要素
]);
// URL を持つ属性。hast の名前。
const urlProperties = ['href', 'src', 'cite', 'longDesc', 'dataSrc', 'dataCanonicalSrc', 'poster', 'action', 'formAction', 'data', 'xLinkHref'];
// style を持ってよいのは、表のセル (text-align) と、色のチップ (ColorFilter が検証した色) だけ。
const styleAllowed = (node, parent) =>
  (['th', 'td'].includes(node.tagName) && /^text-align: (center|left|right)$/.test(node.properties.style)) ||
  (node.tagName === 'span' && parent?.tagName === 'span' && (parent.properties.className || []).includes('gfm-color_chip') &&
    /^background-color: [#A-Za-z0-9(),.% ]+;$/.test(node.properties.style));

function problems(html) {
  const found = [];
  const tree = fromHtml(html, { fragment: true });
  visit(tree, (node, index, parent) => {
    if (node.type === 'comment') found.push('コメント');
    if (node.type !== 'element') return;
    if (!allowedTags.has(node.tagName)) found.push(`要素 <${node.tagName}>`);
    for (const [name, value] of Object.entries(node.properties)) {
      const text = Array.isArray(value) ? value.join(' ') : String(value);
      if (/^on/i.test(name)) found.push(`属性 ${name}`);
      if (name === 'style' && !styleAllowed(node, parent)) found.push(`style="${text}"`);
      if (['srcDoc', 'formAction', 'action', 'data', 'xLinkHref', 'ping', 'background', 'manifest'].includes(name)) found.push(`属性 ${name}`);
      if (urlProperties.includes(name)) {
        // ブラウザは、スキームの中のタブ・改行と、前の空白や制御文字を捨ててから解釈する。
        const compact = text.replace(/[\u0000- ]+/g, '').toLowerCase();
        if (/^(javascript|vbscript|data|livescript|mocha|jar|blob|filesystem):/.test(compact)) found.push(`${name}="${text}"`);
      }
    }
  });
  return found;
}

const payloads = [
  '<script>alert(1)</script>',
  '<SCRIPT SRC=//example.com/x.js></SCRIPT>',
  '<scr<script>ipt>alert(1)</scr</script>ipt>',
  '<img src=x onerror=alert(1)>',
  '<img src="x" ONERROR="alert(1)">',
  '<img src=x onerror\n=alert(1)>',
  '<img/src=x/onerror=alert(1)>',
  '<img src="javascript:alert(1)">',
  '<img src="data:image/svg+xml,<svg onload=alert(1)>">',
  '<a href="javascript:alert(1)">x</a>',
  '<a href="JaVaScRiPt:alert(1)">x</a>',
  '<a href=" javascript:alert(1)">x</a>',
  '<a href="java\tscript:alert(1)">x</a>',
  '<a href="java\nscript:alert(1)">x</a>',
  '<a href="java&#x09;script:alert(1)">x</a>',
  '<a href="&#106;avascript:alert(1)">x</a>',
  '<a href="&#x6A;&#x61;&#x76;&#x61;&#x73;&#x63;&#x72;&#x69;&#x70;&#x74;&#x3A;alert(1)">x</a>',
  '<a href="\u0001javascript:alert(1)">x</a>',
  '<a href="vbscript:msgbox(1)">x</a>',
  '<a href="data:text/html,<script>alert(1)</script>">x</a>',
  '<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">x</a>',
  '[x](javascript:alert(1))',
  '[x](JAVASCRIPT:alert(1))',
  '[x](<javascript:alert(1)>)',
  '[x](data:text/html,<script>alert(1)</script>)',
  '[x]: javascript:alert(1)\n\n[x]',
  '<javascript:alert(1)>',
  '![x](javascript:alert(1))',
  '![x](x.png "t\\" onerror=\\"alert(1)")',
  '![x](x.png){width=1 onerror=alert(1)}',
  '![x](x.png){width="1\\" onerror=\\"alert(1)"}',
  '![x](x" onerror="alert(1).png)',
  '![x](x.mp4" onerror="alert(1))',
  '<iframe src="javascript:alert(1)"></iframe>',
  '<iframe srcdoc="<script>alert(1)</script>"></iframe>',
  '<object data="javascript:alert(1)"></object>',
  '<embed src="javascript:alert(1)">',
  '<svg onload=alert(1)>',
  '<svg><script>alert(1)</script></svg>',
  '<svg><a xlink:href="javascript:alert(1)"><text>x</text></a></svg>',
  '<svg><style><img src=x onerror=alert(1)></style></svg>',
  '<math><mi xlink:href="javascript:alert(1)">x</mi></math>',
  '<math><mtext><table><mglyph><style><!--</style><img title="--&gt;&lt;/mglyph&gt;&lt;img src=1 onerror=alert(1)&gt;">',
  '<noscript><p title="</noscript><img src=x onerror=alert(1)>">',
  '<textarea><img src=x onerror=alert(1)></textarea>',
  '<title><img src=x onerror=alert(1)></title>',
  '<xmp><img src=x onerror=alert(1)></xmp>',
  '<plaintext><img src=x onerror=alert(1)>',
  '<listing><img src=x onerror=alert(1)></listing>',
  '<style>@import "javascript:alert(1)";</style>',
  '<style><img src=x onerror=alert(1)></style>',
  '<div style="background:url(javascript:alert(1))">x</div>',
  '<td style="text-align: center; background:url(javascript:alert(1))">x</td>',
  '<table><tr><td style="text-align: left\\; position: fixed">x</td></tr></table>',
  '<form action="javascript:alert(1)"><input type=submit formaction="javascript:alert(1)"></form>',
  '<button formaction="javascript:alert(1)">x</button>',
  '<input type="image" src="x" onerror="alert(1)">',
  '<input type="checkbox" class="task-list-item-checkbox" onclick="alert(1)">',
  '<ul><li><input type="checkbox" class="task-list-item-checkbox" onclick="alert(1)" autofocus onfocus="alert(1)"> x</li></ul>',
  '<details open ontoggle=alert(1)><summary>x</summary></details>',
  '<video src=x onerror=alert(1)></video>',
  '<video><source onerror=alert(1)></video>',
  '<audio src=x onerror=alert(1)></audio>',
  '<body onload=alert(1)>',
  '<meta http-equiv="refresh" content="0;url=javascript:alert(1)">',
  '<link rel="stylesheet" href="javascript:alert(1)">',
  '<base href="javascript:alert(1)//">',
  '<a href="https://example.com" target="_blank" onclick="alert(1)" onmouseover="alert(1)">x</a>',
  '<a href="x"><a href="javascript:alert(1)">y</a></a>',
  '<p class="x" id="gp-content" name="body">x</p>',
  '<img name="body" id="gp-header" src="x.png">',
  '<a id="gp-banner" name="getElementById">x</a>',
  '<h1 id="gp-content">x</h1>',
  '<!-- --><script>alert(1)</script>',
  '<!--><script>alert(1)</script>-->',
  '<![CDATA[<script>alert(1)</script>]]>',
  '<?xml version="1.0"?><script>alert(1)</script>',
  '`<script>alert(1)</script>`',
  '```html\n<script>alert(1)</script>\n```',
  '```"><script>alert(1)</script>\ncode\n```',
  '```js" onmouseover="alert(1)\ncode\n```',
  '```mermaid\ngraph TD; A["<img src=x onerror=alert(1)>"]\n```',
  '```math\n\\href{javascript:alert(1)}{x}\n```',
  '$`\\href{javascript:alert(1)}{x}`$',
  '```json:table\n{"items":[{"a":"<script>alert(1)</script>","b":"<img src=x onerror=alert(1)>"}],"markdown":true}\n```',
  '```json:table\n{"fields":[{"key":"a","label":"<img src=x onerror=alert(1)>"}],"items":[{"a":"[x](javascript:alert(1))"}],"markdown":true,"caption":"<script>alert(1)</script>"}\n```',
  '`#F00" onmouseover="alert(1)`',
  '`RGB(0,0,0);background:url(javascript:alert(1))`',
  '`#000;}</style><script>alert(1)</script>`',
  '{+ <script>alert(1)</script> +} {- <img src=x onerror=alert(1)> -}',
  '[[_TOC_]]\n\n# <img src=x onerror=alert(1)>\n\n## <script>alert(1)</script>\n\n### [x](javascript:alert(1))',
  '# x" onmouseover="alert(1)',
  'x[^1]\n\n[^1]: <script>alert(1)</script><img src=x onerror=alert(1)>',
  'x[^"><img src=x onerror=alert(1)>]\n\n[^"><img src=x onerror=alert(1)>]: y',
  '> [!note] <img src=x onerror=alert(1)>\n> <script>alert(1)</script>',
  '- [ ] <img src=x onerror=alert(1)>\n- [x] <script>alert(1)</script>\n- [~] [x](javascript:alert(1))',
  '| a |\n|---|\n| <img src=x onerror=alert(1)> |\n| [x](javascript:alert(1)) |',
  '---\ntitle: "</code></pre><script>alert(1)</script>"\n---\n\nx',
  '[[javascript:alert(1)]] [[x|javascript:alert(1)]]',
  'https://example.com/"onmouseover="alert(1)',
  '<https://example.com/"><script>alert(1)</script>>',
  '%{<script>alert(1)</script>} [x](%{javascript:alert(1)})',
  ':smile: <gl-emoji onclick="alert(1)">x</gl-emoji>',
  '<copy-code onclick="alert(1)"></copy-code><insert-code-snippet onclick="alert(1)"></insert-code-snippet>',
  '<span class="gfm-color_chip"><span style="background-color: red; position: fixed; top: 0">x</span></span>',
  '<div class="gl-relative markdown-code-block js-markdown-code"><pre class="code highlight" onmouseover="alert(1)"><code>x</code></pre></div>',
  '<pre data-canonical-lang="mermaid" onmouseover="alert(1)"><code>graph TD</code></pre>',
  '<pre lang="js" class="js-render-mermaid" data-math-style="display" onclick="alert(1)"><code onclick="alert(1)">x</code></pre>',
  '<a class="no-attachment-icon" href="javascript:alert(1)"><img src="x.png"></a>',
  '<section data-footnotes class="footnotes" onclick="alert(1)"><ol><li id="fn-1" onclick="alert(1)">x</li></ol></section>',
  '<table data-table-fields=\'[{"key":"a","label":"<script>alert(1)</script>"}]\' data-table-markdown="true" onclick="alert(1)"><tr><td>x</td></tr></table>',
  'a\u0000<script>alert(1)</script>',
  '<a href="//example.com" onclick=alert(1)//>x</a>',
  '<<script>script>alert(1)<</script>/script>',
  '<img src=x:alert(1) onerror=eval(src)>',
  '<a href="x" style="position:fixed;top:0;left:0;width:100%;height:100%">overlay</a>',
  '<div align="center" style="position:fixed;inset:0;z-index:9999">overlay</div>',
];

test('悪意のある入力から、スクリプトが動く経路が出力に残らない', () => {
  for (const payload of payloads) {
    for (const text of [payload, `段落 ${payload} 続き`, `> ${payload}`, `- ${payload}`, `| a |\n|---|\n| ${payload.replaceAll('\n', ' ')} |`, `<div>\n\n${payload}\n\n</div>`]) {
      const { html } = renderDocument(text + '\n');
      assert.deepEqual(problems(html), [], `入力: ${JSON.stringify(text)}\n出力: ${html}`);
    }
  }
});

test('見本の入力でも、許可していない要素・属性が出力に無い', () => {
  const dir = new URL('fixtures/', import.meta.url);
  for (const name of readdirSync(dir).filter((file) => file.endsWith('.md'))) {
    const { html } = renderDocument(readFileSync(new URL(name, dir), 'utf8'));
    assert.deepEqual(problems(html), [], name);
  }
});

test('出力を読み直して書き出し直しても、危ない要素・属性が現れない', async () => {
  // ブラウザは出力を 1 回読む (上の試験)。ここでは、読んで書き出した HTML をもう 1 回読んでも安全なことを確かめる
  // (段落の中の表のように、読み直すと入れ子が変わる出力があるため)。
  const { toHtml } = await import('hast-util-to-html');
  const dir = new URL('fixtures/', import.meta.url);
  const texts = [...payloads, ...readdirSync(dir).filter((file) => file.endsWith('.md')).map((name) => readFileSync(new URL(name, dir), 'utf8'))];
  for (const text of texts) {
    const { html } = renderDocument(text + '\n');
    const again = toHtml(fromHtml(html, { fragment: true }));
    assert.deepEqual(problems(again), [], `入力: ${JSON.stringify(text.slice(0, 200))}\n読み直した出力: ${again}`);
    // 2 回目からは変わらない
    assert.equal(toHtml(fromHtml(again, { fragment: true })), again, `入力: ${JSON.stringify(text.slice(0, 200))}`);
  }
});

test('検査そのものが、危ない出力を見つける', () => {
  assert.deepEqual(problems('<p onclick="x">a</p>'), ['属性 onClick']);
  assert.deepEqual(problems('<a href="java\tscript:alert(1)">a</a>'), ['href="java\tscript:alert(1)"']);
  assert.deepEqual(problems('<svg></svg>'), ['要素 <svg>']);
  assert.deepEqual(problems('<p style="color: red">a</p>'), ['style="color: red"']);
  assert.deepEqual(problems('<!-- x -->'), ['コメント']);
  assert.deepEqual(problems('<img src="data:image/png;base64,AAAA">'), ['src="data:image/png;base64,AAAA"']);
});

test('JavaScript の特別な名前の要素・言語・属性でも、落ちずに描く', () => {
  const fence = String.fromCharCode(96).repeat(3);
  const names = ['constructor', '__proto__', 'prototype', 'toString', 'valueOf', 'hasOwnProperty', 'isPrototypeOf', '__defineGetter__'];
  for (const name of names) {
    for (const text of [
      `<${name} class="x" id="y" style="z">text</${name}>`,
      `${fence}${name}\ncode\n${fence}`,
      `${fence}${name}:${name}\ncode\n${fence}`,
      `<pre lang="${name}"><code class="language-${name} ${name}">code</code></pre>`,
      `<span class="${name}">a</span> <div class="${name}">b</div> <li class="${name}">c</li>`,
      `<a ${name}="1" href="x" class="${name}">link</a> <img src="x.png" ${name}="1" class="${name}">`,
      `![x](x.png){${name}=1 width=10}`,
      `${fence}json:table\n{"fields":[{"key":"${name}","label":"${name}"}],"items":[{"${name}":"v","a":1}],"markdown":true}\n${fence}`,
      `[${name}]: https://example.com\n\n[${name}] [x][${name}] :${name}: {+ ${name} +} \`${name}\``,
    ]) {
      const { html } = renderDocument(text + '\n');
      assert.deepEqual(problems(html), [], `入力: ${JSON.stringify(text)}\n出力: ${html}`);
      assert.doesNotMatch(html, /native code|\[object /, `入力: ${JSON.stringify(text)}`);
    }
  }
});

test('入れ子が深すぎる文書は、途中の HTML を返さず、分かる誤りになる', () => {
  for (const text of ['<div>'.repeat(20000) + 'x\n', '>'.repeat(5000) + ' x\n']) {
    assert.throws(
      () => renderDocument(text),
      (error) => error instanceof DocumentError && error.message === '入れ子が深すぎるため、描画できません。' && error.cause instanceof RangeError,
    );
  }
  // その後も描ける
  assert.match(renderDocument('ok\n').html, /<p [^>]*>ok<\/p>/);
  // ふつうの深さ (数百段) は描く
  assert.match(renderDocument('>'.repeat(200) + ' x\n').html, /^<blockquote /);
});

test('readInclude と exists を渡しても、既存の悪意のある入力を安全に描く', () => {
  const context = { file: 'docs/main.md', dir: 'docs/', readInclude: () => null, exists: () => false };
  for (const payload of payloads) {
    for (const text of [payload, '段落 ' + payload + ' 続き', '> ' + payload, '- ' + payload, '| a |\n|---|\n| ' + payload.replaceAll('\n', ' ') + ' |', '<div>\n\n' + payload + '\n\n</div>']) {
      const { html } = renderDocument(text + '\n', context);
      assert.deepEqual(problems(html), [], '入力: ' + JSON.stringify(text) + '\n出力: ' + html);
    }
  }
});

test('取り込んだ本文に悪意のある入力があっても、同じ消毒を通す', async () => {
  const { toHtml } = await import('hast-util-to-html');
  for (const payload of payloads) {
    for (const text of [payload, '段落 ' + payload + ' 続き', '> ' + payload, '- ' + payload, '| a |\n|---|\n| ' + payload.replaceAll('\n', ' ') + ' |', '<div>\n\n' + payload + '\n\n</div>']) {
      const result = renderDocument('::include{file=payload.md}\n', { file: 'docs/main.md', dir: 'docs/', readInclude: () => text, exists: () => false });
      assert.equal(result.includes[0].ok, true);
      assert.deepEqual(problems(result.html), [], '取り込んだ入力: ' + JSON.stringify(text) + '\n出力: ' + result.html);
      const again = toHtml(fromHtml(result.html, { fragment: true }));
      assert.deepEqual(problems(again), [], '読み直した出力: ' + again);
    }
  }
});

test('include の指定名が悪意のある入力でも、失敗文に実行できる経路を作らない', () => {
  for (const payload of payloads) {
    const filename = payload.replaceAll('\n', ' ');
    const { html } = renderDocument('::include{file=' + filename + '}\n', { readInclude: () => null, exists: () => false });
    assert.deepEqual(problems(html), [], '指定名: ' + JSON.stringify(filename) + '\n出力: ' + html);
  }
});
