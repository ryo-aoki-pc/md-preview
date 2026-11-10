import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { renderDocument } from '../lib/pipeline.mjs';
import { expandIncludes, MAX_INCLUDES, MAX_EXPANDED_BYTES, escapeInline, escapeLinkDestination } from '../lib/include.mjs';
import { fragment, elements, content } from '../lib/tree.mjs';
import { renderMarkdown } from '../lib/engine.mjs';
const directive = file => '::include{file=' + file + '}';
const plain = result => content(fragment(result.html));

test('include は Markdown を描き、原文の行と正規化したパス・本文の行数を返す', () => {
  const paths = [];
  const result = renderDocument('前\n\n' + directive('../part.md') + '\n\n後', { file: 'docs/topic/main.md', readInclude(path) { paths.push(path); return '# 取り込み\n\n**太字**\n'; } });
  assert.deepEqual(paths, ['docs/part.md']);
  assert.deepEqual(result.includes, [{ line: 3, path: 'docs/part.md', lines: 3, ok: true }]);
  assert.match(result.html, /<h1[^>]*>取り込み/); assert.match(result.html, /<strong[^>]*>太字/);
  assert.match(result.html, /data-sourcepos="8:1-8:3"[^>]*>後/);
  assert.deepEqual(result.notices, []);
});
for (const source of ['文中 '+directive('x.md'), ' '+directive('x.md'), '\t'+directive('x.md'), '> '+directive('x.md'), '- '+directive('x.md'), directive('x.md')+' 後', directive('x.md')+' ', '::include{file=}', directive('a'.repeat(1025)), '前\u2028'+directive('x.md')]) test('include の記法に合わないものは変更しない: '+JSON.stringify(source.slice(0,90)), () => {
  const result=expandIncludes(source,{readInclude(){assert.fail('読んではいけない');}});
  assert.equal(result.text,source); assert.deepEqual(result.includes,[]);
});
test('include は LF・CRLF・終端と Ruby の改行境界を認識する', () => {
  for (const ending of ['\n','\r\n','','\v','\f','\u0085','\u2028','\u2029']) {
    const source=directive('x.md')+ending;
    const result=expandIncludes(source,{readInclude:()=> '本文'});
    assert.equal(result.text,'本文'+ending); assert.equal(result.includes[0].ok,true);
  }
  assert.equal(renderDocument(directive('x.md')+'\r\n',{readInclude:()=> '一\r\n二\r\n'}).includes[0].lines,2);
});
test('include のファイル名は1024文字までで、閉じ括弧を含む名前も最後まで取る', () => {
  for (const name of ['a'.repeat(1024),'😀'.repeat(1024),'a}b.md']) {
    const result=expandIncludes(directive(name),{readInclude(path){assert.equal(path,name);return '本文';}});
    assert.equal(result.text,'本文');
  }
});
test('include はコードフェンス内でも文字列として処理し、取り込み先の include は再帰しない', () => {
  const paths=[];
  const result=renderDocument('~~~text\n'+directive('a.md')+'\n~~~',{readInclude(path){paths.push(path);return directive('b.md');}});
  assert.deepEqual(paths,['a.md']); assert.equal(result.includes.length,1);
  assert.match(plain(result),/::include\{file=b.md\}/); assert.match(result.html,/markdown-code-block/);
});
test('include の上限は32回で、残りの文字列はそのまま後ろに付ける', () => {
  const paths=[],source=Array.from({length:34},(_,i)=>directive(i+'.md')).join('\n');
  const result=expandIncludes(source,{readInclude(path){paths.push(path);return '本文';}});
  assert.equal(MAX_INCLUDES,32); assert.equal(paths.length,32); assert.equal(result.includes.length,34);
  assert.deepEqual(result.includes.map(i=>i.ok),[...Array(32).fill(true),false,false]);
  assert.ok(result.text.endsWith(directive('32.md')+'\n'+directive('33.md')));
});
test('失敗とURLの置換も上限に数え、同じ指定名の読み込みは文書内だけで共有する', () => {
  let reads=0;
  const source=Array(33).fill(directive('a.md')).join('\n');
  const result=expandIncludes(source,{readInclude(){reads++;return null;}});
  assert.equal(reads,1); assert.ok(result.text.endsWith(directive('a.md')));
  const remote=expandIncludes(Array(33).fill(directive('https://example.com/a')).join('\n'),{readInclude(){assert.fail('URLを読んではいけない');}});
  assert.ok(remote.text.endsWith(directive('https://example.com/a')));
  expandIncludes(directive('a.md'),{readInclude(){reads++;return '';}}); assert.equal(reads,2);
});
test('同じパスでも生の指定名が違うものは別のキャッシュキーにする', () => {
  const paths=[];
  expandIncludes(directive('x.md')+'\n'+directive('./x.md'),{readInclude(path){paths.push(path);return '';}});
  assert.deepEqual(paths,['x.md','x.md']);
});
for (const [file, target, expected] of [
  ['doc/api/README.adoc','users.adoc','doc/api/users.adoc'],
  ['doc/api','users.adoc','doc/api/users.adoc'],
  ['doc/api/README.adoc','../update/x.adoc','doc/update/x.adoc'],
  ['doc/api/README.adoc','./a/../b.md','doc/api/b.md'],
  ['doc/api/README.adoc','/shared/x.md','shared/x.md'],
  ['doc/api/README.adoc','/../../x.md','x.md'],
  [undefined,'x.md','x.md'],
  ['.hidden','x.md','.hidden/x.md'],
  ['docs/main.','x.md','docs/x.md'],
]) test('include の基準と cleanpath: '+String(file)+' + '+target, () => {
  const result=renderDocument(directive(target),{file,dir:'使わない/',readInclude(path){assert.equal(path,expected);return '本文';}});
  assert.equal(result.includes[0].path,expected); assert.equal(result.includes[0].ok,true);
});
for (const target of ['../secret.md','../../secret.md','a/../../secret.md','..\\secret.md','C:/secret.md','./C:/secret.md','/C:/secret.md','a/../C:/secret.md']) test('ルートの外やWindowsの絶対指定はコールバックへ渡さない: '+target, () => {
  const result=renderDocument(directive(target),{readInclude(){assert.fail('外を読んではいけない');}});
  assert.equal(result.includes[0].ok,false); assert.match(plain(result),/not found/); assert.equal(result.notices[0].kind,'include');
});
for (const [returned, reason] of [[null,'not found'],[{error:'not readable'},'not readable'],[{error:'too large'},'too large']]) test('読み込みの失敗を GitLab と同じ文にする: '+reason, () => {
  const result=renderDocument(directive('a.md'),{readInclude:()=>returned});
  assert.equal(plain(result),"Error including 'a.md': "+reason);
  assert.match(result.html,/<strong/); assert.match(result.html,/href="a.md"/);
  assert.deepEqual(result.includes,[{line:1,path:'a.md',lines:0,ok:false}]);
});
test('読み込みの例外や文字列でない値は not readable として扱う', () => {
  for(const readInclude of [()=>{throw new Error('失敗');},()=>({}),()=>42]) {
    assert.match(plain(renderDocument(directive('a.md'),{readInclude})),/not readable/);
  }
});
test('URLは読み込まず、GitLabの許可されない場合と同じリンクにする', () => {
  const result=renderDocument(directive('https://example.com/a?x=1&y=2'),{readInclude(){assert.fail('URLを読んではいけない');}});
  assert.equal(plain(result),'https://example.com/a?x=1&y=2');
  assert.match(result.html,/href="https:\/\/example.com\/a\?x=1&#x26;y=2"/);
  assert.doesNotMatch(plain(result),/Error including/); assert.equal(result.includes[0].ok,false);
});
test('readInclude がないAPI相当では何も置き換えず、GitLabの見本と一致する', () => {
  const source=readFileSync(new URL('./fixtures/include.md',import.meta.url),'utf8');
  const oracle=fragment(readFileSync(new URL('./fixtures/include.gitlab.html',import.meta.url),'utf8'));
  const result=renderDocument(source);
  const normalize=node=>node.type==='text'?{type:'text',value:node.value}:{type:node.type,...(node.tagName?{tagName:node.tagName,properties:node.properties}:{}),children:node.children.map(normalize)};
  assert.deepEqual(normalize(fragment(result.html)),normalize(oracle));
  assert.equal(result.includes.length,2); assert.ok(result.includes.every(i=>!i.ok));
  assert.deepEqual(result.notices.map(n=>[n.kind,n.line]),[['include',1],['include',5]]);
});
test('取り込んだ文字を足したり削ったりせず、CRはMarkdownFilter相当で除く', () => {
  for(const [value,lines] of [['',0],['一',1],['一\n',1],['一\n二',2],['一\n\n',2]]) {
    const result=expandIncludes(directive('a.md')+'\n後',{readInclude:()=>value});
    assert.equal(result.text,value+'\n後'); assert.equal(result.includes[0].lines,lines); assert.equal(result.includes[0].ok,true);
  }
  assert.equal(plain(renderDocument(directive('a.md'),{readInclude:()=> '一\r二'})),'一二');
});
test('取り込んだHTMLにも同じ消毒を掛ける', () => {
  const result=renderDocument(directive('a.md'),{readInclude:()=> '<script>alert(1)</script>\n\n<img src="x.png" onerror="alert(1)">'});
  assert.doesNotMatch(result.html,/<script|onerror/i);
  assert.equal(elements(fragment(result.html),n=>n.tagName==='img').length,1);
});
test('16 MiB ちょうどは受け入れ、超える本文と展開全体を too large にする', () => {
  const exact=expandIncludes(directive('a.md'),{readInclude:()=> 'x'.repeat(MAX_EXPANDED_BYTES)});
  assert.equal(exact.includes[0].ok,true);
  const oversized=expandIncludes(directive('a.md'),{readInclude:()=> 'あ'.repeat(Math.floor(MAX_EXPANDED_BYTES/3)+1)});
  assert.equal(oversized.includes[0].ok,false); assert.match(oversized.text,/too large/);
  const combined=expandIncludes(directive('a.md')+'\n'+directive('b.md'),{readInclude:()=> 'x'.repeat(MAX_EXPANDED_BYTES/2)});
  assert.deepEqual(combined.includes.map(i=>i.ok),[true,false]); assert.ok(combined.text.endsWith("too large**"));
});
test('includeの失敗文にあるファイル名はMarkdownやHTMLとして抜け出さない', () => {
  for(const filename of ['x)*[注](javascript:alert(1))','<img src=x onerror=alert(1)>','x] <script>alert(1)</script>','a b(c).md']) {
    const result=renderDocument(directive(filename),{readInclude:()=>null});
    assert.doesNotMatch(result.html,/<script|<img/i);
    assert.equal(elements(fragment(result.html),n=>Object.keys(n.properties).some(k=>/^on/i.test(k))).length,0);
    assert.ok(plain(result).includes(filename));
  }
});

// Comrak 0.56.0 の tests/escape.rs の escape_inline_baseline・escape_strikethrough の期待値。
// 描画も比較する部分は、元の Options::default と同じく自動リンクを無効にする。
for (const [input, expected] of [
  ['abcdefg', 'abcdefg'],
  ['*hello*', '\\*hello\\*'],
  ['[A link](https://link.com)', '\\[A link\\]\\(https://link\\.com\\)'],
  ["some <\"complicated\"> & '/problematic\\' input", "some \\<\\\"complicated\\\"\\> \\& '/problematic\\\\' input"],
  ['~~text~~', '\\~\\~text\\~\\~'],
]) test('Comrak の inline のエスケープ: ' + input, () => {
  assert.equal(escapeInline(input), expected);
  assert.equal(content(fragment(renderMarkdown(expected, { autolink: false, relaxed_autolinks: false }).trimEnd())), input);
});

test('Comrak の escape_link_target の期待値と描画後の URL を維持する', () => {
  const url = 'rabbits) <cup\rcakes\n> [%7Bhya%cinth%7d](';
  const escaped = '<rabbits) \\<cup%0Dcakes%0A\\> [%7Bhya%cinth%7d](>';
  assert.equal(escapeLinkDestination(url), escaped);
  const tree = fragment(renderMarkdown('[link](' + escaped + ')'));
  const href = elements(tree, n => n.tagName === 'a')[0].node.properties.href;
  assert.equal(href, 'rabbits)%20%3Ccup%0Dcakes%0A%3E%20%5B%7Bhya%25cinth%7d%5D(');
  assert.equal(decodeURIComponent(href), 'rabbits) <cup\rcakes\n> [{hya%cinth}](');
});

test('Comrak の制御文字の数値参照と対象外の文字を同じにする', () => {
  const controls = Array.from({length:32}, (_,i) => String.fromCharCode(i)).join('');
  assert.equal(escapeInline(controls), Array.from({length:32}, (_,i) => '&#' + i + ';').join(''));
  const unchanged = "é尾😀 $ % ' : ; ? / @ { | }";
  assert.equal(escapeInline(unchanged), unchanged);
  assert.equal(escapeInline(String.fromCharCode(96)), String.fromCharCode(92,96));
  assert.equal(escapeLinkDestination('a b(c)\\d'), '<a b(c)\\d>');
});
