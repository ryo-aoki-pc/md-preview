import test from 'node:test';
import assert from 'node:assert/strict';
import { toHtml } from 'hast-util-to-html';
import { fragment, elements, element, text, content } from '../lib/tree.mjs';
import codeLanguage from '../lib/filters/code-language.mjs';
import jsonTable from '../lib/filters/json-table.mjs';
import spacedLink from '../lib/filters/spaced-link.mjs';
import attributes from '../lib/filters/attributes.mjs';
import math from '../lib/filters/math.mjs';
import color, { parseColor } from '../lib/filters/color.mjs';
import mermaid from '../lib/filters/mermaid.mjs';
import videoLink from '../lib/filters/video-link.mjs';
import audioLink from '../lib/filters/audio-link.mjs';
import tableOfContentsTag from '../lib/filters/table-of-contents-tag.mjs';
import footnote from '../lib/filters/footnote.mjs';
import inlineDiff from '../lib/filters/inline-diff.mjs';
import imageLink from '../lib/filters/image-link.mjs';
import externalLink from '../lib/filters/external-link.mjs';
import taskList from '../lib/filters/task-list.mjs';
import setDirection from '../lib/filters/set-direction.mjs';
const apply = (filter, html) => filter(fragment(html), {});
const output = (filter, html) => toHtml(apply(filter, html));
const first = (tree, tag) => elements(tree, n => n.tagName === tag)[0]?.node;

test('CodeLanguage は data-lang・class・lang の優先順と meta を使う', () => {
  const tree = apply(codeLanguage, '<pre data-lang="ruby:red file" data-meta="ignored" lang="python"><code data-lang="js" class="language-html keep" lang="css">x</code></pre>');
  const pre = first(tree, 'pre'), code = first(tree, 'code');
  assert.deepEqual(pre.properties, { dataLang: 'ruby', dataCanonicalLang: 'ruby', dataLangParams: 'red file' });
  assert.deepEqual(code.properties, { className: ['keep'] });
});
for (const [source, language, params] of [
  ['<pre><code data-lang="python">x</code></pre>', 'python', undefined],
  ['<pre><code class="language-js">x</code></pre>', 'js', undefined],
  ['<pre lang="ruby"><code lang="js">x</code></pre>', 'ruby', undefined],
  ['<pre><code lang="ruby">x</code></pre>', 'ruby', undefined],
  ['<pre data-meta="line"><code data-lang="ruby:">x</code></pre>', 'ruby', 'line'],
  ['<pre><code lang="ruby" data-meta="meta">x</code></pre>', 'ruby', 'meta'],
]) test('CodeLanguage の言語抽出: ' + source, () => {
  const tree = apply(codeLanguage, source), pre = first(tree, 'pre');
  assert.equal(pre.properties.dataCanonicalLang, language); assert.equal(pre.properties.dataLangParams, params);
  assert.equal(first(tree, 'code').properties.lang, undefined);
});
test('CodeLanguage は裸の pre を包み、複数の子要素には触らない', () => {
  assert.equal(output(codeLanguage, '<pre lang="ruby">a<b>b</b></pre>'), '<pre data-lang="ruby" data-canonical-lang="ruby"><code>a<b>b</b></code></pre>');
  assert.equal(output(codeLanguage, '<pre lang="ruby"><code>x</code><b>y</b></pre>'), '<pre lang="ruby"><code>x</code><b>y</b></pre>');
  assert.equal(output(codeLanguage, '<pre><code>x</code></pre>'), '<pre><code>x</code></pre>');
});
const jsonBlock = input => '<pre data-canonical-lang="json" data-lang-params="table"><code>' + String(input).replaceAll('&', '&amp;').replaceAll('<', '&lt;') + '</code></pre>';
test('JsonTable はラベルを文字として、caption とセルを Markdown として処理する', () => {
  const tree = apply(jsonTable, jsonBlock(JSON.stringify({ fields: [{key:'a',label:'<b>A</b>',sortable:true}], items:[{a:'**太字**'}], caption:'*題*', filter:true, markdown:true })));
  assert.equal(content(first(tree, 'th')), '<b>A</b>');
  assert.equal(toHtml(first(tree, 'td')), '<td><strong>太字</strong></td>');
  assert.equal(toHtml(first(tree, 'caption')), '<caption><p><em>題</em></p></caption>');
  assert.equal(first(tree, 'table').properties.dataTableFilter, 'true');
  assert.doesNotMatch(toHtml(tree), /data-sourcepos/);
});
test('JsonTable は最初の item のキーを推定し、複数段落と欠けたセルを扱う', () => {
  const tree = apply(jsonTable, jsonBlock(JSON.stringify({ items:[{a:'一\n\n二',b:3},{b:false,c:'余分'}], markdown:true })));
  assert.equal(elements(tree, n => n.tagName === 'th').length, 2);
  assert.equal(elements(first(tree,'td'), n => n.tagName === 'p').length, 2);
  assert.equal(elements(tree, n => n.tagName === 'td')[2].node.children.length, 0);
});
for (const input of ['oops', 'null', '[]', '{"items":[{}]}', '{"markdown":false,"items":[{}]}', '{"markdown":true,"items":[]}', '{"markdown":true,"items":[1]}', '{"markdown":true,"items":[{}],"fields":[1]}']) test('JsonTable の無効入力はコードのまま: ' + input, () => assert.ok(first(apply(jsonTable, jsonBlock(input)), 'pre')));
test('SpacedLink は空白を URL エンコードしてリンクと画像を作る', () => {
  const tree = apply(spacedLink, '<p>[頁](page slug "題") ![図](test image.png)</p>');
  assert.equal(first(tree, 'a').properties.href, 'page%20slug');
  assert.equal(first(tree, 'a').properties.title, '題');
  assert.equal(first(tree, 'img').properties.src, 'test%20image.png');
});
for (const wrapper of ['a', 'code', 'kbd', 'pre', 'script', 'style', 'span data-math-style="inline"']) test('SpacedLink の除外する親: ' + wrapper, () => {
  const tag = wrapper.split(' ')[0]; const source = '<' + wrapper + '>[頁](page slug)</' + tag + '>';
  assert.equal(output(spacedLink, source), toHtml(fragment(source)));
});
test('Attributes は画像直後の幅・高さ・float だけを使う', () => {
  const tree = apply(attributes, '<p><img src="x">{width="50%" height=120px .float-right onclick=x bad=1}後</p>');
  const img = first(tree, 'img');
  assert.equal(img.properties.width, '50%'); assert.equal(img.properties.height, '120px'); assert.deepEqual(img.properties.className, ['glfm-float-right']);
  assert.equal(content(first(tree, 'p')), '後');
});
test('Attributes は間の空白・複数行・長すぎる値・不正なサイズを拒む', () => {
  assert.match(output(attributes, '<img src="x"> {width=1}'), /\{width=1\}/);
  assert.match(output(attributes, '<img src="x">{width=1\nheight=2}'), /height=2/);
  const img = first(apply(attributes, '<img src="x">{width=10000 height=-1 .other}'), 'img');
  assert.deepEqual(img.properties, {src:'x'});
  assert.match(output(attributes, '<img src="x">{' + 'x'.repeat(101) + '}'), /x{101}/);
});
test('Math は math の pre に display と class を足し、既存の style は保つ', () => {
  const tree = apply(math, '<pre data-canonical-lang="math"><code>x</code></pre><span data-math-style="inline">x</span><code data-math-style="inline">y</code>');
  assert.equal(first(tree, 'pre').properties.dataMathStyle, 'display');
  for (const {node} of elements(tree, n => n.properties.dataMathStyle !== undefined)) assert.deepEqual(node.properties.className, ['js-render-math']);
});
test('Math は 1000 ノードで止める', () => {
  const tree = apply(math, '<span data-math-style="inline">x</span>'.repeat(1001));
  assert.equal(elements(tree, n => n.properties.className?.includes('js-render-math')).length, 1000);
});
for (const source of ['#abc','#abcd','#abcdef','#abcdef12','RGB(0,255,99)','rgba(0%,100%,00%,.5)','rgb(1, 2, 3,100%)','hsl(-90deg,50%,100%)','hsla(.5rad,0%,99%,1.00)']) test('ColorParser の許可する色: ' + source, () => assert.equal(parseColor(source), source));
for (const source of ['red','#ab','#abcde','#abc\n','rgb(256,0,0)','rgb(1%,2,3)','rgba(1,2,3,1.1)','hsl(1.5,1%,1%)','hsl(0,101%,1%)','rgb( 1,2,3)','rgb(1 2 3)','url(javascript:x)']) test('ColorParser の拒む色: ' + source, () => assert.equal(parseColor(source), null));
test('Color は chip を作り、バックスラッシュ付きは文字だけに戻す', () => {
  const tree = apply(color, '<code>#abc</code><code>\\#abc</code><code>無効</code>');
  assert.equal(first(tree,'span').properties.className[0], 'gfm-color_chip');
  assert.match(toHtml(tree), /background-color: #abc;/);
  assert.equal(elements(tree, n => n.tagName === 'code')[1].node.children[0].value, '#abc');
  assert.equal(elements(tree, n => n.tagName === 'code')[2].node.children.length, 1);
});
test('Mermaid は対象コードだけに目印を付ける', () => {
  assert.equal(output(mermaid, '<pre data-canonical-lang="mermaid"><code>x</code></pre><pre><code>y</code></pre>'), '<pre data-canonical-lang="mermaid"><code class="js-render-mermaid">x</code></pre><pre><code>y</code></pre>');
});
for (const [filter, tag, ext] of [[videoLink,'video','mp4'],[videoLink,'video','m4v'],[videoLink,'video','mov'],[videoLink,'video','webm'],[videoLink,'video','ogv'],[audioLink,'audio','mp3'],[audioLink,'audio','oga'],[audioLink,'audio','ogg'],[audioLink,'audio','spx'],[audioLink,'audio','wav']]) test('PlayableLink の拡張子: ' + ext, () => {
  const tree = apply(filter, '<img src="clip.' + ext.toUpperCase() + '" alt="題" width="50" height="40">');
  const media = first(tree,tag); assert.ok(media); assert.equal(media.properties.dataTitle,'題'); assert.equal(media.properties.controls,'true');
  if (tag === 'video') { assert.equal(media.properties.width,50); assert.equal(media.properties.preload,'metadata'); } else assert.equal(media.properties.width,undefined);
});
test('PlayableLink はリンク内・不明な拡張子・query 付き・空の src を処理しない', () => {
  const tree = apply(videoLink, '<a href="x"><img src="x.mp4"></a><img src="x.exe"><img src="x.mp4?v=1"><img>');
  assert.equal(elements(tree,n=>n.tagName==='img').length,4);
});
test('PlayableLink は canonical-src と title を優先する', () => {
  const tree = apply(videoLink, '<img src="/proxy" data-canonical-src="https://example.com/x.mp4" title="題" alt="代替">');
  const media = first(tree,'video'); assert.equal(media.properties.src,'/proxy'); assert.equal(media.properties.dataCanonicalSrc,'https://example.com/x.mp4'); assert.equal(media.properties.dataTitle,'題');
});
const heading = (level, id, title) => '<h' + level + ' id="user-content-' + id + '">' + title + '<a class="anchor" href="#' + id + '"></a></h' + level + '>';
test('TableOfContentsTag は段差と同じ深さを扱い、書式を文字にする', () => {
  const tree = apply(tableOfContentsTag, '<p>[tOc]</p>' + heading(2,'a','<b>A</b>') + heading(4,'b','B') + heading(2,'c','C') + heading(1,'d','D'));
  assert.equal(toHtml(tree.children[0]), '<ul class="section-nav"><li><a href="#a">A</a><ul><li><a href="#b">B</a></li></ul></li><li><a href="#c">C</a></li><li><a href="#d">D</a></li></ul>');
});
test('TableOfContentsTag は gollum と複数の目次を処理し、文中のタグは残す', () => {
  const tree = apply(tableOfContentsTag, '<p><a data-wikilink="true">_TOC_</a></p><p>[TOC]</p><p>文中 [TOC]</p>' + heading(1,'a','A'));
  assert.equal(elements(tree,n=>n.properties.className?.includes('section-nav')).length,2); assert.match(toHtml(tree), /文中 \[TOC\]/);
});
test('TableOfContentsTag はアンカーのない見出しを無視し、空の目次を消す', () => assert.equal(output(tableOfContentsTag,'<p>[TOC]</p><h1>見出し</h1>'), '<h1>見出し</h1>'));
const notes = '<p><sup><a data-footnote-ref id="fnref-a" href="#fn-a">1</a></sup></p><section data-footnotes><ol><li id="fn-a"><a href="#fnref-a" data-footnote-backref-idx="1">戻る</a></li></ol></section>';
test('Footnote は乱数を足さず class と戻り先のラベルを復元する', () => {
  const tree = apply(footnote,notes);
  assert.deepEqual(first(tree,'section').properties.className,['footnotes']);
  assert.deepEqual(first(tree,'sup').properties.className,['footnote-ref']);
  const back = elements(tree,n=>n.tagName==='a')[1].node; assert.equal(back.properties.title,'Back to reference 1'); assert.equal(back.properties.ariaLabel,back.properties.title); assert.equal(first(tree,'li').properties.id,'fn-a');
});
test('Footnote は section・参照先・id がないときにも落ちない', () => {
  assert.equal(output(footnote,'<sup><a data-footnote-ref>1</a></sup>'),'<sup><a data-footnote-ref="">1</a></sup>');
  const tree=apply(footnote,'<sup><a data-footnote-ref id="fnref-missing">1</a></sup><section data-footnotes></section>'); assert.equal(first(tree,'sup').properties.className,undefined);
});
test('Footnote は 1000 個を超すと処理しない', () => {
  const tree=apply(footnote,'<sup><a data-footnote-ref>1</a></sup>'.repeat(1001)+'<section data-footnotes></section>'); assert.equal(first(tree,'section').properties.className,undefined);
});
test('InlineDiff は両方の括弧・空の内容・入れ子・HTML 文字のエスケープを扱う', () => {
  const html=output(inlineDiff,'<p>{+a+} [+b+] {-c-} [-d-] {++} {-x [+y+]-} {+&lt;script&gt;+}</p>');
  assert.equal((html.match(/idiff/g)||[]).length,8); assert.match(html,/(?:&lt;|&#x3C;)script>/); assert.doesNotMatch(html,/<script>/);
});
for (const tag of ['pre','code','tt']) test('InlineDiff の除外する親: '+tag,()=>assert.equal(output(inlineDiff,'<'+tag+'>{+x+}</'+tag+'>'),'<'+tag+'>{+x+}</'+tag+'>'));
test('ImageLink は裸の画像を包み、既存のリンクと data-src が空の画像には触らない',()=>{
  const tree=apply(imageLink,'<img src="x.png"><a href="x"><img src="y"></a><img data-src="" src="z">');
  assert.equal(elements(tree,n=>n.tagName==='a').length,2); assert.equal(first(tree,'a').properties.href,'x.png'); assert.deepEqual(first(tree,'a').properties.rel,['noopener','noreferrer']);
});
test('ImageLink は元の URL と図の情報をリンクへ移す',()=>{
  const tree=apply(imageLink,'<img src="proxy" data-src="lazy" data-canonical-src="origin" data-diagram="mermaid" data-diagram-src="source">');
  assert.equal(first(tree,'a').properties.href,'lazy'); assert.equal(first(tree,'a').properties.dataCanonicalSrc,'origin'); assert.equal(first(tree,'a').properties.dataDiagram,'mermaid'); assert.equal(first(tree,'img').properties.dataDiagram,undefined);
});
test('ExternalLink は http(s)・scheme のないホストに rel と target を足す',()=>{
  for(const href of ['https://example.com/a','http://example.com','//example.com']) {
    const tree=apply(externalLink,'<a href="'+href+'" rel="license">文字</a>'); const p=first(tree,'a').properties;
    assert.equal(p.target,'_blank'); assert.deepEqual(p.rel,['nofollow','noreferrer','noopener','license']);
  }
});
test('ExternalLink は相対・フラグメント・別の scheme を区別する',()=>{
  for(const href of ['/a','../a','#a','mailto:a@example.com','ftp://example.com/a']) assert.equal(first(apply(externalLink,'<a href="'+href+'">文字</a>'),'a').properties.target,undefined);
});
test('ExternalLink は不正な URL を外部とし、RTLO を表示用に置き換える',()=>{
  const tree=apply(externalLink,'<a href="http://[broken">危\u202e険</a>'); const a=first(tree,'a'); assert.equal(a.properties.target,'_blank'); assert.equal(content(a),'危%E2%80%AE険');
});
test('ExternalLink は punycode と RTLO の URL を tooltip で見せる',()=>{
  for(const href of ['https://xn--r8jz45g.xn--zckzah/a','https://example.com/%E2%80%AE']) {
    const a=first(apply(externalLink,'<a href="'+href+'">文字</a>'),'a'); assert.ok(a.properties.className.includes('has-tooltip')); assert.equal(a.properties.title,new URL(href).href);
  }
});
test('ExternalLink は canonical-src を判定に使う',()=>{
  const a=first(apply(externalLink,'<a href="/proxy" data-canonical-src="https://example.com/a">文字</a>'),'a'); assert.equal(a.properties.target,'_blank'); assert.equal(a.properties.href,'/proxy');
});
test('TaskList はサブリストを含めずに本文からラベルを作る',()=>{
  const input=first(apply(taskList,'<ul><li><input class="task-list-item-checkbox"> 本文 <b>強調</b><ul><li>子</li></ul></li></ul>'),'input'); assert.equal(input.properties.ariaLabel,'Check option: 本文 強調');
});
test('TaskList は長い本文を単語の切れ目で短くする',()=>{
  const input=first(apply(taskList,'<ul><li><input class="task-list-item-checkbox">'+('word '.repeat(30))+'</li></ul>'),'input'); assert.ok(input.properties.ariaLabel.endsWith('…')); assert.ok(input.properties.ariaLabel.length<=114);
});
test('TaskList は表の同じ列の見出しからラベルを作る',()=>{
  const tree=apply(taskList,'<table><tr><th>名前</th><th>完了</th></tr><tr><td>A</td><td class="task-table-item"><input class="task-list-item-checkbox"></td></tr></table>'); assert.equal(first(tree,'input').properties.ariaLabel,'Check option in column "完了"');
});
test('TaskList は空の本文に既定のラベルを使う',()=>assert.equal(first(apply(taskList,'<ul><li><input class="task-list-item-checkbox"></li></ul>'),'input').properties.ariaLabel,'Check option'));
test('TaskList の [~] は本文の文字だけを s で包み、段落とサブリストを避ける',()=>{
  const tree=apply(taskList,'<ul><li><input class="task-list-item-checkbox" data-inapplicable> 本文 <b>強調</b><p>段落</p><div>区画</div><ul><li>子</li></ul></li></ul>');
  assert.equal(elements(tree,n=>n.tagName==='s').length,2); assert.equal(first(tree,'p').children[0].type,'text'); assert.match(toHtml(tree), /<input[^>]*> <s/);
});
test('SetDirection は最上位だけを変更し、section-nav を除く',()=>{
  const tree=apply(setDirection,'<p dir="rtl">本文</p><div><p>中</p></div><ul class="section-nav"><li>目次</li></ul><table></table>');
  assert.equal(first(tree,'p').properties.dir,'auto'); assert.equal(elements(tree,n=>n.tagName==='p')[1].node.properties.dir,undefined); assert.equal(first(tree,'ul').properties.dir,undefined); assert.equal(first(tree,'table').properties.dir,'auto');
});

test('InlineDiff は追加が削除をまたぐ入れ子も保持する', () => {
  assert.equal(output(inlineDiff, '<p>[+前 {-中-} 後+]</p>'), '<p><span class="idiff left right addition">前 <span class="idiff left right deletion">中</span> 後</span></p>');
});
test('JsonTable は false の caption を省き、配列と Hash は Ruby の文字列表現にする', () => {
  const tree = apply(jsonTable, jsonBlock(JSON.stringify({markdown:true, caption:false, items:[{a:[1,null], b:{x:true}}]})));
  assert.equal(first(tree,'caption'),undefined);
  assert.deepEqual(elements(tree,n=>n.tagName==='td').map(e=>content(e.node)),['[1, nil]','{"x"=>true}']);
});
test('CodeLanguage は空白だけの meta を使わない', () => {
  const pre=first(apply(codeLanguage,'<pre data-meta="   "><code lang="ruby:">x</code></pre>'),'pre');
  assert.equal(pre.properties.dataLangParams,undefined);
});
test('ExternalLink は大文字の http と子要素の RTLO を処理する', () => {
  const a=first(apply(externalLink,'<a href="HTTPS://example.com"><img alt="危\u202e険" src="x"></a>'),'a');
  assert.deepEqual(a.properties.rel,['nofollow','noreferrer','noopener']);
  assert.equal(first(a,'img').properties.alt,'危%E2%80%AE険');
});
test('TaskList の省略は絵文字を一文字として数える', () => {
  const a=first(apply(taskList,'<ul><li><input class="task-list-item-checkbox">'+ '😀'.repeat(80) +'</li></ul>'),'input');
  assert.equal(a.properties.ariaLabel,'Check option: '+'😀'.repeat(80));
  const b=first(apply(taskList,'<ul><li><input class="task-list-item-checkbox">'+ '😀'.repeat(101) +'</li></ul>'),'input');
  assert.equal(b.properties.ariaLabel,'Check option: '+'😀'.repeat(99)+'…');
});

test('Ruby の正規表現に合わせ、Unicode の空白と改行を区別する', () => {
  assert.equal(parseColor('rgb(1,\u00a02,3)'), null);
  assert.equal(parseColor('#abc\u2028'), null);
  assert.match(output(inlineDiff, '<p>{+a\u2028b+}</p>'), /class="idiff left right addition"/);
});
