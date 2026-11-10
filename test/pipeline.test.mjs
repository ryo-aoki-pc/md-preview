import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { renderDocument, frontMatter } from '../lib/pipeline.mjs';
import { fragment, elements } from '../lib/tree.mjs';
const fixtures=new URL('./fixtures/',import.meta.url);
const render=name=>renderDocument(readFileSync(new URL(name,fixtures),'utf8'));
for(const name of readdirSync(fixtures).filter(n=>n.endsWith('.md'))) test('素材全体を安全に描画する: '+name,()=>{
  const {html}=render(name);
  assert.doesNotMatch(html,/<script\b|javascript:|\son[a-z]+\s*=/i);
  assert.equal(typeof html,'string');
});
test('見出し一覧は最終の見出しから作り、アンカーの文字を含めない',()=>{
  assert.deepEqual(renderDocument('# 題と *強調*\n\n## 二\n\n# 題と *強調*').headings,[{level:1,id:'題と-強調',text:'題と 強調'},{level:2,id:'二',text:'二'},{level:1,id:'題と-強調-1',text:'題と 強調'}]);
  assert.deepEqual(renderDocument('<h2 id="bad">生の見出し</h2>').headings,[{level:2,id:'',text:'生の見出し'}]);
});
test('両方の目次タグを描画する',()=>{
  assert.match(render('toc.md').html,/class="section-nav"/);
  assert.match(render('toc-bracket.md').html,/class="section-nav"/);
});
test('アラート・タスク・不適用タスクを描画する',()=>{
  assert.match(render('alerts-quotes.md').html,/markdown-alert markdown-alert-note/);
  const html=render('lists.md').html;
  assert.match(html,/task-list-item-checkbox/); assert.match(html,/data-inapplicable/); assert.match(html,/<s class="inapplicable">/); assert.match(html,/aria-label="Check option/);
});
test('数式と mermaid の目印を残し、コードブロックを包む',()=>{
  const html=render('math-diagrams.md').html;
  assert.match(html,/js-render-math/); assert.match(html,/js-render-mermaid/); assert.match(html,/markdown-code-block js-markdown-code/); assert.match(html,/data-lang="mermaid"/);
});
test('脚注の id を安定させ、戻り先の class を復元する',()=>{
  const first=render('footnotes.md').html;
  assert.equal(first,render('footnotes.md').html); assert.match(first,/class="footnotes"/); assert.match(first,/class="footnote-backref"/); assert.match(first,/id="fn-1"/);
});
test('画像をリンクで包み、音声・動画を置く',()=>{
  const html=render('media.md').html;
  assert.match(html,/no-attachment-icon/); assert.match(html,/<video /); assert.match(html,/<audio /); assert.match(html,/video-container/); assert.match(html,/audio-container/);
});
test('色・差分・絵文字を描画する',()=>{
  const html=render('inline-glfm.md').html;
  assert.match(html,/gfm-color_chip/); assert.match(html,/idiff left right addition/); assert.match(html,/idiff left right deletion/); assert.ok(!html.includes(':smile:')); assert.doesNotMatch(html,/<gl-emoji/);
});
test('JSON 表の Markdown セルを消毒後に出す',()=>{
  const html=render('json-table.md').html;
  assert.match(html,/data-table-markdown="true"/); assert.match(html,/<strong>22<\/strong>/); assert.match(html,/<caption>/);
  const source='~~~json:table\n'+JSON.stringify({markdown:true,items:[{a:'<script>alert(1)</script><a href="javascript:x">危険</a>'}]})+'\n~~~';
  const result=renderDocument(source).html; assert.doesNotMatch(result,/<script|javascript:/); assert.match(result,/危険/);
});
test('front matter は既定の言語と明示された言語のコードブロックにする',()=>{
  for(const [source,language] of [['---\na: b\n---\n本文','yaml'],['+++\na=1\n+++\n本文','toml'],[';;;\n{}\n;;;\n本文','json'],['---ruby\nx\n...\n本文','ruby']]) {
    assert.match(frontMatter(source),new RegExp('^'+String.fromCharCode(96).repeat(3)+language+':frontmatter'));
    const html=renderDocument(source).html; assert.match(html,/data-lang-params="frontmatter"/); assert.match(html,/markdown-code-block/);
  }
  assert.match(render('frontmatter.md').html,/data-canonical-lang="yaml"/); assert.match(render('frontmatter-toml.md').html,/data-canonical-lang="toml"/);
});
test('front matter の encoding・前置空白・異なる終端・終端なしを区別する',()=>{
  const fence=String.fromCharCode(96).repeat(3);
  assert.equal(frontMatter('# coding: utf-8\n\n---\nx\n+++\n本文'),'\n\n'+fence+'yaml:frontmatter\nx\n'+fence+'\n本文');
  for(const source of ['本文\n---\nx\n---\n','---\nx\n','---\nx\n--- trailing\n']) assert.equal(frontMatter(source),source);
  assert.equal(frontMatter('---\nx\n---'),fence+'yaml:frontmatter\nx\n'+fence+'\n');
});
test('CR を除き、空の文書・未知の context・型の違う入力を扱う',()=>{
  assert.deepEqual(renderDocument('# 題\r\n\r\n本文'),renderDocument('# 題\n\n本文'));
  assert.deepEqual(renderDocument('',{unknown:true}),{html:'',headings:[]});
  assert.deepEqual(renderDocument('本文',{unknown:true}),renderDocument('本文'));
  assert.throws(()=>renderDocument(null),TypeError);
});
test('文書間で目次と脚注の状態を共有しない',()=>{
  renderDocument('[TOC]\n\n# A\n\n注[^1]\n\n[^1]: 本文');
  const result=renderDocument('[TOC]\n\n# B'); assert.doesNotMatch(result.html,/>A<|footnotes/); assert.match(result.html,/>B</);
});
test('生の HTML から実行可能な属性と要素を残さない',()=>{
  const {html}=render('html.md'),tree=fragment(html);
  assert.equal(elements(tree,n=>['script','iframe','object','embed','form','style','link','meta','base','svg','math'].includes(n.tagName)).length,0);
  for(const {node} of elements(tree)) for(const [key,value] of Object.entries(node.properties)) {
    assert.doesNotMatch(key,/^on/i); if(['href','src'].includes(key)) assert.doesNotMatch(String(value),/^(?:javascript|data|vbscript):/i);
  }
});

test('front matter の空白と行頭は Ruby の正規表現の意味を保つ', () => {
  for (const source of ['\u00a0---\nx\n---\n', '\u2028---\nx\n---\n', '---\nx\u2028---\n']) assert.equal(frontMatter(source), source);
  assert.ok(frontMatter('\n---\nx\n---\n').includes('yaml:frontmatter'));
});
