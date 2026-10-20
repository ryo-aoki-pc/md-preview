import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";

const source = await readFile(new URL("../src/sticky-table.js", import.meta.url), "utf8");
const context = { module: { exports: {} } };
vm.runInNewContext(source, context, { filename: "src/sticky-table.js" });
const { buildGrid, frozenRegion, rebuildBorders } = context.module.exports;
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
