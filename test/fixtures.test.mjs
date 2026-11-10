// 許可する正規化（この一覧以外は、既定で要素・属性・文字を厳密に比べる）:
// 1. 属性の順序: HTML の属性順には意味がないため、属性名を並べ替える。
// 2. 遅延画像: ImageLazyLoadFilter の lazy・data-src・仮画像と decoding=async を戻す。
//    同フィルターが設定した decoding も遅延画像だけから外し、それ以外の画像属性は残す。
// 3. 脚注: 参照先の ID が実在することを先に確かめてから、GitLab の乱数接尾辞だけを外す。
// 4. 絵文字: gl-emoji の囲みを中の文字に戻す（手元ではエンジンが Unicode にする）。
// 5. 配色の字句: pre.code.highlight の span.line 内の span を文字に戻す。
//    行の要素・属性・文字は保ち、既知の言語には別に字句 span があることを確かめる。
// 解決された参照を文字へ戻す正規化は行わない。
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { inspect } from 'node:util';
import { fromHtml } from 'hast-util-from-html';
import { renderDocument } from '../lib/pipeline.mjs';
import { elements, content, hasClass } from '../lib/tree.mjs';
const fixtures = new URL('./fixtures/', import.meta.url);
const metadata = JSON.parse(readFileSync(new URL('fixtures.json', fixtures), 'utf8'));

// 既知の差の表。場所は正規化後の木の一箇所に限定し、理由と両側の値を固定する。
export const KNOWN_DIFFERENCES = [
  {
    "fixture": "basics",
    "kind": "attributes",
    "path": [
      "children",
      18,
      "children",
      2,
      "properties"
    ],
    "oraclePath": [
      "children",
      18,
      "children",
      2,
      "properties"
    ],
    "reason": "24 行の外部画像のリンク: AssetProxy の署名・設定を持たないため、元の URL を使う",
    "values": {
      "href": {
        "local": "https://example.com/b.png",
        "oracle": "https://user-content.gitlab-static.net/55889327826d97cec59dd5441ea2b66e2dc49364/68747470733a2f2f6578616d706c652e636f6d2f622e706e67"
      },
      "dataCanonicalSrc": {
        "local": null,
        "oracle": "https://example.com/b.png"
      }
    }
  },
  {
    "fixture": "basics",
    "kind": "attributes",
    "path": [
      "children",
      18,
      "children",
      2,
      "children",
      0,
      "properties"
    ],
    "oraclePath": [
      "children",
      18,
      "children",
      2,
      "children",
      0,
      "properties"
    ],
    "reason": "24 行の外部画像: AssetProxy の署名・設定を持たない",
    "values": {
      "src": {
        "local": "https://example.com/b.png",
        "oracle": "https://user-content.gitlab-static.net/55889327826d97cec59dd5441ea2b66e2dc49364/68747470733a2f2f6578616d706c652e636f6d2f622e706e67"
      },
      "dataCanonicalSrc": {
        "local": null,
        "oracle": "https://example.com/b.png"
      }
    }
  },
  {
    "fixture": "media",
    "kind": "attributes",
    "path": [
      "children",
      18,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "oraclePath": [
      "children",
      18,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "reason": "19 行の外部動画: AssetProxy の署名・設定を持たない",
    "values": {
      "src": {
        "local": "https://example.com/v.mov",
        "oracle": "https://user-content.gitlab-static.net/6d81b96e84b7de3fefa592475836757dcffa4dad/68747470733a2f2f6578616d706c652e636f6d2f762e6d6f76"
      },
      "dataCanonicalSrc": {
        "local": null,
        "oracle": "https://example.com/v.mov"
      }
    }
  },
  {
    "fixture": "media",
    "kind": "attributes",
    "path": [
      "children",
      22,
      "children",
      0,
      "properties"
    ],
    "oraclePath": [
      "children",
      22,
      "children",
      0,
      "properties"
    ],
    "reason": "23 行の外部画像のリンク: AssetProxy の署名・設定を持たない",
    "values": {
      "href": {
        "local": "https://example.com/x.png",
        "oracle": "https://user-content.gitlab-static.net/8ff07430de1fea0d1fda13fb2ea129a0a66bbc1a/68747470733a2f2f6578616d706c652e636f6d2f782e706e67"
      },
      "dataCanonicalSrc": {
        "local": null,
        "oracle": "https://example.com/x.png"
      }
    }
  },
  {
    "fixture": "media",
    "kind": "attributes",
    "path": [
      "children",
      22,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "oraclePath": [
      "children",
      22,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "reason": "23 行の外部画像: AssetProxy の署名・設定を持たない",
    "values": {
      "src": {
        "local": "https://example.com/x.png",
        "oracle": "https://user-content.gitlab-static.net/8ff07430de1fea0d1fda13fb2ea129a0a66bbc1a/68747470733a2f2f6578616d706c652e636f6d2f782e706e67"
      },
      "dataCanonicalSrc": {
        "local": null,
        "oracle": "https://example.com/x.png"
      }
    }
  },
  {
    "fixture": "math-diagrams",
    "kind": "attributes",
    "path": [
      "children",
      8,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "oraclePath": [
      "children",
      8,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "reason": "mermaid のコード: GitLab では AssetProxy が有効だが、手元は使わない",
    "values": {
      "dataProxiedUrls": {
        "local": null,
        "oracle": "{}"
      }
    }
  },
  {
    "fixture": "math-diagrams",
    "kind": "attributes",
    "path": [
      "children",
      10,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "oraclePath": [
      "children",
      10,
      "children",
      0,
      "children",
      0,
      "properties"
    ],
    "reason": "mermaid のコード: GitLab では AssetProxy が有効だが、手元は使わない",
    "values": {
      "dataProxiedUrls": {
        "local": null,
        "oracle": "{\"Bob:\":\"https://user-content.gitlab-static.net/47f7765dc19f53f0baa8b6ee0ce4923778334f52/426f623a\"}"
      }
    }
  },
  {
    "fixture": "inline-glfm",
    "kind": "attributes",
    "path": [
      "children",
      14,
      "children",
      7,
      "properties"
    ],
    "oraclePath": [
      "children",
      14,
      "children",
      7,
      "properties"
    ],
    "reason": "15 行の外部 URL の wikilink: WikiLinkGollumFilter は今回の対象外",
    "values": {
      "className": {
        "local": null,
        "oracle": [
          "gfm"
        ]
      },
      "dataCanonicalSrc": {
        "local": null,
        "oracle": "https://example.com"
      },
      "dataLink": {
        "local": null,
        "oracle": "true"
      },
      "dataGollum": {
        "local": null,
        "oracle": "true"
      }
    }
  },
  {
    "fixture": "code",
    "kind": "node",
    "path": [
      "children",
      28
    ],
    "reason": "61 行の PlantUML: 手元の plantuml を有効にしていない場合は原文のまま",
    "local": {
      "type": "element",
      "tagName": "div",
      "properties": {
        "className": [
          "gl-relative",
          "markdown-code-block",
          "js-markdown-code"
        ]
      },
      "children": [
        {
          "type": "element",
          "tagName": "pre",
          "properties": {
            "className": [
              "code",
              "highlight",
              "js-syntax-highlight",
              "language-plantuml"
            ],
            "dataCanonicalLang": "plantuml",
            "dataSourcepos": "61:1-63:3",
            "v-pre": "true"
          },
          "children": [
            {
              "type": "element",
              "tagName": "code",
              "properties": {},
              "children": [
                {
                  "type": "element",
                  "tagName": "span",
                  "properties": {
                    "className": [
                      "line"
                    ],
                    "dataLang": "plantuml",
                    "id": "LC1"
                  },
                  "children": [
                    {
                      "type": "text",
                      "value": "Bob -> Alice : hello"
                    }
                  ]
                }
              ]
            }
          ]
        },
        {
          "type": "element",
          "tagName": "copy-code",
          "properties": {},
          "children": []
        },
        {
          "type": "element",
          "tagName": "insert-code-snippet",
          "properties": {},
          "children": []
        }
      ]
    },
    "oracle": {
      "type": "element",
      "tagName": "a",
      "properties": {
        "className": [
          "no-attachment-icon"
        ],
        "dataDiagram": "plantuml",
        "dataDiagramSrc": "data:text/plain;base64,Qm9iIC0+IEFsaWNlIDogaGVsbG8=",
        "href": "https://plantuml.gitlab-static.net/png/U9npoazIqBLJSCp9J4vLi5B8ICt9oGS0F7S6Am00",
        "rel": [
          "nofollow",
          "noreferrer",
          "noopener"
        ],
        "target": "_blank"
      },
      "children": [
        {
          "type": "element",
          "tagName": "img",
          "properties": {
            "src": "https://plantuml.gitlab-static.net/png/U9npoazIqBLJSCp9J4vLi5B8ICt9oGS0F7S6Am00"
          },
          "children": []
        }
      ]
    }
  },
  {
    "fixture": "html",
    "kind": "oracle-only",
    "path": [
      "children",
      22
    ],
    "reason": "36 行のコメント: Markdown API は非 IE なら allow_comments を有効にするが、手元は依頼どおりコメントを除く",
    "oracle": {
      "type": "comment",
      "value": " コメント "
    }
  },
  {
    "fixture": "html",
    "kind": "oracle-only",
    "path": [
      "children",
      18
    ],
    "reason": "30 行の iframe: GitLab の IframeToImg と AssetProxy の設定が必要なので今回の実装対象外",
    "oracle": {
      "type": "element",
      "tagName": "a",
      "properties": {
        "className": [
          "no-attachment-icon"
        ],
        "dataCanonicalSrc": "https://example.com",
        "href": "https://user-content.gitlab-static.net/83358f7feafaab45a479bdd987c5c9a608f676ce/68747470733a2f2f6578616d706c652e636f6d",
        "rel": [
          "nofollow",
          "noreferrer",
          "noopener"
        ],
        "target": "_blank"
      },
      "children": [
        {
          "type": "element",
          "tagName": "img",
          "properties": {
            "dataCanonicalSrc": "https://example.com",
            "src": "https://user-content.gitlab-static.net/83358f7feafaab45a479bdd987c5c9a608f676ce/68747470733a2f2f6578616d706c652e636f6d"
          },
          "children": []
        }
      ]
    }
  }
];
function mergeText(children) {
  const result = [];
  for (const child of children) {
    const last = result.at(-1);
    if (last?.type === 'text' && child.type === 'text') last.value += child.value;
    else result.push(child);
  }
  return result;
}
function validateFootnotes(tree) {
  const ids = new Set(elements(tree, n => n.properties.id !== undefined).map(e => e.node.properties.id));
  for (const { node } of elements(tree, n => n.tagName === 'a' && (n.properties.dataFootnoteRef !== undefined || n.properties.dataFootnoteBackref !== undefined))) {
    const href = node.properties.href;
    assert.ok(typeof href === 'string' && href.startsWith('#') && ids.has(href.slice(1)), '脚注の参照先が存在しない: ' + href);
  }
}
export function normalizeHtml(html, { oracle = false } = {}) {
  const tree = fromHtml(html, { fragment: true });
  validateFootnotes(tree);
  let suffix;
  if (oracle) {
    const ids = elements(tree, n => n.tagName === 'li' && String(n.properties.id || '').startsWith('fn-')).filter(e => e.ancestors.some(n => n.tagName === 'section' && n.properties.dataFootnotes !== undefined)).map(e => e.node.properties.id);
    if (ids.length) {
      suffix = ids[0].match(/-(\d+)$/)?.[0];
      assert.ok(suffix, 'GitLab の脚注に乱数の接尾辞がない');
      assert.ok(ids.every(id => id.endsWith(suffix)), '脚注の乱数が文書内で一致していない');
    }
  }
  function convert(node, inHighlight = false, inLine = false) {
    if (node.type === 'text' || node.type === 'comment') return [{ type: node.type, value: node.value }];
    if (node.type === 'doctype') return [{ type: 'doctype' }];
    const highlighted = inHighlight || node.tagName === 'pre' && hasClass(node, 'code') && hasClass(node, 'highlight');
    const line = inLine || highlighted && node.tagName === 'span' && hasClass(node, 'line');
    const children = mergeText((node.children || []).flatMap(child => convert(child, highlighted, line)));
    if (node.tagName === 'gl-emoji') return [{ type: 'text', value: content(node) }];
    if (inLine && node.tagName === 'span') return children;
    if (node.type === 'root') return [{ type: 'root', children }];
    const properties = structuredClone(node.properties);
    if (node.tagName === 'img' && hasClass(node, 'lazy') && Object.hasOwn(properties, 'dataSrc')) {
      assert.equal(properties.src, 'data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==', '遅延画像の仮画像が変わった');
      properties.src = properties.dataSrc; delete properties.dataSrc;
      properties.className = properties.className.filter(c => c !== 'lazy');
      if (!properties.className.length) delete properties.className;
      if (properties.decoding === 'async') delete properties.decoding;
    }
    if (suffix) {
      if ((node.tagName === 'li' || properties.dataFootnoteRef !== undefined) && /^fn(?:ref)?-/.test(String(properties.id)) && properties.id.endsWith(suffix)) properties.id = properties.id.slice(0, -suffix.length);
      if ((properties.dataFootnoteRef !== undefined || properties.dataFootnoteBackref !== undefined) && /^#fn(?:ref)?-/.test(String(properties.href)) && properties.href.endsWith(suffix)) properties.href = properties.href.slice(0, -suffix.length);
    }
    return [{ type: 'element', tagName: node.tagName, properties: Object.fromEntries(Object.entries(properties).sort(([a], [b]) => a.localeCompare(b))), children }];
  }
  return convert(tree)[0];
}
function atPath(object, path) { return path.reduce((value, key) => value?.[key], object); }
function maskKnownDifferences(name, local, oracle) {
  for (const difference of KNOWN_DIFFERENCES.filter(d => d.fixture === name)) {
    const left = atPath(local, difference.path), right = atPath(oracle, difference.oraclePath || difference.path);
    if (difference.kind === 'attributes') {
      for (const [key, values] of Object.entries(difference.values)) {
        assert.deepEqual(left[key] ?? null, values.local, '既知の差の手元の属性が変わった: ' + difference.reason + '/' + key);
        assert.deepEqual(right[key] ?? null, values.oracle, '既知の差の GitLab の属性が変わった: ' + difference.reason + '/' + key);
        left[key] = '[既知の差]'; right[key] = '[既知の差]';
      }
    } else if (difference.kind === 'unwrap-local') {
      assert.deepEqual(left, difference.local, '既知の差の囲みが変わった: ' + difference.reason);
      const parentPath = difference.path.slice(0, -1), siblings = atPath(local, parentPath), index = difference.path.at(-1);
      siblings.splice(index, 1, ...left.children);
      const merged = mergeText(siblings); siblings.splice(0, siblings.length, ...merged);
    } else if (difference.kind === 'oracle-only') {
      assert.deepEqual(right, difference.oracle, '既知の差の GitLab の要素が変わった: ' + difference.reason);
      const parentPath = difference.path.slice(0, -1), siblings = atPath(oracle, parentPath), index = difference.path.at(-1);
      siblings.splice(index, 1);
      const merged = mergeText(siblings); siblings.splice(0, siblings.length, ...merged);
    } else {
      assert.equal(difference.kind, 'node');
      assert.deepEqual(left, difference.local, '既知の差の手元の要素が変わった: ' + difference.reason);
      assert.deepEqual(right, difference.oracle, '既知の差の GitLab の要素が変わった: ' + difference.reason);
      const parentPath = difference.path.slice(0, -1), key = difference.path.at(-1);
      atPath(local, parentPath)[key] = {type: 'known', value: difference.reason};
      atPath(oracle, parentPath)[key] = {type: 'known', value: difference.reason};
    }
  }
}
export function firstDifference(left, right, path = []) {
  if (Object.is(left, right)) return null;
  if (typeof left !== typeof right || left === null || right === null || typeof left !== 'object') return { path, local: left, oracle: right };
  if (Array.isArray(left) && Array.isArray(right)) {
    for (let i = 0; i < Math.min(left.length, right.length); i++) { const difference = firstDifference(left[i], right[i], [...path, i]); if (difference) return difference; }
    return left.length === right.length ? null : { path: [...path, Math.min(left.length, right.length)], local: left[Math.min(left.length, right.length)], oracle: right[Math.min(left.length, right.length)] };
  }
  const leftKeys = Object.keys(left).sort(), rightKeys = Object.keys(right).sort();
  if (leftKeys.length !== rightKeys.length || leftKeys.some((key, i) => key !== rightKeys[i])) return { path, local: left, oracle: right };
  for (const key of leftKeys) { const difference = firstDifference(left[key], right[key], [...path, Array.isArray(left) ? Number(key) : key]); if (difference) return difference; }
  return null;
}
function contextAt(tree, path) {
  const index = path.findLastIndex(key => typeof key === 'number');
  if (index < 0) return atPath(tree, path.slice(0, -1));
  const siblings = atPath(tree, path.slice(0, index)), position = path[index];
  return { index: position, nodes: siblings?.slice(Math.max(0, position - 1), position + 2) };
}
const nearby = value => inspect(value, {depth: 5, maxArrayLength: 5, maxStringLength: 240, breakLength: 100}).slice(0, 2200);
for (const name of Object.keys(metadata.fixtures)) test('GitLab の見本と厳密に比較する: ' + name + (KNOWN_DIFFERENCES.some(d => d.fixture === name) ? '（既知の差あり）' : '（一致）'), () => {
  const localHtml = renderDocument(readFileSync(new URL(name + '.md', fixtures), 'utf8')).html;
  const oracleHtml = readFileSync(new URL(name + '.gitlab.html', fixtures), 'utf8');
  const local = normalizeHtml(localHtml), oracle = normalizeHtml(oracleHtml, { oracle: true });
  maskKnownDifferences(name, local, oracle);
  const difference = firstDifference(local, oracle);
  if (difference) {
    assert.fail('最初の相違: ' + JSON.stringify(difference.path) + '\n手元: ' + nearby(difference.local) + '\nGitLab: ' + nearby(difference.oracle) + '\n前後（手元）: ' + nearby(contextAt(local, difference.path)) + '\n前後（GitLab）: ' + nearby(contextAt(oracle, difference.path)));
  }
});
test('既知の言語のコードブロックには字句の span がある', () => {
  const tree = fromHtml(renderDocument(readFileSync(new URL('code.md', fixtures), 'utf8')).html, {fragment:true});
  for (const language of ['javascript', 'ruby', 'python', 'shell', 'json', 'yaml', 'diff', 'html']) {
    const pre = elements(tree, n => n.tagName === 'pre' && hasClass(n, 'language-' + language))[0]?.node;
    assert.ok(pre, 'コードブロックがない: ' + language);
    assert.ok(elements(pre, n => n.tagName === 'span' && !hasClass(n, 'line')).length, '字句の span がない: ' + language);
  }
});

test('有効な PlantUML は URL・rel・行の位置以外を GitLab の図の見本と比較する', () => {
  const result = renderDocument(readFileSync(new URL('code.md', fixtures), 'utf8'), { plantuml: {} });
  const tree = normalizeHtml(result.html);
  const actual = elements(tree, (node) => node.tagName === 'a' && node.properties.dataDiagram === 'plantuml')[0].node;
  const expected = structuredClone(
    KNOWN_DIFFERENCES.find((difference) => difference.reason.startsWith('61 行の PlantUML')).oracle,
  );
  assert.equal(actual.children[0].properties.dataSourcepos, '61:1-63:3');
  delete actual.properties.href;
  delete expected.properties.href;
  delete actual.properties.rel;
  delete expected.properties.rel;
  delete actual.children[0].properties.src;
  delete expected.children[0].properties.src;
  delete actual.children[0].properties.dataSourcepos;
  assert.deepEqual(actual, expected);
});

test('比較器はコードの空白・class・リンク・チェック状態・sourcepos を厳密に区別する', () => {
  const samples = [
    ['<pre><code> x\n</code></pre>', '<pre><code>x\n</code></pre>'],
    ['<p class="a b">x</p>', '<p class="b a">x</p>'],
    ['<a href="/a">x</a>', '<a href="/b">x</a>'],
    ['<input type="checkbox" checked>', '<input type="checkbox">'],
    ['<p data-sourcepos="1:1-1:2">x</p>', '<p data-sourcepos="2:1-2:2">x</p>'],
    ['<p><span>x</span></p>', '<p>x</p>'],
    ['<a class="gfm">@name</a>', '@name'],
    ['<p>x\n y</p>', '<p>x y</p>'],
  ];
  for (const [left, right] of samples) assert.ok(firstDifference(normalizeHtml(left), normalizeHtml(right)), left);
  assert.equal(firstDifference(normalizeHtml('<p title="x" dir="auto">x</p>'), normalizeHtml('<p dir="auto" title="x">x</p>')), null);
});
test('配色の字句を除いても行番号・言語・空白は比較する', () => {
  const block = contents => '<pre class="code highlight"><code><span id="LC1" class="line" data-lang="javascript">' + contents + '</span></code></pre>';
  const plain = normalizeHtml(block('x '));
  assert.equal(firstDifference(plain, normalizeHtml(block('<span class="k">x</span> '))), null);
  assert.ok(firstDifference(plain, normalizeHtml(block('x'))));
  assert.ok(firstDifference(plain, normalizeHtml(block('x ').replace('LC1', 'LC2'))));
  assert.ok(firstDifference(plain, normalizeHtml(block('x ').replace('javascript', 'python'))));
});
test('脚注の乱数を外す前に、行き先と戻り先の存在を確かめる', () => {
  assert.throws(() => normalizeHtml('<sup><a data-footnote-ref href="#fn-missing">1</a></sup>'), /参照先が存在しない/);
  const html = '<sup><a data-footnote-ref id="fnref-1-2-4567" href="#fn-1-4567">1</a></sup><section data-footnotes><ol><li id="fn-1-4567"><a data-footnote-backref href="#fnref-1-2-4567">戻る</a></li></ol></section>';
  const result = normalizeHtml(html, {oracle:true});
  assert.equal(result.children[0].children[0].properties.id, 'fnref-1-2');
  assert.equal(result.children[0].children[0].properties.href, '#fn-1');
});
