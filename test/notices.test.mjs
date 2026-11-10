import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { renderDocument } from '../lib/pipeline.mjs';
import { collectNotices } from '../lib/notices.mjs';
import { expandIncludes } from '../lib/include.mjs';
import { fragment } from '../lib/tree.mjs';
import { DIAGRAM_LANGUAGES } from '../lib/highlight.mjs';
const notices=(text,context={})=>renderDocument(text,context).notices;
const ofKind=(text,kind,context={})=>notices(text,context).filter(n=>n.kind===kind);
const directive=file=>'::include{file='+file+'}';
const tick=String.fromCharCode(96);
for(const reference of ['#123','!123','&123','$123','@alice','@group/sub','~bug','~"multi word"','~不具合','%v1.0','%"release 1"','group/project#123','group/sub/project!123','group/project$1','group/project~"multi word"','[issue:123]','[merge_request:123]','[epic:123]','[snippet:123]']) test('参照候補を拾う: '+reference,()=>{
  assert.deepEqual(ofKind('前 '+reference+' 後','reference'),[{kind:'reference',line:1,text:reference}]);
});
for(const source of ['abcdef1 abcdef1234567890abcdef1234567890abcdef12','# 見出し\n\n## 二','$x$ $'+tick+'x'+tick+'$ $$ x $$','$5 $10 $1.99 $1,000 $123.45 $123,456','$123USD $123円','#F00 #ff0000 #123abc','mail@example.com user@host.invalid','foo#123 foo!123 foo@alice','100%done %20','[リンク #123 @alice :tanuki:](https://example.com)','![#123](image.png)',tick+'#123 @alice :tanuki:'+tick,'~~~text\n#123 @alice :tanuki:\n~~~','\\#123 \\!123 \\&123 \\$123 \\@alice \\~bug \\%v1.0','<span data-escaped-char>#123</span>','~"閉じない %"閉じない','#0 !0 &0']) test('参照候補を拾わない: '+JSON.stringify(source),()=>{
  assert.deepEqual(ofKind(source,'reference'),[]);
});
test('強調の中を拾い、語やメールの途中ではタグをまたいでも拾わない',()=>{
  assert.deepEqual(ofKind('**#123** と _@alice_','reference').map(n=>n.text),['#123','@alice']);
  assert.deepEqual(ofKind('<b>mail</b>@example.com','reference'),[]);
});
for(const language of DIAGRAM_LANGUAGES) test('図の通知: '+language,()=>{
  const found=ofKind('~~~'+language+'\n図\n~~~','diagram');
  assert.equal(found.length,Number(language!=='mermaid'));
  if(found.length)assert.deepEqual(found[0],{kind:'diagram',line:1,text:'~~~'+language});
});
test('suggestion のコードだけを知らせ、ふつうのコードに同じ文字があっても拾わない',()=>{
  assert.deepEqual(ofKind('\n~~~suggestion\n提案\n~~~','suggestion'),[{kind:'suggestion',line:2,text:'~~~suggestion'}]);
  assert.deepEqual(notices('~~~text\nplantuml suggestion\n~~~'),[]);
});
test('取り込めなかった include を知らせ、成功したものには通知しない',()=>{
  const source=[directive('ok.md'),'',directive('missing.md'),'',directive('https://example.com/a'),'',directive('../secret.md')].join('\n');
  const context={readInclude:p=>p==='ok.md'?'本文\n':null};
  assert.deepEqual(ofKind(source,'include',context).map(n=>[n.line,n.text]),[[3,directive('missing.md')],[5,directive('https://example.com/a')],[7,directive('../secret.md')]]);
  assert.deepEqual(ofKind(directive('a.md'),'include'),[{kind:'include',line:1,text:directive('a.md')}]);
});
test('上限を超えて取り込めなかった include も原文の行で知らせる',()=>{
  const source=Array.from({length:34},(_,i)=>directive(i+'.md')).join('\n');
  assert.deepEqual(ofKind(source,'include',{readInclude:()=>''}).map(n=>n.line),[33,34]);
});
test('文字で残る絵文字だけを拾い、既知の絵文字・時刻・コード・リンクは拾わない',()=>{
  assert.deepEqual(ofKind(':tanuki: :unknown_emoji: :smile: 😀','emoji').map(n=>n.text),[':tanuki:',':unknown_emoji:']);
  for(const source of ['12:30:45','時刻 12:30:45 と 01:02:03','~~~text\n:tanuki:\n~~~',tick+':tanuki:'+tick,'[ :tanuki: ](/x)','単語:tanuki:中','/a/:tanuki:/b'])assert.deepEqual(ofKind(source,'emoji'),[],source);
});
test('missing は exists を渡したときだけ調べ、dir・root・query・fragment・パーセントを扱う',()=>{
  const source='![図](../img/%E5%9B%B3.png?size=2#part)\n\n![動画](/v/a.mp4?v=1)\n\n![音声](a.wav#part)';
  const paths=[];
  const context={dir:'docs/topic/',exists(p){paths.push(p);return p==='v/a.mp4';}};
  const found=ofKind(source,'missing',context);
  assert.deepEqual(paths,['docs/img/図.png','v/a.mp4','docs/topic/a.wav']);
  assert.deepEqual(found.map(n=>n.line),[1,5]);
  assert.deepEqual(ofKind(source,'missing'),[]);
});
test('画像・動画・音声と、リンク内の画像にも missing を調べる',()=>{
  const source='[![図](x.png)](/link)\n\n![動画](clip.mp4)\n\n![音声](clip.ogg)';
  const paths=[];
  assert.equal(ofKind(source,'missing',{exists:p=>(paths.push(p),false)}).length,3);
  assert.deepEqual(paths,['x.png','clip.mp4','clip.ogg']);
});
test('外部 URL・フラグメント・空のURLは exists を呼ばない',()=>{
  for(const source of ['![x](https://example.com/x.png)','![x](http://example.com/x.mp4)','![x](//example.com/x.png)','![x](#part)','![x]()']) {
    assert.deepEqual(ofKind(source,'missing',{exists(){assert.fail('外部を調べてはいけない');}}),[],source);
  }
});
test('missing のルート外と不正なエンコードは外のパスを渡さず、通知にする',()=>{
  for(const source of ['![x](../outside.png)','![x](%2e%2e/outside.png)','![x](bad%GG.png)','![x](C:/outside.png)','![x](..%5Coutside.png)']) {
    const found=ofKind(source,'missing',{exists(){assert.fail('不正なパスを調べてはいけない');}});
    // scheme 付きの C: は URL として除外される。
    assert.equal(found.length,source.includes('C:')?0:1,source);
  }
});
test('exists が失敗しても HTML は変えず、missing として知らせる',()=>{
  const source='![x](x.png)',plain=renderDocument(source).html;
  const result=renderDocument(source,{exists(){throw new Error('失敗');}});
  assert.equal(result.html,plain);assert.equal(result.notices[0].kind,'missing');
});
test('通知の行は段落の途中・強調・見出しでも原文へ対応する',()=>{
  const source='一行目\n二行目 #123 と **!456**\n三行目 :tanuki:\n\n## ~bug';
  assert.deepEqual(notices(source).map(n=>[n.kind,n.line,n.text]),[['reference',2,'#123'],['reference',2,'!456'],['emoji',3,':tanuki:'],['reference',5,'~bug']]);
});
for(const body of ['一\n\n二\n','一\n二','一',''])test('include の後ろも原文の行になる: '+JSON.stringify(body),()=>{
  const source=directive('a.md')+'\n\n#123\n\n:tanuki:';
  const result=renderDocument(source,{readInclude:()=>body});
  assert.deepEqual(result.notices.map(n=>[n.kind,n.line]),[['reference',3],['emoji',5]]);
});
test('複数のinclude、取り込み先の参照、図、画像も指示した原文の行へ戻す',()=>{
  const source=directive('a.md')+'\n\n'+directive('b.md')+'\n\n!123\n\n![x](x.png)';
  const context={readInclude:p=>p==='a.md'?'参照 #123\n\n~~~plantuml\n図\n~~~\n':'二\n三\n',exists:()=>false};
  const found=notices(source,context);
  assert.deepEqual(found.map(n=>[n.kind,n.line]),[['reference',1],['diagram',1],['reference',5],['missing',7]]);
  assert.equal(found[0].text,directive('a.md')); assert.equal(found[1].text,directive('a.md'));
});
test('front matter と encoding 行があっても通知の原文の行は変わらない',()=>{
  assert.deepEqual(notices('# coding: utf-8\n---\ntitle: test\n---\n\n#123').map(n=>n.line),[6]);
});
test('sourcepos のない生のHTMLは行不明として返す',()=>{
  const result=notices('<div>#123 :tanuki:</div>\n\n<pre data-canonical-lang="plantuml"><code>図</code></pre>');
  assert.ok(result.length); assert.ok(result.every(n=>n.line===null));
});
test('同じ行・種類・文字の通知はまとめ、違う文字や行は残す',()=>{
  assert.deepEqual(notices('#123 #123 !123 :tanuki: :tanuki:\n\n#123').map(n=>[n.kind,n.line,n.text]),[['reference',1,'#123'],['reference',1,'!123'],['emoji',1,':tanuki:'],['reference',3,'#123']]);
});
test('抜き出しは Unicode の文字を壊さず80文字まで',()=>{
  const source='~~~plantuml:'+ '😀'.repeat(100)+'\n図\n~~~';
  const found=ofKind(source,'diagram')[0];
  assert.equal(Array.from(found.text).length,80);assert.equal(found.text.endsWith('😀'),true);
});
test('通知全体を200件までにし、残りの数を最後に知らせる',()=>{
  const source=Array.from({length:205},(_,i)=>'#'+(i+1)).join('\n\n');
  const found=notices(source);
  assert.equal(found.length,200);assert.deepEqual(found.at(-1),{kind:'more',line:null,text:'6'});
  const exact=notices(Array.from({length:200},(_,i)=>'#'+(i+1)).join('\n\n'));
  assert.equal(exact.length,200);assert.equal(exact.some(n=>n.kind==='more'),false);
});
test('同じ候補を何度書いても、まとめた後の残りの件数を返す',()=>{
  const source=Array.from({length:201},(_,i)=>'#'+(i+1)+' #'+(i+1)).join('\n\n');
  assert.deepEqual(notices(source).at(-1),{kind:'more',line:null,text:'2'});
});
test('通知を作っても木と見本のHTMLを変えない',()=>{
  const directory=new URL('./fixtures/',import.meta.url);
  for(const name of readdirSync(directory).filter(n=>n.endsWith('.md'))) {
    const source=readFileSync(new URL(name,directory),'utf8');
    const expected=renderDocument(source).html;
    assert.equal(renderDocument(source,{exists:()=>false}).html,expected,name);
    if(name!=='include.md')assert.equal(renderDocument(source,{readInclude:()=>null,exists:()=>false}).html,expected,name);
  }
  const source='#123 :tanuki: ![x](x.png)',tree=fragment(renderDocument(source).html),before=structuredClone(tree);
  collectNotices(tree,source,expandIncludes(source),{exists:()=>false});
  assert.deepEqual(tree,before);
});

test('正規化やパーセントの復元で現れるWindowsの絶対パスも exists に渡さない', () => {
  for (const source of ['![x](/C:/outside.png)', '![x](.%2fC%3a%2foutside.png)', '![x](a/../C:/outside.png)']) {
    const found = ofKind(source, 'missing', {exists(){assert.fail('ドライブの絶対パスを渡してはいけない');}});
    assert.equal(found.length, 1, source);
  }
});
