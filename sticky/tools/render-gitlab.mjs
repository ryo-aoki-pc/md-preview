import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const fixture = new URL("../test/fixtures/", import.meta.url);

// API の失敗本文と例外は、トークンを含むおそれがあるため出さない。
export async function renderGitlab({
  sourcePath = fileURLToPath(new URL("sample.md", fixture)),
  outputPath = fileURLToPath(new URL("sample.gitlab.html", fixture)),
  token = process.env.GITLAB_TOKEN,
  request = fetch,
} = {}) {
  if (!token) throw new Error("GITLAB_TOKEN を設定してください");
  const text = await readFile(sourcePath, "utf8");
  const response = await request("https://gitlab.com/api/v4/markdown", {
    method: "POST",
    headers: { "Content-Type": "application/json", "PRIVATE-TOKEN": token },
    body: JSON.stringify({ text, gfm: true }),
  });
  if (!response.ok) throw new Error("Markdown API から成功の返事がありませんでした");
  const result = await response.json();
  if (typeof result.html !== "string") throw new Error("Markdown API の返事に HTML がありません");
  const html = result.html
    .replace(/\s+data-sourcepos\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)/giu, "")
    .replace(/\r\n?/gu, "\n").replace(/\n*$/u, "\n");
  await writeFile(outputPath, html, "utf8");
}

async function main() {
  if (process.argv.length > 2) {
    console.error("描き直し: 引数は使いません。test/fixtures/sample.md を読みます");
    process.exitCode = 2;
    return;
  }
  if (!process.env.GITLAB_TOKEN) {
    console.error("描き直し: GITLAB_TOKEN を設定してください");
    process.exitCode = 2;
    return;
  }
  try {
    await renderGitlab();
    console.log("描き直し: test/fixtures/sample.gitlab.html を作り直しました");
  } catch {
    console.error("描き直し: Markdown API の呼び出しか、ファイルの読み書きに失敗しました");
    process.exitCode = 2;
  }
}

if (typeof process !== "undefined" && process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  await main();
}
