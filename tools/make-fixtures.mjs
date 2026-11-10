#!/usr/bin/env node
// 開発用: 試験の見本 (GitLab の本物の出力) を作り直す。アプリからは呼ばれない。
//
//   GITLAB_TOKEN=<read_api のトークン> node tools/make-fixtures.mjs [名前 …]
//
// test/fixtures/*.md (このリポジトリにある合成の Markdown だけ) を、gitlab.com の Markdown API
// (POST /api/v4/markdown、gfm: true、project なし) に 1 つずつ送り、返ってきた HTML を加工せずに
// test/fixtures/<名前>.gitlab.html に保存する。取得した条件は test/fixtures/fixtures.json に書く。
// 名前を渡すと、その見本だけを作り直す。
//
// トークンは環境変数から読んでヘッダーに入れるだけで、表示もファイルへの保存もしない。
// 失敗したときも、応答の本文は表示しない。試験 (npm test) は GitLab に繋がない。
import { readdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const ORIGIN = 'https://gitlab.com';
const INTERVAL = 400; // 回数制限に掛からないよう、要求の間を空ける (ミリ秒)
const fixtures = new URL('../test/fixtures/', import.meta.url);

function fail(message) {
  console.error(`make-fixtures: ${message}`);
  process.exit(1);
}

const token = (process.env.GITLAB_TOKEN || '').trim();
if (!token) fail('環境変数 GITLAB_TOKEN が要ります (read_api のトークン)。');

async function request(path, init = {}) {
  let response;
  try {
    response = await fetch(ORIGIN + path, {
      ...init,
      headers: { 'PRIVATE-TOKEN': token, Accept: 'application/json', ...init.headers },
      signal: AbortSignal.timeout(30000),
      redirect: 'error',
    });
  } catch (error) {
    fail(`${path} に接続できません (${error.name})。`);
  }
  if (!response.ok) fail(`${path} が HTTP ${response.status} を返しました。`);
  return response.json();
}

const wanted = new Set(process.argv.slice(2));
const names = (await readdir(fixtures)).filter((name) => name.endsWith('.md')).map((name) => name.slice(0, -3)).sort();
for (const name of wanted) if (!names.includes(name)) fail(`見本の入力がありません: ${name}.md`);
const targets = names.filter((name) => wanted.size === 0 || wanted.has(name));
if (targets.length === 0) fail('見本の入力 (test/fixtures/*.md) がありません。');

const version = await request('/api/v4/version');
if (typeof version.version !== 'string') fail('GitLab の版を読めません。');

const metaUrl = new URL('fixtures.json', fixtures);
let meta = { schema: 1, source: `${ORIGIN}/api/v4/markdown`, request: { gfm: true, project: null }, fixtures: {} };
try {
  const saved = JSON.parse(await readFile(metaUrl, 'utf8'));
  if (saved && saved.schema === 1 && saved.fixtures) meta = saved;
} catch {
  // 初めて作るとき
}

for (const name of targets) {
  const text = (await readFile(new URL(`${name}.md`, fixtures), 'utf8')).replaceAll('\r\n', '\n');
  const data = await request('/api/v4/markdown', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text, gfm: true }),
  });
  if (typeof data.html !== 'string') fail(`${name}: 応答に html がありません。`);
  await writeFile(new URL(`${name}.gitlab.html`, fixtures), data.html);
  meta.fixtures[name] = { gitlab: version.version, revision: version.revision, fetched: new Date().toISOString().slice(0, 10) };
  console.log(`${name}: ${data.html.length} 文字`);
  await new Promise((resolve) => setTimeout(resolve, INTERVAL));
}
for (const name of Object.keys(meta.fixtures)) if (!names.includes(name)) delete meta.fixtures[name];
meta.fixtures = Object.fromEntries(Object.entries(meta.fixtures).sort(([a], [b]) => (a < b ? -1 : 1)));
await writeFile(metaUrl, JSON.stringify(meta, null, 2) + '\n');
console.log(`GitLab ${version.version} (${fileURLToPath(metaUrl)})`);
