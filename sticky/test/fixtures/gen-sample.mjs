// 確かめ用の Markdown（結合セルを含む大きな表）を作る。中身は架空のもの。
import { writeFileSync } from "node:fs";

const out = [];
const p = (s) => out.push(s);
const months = ["1 月", "2 月", "3 月", "4 月"];
const num = (r, c) => String(((r * 37 + c * 11) % 900) + 100);

p("# 表の固定の確かめ用");
p("");
p("架空の数値。gitlab.com と self-managed の GitLab で、先頭行・先頭列の固定を確かめるための表。");
p("");

// A: thead が 2 行。見出しに rowspan・colspan。本文の先頭列は th の rowspan。
p("## A: thead が 2 行、先頭列に rowspan");
p("");
p("<table>");
p("<thead>");
p('<tr><th rowspan="2">地域</th><th rowspan="2">店舗</th><th colspan="4">2025 年</th><th colspan="4">2026 年</th><th rowspan="2">備考</th></tr>');
p("<tr>" + [...months, ...months].map((m) => `<th>${m}</th>`).join("") + "</tr>");
p("</thead>");
p("<tbody>");
const regions = ["東", "西", "南", "北", "中央"];
let r = 0;
for (const region of regions) {
  for (let i = 1; i <= 6; i++, r++) {
    const head = i === 1 ? `<th rowspan="6">${region}</th>` : "";
    const cells = Array.from({ length: 8 }, (_, c) => `<td align="right">${num(r, c)}</td>`).join("");
    p(`<tr>${head}<td>${region}の店 ${i}</td>${cells}<td>備考の文は長めにして、横にスクロールさせる。${region}-${i}</td></tr>`);
  }
}
p("</tbody>");
p("</table>");
p("");

// B: thead が無い。先頭の 2 行が th。2 列の行の見出し。全列の区切りの行。
p("## B: thead が無い、2 列の見出し、区切りの行");
p("");
p("<table>");
p('<tr><th colspan="2">分類</th><th colspan="8">指標</th></tr>');
p("<tr><th>大</th><th>小</th>" + Array.from({ length: 8 }, (_, c) => `<th>指標 ${c + 1}</th>`).join("") + "</tr>");
let rb = 0;
for (let part = 1; part <= 3; part++) {
  p(`<tr><td colspan="10">区切り: 第 ${part} 部（この行は全列に広がる）</td></tr>`);
  for (let big = 1; big <= 3; big++) {
    for (let small = 1; small <= 3; small++, rb++) {
      const head = small === 1 ? `<th rowspan="3">大 ${part}-${big}</th>` : "";
      const cells = Array.from({ length: 8 }, (_, c) => `<td>値 ${num(rb, c)}（説明の文を足して幅を広げる）</td>`).join("");
      p(`<tr>${head}<th>小 ${small}</th>${cells}</tr>`);
    }
  }
}
p("</table>");
p("");

// C: 角のセルが rowspan と colspan を両方持つ。
p("## C: 角のセルが rowspan と colspan を両方持つ");
p("");
p("<table>");
p("<thead>");
p('<tr><th rowspan="2" colspan="2">区分</th><th colspan="3">前期</th><th colspan="3">後期</th></tr>');
p("<tr>" + ["計画", "実績", "差", "計画", "実績", "差"].map((s) => `<th>${s}</th>`).join("") + "</tr>");
p("</thead>");
p("<tbody>");
let rc = 0;
for (let g = 1; g <= 8; g++) {
  for (let k = 1; k <= 3; k++, rc++) {
    const head = k === 1 ? `<th rowspan="3">部門 ${g}</th>` : "";
    const cells = Array.from({ length: 6 }, (_, c) => `<td align="right">${num(rc, c)},000 円</td>`).join("");
    p(`<tr>${head}<th>課 ${g}-${k}</th>${cells}</tr>`);
  }
}
p("</tbody>");
p("</table>");
p("");

// D: パイプの表（見出しは 1 行）。結合セルは無く、master では並べ替えができる。
p("## D: パイプの表");
p("");
p("| 番号 | " + Array.from({ length: 11 }, (_, c) => `列 ${c + 1}`).join(" | ") + " |");
p("| --- | " + Array.from({ length: 11 }, () => "---").join(" | ") + " |");
for (let i = 1; i <= 40; i++) {
  p(`| ${i} | ` + Array.from({ length: 11 }, (_, c) => `値 ${num(i, c)} の説明`).join(" | ") + " |");
}
p("");

// E: rowspan=0、caption、tfoot。
p("## E: rowspan=0、caption、tfoot");
p("");
p("<table>");
p("<caption>rowspan=0 は行のまとまりの終わりまで広がる</caption>");
p("<thead>");
p("<tr><th>群</th>" + Array.from({ length: 9 }, (_, c) => `<th>項目 ${c + 1}</th>`).join("") + "</tr>");
p("</thead>");
p("<tbody>");
for (let i = 0; i < 25; i++) {
  const head = i === 0 ? '<th rowspan="0">第 1 群（rowspan=0）</th>' : "";
  p(`<tr>${head}` + Array.from({ length: 9 }, (_, c) => `<td>値 ${num(i, c)} と長めの説明</td>`).join("") + "</tr>");
}
p("</tbody>");
p("<tfoot>");
p("<tr><th>計</th>" + Array.from({ length: 9 }, (_, c) => `<td>合計 ${c + 1}</td>`).join("") + "</tr>");
p("</tfoot>");
p("</table>");
p("");

// F: 小さい表（スクロールしない）。
p("## F: 小さい表");
p("");
p("| 名前 | 値 |");
p("| --- | --- |");
p("| a | 1 |");
p("| b | 2 |");
p("");

// G: セルの中に入れ子の表。
p("## G: 入れ子の表");
p("");
p("<table>");
p("<thead><tr><th>外</th><th>中身</th>" + Array.from({ length: 6 }, (_, c) => `<th>列 ${c + 1}</th>`).join("") + "</tr></thead>");
p("<tbody>");
for (let i = 1; i <= 12; i++) {
  const inner = i === 2 ? "<table><tr><th>内</th><td>入れ子</td></tr><tr><th>内 2</th><td>入れ子 2</td></tr></table>" : `中身 ${i}`;
  p(`<tr><th>外 ${i}</th><td>${inner}</td>` + Array.from({ length: 6 }, (_, c) => `<td>値 ${num(i, c)} の説明の文</td>`).join("") + "</tr>");
}
p("</tbody>");
p("</table>");
p("");

// H: 折りたたみの中の表（開くまで大きさが 0）。
p("## H: 折りたたみの中の表");
p("");
p("<details>");
p("<summary>開くと表が出る</summary>");
p("");
p("<table>");
p("<tr><th>行の見出し</th>" + Array.from({ length: 10 }, (_, c) => `<th>列 ${c + 1}</th>`).join("") + "</tr>");
for (let i = 1; i <= 30; i++) {
  p(`<tr><th>行 ${i}</th>` + Array.from({ length: 10 }, (_, c) => `<td>値 ${num(i, c)} の説明の文</td>`).join("") + "</tr>");
}
p("</table>");
p("");
p("</details>");
p("");

// I: 本文の先頭行の一部の th は、GitLab 自身の上の固定だけを外す。
p("## I: 本文の先頭行の一部だけが th");
p("");
p("<table>");
p("<thead>");
p("<tr><th>行の見出し</th>" + Array.from({ length: 10 }, (_, c) => `<th>列 ${c + 1}</th>`).join("") + "</tr>");
p("</thead>");
p("<tbody>");
for (let i = 1; i <= 30; i++) {
  const second = i === 1 ? "<th>本文の th</th>" : `<td>本文の値 ${num(i, 0)}</td>`;
  p(`<tr><th>行 ${i}</th>${second}` + Array.from({ length: 9 }, (_, c) => `<td>値 ${num(i, c + 1)} の説明の文</td>`).join("") + "</tr>");
}
p("</tbody>");
p("</table>");
p("");

writeFileSync(process.argv[2], out.join("\n"), "utf8");
