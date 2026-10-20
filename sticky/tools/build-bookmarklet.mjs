import { readFile, mkdir, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const defaultRoot = fileURLToPath(new URL("../", import.meta.url));

// 中核の書き方の制約は、test/build.test.mjs で確かめる。
export function compactSource(source) {
  return source.split(/\r?\n/u)
    .filter((line) => line.trim() && !/^\s*\/\//u.test(line))
    .map((line) => line.trimStart())
    .join("\n") + "\nvoid 0;";
}

export function generateBookmarklet(source) {
  return "javascript:" + encodeURIComponent(compactSource(source));
}

function escapeHtml(value) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}

export function generateArtifacts(source) {
  const url = generateBookmarklet(source);
  const escaped = escapeHtml(url);
  const name = "GitLab の表を固定";
  const page = [
    "<!doctype html>",
    '<html lang="ja">',
    "<head>",
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    "<title>" + name + "</title>",
    "<style>",
    "body { max-width: 54rem; margin: 3rem auto; padding: 0 1.25rem; font-family: sans-serif; line-height: 1.7; color: #1f1e24; background: #fff; }",
    "a, button { display: inline-block; margin: .5rem .75rem .5rem 0; padding: .6rem 1rem; border: 1px solid #626168; border-radius: .3rem; font: inherit; }",
    "textarea { display: block; box-sizing: border-box; width: 100%; min-height: 10rem; font: .85rem monospace; }",
    "</style>",
    "</head>",
    "<body>",
    "<h1>" + name + "</h1>",
    "<p>下のリンクをブラウザーのブックマークバー（Edge はお気に入りバー）へドラッグしてください。</p>",
    '<p><a href="' + escaped + '">' + name + "</a></p>",
    "<p>URL を写して、ブックマークの管理画面で新しいブックマークの URL 欄に貼ることもできます。アドレスバーには貼りません。</p>",
    '<button id="copy" type="button">URL を写す</button>',
    '<p id="notice" role="status"></p>',
    '<label for="url">ブックマークの URL</label>',
    '<textarea id="url" readonly spellcheck="false">' + escaped + "</textarea>",
    "<h2>使い方</h2>",
    "<p>GitLab の Markdown の表があるページで、登録したブックマークを押します。もう一度押すと元に戻ります。ページを読み込み直したら押し直してください。</p>",
    "<p>対象は GitLab 19.5 以降、Chrome・Edge です。詳しい決まりと制限は README を見てください。</p>",
    "<script>",
    'document.getElementById("copy").addEventListener("click", async () => {',
    '  const field = document.getElementById("url");',
    "  try {",
    "    await navigator.clipboard.writeText(field.value);",
    '    document.getElementById("notice").textContent = "URL を写しました";',
    "  } catch {",
    "    field.focus();",
    "    field.select();",
    '    document.getElementById("notice").textContent = "自動で写せませんでした。選んだ URL を手動で写してください";',
    "  }",
    "});",
    "</script>",
    "</body>",
    "</html>",
    "",
  ].join("\n");
  return {
    "bookmarklet.txt": url + "\n",
    "bookmarklet.html": page,
  };
}

export async function buildArtifacts({ root = defaultRoot, check = false } = {}) {
  const source = await readFile(join(root, "src/sticky-table.js"), "utf8");
  const artifacts = generateArtifacts(source);
  const different = [];
  for (const [name, content] of Object.entries(artifacts)) {
    let current;
    try {
      current = await readFile(join(root, "dist", name), "utf8");
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    if (current !== content) different.push(name);
  }
  if (!check) {
    await mkdir(join(root, "dist"), { recursive: true });
    for (const [name, content] of Object.entries(artifacts)) {
      await writeFile(join(root, "dist", name), content, "utf8");
    }
  }
  return different;
}

async function main() {
  if (process.argv.slice(2).some((arg) => arg !== "--check")) {
    console.error("組み立て: 使える引数は --check だけです");
    process.exitCode = 2;
    return;
  }
  const check = process.argv.includes("--check");
  try {
    const different = await buildArtifacts({ check });
    if (check && different.length) {
      console.error("組み立て: dist が古くなっています。npm run build で作り直してください");
      process.exitCode = 1;
    }
  } catch {
    console.error("組み立て: ファイルを読み書きできませんでした");
    process.exitCode = 2;
  }
}

if (typeof process !== "undefined" && process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  await main();
}
