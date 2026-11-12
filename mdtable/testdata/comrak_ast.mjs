// テスト用: comrak（GitLab と同じ拡張の設定）で Markdown を読み、ブロックの種類・親・行を返す。
// 標準入力: 文字列の配列（JSON）。標準出力: 文書ごとの [{type, parent, start, end}, ...]（JSON）
// comrak の場所は環境変数 MDTABLE_COMRAK_DIR（node_modules/comrak を持つフォルダー）
import { createRequire } from "node:module";
import { text } from "node:stream/consumers";

const base = process.env.MDTABLE_COMRAK_DIR;
const require = createRequire(base.replace(/[\\/]?$/, "/") + "package.json");
const { parseMarkdown } = require("comrak");

const options = {
  extension: {
    descriptionLists: true, table: true, strikethrough: true, autolink: true, tasklist: true,
    footnotes: true, multilineBlockQuotes: true, mathDollars: true, mathCode: true, alerts: true,
  },
  parse: { relaxedTasklistMatching: true, leaveFootnoteDefinitions: true },
};

function nodeType(node) {
  const value = node.data.value;
  return typeof value === "string" ? value : Object.keys(value)[0];
}

const inputs = JSON.parse(await text(process.stdin));
const results = inputs.map((source) => parseMarkdown(source, options).nodes.map((node) => {
  const pos = node.data.sourcepos;
  return {
    type: nodeType(node),
    parent: node.parent === undefined ? null : node.parent,
    start: pos ? pos.start.line : null,
    end: pos ? pos.end.line : null,
  };
}));
process.stdout.write(JSON.stringify(results));
