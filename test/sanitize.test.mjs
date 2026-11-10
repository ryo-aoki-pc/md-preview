import test from 'node:test';
import assert from 'node:assert/strict';
import { toHtml } from 'hast-util-to-html';
import { fragment, element, text, elements } from '../lib/tree.mjs';
import { sanitizeDocument, sanitizeLinks, safeProtocol, permitUrl } from '../lib/sanitize.mjs';
const cleanTree = html => sanitizeLinks(sanitizeDocument(fragment(html)));
const clean = html => toHtml(cleanTree(html));

test('script は内容ごと消し、コメントも落とす', () => {
  assert.equal(clean('<script>alert(1)</script><p>安全<!--秘密--></p>'), '<p>安全</p>');
});
for (const tag of ['iframe', 'object', 'embed', 'form', 'style', 'link', 'meta', 'base', 'svg', 'math']) test('許可しない要素: ' + tag, () => {
  const tree = cleanTree('<' + tag + ' src="x" onload="alert(1)">文字</' + tag + '>');
  assert.equal(elements(tree, n => n.tagName === tag).length, 0);
  assert.doesNotMatch(toHtml(tree), /\son[a-z]+=/i);
});
test('イベント属性は全要素で落とす', () => {
  assert.equal(clean('<p onclick="x" onmouseover="x">文字<img src="x" onerror="x"></p>'), '<p>文字<img src="x"></p>');
});
for (const source of ['javascript:alert(1)', 'JaVaScRiPt:alert(1)', ' javascript:alert(1) ', 'java\tscript:alert(1)', 'java\nscript:alert(1)', 'java\rscript:alert(1)', 'java1script:alert(1)', 'data:text/html,x', 'DATA:text/html,x', 'vbscript:msgbox(1)', 'vB\tscript:x', 'javascript&#58;alert(1)', '&#106;avascript:alert(1)']) test('危険なリンクを落とす: ' + JSON.stringify(source), () => {
  const tree = cleanTree('<a href="' + source + '">文字</a><img src="' + source + '">');
  assert.equal(elements(tree).some(e => Object.keys(e.node.properties).some(k => ['href', 'src'].includes(k))), false);
});
test('リンクの前後の空白を取り除き、不正な URL を拒む', () => {
  assert.equal(clean('<a href="  https://example.com/x  ">文字</a>'), '<a href="https://example.com/x">文字</a>');
  assert.equal(clean('<a href="http://[broken">文字</a>'), '<a>文字</a>');
});
test('a の独自プロトコルは残し、img と cite は http(s) だけにする', () => {
  assert.match(clean('<a href="custom:thing">文字</a>'), /href="custom:thing"/);
  assert.equal(clean('<img src="ftp://example.com/a" longdesc="javascript:x"><q cite="ftp://example.com">文字</q>'), '<img><q>文字</q>');
  assert.match(clean('<img src="HTTPS://example.com/a">'), /src="https:\/\/example.com\/a"/);
});
test('SanitizeLink は四つの URL 属性を同じ規則で処理する', () => {
  const tree = { type: 'root', children: [element('a', { href: ' x ', src: 'JavaScript:x', dataSrc: 'data:x', dataCanonicalSrc: 'vbscript:x' })] };
  sanitizeLinks(tree);
  assert.deepEqual(tree.children[0].properties, { href: 'x' });
  assert.equal(safeProtocol('Java1Script'), false);
  assert.equal(safeProtocol('https'), true);
  assert.equal(safeProtocol(null), false);
  assert.equal(permitUrl('https://[broken'), false);
  assert.equal(permitUrl('\ud800'), false);
});
test('入れ子の a をほどく（HTML5 の補正を通さない木でも処理する）', () => {
  const tree = { type: 'root', children: [element('a', { href: '/outer' }, [element('div', {}, [element('a', { href: '/inner' }, [text('文字')])])])] };
  const result = sanitizeDocument(tree);
  assert.equal(toHtml(result), '<a href="/outer"><div>文字</div></a>');
});
test('foreign content を含む入れ子の a にもリンクを残さない', () => {
  const tree = cleanTree('<a href="/outer"><svg><a xlink:href="javascript:x">内側</a></svg></a>');
  for (const { ancestors } of elements(tree, n => n.tagName === 'a')) assert.equal(ancestors.some(n => n.tagName === 'a'), false);
});
test('名前空間付きの属性を全て落とす', () => {
  const tree = { type: 'root', children: [element('a', { href: '/safe', xLinkHref: 'javascript:x', 'xlink:href': 'javascript:x', xmlLang: 'ja', xmlns: 'x' }, [text('文字')])] };
  assert.equal(toHtml(sanitizeDocument(tree)), '<a href="/safe">文字</a>');
});
test('リストの外の li と表の外の部品は中身だけ残す', () => {
  const tree = { type: 'root', children: [element('li', {}, [text('項目')]), element('tbody', {}, [element('tr', {}, [element('td', {}, [text('セル')])])])] };
  assert.equal(toHtml(sanitizeDocument(tree)), '項目セル');
  assert.equal(clean('<ul><li>中</li></ul><table><tr><td>中</td></tr></table>'), '<ul><li>中</li></ul><table><tbody><tr><td>中</td></tr></tbody></table>');
});
test('表の style は認めた text-align だけ残す', () => {
  assert.equal(clean('<p style="text-align: center">外</p><table><tr><td style="color:red;text-align: right; background:url(javascript:x)">右</td><th style="text-align:justify">消す</th></tr></table>'), '<p>外</p><table><tbody><tr><td style="text-align: right">右</td><th>消す</th></tr></tbody></table>');
});
test('JSON 表の fields は key・label・sortable だけ残す', () => {
  const tree = { type: 'root', children: [element('table', { dataTableFields: JSON.stringify([{ key: 'x', label: 'X', sortable: true, formatter: 'alert' }]), dataTableMarkdown: 'true', dataTableFilter: 'true' })] };
  const p = sanitizeDocument(tree).children[0].properties;
  assert.equal(p.dataTableFields, '[{"key":"x","label":"X","sortable":true}]');
  assert.equal(p.dataTableMarkdown, 'true'); assert.equal(p.dataTableFilter, 'true');
});
for (const fields of ['oops', '{}', '[1]', '[null]']) test('不正な fields を落とす: ' + fields, () => {
  const tree = { type: 'root', children: [element('table', { dataTableFields: fields })] };
  assert.equal(sanitizeDocument(tree).children[0].properties.dataTableFields, undefined);
});
test('id は見出しと脚注だけに限り、name は a だけ残す', () => {
  const tree = cleanTree('<h1 id="user-content-ok">見出し</h1><h2 id="bad">見出し</h2><a id="fnref-a" name="keep">脚注</a><a id="user-content-no">消す</a><ul><li id="fn-a">注</li><li id="bad">消す</li></ul><div id="user-content-no" name="no">消す</div>');
  assert.deepEqual(elements(tree, n => n.properties.id !== undefined).map(e => e.node.properties.id), ['user-content-ok', 'fnref-a', 'fn-a']);
  assert.deepEqual(elements(tree, n => n.properties.name !== undefined).map(e => e.node.properties.name), ['keep']);
});
const classCases = [
  ['a', 'anchor', true], ['a', 'anchor extra', false], ['div', 'markdown-alert markdown-alert-note', true],
  ['div', 'markdown-alert markdown-alert-unknown', false], ['p', 'markdown-alert-title', true], ['p', 'note', false],
  ['span', 'idiff left right addition', true], ['span', 'idiff evil', false], ['span', 'addition', false],
  ['code', 'idiff', true], ['code', 'language-js', false], ['ul', 'task-list', true], ['ol', 'task-list extra', false],
  ['li', 'task-list-item', true], ['li', 'inapplicable task-list-item', true], ['li', 'task-list-item inapplicable', false],
  ['td', 'task-table-item', true], ['th', 'task-table-item extra', false], ['img', 'glfm-float-left glfm-float-none', true], ['img', 'glfm-float-right evil', false],
];
for (const [tag, value, keep] of classCases) test('class の条件: ' + tag + '.' + value, () => {
  let node = element(tag, { className: value.split(' ') });
  const target = node;
  if (tag === 'li') node = element('ul', {}, [node]);
  if (['td', 'th'].includes(tag)) node = element('table', {}, [element('tr', {}, [node])]);
  const result = sanitizeDocument({ type: 'root', children: [node] });
  const found = elements(result, n => n.tagName === target.tagName)[0].node;
  assert.equal(found.properties.className !== undefined, keep);
});
for (const [parent, type, className, keep] of [['li', 'checkbox', 'task-list-item-checkbox', true], ['td', 'checkbox', 'task-list-item-checkbox', true], ['th', 'checkbox', 'task-list-item-checkbox', true], ['p', 'checkbox', 'task-list-item-checkbox', false], ['li', 'radio', 'task-list-item-checkbox', false], ['li', 'checkbox', 'task-list-item-checkbox evil', false], ['li', 'checkbox', '', false]]) test('input の条件: ' + [parent, type, className].join('/'), () => {
  let wrapper = element(parent, {}, [element('input', { type, className: className.split(' '), disabled: true, dataInapplicable: '', dataIndeterminate: '', dataCheckboxSourcepos: '1:1-1:1' })]);
  if (parent === 'li') wrapper = element('ul', {}, [wrapper]);
  if (['td', 'th'].includes(parent)) wrapper = element('table', {}, [element('tr', {}, [wrapper])]);
  const inputs = elements(sanitizeDocument({ type: 'root', children: [wrapper] }), n => n.tagName === 'input');
  assert.equal(inputs.length, Number(keep));
  if (keep) { assert.equal(inputs[0].node.properties.dataIndeterminate, ''); assert.equal(inputs[0].node.properties.dataCheckboxSourcepos, '1:1-1:1'); }
});
test('rel は license だけを残す', () => {
  assert.equal(clean('<a href="/x" rel="license">残す</a><a rel="nofollow license">消す</a>'), '<a href="/x" rel="license">残す</a><a>消す</a>');
});
test('数式・見出し・脚注・元位置に必要な属性を残す', () => {
  const html = clean('<span data-math-style="inline" data-placeholder="name" data-sourcepos="1:1-1:2" data-escaped-char="#">x</span><section data-footnotes><a data-footnote-ref data-footnote-backref data-footnote-backref-idx="1" data-heading-content="題">注</a></section>');
  for (const attr of ['data-math-style', 'data-placeholder', 'data-sourcepos', 'data-escaped-char', 'data-footnotes', 'data-footnote-ref', 'data-footnote-backref', 'data-footnote-backref-idx', 'data-heading-content']) assert.ok(html.includes(attr), attr);
});

test('SanitizeLink は URL のある要素が一万個を超すと安全なエラー本文にする', () => {
  const tree={type:'root',children:Array.from({length:10001},()=>element('a',{href:'/safe'}))};
  const result=sanitizeLinks(tree);
  assert.equal(elements(result,n=>n.tagName==='a').length,0);
  assert.match(toHtml(result),/Timeout while sanitizing links/);
});
