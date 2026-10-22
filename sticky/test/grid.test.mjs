import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const source = await readFile(new URL("../src/sticky-table.js", import.meta.url), "utf8");
const context = { module: { exports: {} } };
vm.runInNewContext(source, context, { filename: "src/sticky-table.js" });
const { buildGrid, frozenRegion, rebuildBorders, usableRowBoundaries, usableColBoundaries, stepBoundary, headerRowCount, columnLabels, initialWrap, cellWrapModes, columnMinWidths, pickBoundaries, crossCells, searchMatches, filterRows, tableMatrix, toTsv, toHtml } = context.module.exports;
const th = (rowspan = 1, colspan = 1) => ({ rowspan, colspan, header: true });
const td = (rowspan = 1, colspan = 1) => ({ rowspan, colspan, header: false });
const many = (count, make = td) => Array.from({ length: count }, () => make());
const group = (kind, rows) => ({ kind, rows });
const body = (rows) => [group("tbody", rows)];
const plain = (value) => JSON.parse(JSON.stringify(value));
const result = (groups) => {
  const grid = buildGrid(groups);
  return { grid, region: frozenRegion(grid) };
};
const dimensions = (region) => [region.rows, region.cols];

// vm の別の領域から返る配列とオブジェクトは、値に直して比べる。
test("単純な表では先頭の 1 行と 1 列を固定する", () => {
  const { grid, region } = result(body([many(3, th), many(3), many(3)]));
  assert.equal(grid.rowCount, 3);
  assert.equal(grid.colCount, 3);
  assert.equal(grid.overlap, false);
  assert.deepEqual(plain(grid.grid), [[0, 1, 2], [3, 4, 5], [6, 7, 8]]);
  assert.deepEqual(dimensions(region), [1, 1]);
  assert.equal(region.headerCols, 0);
  assert.deepEqual(plain(region.roles[0]), { top: true, left: true, cross: false });
  assert.deepEqual(plain(region.roles[1]), { top: true, left: false, cross: false });
  assert.deepEqual(plain(region.roles[3]), { top: false, left: true, cross: false });
  assert.deepEqual(plain(region.roles[4]), { top: false, left: false, cross: false });
});

test("thead の 2 行と、その中の colspan を扱う", () => {
  const { grid, region } = result([
    group("thead", [[th(), th(1, 2)], many(3, th)]),
    group("tbody", [many(3), many(3)]),
  ]);
  assert.deepEqual(dimensions(region), [2, 1]);
  assert.equal(grid.cells[1].kind, "thead");
  assert.deepEqual(plain(grid.grid[0]), [0, 1, 1]);
  assert.equal(region.roles[2].top, true);
});

test("先頭列の rowspan のセルは全体を左に固定する", () => {
  const { grid, region } = result(body([
    many(3, th),
    [th(2), td(), td()],
    [td(), td()],
    [th(), td(), td()],
  ]));
  assert.deepEqual(dimensions(region), [1, 1]);
  assert.equal(region.headerCols, 1);
  assert.equal(grid.grid[1][0], grid.grid[2][0]);
  assert.deepEqual(plain(region.roles[3]), { top: false, left: true, cross: false });
});

test("角のセルが rowspan と colspan の両方を持つ", () => {
  const { grid, region } = result([
    group("thead", [[th(2, 2), th(1, 2)], many(2, th)]),
    group("tbody", [many(4), many(4)]),
  ]);
  assert.deepEqual(dimensions(region), [2, 2]);
  assert.equal(grid.cells[0].rowspan, 2);
  assert.equal(grid.cells[0].colspan, 2);
  assert.deepEqual(plain(region.roles[0]), { top: true, left: true, cross: false });
});

test("本文の全列の区切りは行の見出しの数から除き、cross にする", () => {
  const { region } = result(body([
    many(4, th),
    [th(), th(), td(), td()],
    [td(1, 4)],
    [th(), th(), td(), td()],
  ]));
  assert.deepEqual(dimensions(region), [1, 2]);
  assert.equal(region.headerCols, 2);
  assert.deepEqual(plain(region.roles[8]), { top: false, left: false, cross: true });
});

test("全列に広がる題は top と cross にし、全列へは広げない", () => {
  const { region } = result(body([
    [th(1, 4)],
    many(4, th),
    [th(), td(), td(), td()],
    [th(), td(), td(), td()],
  ]));
  assert.deepEqual(dimensions(region), [2, 1]);
  assert.deepEqual(plain(region.roles[0]), { top: true, left: false, cross: true });
});

test("rowspan=0 はその行のまとまりの最後までで止まる", () => {
  const { grid, region } = result([
    group("thead", [many(3, th)]),
    group("tbody", [[th(0), td(), td()], [td(), td()], [td(), td()]]),
    group("tfoot", [[th(), td(), td()]]),
  ]);
  assert.equal(grid.cells[3].rowspan, 3);
  assert.equal(grid.grid[1][0], grid.grid[3][0]);
  assert.notEqual(grid.grid[3][0], grid.grid[4][0]);
  assert.equal(grid.cells[10].kind, "tfoot");
  assert.deepEqual(dimensions(region), [1, 1]);
});

test("rowspan は tbody をまたがず、後のまとまりの列を空ける", () => {
  const grid = buildGrid([
    group("tbody", [[td(20), td()], [td()]]),
    group("tbody", [[td(), td()]]),
  ]);
  assert.equal(grid.cells[0].rowspan, 2);
  assert.equal(grid.cells[3].row, 2);
  assert.equal(grid.cells[3].col, 0);
  assert.equal(grid.overlap, false);
});

test("セルはその行の次の空いた列から置く", () => {
  const grid = buildGrid(body([[td(), td(2), td()], [td(), td()]]));
  assert.deepEqual(plain(grid.grid), [[0, 1, 2], [3, 1, 4]]);
  assert.equal(grid.cells[4].col, 2);
});

test("途中の占有セルへ colspan が重なれば overlap を返す", () => {
  const grid = buildGrid(body([[td(), td(2), td()], [td(1, 2), td()]]));
  assert.equal(grid.overlap, true);
});

test("th の行が無くても先頭行を固定する", () => {
  const { region } = result(body([many(3), many(3), many(3)]));
  assert.deepEqual(dimensions(region), [1, 1]);
});

test("先頭の th の行は、その行に始まるセルで数える", () => {
  const { region } = result(body([
    [th(2), th(), th()],
    [th(), th()],
    many(3),
    many(3),
  ]));
  assert.deepEqual(dimensions(region), [2, 1]);
});

test("セルが始まらない行で th の連続は止める", () => {
  const { region } = result(body([
    [th(2), th(2)],
    [],
    [th(), th()],
    [td(), td()],
  ]));
  assert.deepEqual(dimensions(region), [2, 1]);
});

test("固定する行の境目をまたぐ rowspan を連鎖して取り込む", () => {
  const { region } = result(body([
    [th(2), td()],
    [td(2)],
    [td()],
    [td(), td()],
  ]));
  assert.deepEqual(dimensions(region), [3, 1]);
});

test("固定する行の colspan を連鎖して取り込む", () => {
  const { region } = result([
    group("thead", [[th(1, 2), th(1, 3)], [th(), th(1, 2), th(), th()]]),
    group("tbody", [many(5), many(5)]),
  ]);
  assert.deepEqual(dimensions(region), [2, 3]);
  assert.equal(region.roles[1].cross, true);
});

test("行の見出しは rowspan に覆われたマス目も数える", () => {
  const { region } = result(body([
    many(4, th),
    [th(2), th(), td(), td()],
    [th(), td(), td()],
    [th(), td(), td(), td()],
  ]));
  assert.equal(region.headerCols, 1);
  assert.deepEqual(dimensions(region), [1, 1]);
});

test("空いたマス目は行の見出しに数えない", () => {
  const { region } = result(body([many(3, th), [th(), th(), td()], []]));
  assert.equal(region.headerCols, 0);
  assert.equal(region.cols, 1);
});

test("すべての行や列を固定することになるときはその向きを外す", () => {
  const one = result(body([[th()]]));
  assert.deepEqual(dimensions(one.region), [0, 0]);
  assert.deepEqual(plain(one.region.roles[0]), { top: false, left: false, cross: false });
  assert.equal(result(body([many(3, th), many(3, th)])).region.rows, 0);
  assert.equal(result(body([[td()], [td()], [td()]])).region.cols, 0);
});

test("colspan の 0 と不正な値は 1 にし、上限を 1000 にする", () => {
  for (const value of [0, -1, 1.5, "不正", NaN, null, undefined]) {
    const grid = buildGrid(body([[td(1, value)]]));
    assert.equal(grid.cells[0].colspan, 1, String(value));
  }
  for (const value of [1001, 1e21]) {
    const grid = buildGrid(body([[td(1, value)]]));
    assert.equal(grid.cells[0].colspan, 1000);
    assert.equal(grid.colCount, 1000);
  }
});

test("rowspan の不正な値は 1 にし、数の文字列も読む", () => {
  for (const value of [-1, 1.5, "不正", NaN, null, undefined]) {
    const grid = buildGrid(body([[td(value)], [], []]));
    assert.equal(grid.cells[0].rowspan, 1, String(value));
  }
  const grid = buildGrid(body([[td("2", "3")], []]));
  assert.equal(grid.cells[0].rowspan, 2);
  assert.equal(grid.cells[0].colspan, 3);
});

test("rowspan の上限は 65534 とする", () => {
  const rows = Array.from({ length: 65536 }, () => []);
  for (const value of [100000, 1e21]) {
    rows[0] = [td(value)];
    const grid = buildGrid(body(rows));
    assert.equal(grid.cells[0].rowspan, 65534);
    assert.equal(grid.grid[65533][0], 0);
    assert.notEqual(grid.grid[65534][0], 0);
  }
});

test("空の表を受け取っても固定の範囲を作らない", () => {
  const { grid, region } = result([]);
  assert.equal(grid.rowCount, 0);
  assert.equal(grid.colCount, 0);
  assert.deepEqual(dimensions(region), [0, 0]);
});

test("格子と範囲の計算は入力を書き換えない", () => {
  const input = body([[th(2, 2), th()], [th()], many(3)]);
  const before = JSON.stringify(input);
  const grid = buildGrid(input);
  const gridBefore = JSON.stringify(grid);
  frozenRegion(grid);
  assert.equal(JSON.stringify(input), before);
  assert.equal(JSON.stringify(grid), gridBefore);
});

// sample.md と同じ行数、列数、結合セルを作る。セルの文は計算に要らない。
const samples = {
  A() {
    return [
      group("thead", [[th(2), th(2), th(1, 4), th(1, 4), th(2)], many(8, th)]),
      group("tbody", Array.from({ length: 30 }, (_, i) => [...(i % 6 === 0 ? [th(6)] : []), ...many(10)])),
    ];
  },
  B() {
    const rows = [[th(1, 2), th(1, 8)], many(10, th)];
    for (let part = 0; part < 3; part++) {
      rows.push([td(1, 10)]);
      for (let i = 0; i < 9; i++) rows.push([...(i % 3 === 0 ? [th(3)] : []), th(), ...many(8)]);
    }
    return body(rows);
  },
  C() {
    return [
      group("thead", [[th(2, 2), th(1, 3), th(1, 3)], many(6, th)]),
      group("tbody", Array.from({ length: 24 }, (_, i) => [...(i % 3 === 0 ? [th(3)] : []), th(), ...many(6)])),
    ];
  },
  D() {
    return [group("thead", [many(12, th)]), group("tbody", Array.from({ length: 40 }, () => many(12)))];
  },
  E() {
    return [
      group("thead", [many(10, th)]),
      group("tbody", Array.from({ length: 25 }, (_, i) => [...(i === 0 ? [th(0)] : []), ...many(9)])),
      group("tfoot", [[th(), ...many(9)]]),
    ];
  },
  F() {
    return [group("thead", [many(2, th)]), group("tbody", [many(2), many(2)])];
  },
  G() {
    return [group("thead", [many(8, th)]), group("tbody", Array.from({ length: 12 }, () => [th(), ...many(7)]))];
  },
  H() {
    return body([many(11, th), ...Array.from({ length: 30 }, () => [th(), ...many(10)])]);
  },
};

for (const [name, expected] of Object.entries({ A: [2, 1], B: [2, 2], C: [2, 2], D: [1, 1], E: [1, 1], F: [1, 1], G: [1, 1], H: [1, 1] })) {
  test("sample.md の表 " + name + " と同じ形の範囲を求める", () => {
    const { grid, region } = result(samples[name]());
    assert.equal(grid.overlap, false);
    assert.deepEqual(dimensions(region), expected);
    if (name === "A") assert.equal(grid.cells.find((cell) => cell.row === 2 && cell.col === 0).rowspan, 6);
    if (name === "B") {
      const divider = grid.cells.findIndex((cell) => cell.row === 2);
      assert.deepEqual(plain(region.roles[divider]), { top: false, left: false, cross: true });
    }
    if (name === "C") assert.deepEqual(plain(region.roles[0]), { top: true, left: true, cross: false });
    if (name === "E") assert.equal(grid.cells.find((cell) => cell.row === 1 && cell.col === 0).rowspan, 25);
  });
}

const border = (width = 1, style = "solid", color = "rgb(80, 80, 80)") => ({ width, style, color });
const cellBorders = (edge = border()) => ({ top: { ...edge }, right: { ...edge }, bottom: { ...edge }, left: { ...edge } });
const borderWidths = (edges) => Object.fromEntries(Object.entries(edges).map(([side, edge]) => [side, edge.width]));

test("2×2 の罫線は右と下に置き、上と左は外周だけに置く", () => {
  const grid = buildGrid(body([many(2), many(2)]));
  const borders = Array.from({ length: 4 }, () => cellBorders());
  const rebuilt = rebuildBorders(grid, borders);
  assert.deepEqual(plain(rebuilt.map(borderWidths)), [
    { top: 1, right: 1, bottom: 1, left: 1 },
    { top: 1, right: 1, bottom: 1, left: 0 },
    { top: 0, right: 1, bottom: 1, left: 1 },
    { top: 0, right: 1, bottom: 1, left: 0 },
  ]);
  assert.deepEqual(plain(rebuilt[0].top), borders[0].top);
  assert.deepEqual(plain(rebuilt[3].right), borders[3].right);
  assert.deepEqual(plain(rebuilt[3].bottom), borders[3].bottom);
});

test("先頭行の下の罫線が 0 でも、下のセルの上の罫線を残す", () => {
  const grid = buildGrid(body([[th()], [td()]]));
  const borders = [cellBorders(), cellBorders()];
  borders[0].bottom = border(0);
  borders[1].top = border(1, "solid", "rgb(20, 40, 60)");
  const rebuilt = rebuildBorders(grid, borders);
  assert.deepEqual(plain(rebuilt[0].bottom), borders[1].top);
  assert.equal(rebuilt[1].top.width, 0);
});

test("結合セルに接する複数の罫線では、縦横とも最も太いものを使う", () => {
  const vertical = buildGrid(body([[td(2), td()], [td()]]));
  const verticalBorders = Array.from({ length: 3 }, () => cellBorders());
  verticalBorders[1].left = border(2, "solid", "rgb(10, 20, 30)");
  verticalBorders[2].left = border(3, "dashed", "rgb(40, 50, 60)");
  assert.deepEqual(plain(rebuildBorders(vertical, verticalBorders)[0].right), verticalBorders[2].left);
  const horizontal = buildGrid(body([[td(1, 2)], [td(), td()]]));
  const horizontalBorders = Array.from({ length: 3 }, () => cellBorders());
  horizontalBorders[1].top = border(4, "double", "rgb(70, 80, 90)");
  horizontalBorders[2].top = border(2, "solid", "rgb(100, 110, 120)");
  assert.deepEqual(plain(rebuildBorders(horizontal, horizontalBorders)[0].bottom), horizontalBorders[1].top);
});

test("none と幅 0 は線として数えず、隣の線を残す", () => {
  const grid = buildGrid(body([[td(), td()]]));
  const borders = [cellBorders(), cellBorders()];
  borders[0].top = border(8, "none");
  borders[0].left = border(0, "solid");
  borders[0].right = border(8, "none");
  borders[1].left = border(2, "solid", "rgb(1, 2, 3)");
  borders[1].bottom = border(0, "solid");
  const rebuilt = rebuildBorders(grid, borders);
  assert.deepEqual(plain(rebuilt[0].right), borders[1].left);
  for (const [index, side] of [[0, "top"], [0, "left"], [1, "bottom"]]) {
    assert.equal(rebuilt[index][side].width, 0);
    assert.equal(rebuilt[index][side].style, "none");
  }
  borders[0].right = border(2, "solid", "rgb(4, 5, 6)");
  borders[1].left = border(6, "none");
  assert.deepEqual(plain(rebuildBorders(grid, borders)[0].right), borders[0].right);
  borders[0].right = border(0, "solid");
  assert.equal(rebuildBorders(grid, borders)[0].right.width, 0);
});

test("共有する縦の辺では、どちら側の hidden も隣の線を消す", () => {
  const grid = buildGrid(body([[td(), td()]]));
  for (const width of [0, 8]) {
    for (const [index, side] of [[0, "right"], [1, "left"]]) {
      const borders = [cellBorders(border(2)), cellBorders(border(3))];
      borders[index][side] = border(width, "hidden");
      const rebuilt = rebuildBorders(grid, borders);
      assert.equal(rebuilt[0].right.width, 0, index + " の " + side + "、幅 " + width);
      assert.equal(rebuilt[0].right.style, "none");
      assert.equal(rebuilt[1].left.width, 0);
    }
  }
});

test("共有する横の辺では、どちら側の hidden も隣の線を消す", () => {
  const grid = buildGrid(body([[td()], [td()]]));
  for (const width of [0, 8]) {
    for (const [index, side] of [[0, "bottom"], [1, "top"]]) {
      const borders = [cellBorders(border(2)), cellBorders(border(3))];
      borders[index][side] = border(width, "hidden");
      const rebuilt = rebuildBorders(grid, borders);
      assert.equal(rebuilt[0].bottom.width, 0, index + " の " + side + "、幅 " + width);
      assert.equal(rebuilt[0].bottom.style, "none");
      assert.equal(rebuilt[1].top.width, 0);
    }
  }
});

test("結合した辺の一部に hidden があれば、その辺全体の線を消す", () => {
  const vertical = buildGrid(body([[td(2), td()], [td()]]));
  const verticalBorders = Array.from({ length: 3 }, () => cellBorders());
  verticalBorders[1].left = border(5, "double");
  verticalBorders[2].left = border(0, "hidden");
  const right = rebuildBorders(vertical, verticalBorders)[0].right;
  assert.equal(right.width, 0);
  assert.equal(right.style, "none");
  const horizontal = buildGrid(body([[td(1, 2)], [td(), td()]]));
  const horizontalBorders = Array.from({ length: 3 }, () => cellBorders());
  horizontalBorders[1].top = border(4, "double");
  horizontalBorders[2].top = border(0, "hidden");
  const bottom = rebuildBorders(horizontal, horizontalBorders)[0].bottom;
  assert.equal(bottom.width, 0);
  assert.equal(bottom.style, "none");
});

test("外周の hidden は線を描かない", () => {
  const grid = buildGrid(body([[td()]]));
  const rebuilt = rebuildBorders(grid, [cellBorders(border(8, "hidden"))]);
  for (const side of ["top", "right", "bottom", "left"]) {
    assert.equal(rebuilt[0][side].width, 0);
    assert.equal(rebuilt[0][side].style, "none");
  }
});

test("1 行目が短くても、その外に始まるセルの上の罫線を残す", () => {
  const grid = buildGrid(body([many(2), many(3)]));
  const borders = Array.from({ length: 5 }, () => cellBorders());
  borders[4].top = border(3, "dashed", "rgb(10, 20, 30)");
  const rebuilt = rebuildBorders(grid, borders);
  assert.equal(grid.cells[4].row, 1);
  assert.equal(grid.cells[4].col, 2);
  assert.deepEqual(plain(rebuilt[4].top), borders[4].top);
  assert.equal(rebuilt[2].top.width, 0);
  assert.equal(rebuilt[3].top.width, 0);
});

test("結合セルの上に一部だけ空きがあれば、上の罫線を重ねない", () => {
  const grid = buildGrid(body([[td()], [td(1, 2)]]));
  const borders = [cellBorders(), cellBorders()];
  borders[1].top = border(2, "double", "rgb(40, 50, 60)");
  const rebuilt = rebuildBorders(grid, borders);
  assert.deepEqual(plain(grid.grid), [[0], [1, 1]]);
  assert.equal(rebuilt[1].top.width, 0);
  assert.equal(rebuilt[1].top.style, "none");
  assert.deepEqual(plain(rebuilt[0].bottom), borders[1].top);
});

test("結合セルの左に一部だけ空きがあれば、左の罫線を重ねない", () => {
  const grid = buildGrid(body([[td(), td(2)], []]));
  const borders = [cellBorders(), cellBorders()];
  borders[1].left = border(4, "dashed", "rgb(70, 80, 90)");
  const rebuilt = rebuildBorders(grid, borders);
  assert.equal(grid.cells[1].col, 1);
  assert.equal(grid.grid[0][0], 0);
  assert.equal(grid.grid[1][0], undefined);
  assert.equal(rebuilt[1].left.width, 0);
  assert.equal(rebuilt[1].left.style, "none");
  assert.deepEqual(plain(rebuilt[0].right), borders[1].left);
});

test("結合セルの上か左がすべて空いていれば、外周の罫線を残す", () => {
  const topGrid = buildGrid(body([[], [td(1, 2)]]));
  const topBorders = [cellBorders()];
  topBorders[0].top = border(2, "dashed", "rgb(10, 20, 30)");
  assert.deepEqual(plain(rebuildBorders(topGrid, topBorders)[0].top), topBorders[0].top);
  const leftGrid = buildGrid(body([[td(2)], []]));
  const leftBorders = [cellBorders()];
  leftBorders[0].left = border(3, "double", "rgb(40, 50, 60)");
  assert.deepEqual(plain(rebuildBorders(leftGrid, leftBorders)[0].left), leftBorders[0].left);
});

test("同じ太さの共有罫線は上か左のセルの色と線種を使う", () => {
  const grid = buildGrid(body([many(2), many(2)]));
  const borders = Array.from({ length: 4 }, () => cellBorders());
  borders[0].right = border(2, "dashed", "rgb(200, 0, 0)");
  borders[1].left = border(2, "solid", "rgb(0, 0, 200)");
  borders[0].bottom = border(3, "double", "rgb(0, 200, 0)");
  borders[2].top = border(3, "solid", "rgb(200, 200, 0)");
  const rebuilt = rebuildBorders(grid, borders);
  assert.deepEqual(plain(rebuilt[0].right), borders[0].right);
  assert.deepEqual(plain(rebuilt[0].bottom), borders[0].bottom);
});

test("罫線を付け直す計算は格子と元の罫線を書き換えない", () => {
  const grid = buildGrid(body([[td(2), td()], [td()]]));
  const borders = Array.from({ length: 3 }, () => cellBorders());
  borders[2].left = border(4, "double", "rgb(50, 60, 70)");
  const before = JSON.stringify({ grid, borders });
  const rebuilt = rebuildBorders(grid, borders);
  assert.equal(JSON.stringify({ grid, borders }), before);
  rebuilt[0].right.width = 99;
  assert.equal(JSON.stringify({ grid, borders }), before);
});

// 境目は全体を固定しない範囲で選び、結合セルを分けない。
test("表 A の行の境目は見出しと本文の rowspan をまたがない", () => {
  const model = buildGrid(samples.A());
  const boundaries = plain(usableRowBoundaries(model));
  assert.ok(boundaries.includes(0));
  assert.ok(boundaries.includes(2));
  assert.ok(!boundaries.includes(1));
  assert.ok(!boundaries.includes(3));
  assert.equal(stepBoundary(boundaries, 2, 1), 8);
  assert.ok(!boundaries.includes(model.rowCount));
  for (const boundary of boundaries) {
    assert.ok(!model.cells.some((cell) => cell.row < boundary && boundary < cell.row + cell.rowspan));
  }
});

test("表 C の列の境目は固定する見出しの結合だけを避ける", () => {
  const model = buildGrid(samples.C());
  const withHeaders = plain(usableColBoundaries(model, 2));
  assert.ok(withHeaders.includes(0));
  assert.ok(withHeaders.includes(2));
  assert.ok(!withHeaders.includes(1));
  assert.ok(!withHeaders.includes(model.colCount));
  assert.deepEqual(plain(usableColBoundaries(model, 0)), Array.from({ length: model.colCount }, (_, index) => index));
});

test("本文の colspan は使える列の境目を狭めない", () => {
  const model = buildGrid(body([many(4, th), [td(1, 4)], many(4)]));
  assert.deepEqual(plain(usableColBoundaries(model, 1)), [0, 1, 2, 3]);
  assert.deepEqual(plain(usableColBoundaries(model, 2)), [0]);
});

test("rowspan=0 の中間の行は境目に使わず、次のまとまりを使う", () => {
  const model = buildGrid([
    group("tbody", [[td(0), td()], [td()], [td()]]),
    group("tbody", [many(2), many(2)]),
  ]);
  assert.deepEqual(plain(usableRowBoundaries(model)), [0, 3, 4]);
});

test("空の表と 1 マスの表では全行や全列の境目を作らない", () => {
  const empty = buildGrid([]);
  assert.deepEqual(plain(usableRowBoundaries(empty)), []);
  assert.deepEqual(plain(usableColBoundaries(empty, 0)), []);
  const single = buildGrid(body([[td()]]));
  assert.deepEqual(plain(usableRowBoundaries(single)), [0]);
  assert.deepEqual(plain(usableColBoundaries(single, 0)), [0]);
});

test("境目の増減は最も近い使える数へ進み、端なら進めない", () => {
  const boundaries = [0, 2, 5];
  assert.equal(stepBoundary(boundaries, 0, 1), 2);
  assert.equal(stepBoundary(boundaries, 2, 1), 5);
  assert.equal(stepBoundary(boundaries, 5, 1), null);
  assert.equal(stepBoundary(boundaries, 5, -1), 2);
  assert.equal(stepBoundary(boundaries, 2, -1), 0);
  assert.equal(stepBoundary(boundaries, 0, -1), null);
  assert.equal(stepBoundary(boundaries, 3, 1), 5);
  assert.equal(stepBoundary(boundaries, 3, -1), 2);
  assert.equal(stepBoundary([], 0, 1), null);
  assert.equal(stepBoundary([], 0, -1), null);
});

test("使える境目とその増減の計算は入力を書き換えない", () => {
  const model = buildGrid(samples.C());
  const modelBefore = JSON.stringify(model);
  const boundaries = usableRowBoundaries(model);
  usableColBoundaries(model, 2);
  const boundariesBefore = JSON.stringify(boundaries);
  stepBoundary(boundaries, 2, 1);
  stepBoundary(boundaries, 2, -1);
  assert.equal(JSON.stringify(model), modelBefore);
  assert.equal(JSON.stringify(boundaries), boundariesBefore);
});

// 選んだセルの上と左を、結合セルを切らない境目へ戻す。
test("表 A の本文の rowspan=6 の中では、そのまとまりの上までを固定する", () => {
  const model = buildGrid(samples.A());
  const index = model.cells.findIndex((cell) => cell.row === 5 && cell.col === 3);
  assert.deepEqual(plain(pickBoundaries(model, index)), { rows: 2, cols: 2 });
  const next = model.cells.findIndex((cell) => cell.row === 8 && cell.col === 7);
  assert.deepEqual(plain(pickBoundaries(model, next)), { rows: 8, cols: 6 });
});

test("セルで固定する列は、選んだ行数に含まれる見出しの結合をまたがない", () => {
  const model = buildGrid(samples.C());
  for (const [col, expected] of [[3, 2], [6, 5]]) {
    const index = model.cells.findIndex((cell) => cell.row === 2 && cell.col === col);
    assert.deepEqual(plain(pickBoundaries(model, index)), { rows: 2, cols: expected });
  }
});

test("行 0 と列 0 のセルは、その向きを固定しない", () => {
  const model = buildGrid(body([many(4, th), many(4), many(4)]));
  for (const [row, col, expected] of [[0, 0, [0, 0]], [0, 2, [0, 2]], [2, 0, [2, 0]], [2, 3, [2, 3]]]) {
    const index = model.cells.findIndex((cell) => cell.row === row && cell.col === col);
    assert.deepEqual(dimensions(pickBoundaries(model, index)), expected);
  }
});

// 結合セルの占めるすべての行と列に交わるセルを強調する。
const crossSample = () => buildGrid(body([
  [td(2, 2), td(), td()],
  [td(), td()],
  [td(), td(), td(1, 2)],
  many(4),
]));

test("縦横に結合したセルに載ると、覆う 2 行と 2 列のセルを重複せず返す", () => {
  assert.deepEqual(plain(crossCells(crossSample(), 0)), [0, 1, 2, 3, 4, 5, 6, 8, 9]);
});

test("横に結合したセルに載ると、覆うすべての列とその行に交わるセルを返す", () => {
  assert.deepEqual(plain(crossCells(crossSample(), 7)), [1, 2, 3, 4, 5, 6, 7, 10, 11]);
});

test("普通のセルの行や列が結合セルの途中に交わる場合も、その結合セルを含める", () => {
  const model = crossSample();
  assert.deepEqual(plain(crossCells(model, 3)), [0, 1, 3, 4, 7, 10]);
  assert.deepEqual(plain(crossCells(model, 6)), [0, 5, 6, 7, 9]);
  assert.deepEqual(plain(crossCells(model, 10)), [1, 3, 7, 8, 9, 10, 11]);
});

test("セルの選択と行列の強調の計算は格子を書き換えない", () => {
  const model = crossSample();
  const before = JSON.stringify(model);
  pickBoundaries(model, 6);
  const cells = crossCells(model, 6);
  assert.equal(JSON.stringify(model), before);
  cells.push(99);
  assert.deepEqual(plain(crossCells(model, 6)), [0, 5, 6, 7, 9]);
  assert.equal(JSON.stringify(model), before);
});

// 行数を選び直したときは、その行数で自動の列数を計算し直す。
test("表 C は行を 0 と 2 のどちらにしても行の見出しを 2 列残す", () => {
  const model = buildGrid(samples.C());
  const automatic = frozenRegion(model);
  assert.deepEqual(plain(frozenRegion(model, null)), plain(automatic));
  for (const rows of [0, 2]) {
    const region = frozenRegion(model, rows);
    assert.deepEqual(dimensions(region), [rows, 2]);
    assert.equal(region.headerCols, 2);
    assert.equal(region.roles[0].top, rows === 2);
    assert.equal(region.roles[0].left, true);
  }
});

test("選んだ行数より後の行だけで行の見出しの列数を数え直す", () => {
  const model = buildGrid(body([
    many(4, th),
    [th(), td(), td(), td()],
    [th(), th(), td(), td()],
    [th(), th(), td(), td()],
  ]));
  const automatic = frozenRegion(model);
  assert.deepEqual(dimensions(automatic), [1, 1]);
  assert.equal(automatic.headerCols, 1);
  const zero = frozenRegion(model, 0);
  assert.deepEqual(dimensions(zero), [0, 1]);
  assert.equal(zero.headerCols, 1);
  const two = frozenRegion(model, 2);
  assert.deepEqual(dimensions(two), [2, 2]);
  assert.equal(two.headerCols, 2);
});

test("選んだ行数に入れた colspan だけが自動の列数を広げる", () => {
  const model = buildGrid(body([
    many(5, th),
    [th(1, 3), td(), td()],
    [th(), td(), td(), td(), td()],
    [th(), td(), td(), td(), td()],
  ]));
  const one = frozenRegion(model, 1);
  assert.deepEqual(dimensions(one), [1, 1]);
  assert.equal(one.headerCols, 1);
  assert.deepEqual(plain(one.roles[5]), { top: false, left: false, cross: true });
  const two = frozenRegion(model, 2);
  assert.deepEqual(dimensions(two), [2, 3]);
  assert.equal(two.headerCols, 1);
  assert.deepEqual(plain(two.roles[5]), { top: true, left: true, cross: false });
  assert.deepEqual(dimensions(frozenRegion(model, 0)), [0, 1]);
  assert.deepEqual(plain(frozenRegion(model, null)), plain(frozenRegion(model)));
});

test("行数を指定する範囲の計算も格子を書き換えない", () => {
  const model = buildGrid(samples.C());
  const before = JSON.stringify(model);
  const regions = [frozenRegion(model), frozenRegion(model, null), frozenRegion(model, 0), frozenRegion(model, 2)];
  assert.equal(JSON.stringify(model), before);
  regions[2].roles[0].left = false;
  assert.equal(JSON.stringify(model), before);
  assert.equal(regions[3].roles[0].left, true);
});

// 列の見出しは固定する範囲を広げる前の見出しだけで決める。
test("見出しの行数は thead と先頭から続く th の行数の大きい方", () => {
  const withHead = buildGrid([
    group("thead", [many(3), many(3)]),
    group("tbody", [many(3)]),
  ]);
  assert.equal(headerRowCount(withHead), 2);
  const leading = buildGrid([
    group("thead", [many(3, th)]),
    group("tbody", [many(3, th), many(3, th), many(3)]),
  ]);
  assert.equal(headerRowCount(leading), 3);
  assert.equal(headerRowCount(buildGrid([])), 0);
  assert.equal(headerRowCount(buildGrid(body([many(3), many(3)]))), 0);
});

test("見出しの行数を rowspan に合わせて広げず、空の行で th の連続を止める", () => {
  const crossing = buildGrid(body([[th(3), th()], [td()], [td()], many(2)]));
  assert.equal(headerRowCount(crossing), 1);
  assert.equal(frozenRegion(crossing).rows, 3);
  const emptyRow = buildGrid(body([[th(2), th(2)], [], many(2, th), many(2)]));
  assert.equal(headerRowCount(emptyRow), 1);
  assert.equal(headerRowCount(buildGrid(body([many(2, th), many(2, th)]))), 2);
});

test("列の名前は colspan を各列へ広げ、rowspan の同じセルは一度だけ使う", () => {
  const model = buildGrid([
    group("thead", [[th(2), th(1, 2)], [th(), th()]]),
    group("tbody", [many(3)]),
  ]);
  const texts = [" 地域 ", "2025 年", "1 月", "2 月", "本文の地域", "本文 1", "本文 2"];
  assert.deepEqual(plain(columnLabels(model, texts)), ["地域", "2025 年 / 1 月", "2025 年 / 2 月"]);
});

test("列の名前は空白をまとめ、空の見出しや見出しのない列を空文字にする", () => {
  const model = buildGrid(body([
    [th(), th(), th()],
    [th(), th(), th()],
    many(4),
  ]));
  const texts = ["  東京\n 本社\t ", " \n ", "", "営業　 部", "", "\t", "本文", "本文", "本文", "本文"];
  assert.deepEqual(plain(columnLabels(model, texts)), ["東京 本社 / 営業 部", "", "", ""]);
  assert.deepEqual(plain(columnLabels(buildGrid(body([many(2)])), ["本文 1", "本文 2"])), ["", ""]);
  assert.deepEqual(plain(columnLabels(buildGrid([]), [])), []);
});

test("列の名前はつないだ文字列が 40 文字を超えたときだけ省略する", () => {
  const model = buildGrid(body([[th(), th()], [th(), th()], many(2)]));
  const texts = ["あ".repeat(20), "い".repeat(40), "う".repeat(20), ""];
  assert.deepEqual(plain(columnLabels(model, texts)), [("あ".repeat(20) + " / " + "う".repeat(20)).slice(0, 40) + "…", "い".repeat(40)]);
});

test("折り返しの初めの状態は本文の単独の列に始まるセルがすべて nowrap のときだけ外す", () => {
  const model = buildGrid(body([
    [th(), th(), th(1, 3)],
    [td(), td(), td(1, 2), td()],
  ]));
  assert.deepEqual(plain(initialWrap(model, [true, true, false, true, false, true, true])), [false, true, true, true, false]);
  assert.deepEqual(plain(initialWrap(model, [false, false, false, false, false, false, false])), [true, true, true, true, true]);
  assert.deepEqual(plain(initialWrap(buildGrid([]), [])), []);
});

test("見出しの nowrap が消えても本文の nowrap を初めの状態に使う", () => {
  const model = buildGrid([
    group("thead", [many(3, th)]),
    group("tbody", [many(3), many(3)]),
  ]);
  const wrap = initialWrap(model, [false, false, false, true, true, false, true, true, false]);
  assert.deepEqual(plain(wrap), [false, false, true]);
  assert.deepEqual(plain(cellWrapModes(model, wrap).slice(0, 3)), ["nowrap", "nowrap", "wrap"]);
});

test("本文の単独のセルに nowrap でないものがあれば見出しが nowrap でも折り返す", () => {
  const model = buildGrid([
    group("thead", [many(2, th)]),
    group("tbody", [many(2), many(2)]),
  ]);
  assert.deepEqual(plain(initialWrap(model, [true, true, true, false, false, true])), [true, true]);
});

test("本文に単独のセルがない列だけ全行の単独のセルで初めの状態を決める", () => {
  const model = buildGrid([
    group("thead", [many(4, th)]),
    group("tbody", [[td(), td(1, 3)]]),
  ]);
  assert.deepEqual(plain(initialWrap(model, [false, true, true, false, true, false])), [false, false, false, true]);
});

test("見出しだけの表は全行の単独のセルで初めの状態を決める", () => {
  const model = buildGrid(body([many(2, th), many(2, th)]));
  assert.equal(headerRowCount(model), 2);
  assert.deepEqual(plain(initialWrap(model, [true, false, true, true])), [false, true]);
});

test("本文まで rowspan が続く見出しも本文に始まる単独のセルより優先しない", () => {
  const model = buildGrid(body([[th(2), th()], [td()], [td(), td()]]));
  assert.equal(headerRowCount(model), 1);
  assert.deepEqual(plain(initialWrap(model, [false, false, true, true, true])), [false, false]);
});

test("thead の後に続く th だけの行も除いて本文の初めの状態を決める", () => {
  const model = buildGrid([
    group("thead", [many(2, th)]),
    group("tbody", [many(2, th), many(2)]),
  ]);
  assert.equal(headerRowCount(model), 2);
  assert.deepEqual(plain(initialWrap(model, [false, false, false, false, true, true])), [false, false]);
});

test("rowspan の nowrap は覆う行によらず、そのセルが始まる列の状態に使う", () => {
  const model = buildGrid(body([[th(3), td()], [td()], [td()]]));
  assert.deepEqual(plain(initialWrap(model, [true, false, false, false])), [false, true]);
});

test("結合セルは覆うすべての列が折り返さないときだけ nowrap にする", () => {
  const model = buildGrid(body([[td(1, 2), td()], [td(), td(), td()]]));
  assert.deepEqual(plain(cellWrapModes(model, [false, false, true])), ["nowrap", "wrap", "nowrap", "nowrap", "wrap"]);
  assert.deepEqual(plain(cellWrapModes(model, [false, true, false])), ["wrap", "nowrap", "nowrap", "wrap", "nowrap"]);
  assert.deepEqual(plain(cellWrapModes(model, [true, true, true])), ["wrap", "wrap", "wrap", "wrap", "wrap"]);
  assert.deepEqual(plain(cellWrapModes(model, [false, false, false])), ["nowrap", "nowrap", "nowrap", "nowrap", "nowrap"]);
  assert.deepEqual(plain(cellWrapModes(buildGrid([]), [])), []);
});

test("列の最小の幅は最大の内容の幅と上限の小さい方にする", () => {
  assert.deepEqual(plain(columnMinWidths([0, 40, 191.5, 192, 380], 192)), [0, 40, 191.5, 192, 192]);
  assert.deepEqual(plain(columnMinWidths([20, 40], 0)), [0, 0]);
  assert.deepEqual(plain(columnMinWidths([], 192)), []);
});

test("見出しと折り返しの計算は入力を書き換えず、新しい配列を返す", () => {
  const model = buildGrid(body([[th(2), th()], [th()], many(2)]));
  const texts = ["地域", "日付", "内容", "東京", "10 月"];
  const nowrapFlags = [true, false, false, true, false];
  const wrap = [false, true];
  const widths = [80, 300];
  const before = JSON.stringify({ model, texts, nowrapFlags, wrap, widths });
  headerRowCount(model);
  const labels = columnLabels(model, texts);
  const initial = initialWrap(model, nowrapFlags);
  const modes = cellWrapModes(model, wrap);
  const minimums = columnMinWidths(widths, 192);
  assert.equal(JSON.stringify({ model, texts, nowrapFlags, wrap, widths }), before);
  labels[0] = "変更";
  initial[0] = true;
  modes[0] = "変更";
  minimums[0] = 999;
  assert.equal(JSON.stringify({ model, texts, nowrapFlags, wrap, widths }), before);
});

// 検索は表示の文字を正規化し、結合セルもセルごとに一度だけ数える。
test("表内検索は全角・半角、大文字・小文字、連続する空白を同じ形で比べる", () => {
  const texts = [" ＡＢＣ\t  東\n の店 ", "abc 東 の店", "別の店", "前abc 東 の店後"];
  assert.deepEqual(plain(searchMatches(texts, "　AbC  東\tの店　")), [0, 1, 3]);
  for (const query of ["", " \t\n　"]) assert.deepEqual(plain(searchMatches(texts, query)), []);
  assert.deepEqual(plain(searchMatches(texts, "見つからない")), []);
});

test("結合セルの検索も行・列のセルの順で返す", () => {
  const model = buildGrid(body([[th(2, 2), td()], [td()], many(3)]));
  const texts = ["結合した対象", "対象 1", "対象 2", "本文", "対象 3", "本文"];
  const matches = plain(searchMatches(texts, "対象"));
  assert.deepEqual(matches, [0, 1, 2, 4]);
  assert.equal(model.cells[matches[0]].rowspan, 2);
  assert.equal(model.cells[matches[0]].colspan, 2);
});

test("表 A の 6 行のまとまりは 2 行目だけの一致でも全体を残す", () => {
  const model = buildGrid(samples.A());
  const matched = [model.cells.findIndex(cell => cell.row === 9 && cell.col === 1)];
  assert.deepEqual(plain(filterRows(model, matched, 2)), [
    ...Array.from({ length: 6 }, (_, i) => i + 2),
    ...Array.from({ length: 18 }, (_, i) => i + 14),
  ]);
});

test("rowspan のつながりを推移的にたどり、まとまりの途中の一致を残す", () => {
  const model = buildGrid(body([
    many(3, th),
    [td(2), td(), td()],
    [td(2), td()],
    [td(), td()],
    many(3),
  ]));
  const matched = [model.cells.findIndex(cell => cell.row === 3 && cell.col === 2)];
  assert.deepEqual(plain(filterRows(model, matched, 1)), [4]);
});

test("本文の区切りは後ろに残るまとまりがあるときだけ残す", () => {
  const model = buildGrid(body([
    many(3, th),
    [td(1, 3)],
    many(3),
    [td(1, 3)],
    [td(2), td(), td()],
    [td(), td()],
    [td(1, 3)],
    many(3),
  ]));
  const inSecondPart = model.cells.findIndex(cell => cell.row === 5 && cell.col === 1);
  assert.deepEqual(plain(filterRows(model, [inSecondPart], 1)), [1, 2, 6, 7]);
  const divider = model.cells.findIndex(cell => cell.row === 1);
  assert.deepEqual(plain(filterRows(model, [divider], 1)), [2, 3, 4, 5, 6, 7]);
  assert.deepEqual(plain(filterRows(model, [], 1)), [1, 2, 3, 4, 5, 6, 7]);
});

test("thead・tfoot と keepRows の先頭の行は一致が 0 件でも残す", () => {
  const model = buildGrid([
    group("thead", [many(3, th), many(3, th)]),
    group("tbody", [many(3), many(3), many(3)]),
    group("tfoot", [many(3)]),
  ]);
  assert.deepEqual(plain(filterRows(model, [], 3)), [3, 4]);
  assert.deepEqual(plain(filterRows(model, [], 0)), [2, 3, 4]);
  assert.deepEqual(plain(filterRows(buildGrid(samples.A()), [], 2)), Array.from({ length: 30 }, (_, i) => i + 2));
});

test("必ず残す行に rowspan のまとまりがかかる場合は結合を分けずに残す", () => {
  const model = buildGrid(body([many(2, th), [td(2), td()], [td()], many(2)]));
  assert.deepEqual(plain(filterRows(model, [], 2)), [3]);
});

test("写す格子は隠した行を除き、結合セルの左上だけに文字を入れる", () => {
  const model = buildGrid(body([[th(2), th(1, 2)], [th(), th()], many(3), many(3)]));
  const texts = ["地域", "月", "1 月", "2 月", "隠す 1", "隠す 2", "隠す 3", "東", "100", "200"];
  const matrix = tableMatrix(model, texts, [2]);
  assert.deepEqual(plain(matrix), {
    rowCount: 3, colCount: 3,
    cells: [
      { row: 0, col: 0, rowspan: 2, colspan: 1, header: true, text: "地域" },
      { row: 0, col: 1, rowspan: 1, colspan: 2, header: true, text: "月" },
      { row: 1, col: 1, rowspan: 1, colspan: 1, header: true, text: "1 月" },
      { row: 1, col: 2, rowspan: 1, colspan: 1, header: true, text: "2 月" },
      { row: 2, col: 0, rowspan: 1, colspan: 1, header: false, text: "東" },
      { row: 2, col: 1, rowspan: 1, colspan: 1, header: false, text: "100" },
      { row: 2, col: 2, rowspan: 1, colspan: 1, header: false, text: "200" },
    ],
    grid: [["地域", "月", ""], ["", "1 月", "2 月"], ["東", "100", "200"]],
  });
});

test("写す格子は隠した rowspan のまとまりを除き、後の行を今の順で詰める", () => {
  const model = buildGrid(body([many(2, th), [td(2), td()], [td()], [td(2), td()], [td()]]));
  const matrix = tableMatrix(model, ["見出し 1", "見出し 2", "消す地域", "消す値 1", "消す値 2", "残る地域", "先", "後"], [1, 2]);
  assert.deepEqual(plain(matrix.grid), [["見出し 1", "見出し 2"], ["残る地域", "先"], ["", "後"]]);
  assert.deepEqual(plain(matrix.cells.find(cell => cell.text === "残る地域")), { row: 1, col: 0, rowspan: 2, colspan: 1, header: false, text: "残る地域" });
  assert.deepEqual(plain(tableMatrix(buildGrid([]), [], [])), { rowCount: 0, colCount: 0, cells: [], grid: [] });
});

test("TSV はタブ・改行・引用符を含む値だけを囲み、末尾にも CRLF を付ける", () => {
  const matrix = { grid: [["普通", "タブ\tあり", "改行\nあり", '引用"符', ""], ["空白 あり", "\r\n", "&<>", "終わり", ""]] };
  assert.equal(toTsv(matrix), '普通\t"タブ\tあり"\t"改行\nあり"\t"引用""符"\t\r\n空白 あり\t"\r\n"\t&<>\t終わり\t\r\n');
  assert.equal(toTsv({ grid: [] }), "");
});

test("HTML は th・td と結合を残し、文字を逃がして改行を br にする", () => {
  const model = buildGrid(body([[th(2), th(1, 2)], [td(), td()]]));
  const matrix = tableMatrix(model, ['&<>"', "月\n次", "1", "2"], []);
  assert.equal(toHtml(matrix), '<table><tr><th rowspan="2">&amp;&lt;&gt;&quot;</th><th colspan="2">月<br>次</th></tr><tr><td>1</td><td>2</td></tr></table>');
  assert.equal(toHtml(tableMatrix(buildGrid([]), [], [])), "<table></table>");
});

test("検索・絞り込み・写すの計算は入力を書き換えず、返した値も入力と共有しない", () => {
  const model = buildGrid(body([[th(2), th()], [td()], many(2)]));
  const texts = ["地域", "値", "対象", "東", "100"];
  const matched = [2];
  const hiddenRows = [2];
  const before = JSON.stringify({ model, texts, matched, hiddenRows });
  const matches = searchMatches(texts, "対象");
  const hidden = filterRows(model, matched, 1);
  const matrix = tableMatrix(model, texts, hiddenRows);
  const matrixBefore = JSON.stringify(matrix);
  toTsv(matrix);
  toHtml(matrix);
  assert.equal(JSON.stringify(matrix), matrixBefore);
  matches.push(99);
  hidden.push(99);
  matrix.cells[0].text = "変更";
  matrix.grid[0][0] = "変更";
  assert.equal(JSON.stringify({ model, texts, matched, hiddenRows }), before);
});
