import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const context = { window: {} };
vm.runInNewContext(readFileSync(new URL('../page/edit-grid.js', import.meta.url), 'utf8'), context);
const grid = context.window.gpEditGrid;
// vm の配列とオブジェクトは別のプロトタイプなので、値として比べる。
const plain = (value) => JSON.parse(JSON.stringify(value));
const equal = (actual, expected) => assert.deepEqual(plain(actual), expected);
const cell = (row, col, text, extra = {}) => ({ row, col, rowspan: 1, colspan: 1, text, raw: false, header: false, tag: '<td>', source: text, ...extra });
const rect = (row, col, rows, cols) => ({ row, col, rows, cols });
const pipe = {
  index: 0, kind: 'pipe', rows: 3, cols: 3, copyLocked: '', rowTags: [], delimiters: [':---', '---:', ':---:'],
  cells: Array.from({ length: 9 }, (_, i) => cell(Math.floor(i / 3), i % 3, String(i), { tag: '', header: i < 3 })),
};
const html = {
  index: 1, kind: 'html', rows: 3, cols: 3, copyLocked: '',
  rowTags: ['<tr class="見出し">', '<tr data-row=1>', '<tr style="color:red">'], delimiters: [],
  cells: [
    cell(0, 0, '&<>"\'\n次', { header: true, colspan: 2, tag: '<th colspan="2" class="原文の属性">', source: '<b>別の原文</b>' }),
    cell(0, 2, '右上', { rowspan: 2, tag: '<td rowspan="2" title="保持">' }),
    cell(1, 0, '左下', { colspan: 2, tag: '<td colspan="2">' }),
    cell(2, 0, '複雑', { raw: true, source: '<em>原文</em>\r\n次' }),
    cell(2, 2, '末尾'),
  ],
};
const modelOf = (info) => ({ ...info, slots: grid.slotsOf(info) });
const origin = '0123456789abcdef0123456789abcdef';
const cut = { id: 'abcdef0123456789abcdef0123456789', table: 1, render: 12, hash: 'a'.repeat(64), ...rect(2, 0, 1, 3) };
const validEnvelope = () => plain(grid.envelope({ origin, fragment: grid.valuesFragment([['値']]) }));
const rejected = (change) => {
  const value = validEnvelope();
  change(value);
  assert.equal(grid.parseEnvelope(JSON.stringify(value)), null);
};

test('外部への定義は凍結した gpEditGrid だけ', () => {
  assert.deepEqual(Object.keys(context), ['window']);
  assert.deepEqual(Object.keys(context.window), ['gpEditGrid']);
  assert.ok(Object.isFrozen(grid));
  assert.ok(Object.isFrozen(grid.LIMITS));
  assert.equal(grid.TYPE, 'application/x-gp-table');
});

test('TSV の引用と改行を書き出す', () => {
  assert.equal(grid.toTSV([['値']]), '値');
  assert.equal(grid.toTSV([['']]), '');
  assert.equal(grid.toTSV([['a', 'b']]), 'a\tb\r\n');
  assert.equal(grid.toTSV([['a'], ['b']]), 'a\r\nb\r\n');
  assert.equal(grid.toTSV([['a\tb', 'a"b', 'a\rb', 'a\nb']]), '"a\tb"\t"a""b"\t"a\rb"\t"a\nb"\r\n');
});

for (const [name, values] of [
  ['1 セル', [['文字']]],
  ['空の 1 セル', [['']]],
  ['末尾の空行', [['a'], [''], ['']]],
  ['末尾の空列', [['a', '', ''], ['', '', '']]],
  ['空行と空列', [['', ''], ['', ''], ['', '']]],
  ['引用符', [['"先頭', '中"間', '"', '""']]],
  ['セル内の改行とタブ', [['a\r\nb\rc\nd', '\t'], ['\n', '"\n"']]],
  ['改行で終わる 1 セル', [['a\n']]],
]) {
  test('TSV の往復: ' + name, () => equal(grid.parseTSV(grid.toTSV(values)), values));
}

test('TSV は引用符の外の最後の改行を 1 個だけ除く', () => {
  equal(grid.parseTSV('a\r\n\r\n\r\n'), [['a'], [''], ['']]);
  equal(grid.parseTSV('a\n\n'), [['a'], ['']]);
  equal(grid.parseTSV('a\r\r'), [['a'], ['']]);
  equal(grid.parseTSV('a\t\r\n'), [['a', '']]);
  equal(grid.parseTSV('"a\n"'), [['a\n']]);
  equal(grid.parseTSV('"a\n"\r\n'), [['a\n']]);
  equal(grid.parseTSV('\r\n'), [['']]);
  equal(grid.parseTSV(''), [['']]);
  assert.throws(() => grid.parseTSV('"閉じない\n'), /引用符が閉じていません/);
});

test('表の値は結合の被覆位置と穴を空にし、raw のセルには原文を使う', () => {
  equal(grid.matrix(html, rect(0, 0, 3, 3)), [['&<>"\'\n次', '', '右上'], ['左下', '', ''], ['<em>原文</em>\r\n次', '', '末尾']]);
  equal(grid.matrix(pipe, rect(1, 1, 2, 2)), [['4', '5'], ['7', '8']]);
  assert.equal(grid.matrix(pipe, rect(2, 2, 2, 2)), null);
});

test('HTML は文字をエスケープし、結合とセル内改行だけを出す', () => {
  const result = grid.toHTML(html, rect(0, 0, 3, 3));
  assert.equal(result, '<table><tr><th colspan="2">&amp;&lt;&gt;&quot;&#39;<br style="mso-data-placement:same-cell">次</th><td rowspan="2">右上</td></tr><tr><td colspan="2">左下</td></tr><tr><td>&lt;em&gt;原文&lt;/em&gt;<br style="mso-data-placement:same-cell">次</td><td></td><td>末尾</td></tr></table>');
  assert.doesNotMatch(result, /原文の属性|別の原文|class=|title=|color:red|data-row/);
  assert.equal(grid.toHTML(html, rect(0, 0, 1, 2)), '');
  assert.equal(grid.toHTML(pipe, rect(0, 0, 1, 1)), '');
  assert.equal(grid.toHTML(html, rect(1, 0, 2, 3)), '');
  assert.equal(grid.toHTML(html, rect(0, 0, 4, 3)), '');
});

test('HTML は CR・LF・CRLF を同じセルの改行にする', () => {
  const info = { rows: 1, cols: 2, cells: [cell(0, 0, 'a\rb\nc\r\nd'), cell(0, 1, '')] };
  assert.equal(grid.toHTML(info, rect(0, 0, 1, 2)), '<table><tr><td>a<br style="mso-data-placement:same-cell">b<br style="mso-data-placement:same-cell">c<br style="mso-data-placement:same-cell">d</td><td></td></tr></table>');
});

test('端点の結合セルを囲み、選択方向を正規化する', () => {
  const model = modelOf(html);
  equal(grid.rectOf(model, { anchor: { row: 1, col: 2 }, focus: { row: 0, col: 1 }, kind: 'cells' }), rect(0, 0, 2, 3));
  equal(grid.rectOf(model, { anchor: { row: 0, col: 0 }, focus: { row: 0, col: 0 } }), rect(0, 0, 1, 2));
  assert.equal(grid.rectOf(model, { anchor: { row: 2, col: 1 }, focus: { row: 0, col: 0 } }), null);
  assert.equal(grid.rectOf(model, { anchor: { row: 3, col: 0 }, focus: { row: 0, col: 0 } }), null);
});

test('途中の結合まで閉包を取らず、交差を別に返す', () => {
  const info = { rows: 3, cols: 3, cells: [cell(0, 0, 'a'), cell(0, 1, 'b', { rowspan: 3 }), cell(1, 0, 'c'), cell(1, 2, 'd')] };
  const model = modelOf(info);
  const selected = grid.rectOf(model, { anchor: { row: 0, col: 0 }, focus: { row: 1, col: 2 } });
  equal(selected, rect(0, 0, 2, 3));
  assert.equal(grid.crossing(model, selected), info.cells[1]);
  equal(grid.cellsIn(model, selected), [info.cells[0], info.cells[2], info.cells[3]]);
  assert.equal(grid.crossing(model, rect(0, 0, 3, 3)), null);
});

test('行・列の選択は端点の座標だけを軸へ広げる', () => {
  const model = modelOf(html);
  equal(grid.rectOf(model, { anchor: { row: 2, col: 0 }, focus: { row: 1, col: 2 }, kind: 'rows' }), rect(1, 0, 2, 3));
  equal(grid.rectOf(model, { anchor: { row: 2, col: 2 }, focus: { row: 0, col: 1 }, kind: 'cols' }), rect(0, 1, 3, 2));
  equal(grid.axisRect(model, rect(1, 1, 1, 1), 'rows'), rect(1, 0, 1, 3));
  equal(grid.axisRect({ cells: model.cells, slots: model.slots, rows: [{}, {}, {}] }, rect(1, 1, 1, 1), 'cols'), rect(0, 1, 3, 1));
});

test('step は move と同じく結合セルを 1 個として通り、端で止まる', () => {
  const model = modelOf(html);
  assert.equal(grid.step(model, { row: 0, col: 1 }, 'right'), html.cells[1]);
  assert.equal(grid.step(model, { row: 1, col: 2 }, 'down'), html.cells[4]);
  assert.equal(grid.step(model, { row: 1, col: 2 }, 'left'), html.cells[0]);
  assert.equal(grid.step(model, { row: 1, col: 0 }, 'up'), html.cells[0]);
  assert.equal(grid.step(model, { row: 0, col: 1 }, 'next'), html.cells[1]);
  assert.equal(grid.step(model, { row: 1, col: 0 }, 'previous'), html.cells[1]);
  assert.equal(grid.step(model, { row: 0, col: 0 }, 'up'), null);
  assert.equal(grid.step(model, { row: 2, col: 2 }, 'next'), null);
  assert.equal(grid.step(model, { row: 2, col: 1 }, 'right'), null);
});

test('パイプの写しは原文と選んだ列の区切り行を持つ', () => {
  const info = structuredClone(pipe);
  info.cells[0].source = '**太字** \\| 縦棒';
  const all = grid.fragment(info, rect(0, 0, 3, 3), 'table');
  equal(all, { version: 1, kind: 'pipe', shape: 'table', rows: 3, cols: 3, cells: info.cells.map((c) => ({ row: c.row, col: c.col, rowspan: 1, colspan: 1, text: '', tag: '', source: c.source })), row_tags: [], delimiters: info.delimiters });
  const columns = grid.fragment(info, rect(0, 1, 3, 2), 'cols');
  equal(columns.delimiters, ['---:', ':---:']);
  equal(columns.cells[0], { row: 0, col: 0, rowspan: 1, colspan: 1, text: '', tag: '', source: '1' });
  equal(grid.fragment(info, rect(1, 0, 1, 3), 'rows').delimiters, info.delimiters);
  equal(grid.fragment(info, rect(1, 1, 1, 2), 'cells').delimiters, ['---:', ':---:']);
});

test('HTML の写しは開始タグと原文・結合・穴を保ち、区画の情報を加えない', () => {
  const all = grid.fragment(html, rect(0, 0, 3, 3), 'table');
  equal(all.row_tags, html.rowTags);
  equal(all.delimiters, []);
  assert.equal(all.cells.length, 5);
  equal(all.cells[0], { row: 0, col: 0, rowspan: 1, colspan: 2, text: '', tag: html.cells[0].tag, source: html.cells[0].source });
  equal(grid.fragment(html, rect(0, 2, 3, 1), 'cols').row_tags, []);
  const row = grid.fragment(html, rect(2, 0, 1, 3), 'rows');
  equal(row.row_tags, [html.rowTags[2]]);
  equal(row.cells.map((c) => [c.row, c.col]), [[0, 0], [0, 2]]);
  row.row_tags[0] = '<tr>';
  row.cells[0].source = '変更';
  assert.equal(html.rowTags[2], '<tr style="color:red">');
  assert.equal(html.cells[3].source, '<em>原文</em>\r\n次');
});

test('写しは範囲外・結合の分断・copyLocked・上限超過を断る', () => {
  assert.equal(grid.fragment(html, rect(1, 0, 2, 3), 'rows'), null);
  assert.equal(grid.fragment(html, rect(0, 0, 4, 3), 'table'), null);
  assert.equal(grid.fragment({ ...html, copyLocked: '結合が不正' }, rect(0, 0, 3, 3), 'table'), null);
  assert.equal(grid.fragment({ kind: 'pipe', rows: 1001, cols: 1, cells: [] }, rect(0, 0, 1001, 1)), null);
  assert.equal(grid.fragment({ kind: 'pipe', rows: 101, cols: 100, cells: [] }, rect(0, 0, 101, 100)), null);
  assert.equal(grid.fragment({ ...pipe, cells: [cell(0, 0, '', { source: 'あ'.repeat(grid.LIMITS.bytes / 3) })] }, rect(0, 0, 1, 1)), null);
  assert.equal(grid.valuesFragment([]), null);
  assert.equal(grid.valuesFragment([[]]), null);
  assert.equal(grid.valuesFragment([['a'], []]), null);
  assert.equal(grid.valuesFragment([[1]]), null);
  assert.equal(grid.valuesFragment([['a'.repeat(grid.LIMITS.bytes)]]), null);
});

test('値だけの写しは text だけに値を持つ', () => {
  equal(grid.valuesFragment([['a', ''], ['b', 'c']]), {
    version: 1, kind: '', shape: 'cells', rows: 2, cols: 2,
    cells: [
      { row: 0, col: 0, rowspan: 1, colspan: 1, text: 'a', tag: '', source: '' },
      { row: 0, col: 1, rowspan: 1, colspan: 1, text: '', tag: '', source: '' },
      { row: 1, col: 0, rowspan: 1, colspan: 1, text: 'b', tag: '', source: '' },
      { row: 1, col: 1, rowspan: 1, colspan: 1, text: 'c', tag: '', source: '' },
    ], row_tags: [], delimiters: [],
  });
});

test('包みは版と origin・切り取りを持ち、知らない項目を新しい値へ持ち込まない', () => {
  const value = plain(grid.envelope({ origin, fragment: grid.fragment(html, rect(2, 0, 1, 3), 'rows'), cut }));
  equal(grid.parseEnvelope(JSON.stringify(value)), value);
  value.extra = { nested: true };
  value.fragment.cells[0].extra = '捨てる';
  value.fragment.extra = [];
  value.cut.extra = '捨てる';
  const parsed = grid.parseEnvelope(JSON.stringify(value));
  assert.equal(Object.hasOwn(parsed, 'extra'), false);
  assert.equal(Object.hasOwn(parsed.fragment, 'extra'), false);
  assert.equal(Object.hasOwn(parsed.fragment.cells[0], 'extra'), false);
  assert.equal(Object.hasOwn(parsed.cut, 'extra'), false);
  assert.notEqual(parsed.fragment, value.fragment);
  equal(grid.envelope({ origin, fragment: grid.valuesFragment([['値']]) }), validEnvelope());
});

test('parseEnvelope は JSON・オブジェクト・版・型・origin を検査する', () => {
  for (const text of ['', '{', 'null', '[]', '"文字"', 'true', '1', '{}', JSON.stringify(validEnvelope()) + '{}']) assert.equal(grid.parseEnvelope(text), null);
  for (const change of [
    (v) => { v.format = '別の形式'; }, (v) => { v.version = 2; }, (v) => { v.version = '1'; },
    (v) => { v.origin = 'x'.repeat(32); }, (v) => { v.origin = origin + '0'; },
    (v) => { v.fragment.version = 2; }, (v) => { v.fragment.kind = '別の種類'; },
    (v) => { v.fragment.shape = '別の形'; }, (v) => { v.fragment = null; },
    (v) => { v.fragment.cells = {}; }, (v) => { v.fragment.cells[0].text = 42; },
    (v) => { delete v.fragment.cells[0].tag; }, (v) => { v.fragment.cells[0].source = null; },
    (v) => { v.fragment.cells[0].row = -1; }, (v) => { v.fragment.cells[0].col = 0.1; },
    (v) => { v.fragment.cells[0].rowspan = 0; }, (v) => { v.fragment.cells[0].colspan = '1'; },
    (v) => { v.fragment.row_tags = ['<tr>', '<tr>']; }, (v) => { v.fragment.delimiters = [1]; },
    (v) => { v.cut = false; }, (v) => { delete v.cut; },
  ]) rejected(change);
});

test('parseEnvelope は大きすぎる入力と行・列・セル数を断る', () => {
  assert.equal(grid.parseEnvelope(' '.repeat(grid.LIMITS.chars + 1)), null);
  rejected((v) => { v.fragment.cells[0].text = 'あ'.repeat(grid.LIMITS.bytes / 3); });
  for (const value of [0, -1, 1.5, '1', 1001, Number.MAX_SAFE_INTEGER]) rejected((v) => { v.fragment.rows = value; });
  rejected((v) => { v.fragment.cols = 1001; });
  rejected((v) => { v.fragment.rows = 101; v.fragment.cols = 100; });
  rejected((v) => { v.fragment.cells.push({ ...v.fragment.cells[0] }); });
  rejected((v) => { v.fragment.delimiters = ['---', '---']; });
  const value = validEnvelope();
  value.fragment.rows = 1000; value.fragment.cols = 10;
  assert.ok(grid.parseEnvelope(JSON.stringify(value)));
});

test('parseEnvelope は切り取りの型と照合値を検査する', () => {
  for (const [key, value] of [['id', 'x'], ['table', -1], ['render', 0], ['hash', 'a'], ['row', -1], ['col', '0'], ['rows', 1001], ['cols', 0]]) {
    rejected((v) => { v.cut = { ...cut, [key]: value }; });
  }
});

test('parseEnvelope は __proto__・constructor・prototype をどの深さでも断る', () => {
  for (const key of ['__proto__', 'constructor', 'prototype']) {
    const value = validEnvelope();
    Object.defineProperty(value, key, { value: {}, enumerable: true });
    assert.equal(grid.parseEnvelope(JSON.stringify(value)), null);
    const nested = validEnvelope();
    nested.fragment.cells[0].extra = JSON.parse(`{"${key}":{"汚染":true}}`);
    assert.equal(grid.parseEnvelope(JSON.stringify(nested)), null);
  }
  assert.equal(Object.prototype.汚染, undefined);
});

test('parseEnvelope は原文とタグの意味をサーバーへ委ねる', () => {
  const value = validEnvelope();
  value.fragment.kind = 'html';
  value.fragment.cells[0].tag = '<script>';
  value.fragment.cells[0].source = '</table>';
  assert.ok(grid.parseEnvelope(JSON.stringify(value)));
});

test('placed は行・列の複製と挿入を反映した範囲を返す', () => {
  const info = { index: 2, rows: 12, cols: 8 };
  equal(grid.placed({ type: 'duplicateRows', row: 2, count: 3 }, info), rect(5, 0, 3, 8));
  equal(grid.placed({ type: 'duplicateColumns', col: 1, count: 2 }, info), rect(0, 3, 12, 2));
  equal(grid.placed({ type: 'insertRowsFrom', row: 4, side: 'before', fragment: { rows: 2, cols: 8 } }, info), rect(4, 0, 2, 8));
  equal(grid.placed({ type: 'insertRowsFrom', row: 4, side: 'after', values: [['値']] }, info), rect(5, 0, 1, 8));
  equal(grid.placed({ type: 'insertColumnsFrom', col: 2, side: 'after', fragment: { rows: 12, cols: 3 } }, info), rect(0, 3, 12, 3));
  assert.equal(grid.placed({ type: 'insertRows' }, info), null);
  assert.equal(grid.placed({ type: 'deleteRows' }, info), null);
});

test('placed は同じ表の移動で削除後の座標へ直し、別の表ではそのまま選ぶ', () => {
  const info = { index: 2, rows: 12, cols: 8 };
  const from = { table: 2, row: 2, count: 3 };
  equal(grid.placed({ type: 'moveRows', from, row: 8, side: 'before' }, info), rect(5, 0, 3, 8));
  equal(grid.placed({ type: 'moveRows', from, row: 0, side: 'after' }, info), rect(1, 0, 3, 8));
  equal(grid.placed({ type: 'moveRows', from, row: 4, side: 'after' }, info), rect(2, 0, 3, 8));
  equal(grid.placed({ type: 'moveRows', from: { ...from, table: 1 }, row: 8, side: 'after' }, info), rect(9, 0, 3, 8));
  equal(grid.placed({ type: 'moveColumns', from: { table: 2, col: 1, count: 2 }, col: 6, side: 'after' }, info), rect(0, 5, 12, 2));
  assert.equal(grid.placed({ type: 'moveRows', from, row: 3, side: 'before' }, info), null);
});

test('placed は上書きした写し・値・移動元の大きさを選ぶ', () => {
  const info = { index: 2, rows: 12, cols: 8 };
  equal(grid.placed({ type: 'pasteFragment', row: 4, col: 2, fragment: { rows: 2, cols: 3 } }, info), rect(4, 2, 2, 3));
  equal(grid.placed({ type: 'paste', row: 4, col: 2, values: [['a', 'b']] }, info), rect(4, 2, 1, 2));
  equal(grid.placed({ type: 'moveCells', row: 4, col: 2, from: { rows: 3, cols: 2 } }, info), rect(4, 2, 3, 2));
});

test('foreignHints は参照リンク・脚注・リンクと HTML のパスの目印を返す', () => {
  for (const text of ['[文][参照]', '[^注]', '[文](../画像.png)', '<img SRC="画像.png">', '<a href = "次.md">']) {
    assert.equal(grid.foreignHints(grid.valuesFragment([[text]])), true);
    assert.equal(grid.foreignHints({ cells: [{ text: '', source: text, tag: '' }] }), true);
  }
  assert.equal(grid.foreignHints(grid.valuesFragment([['ただの文字', '配列 [1]']])), false);
});

test('B1 の全見本で Go と JavaScript の写しが一致する', () => {
  // B1 の見本が無い場合も失敗させ、結合時の照合を省かない。
  const fixture = JSON.parse(readFileSync(new URL('../internal/edit/testdata/page-fragments.json', import.meta.url), 'utf8'));
  assert.equal(fixture.version, 1);
  assert.ok(Array.isArray(fixture.cases) && fixture.cases.length > 0);
  for (const item of fixture.cases) assert.deepEqual(plain(grid.fragment(item.table, item.rect, item.shape)), item.fragment, item.name);
});
