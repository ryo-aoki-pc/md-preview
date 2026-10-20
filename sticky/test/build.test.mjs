import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { compactSource, generateBookmarklet, generateArtifacts, buildArtifacts } from "../tools/build-bookmarklet.mjs";

const source = await readFile(new URL("../src/sticky-table.js", import.meta.url), "utf8");

test("生成物は中核から組み立てたものと同じ", async () => {
  assert.deepEqual(await buildArtifacts({ check: true }), []);
  const generated = generateArtifacts(source);
  for (const [name, content] of Object.entries(generated)) {
    assert.equal(await readFile(new URL("../dist/" + name, import.meta.url), "utf8"), content);
    assert.ok(content.endsWith("\n"), name);
    assert.ok(!content.includes("\r"), name);
  }
});

test("中核は組み立てで取り除けるコメントと文字列だけを使う", () => {
  assert.ok(!source.includes("/*"), "ブロックコメントの開始を含まない");
  let quote = "";
  let escaped = false;
  let lineStart = 0;
  for (let i = 0; i < source.length; i += 1) {
    const char = source[i];
    if (quote) {
      if (char === "\n" && quote === "`") assert.fail("テンプレートリテラルは 1 行に収める");
      if (escaped) escaped = false;
      else if (char === "\\") escaped = true;
      else if (char === quote) quote = "";
    } else if (char === '"' || char === "'" || char === "`") {
      quote = char;
    } else if (char === "/" && source[i + 1] === "/") {
      assert.equal(source.slice(lineStart, i).trim(), "", "コメントは行全体にする");
      const next = source.indexOf("\n", i);
      i = next === -1 ? source.length : next - 1;
    }
    if (char === "\n") lineStart = i + 1;
  }
});

test("コメント・空行・字下げを除き、評価の結果を undefined にする", () => {
  assert.equal(compactSource("  // 説明\n\n  (function () {\n    return 1;\n  })();\n"), "(function () {\nreturn 1;\n})();\nvoid 0;");
});

test("生成した URL の中身に構文の誤りが無い", () => {
  const url = generateBookmarklet(source);
  assert.ok(url.startsWith("javascript:"));
  const code = decodeURIComponent(url.slice("javascript:".length));
  assert.doesNotThrow(() => new Function(code));
  assert.equal(new Function(code)(), undefined);
  assert.ok(code.endsWith("void 0;"));
  const generated = generateArtifacts(source);
  assert.deepEqual(Object.keys(generated), ["bookmarklet.txt", "bookmarklet.html"]);
  assert.ok(generated["bookmarklet.html"].includes("navigator.clipboard.writeText"));
});
