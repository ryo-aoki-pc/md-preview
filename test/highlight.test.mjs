import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { toHtml } from 'hast-util-to-html';
import { highlightCode, tokenLines, tokensFromTree, SCOPE_TO_ROUGE, DIAGRAM_LANGUAGES } from '../lib/highlight.mjs';
import syntaxHighlight from '../lib/filters/syntax-highlight.mjs';
import { fragment, elements, content } from '../lib/tree.mjs';
const html = result => toHtml({type:'root', children:result.children});

test('tokenLines は字句を行で分け、末尾の改行で空行を増やさない',()=>{
  assert.deepEqual(tokenLines([{scope:'c',value:'a\nb\n'}]), [[{scope:'c',value:'a'}],[{scope:'c',value:'b'}]]);
  assert.deepEqual(tokenLines([{scope:'c',value:'\n\n'}]),[[],[]]);
  assert.deepEqual(tokenLines([{scope:'s',value:''}]),[]);
  assert.deepEqual(tokenLines([{scope:'k',value:'a'},{scope:'s',value:'b\nc'}]),[[{scope:'k',value:'a'},{scope:'s',value:'b'}],[{scope:'s',value:'c'}]]);
});
test('複数行の comment は同じ Rouge class を行ごとに付ける',()=>{
  const result=highlightCode('/* 一\n二 */\n','js');
  assert.equal(result.children.filter(n=>n.tagName==='span').length,2);
  assert.equal((html(result).match(/class="c"/g)||[]).length,2);
  assert.doesNotMatch(html(result),/class="c">[^<]*\n/);
});
for(const [requested,expected] of [['js','javascript'],['python','python'],['py','python'],['sh','shell'],['text','plaintext'],['unknown-language','plaintext'],[undefined,'plaintext']]) test('言語の決め方: '+requested,()=>{
  const result=highlightCode('x\n',requested); assert.equal(result.language,expected); assert.equal(result.children[0].properties.dataLang,expected);
});
for(const language of ['math','suggestion',...DIAGRAM_LANGUAGES]) test('字句を分けない言語: '+language,()=>{
  const result=highlightCode('if "x"\n',language); assert.equal(result.language,language); assert.equal(result.children[0].children.every(n=>n.type==='text'),true);
});
test('複合・入れ子のスコープを対応づけ、未知のスコープは span にしない',()=>{
  const tokens=tokensFromTree(fragment('<span class="hljs-title function_">f</span><span class="hljs-title class_ inherited__">C</span><span class="hljs-string">a<span class="hljs-subst">b</span>c</span><span class="hljs-unmapped">z</span>'));
  assert.deepEqual(tokens.map(t=>t.scope),['nf','nc','s','si','s',null]);
  assert.equal(tokens.map(t=>t.value).join(''),'fCabcz');
});
test('スコープ対応表は既存の CSS が配色する class だけを使う',()=>{
  // _ref は配布しないため、参照 CSS の許可するクラスを固定して試験する。
  const allowed=new Set('c ch cm c1 cs cd cp cpf err gd gi gh gu ge gs k kc kd kn kp kr kv o ow kt nc m mb mf mh mi il mo mx s sa sb sc dl sd s2 se sh si sx s1 sr ss na no nv vc vg vi nb bp nd ne nf fm nl nn nt w'.split(' '));
  for(const value of Object.values(SCOPE_TO_ROUGE)) if(value) assert.ok(allowed.has(value),value);
});
test('配色は HTML の文字と属性をエスケープする',()=>{
  const source='<script onerror="x">& " \'</script>';
  const result=highlightCode(source,'unknown');
  assert.equal(content({type:'root',children:result.children}),source);
  assert.doesNotMatch(html(result),/<script/);
  assert.equal(elements(fragment(html(result)),n=>Object.keys(n.properties).some(k=>/^on/i.test(k))).length,0);
  assert.match(html(result),/&#x3C;script|&lt;script/);
});
test('Unicode の空白を通常の空白にし、Bidi 制御文字に警告を付ける',()=>{
  const result=highlightCode('a\u00a0b\u202ec','text');
  assert.equal(content({type:'root',children:result.children}),'a b\u202ec');
  assert.match(html(result),/class="unicode-bidi has-tooltip"/);
});
test('SyntaxHighlight は GitLab の包み・行番号・ボタンを作り、余分な子を消す',()=>{
  const tree=fragment('<pre data-sourcepos="1:1-3:3" data-canonical-lang="js">余分<code>const x = 1;\n</code>余分</pre>');
  syntaxHighlight(tree,{});
  const wrapper=tree.children[0], pre=elements(tree,n=>n.tagName==='pre')[0].node;
  assert.deepEqual(wrapper.properties.className,['gl-relative','markdown-code-block','js-markdown-code']);
  assert.equal(pre.properties['v-pre'],'true'); assert.equal(pre.properties.dataSourcepos,'1:1-3:3'); assert.equal(pre.children.length,1); assert.equal(pre.children[0].tagName,'code');
  assert.deepEqual(wrapper.children.map(n=>n.tagName),['pre','copy-code','insert-code-snippet']);
  assert.match(toHtml(tree),/id="LC1" class="line" data-lang="javascript"/); assert.doesNotMatch(toHtml(tree),/余分/);
});
test('SyntaxHighlight は suggestion にボタンを足さず、kroki-style は除外する',()=>{
  const tree=fragment('<pre data-canonical-lang="suggestion"><code>x</code></pre><pre data-kroki-style="display"><code>y</code></pre>'); syntaxHighlight(tree,{});
  assert.equal(elements(tree,n=>n.tagName==='copy-code').length,0);
  assert.equal(tree.children[1].tagName,'pre'); assert.equal(tree.children[1].properties.className,undefined);
});
test('highlight.js は最初のコードブロックまで読み込まない',()=>{
  const code = "import{createRequire}from'node:module';import{renderDocument}from'./lib/pipeline.mjs';const r=createRequire(import.meta.url);const p=r.resolve('highlight.js');if(r.cache[p])throw Error('先に読み込んだ');renderDocument('本文');if(r.cache[p])throw Error('本文で読み込んだ');renderDocument('~~~js\\nconst x = 1;\\n~~~');if(!r.cache[p])throw Error('コードで読み込んでいない');";
  const result=spawnSync(process.execPath,['--input-type=module','-e',code],{cwd:new URL('../',import.meta.url),encoding:'utf8',windowsHide:true});
  assert.equal(result.status,0,result.stderr);
});
