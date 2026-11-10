// 消毒済みの本文を、サーバーも公式資産も要らない単一の HTML にする。
import { isUtf8 } from 'node:buffer';
import { createHash, randomUUID } from 'node:crypto';
import { existsSync } from 'node:fs';
import { open, readFile, realpath, rename, rm, stat } from 'node:fs/promises';
import { basename, dirname, isAbsolute, join, relative, resolve as absolute, sep } from 'node:path';
import { Worker } from 'node:worker_threads';
import { fromHtml } from 'hast-util-from-html';
import { toHtml } from 'hast-util-to-html';
import { visit } from 'unist-util-visit';
import { resolve } from './resolve.mjs';
import { resolvePath } from './paths.mjs';
import { readLibrary } from './page-libs.mjs';

const page = new URL('../page/', import.meta.url);
const maximumBytes = 16 * 1024 * 1024;
const escape = (text) =>
  String(text).replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
const encode = (path) => path.split('/').map(encodeURIComponent).join('/');
const unix = (path) => path.split(sep).join('/');
class ExportError extends Error {}
const error = (message) => new ExportError(message);
// 埋め込むライブラリは、lock.json の大きさと sha256 に合うものだけ (サーバーが配るものと同じ)。
function library(name) {
  const found = readLibrary(name);
  if (!found) throw error('同梱のライブラリを読めません: ' + name);
  return found.body;
}

async function documentContext(file, override) {
  let root;
  if (override) root = await realpath(absolute(override));
  else {
    root = dirname(file);
    for (let folder = root; ; folder = dirname(folder)) {
      if (existsSync(join(folder, '.git'))) {
        root = folder;
        break;
      }
      if (folder === dirname(folder)) break;
    }
  }
  if (!(await stat(root)).isDirectory()) throw error('ルートにはフォルダーを指定してください');
  const name = unix(relative(root, file));
  const found = resolve(root, encode(name));
  if (isAbsolute(name) || found.status !== 200 || !(await stat(found.path)).isFile())
    throw error('文書はルートの内側のファイルを指定してください');
  return { root, file: name, dir: unix(relative(root, dirname(file))) };
}

export async function renderForExport(
  text,
  context,
  { timeoutMs = 60000, workerFactory = (url) => new Worker(url) } = {},
) {
  const worker = workerFactory(new URL('./worker.mjs', import.meta.url));
  let timer;
  try {
    return await new Promise((resolveResult, reject) => {
      timer = setTimeout(() => reject(error('描画が時間切れになりました')), timeoutMs);
      const finish = (callback, value) => {
        clearTimeout(timer);
        callback(value);
      };
      worker.once('error', () => finish(reject, error('文書を描画できません')));
      worker.once('exit', () => finish(reject, error('文書を描画できません')));
      worker.once('message', (message) => {
        if (message.id !== 1 || !message.ok || typeof message.result?.html !== 'string')
          return finish(reject, error(message.message || '文書を描画できません'));
        finish(resolveResult, message.result);
      });
      worker.postMessage({ id: 1, text, context });
    });
  } finally {
    clearTimeout(timer);
    await worker.terminate();
  }
}

// 存在しない参照も安全な親まで resolve() で調べる。リンクや .git から外へ出る参照は直さない。
function safeTarget(root, name) {
  if (!name) return true;
  for (let current = name; current; ) {
    const result = resolve(root, encode(current));
    if (result.status === 200) return true;
    if (result.status !== 404) return false;
    const next = current.includes('/') ? current.slice(0, current.lastIndexOf('/')) : '';
    if (!next) return resolve(root, encode(name), true).status === 404;
    current = next;
  }
  return false;
}

export function rewriteExportUrls(tree, context, outputDirectory) {
  visit(tree, 'element', (node) => {
    for (const property of ['href', 'src', 'poster', 'dataSrc', 'dataCanonicalSrc']) {
      const raw = node.properties[property];
      if (
        typeof raw !== 'string' ||
        !raw ||
        raw.startsWith('#') ||
        raw.startsWith('//') ||
        /^[a-z][a-z\d+.-]*:/i.test(raw)
      )
        continue;
      const split = /^([^?#]*)([?#][\s\S]*)?$/.exec(raw);
      if (!split?.[1]) continue;
      let target;
      try {
        target = decodeURIComponent(split[1]);
      } catch {
        continue;
      }
      const local = resolvePath(target, context.dir);
      if (!local.safe || !safeTarget(context.root, local.path)) continue;
      let rewritten = unix(relative(outputDirectory, join(context.root, ...local.path.split('/'))));
      if (isAbsolute(rewritten)) {
        delete node.properties[property];
        continue;
      }
      rewritten = encode(rewritten || '.');
      // 「:」を含む相対名が URL の scheme として読まれないようにする。
      if (!rewritten.startsWith('.')) rewritten = './' + rewritten;
      node.properties[property] = rewritten + (split[2] || '');
    }
  });
}

async function styles(theme) {
  const fallback = await readFile(new URL('fallback.css', page), 'utf8');
  const preview = await readFile(new URL('preview.css', page), 'utf8');
  // ページのスタイルのうち、本文の部分だけを使う (操作欄・見出しの欄・知らせの欄は入れない)。
  const from = preview.indexOf('#gp-page.file-holder');
  const to = preview.indexOf('/* 見出しの欄');
  if (from < 0 || to <= from) throw error('同梱のスタイルから、本文の部分を取り出せません');
  const body = preview.slice(from, to);
  const darkRules = [...fallback.matchAll(/:root\.gl-dark[^{}]*\{[^{}]*\}/g)]
    .map(([rule]) => rule.replaceAll(':root.gl-dark', ':root[data-theme="system"]'))
    .join('\n');
  return (
    fallback +
    '\n' +
    body +
    `\n#gp-content.md{padding:16px 24px} :root[data-theme="light"]{color-scheme:light} :root[data-theme="dark"]{color-scheme:dark}\n` +
    (theme === 'system' ? '@media(prefers-color-scheme:dark){' + darkRules + ':root{color-scheme:dark}}\n' : '') +
    '@media print{#gp-page.file-holder{max-width:none!important}#gp-content.md{padding:0!important}pre,table{break-inside:avoid;page-break-inside:avoid}}'
  );
}

async function katexStyles() {
  const css = library('katex/katex.min.css').toString('utf8');
  // 同梱 KaTeX の固定した CSS だけを変換する。出所不明の CSS は受け取らない。
  const urls = new Map();
  for (const [, name] of css.matchAll(/url\((fonts\/[a-z\d_-]+\.woff2)\)/gi)) {
    const bytes = library('katex/' + name);
    urls.set(name, 'data:font/woff2;base64,' + bytes.toString('base64'));
  }
  return css
    .replace(/url\((fonts\/[a-z\d_-]+\.woff2)\)/gi, (_, name) => `url(${urls.get(name)})`)
    .replace(/,url\(fonts\/[a-z\d_-]+\.(?:woff|ttf)\) format\("(?:woff|truetype)"\)/gi, '');
}

// ライブラリが作る結果だけを DOM に入れる。原文と失敗の知らせには textContent を使う。
function renderExtras() {
  const content = document.getElementById('gp-content');
  const root = document.documentElement;
  const failure = (out, source, label) => {
    out.className = 'gp-error';
    out.textContent = source + '\n' + label;
  };
  const math = () => {
    for (const el of content.querySelectorAll('.js-render-math')) {
      const display = el.getAttribute('data-math-style') === 'display';
      const box = display ? el.closest('.markdown-code-block') || el.closest('pre') || el : el;
      const out = document.createElement(display ? 'div' : 'span');
      out.className = display ? 'gp-math-display' : 'gp-math-inline';
      try {
        window.katex.render(el.textContent, out, {
          displayMode: display,
          throwOnError: false,
          maxSize: 20,
          maxExpand: 1000,
          trust: false,
        });
      } catch {
        failure(out, el.textContent, '数式を描画できません');
      }
      box.replaceWith(out);
    }
  };
  const codes = [...content.querySelectorAll('.js-render-mermaid')].map((el) => ({
    source: el.textContent,
    box: el.closest('.markdown-code-block') || el.closest('pre') || el,
  }));
  let sequence = 0,
    generation = 0;
  const diagrams = async () => {
    if (!codes.length) return;
    const pass = ++generation;
    const dark =
      root.dataset.theme === 'dark' ||
      (root.dataset.theme === 'system' && matchMedia('(prefers-color-scheme:dark)').matches);
    window.mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: dark ? 'dark' : 'default' });
    for (const item of codes) {
      const out = document.createElement('div');
      out.className = 'gp-mermaid';
      const id = 'gp-mermaid-' + ++sequence;
      try {
        const { svg } = await window.mermaid.render(id, item.source);
        if (pass !== generation) return;
        out.innerHTML = svg;
      } catch {
        document.getElementById('d' + id)?.remove();
        if (pass !== generation) return;
        failure(out, item.source, 'mermaid の図を描画できません');
      }
      item.box.replaceWith(out);
      item.box = out;
    }
  };
  math();
  void diagrams();
  if (root.dataset.theme === 'system')
    matchMedia('(prefers-color-scheme:dark)').addEventListener('change', () => void diagrams());
}

export async function exportDocument({
  file,
  root,
  output,
  theme = 'system',
  width = 'fluid',
  timeoutMs,
  workerFactory,
}) {
  try {
    if (!['system', 'light', 'dark'].includes(theme) || !['fluid', 'fixed'].includes(width))
      throw error('配色または本文幅が正しくありません');
    const unreadable = () => {
      throw error('指定したファイルを読めません');
    };
    const path = await realpath(absolute(file)).catch(unreadable);
    const info = await stat(path).catch(unreadable);
    if (!info.isFile()) throw error('Markdown ファイルを指定してください');
    if (info.size > maximumBytes) throw error('文書の大きさが上限の16 MBを超えています');
    const bytes = await readFile(path).catch(unreadable);
    if (bytes.length > maximumBytes) throw error('文書の大きさが上限の16 MBを超えています');
    if (bytes.includes(0) || !isUtf8(bytes)) throw error('文書は UTF-8 のテキストで指定してください');
    const context = await documentContext(path, root);
    const result = await renderForExport(bytes.toString('utf8').replace(/^\uFEFF/, ''), context, {
      timeoutMs,
      workerFactory,
    });
    const tree = fromHtml(result.html, { fragment: true });
    const outputDirectory = output
      ? await realpath(dirname(absolute(output))).catch(() => {
          throw error('書き出し先のフォルダーがありません');
        })
      : dirname(path);
    rewriteExportUrls(tree, context, outputDirectory);
    let math = false,
      mermaid = false;
    visit(tree, 'element', (node) => {
      const classes = node.properties.className || [];
      if (classes.includes('js-render-math')) math = true;
      if (classes.includes('js-render-mermaid')) mermaid = true;
      if (classes.includes('highlight')) classes.push('code-syntax-highlight-theme');
    });
    let css = await styles(theme);
    const scripts = [];
    if (math) {
      css += '\n' + (await katexStyles());
      scripts.push(library('katex/katex.min.js').toString('utf8'));
    }
    if (mermaid) scripts.push(library('mermaid/mermaid.min.js').toString('utf8'));
    if (math || mermaid) scripts.push('(' + renderExtras.toString() + ')();');
    // HTML の raw-text の終端だけを避け、実際に埋めたバイト列から CSP のハッシュを作る。
    const inline = scripts.map((script) => script.replace(/<\/script/gi, '<\\/script'));
    const hashes = inline.map((script) => "'sha256-" + createHash('sha256').update(script).digest('base64') + "'");
    const csp =
      "default-src 'none'; script-src " +
      (hashes.join(' ') || "'none'") +
      "; style-src 'unsafe-inline'; font-src data:; img-src * data: blob:; media-src * data:; base-uri 'none'; form-action 'none'";
    const html =
      '<!doctype html>\n<html lang="ja" data-theme="' +
      theme +
      '" data-width="' +
      width +
      '"' +
      (theme === 'dark' ? ' class="gl-dark"' : '') +
      '>\n<head>\n<meta charset="utf-8">\n<meta http-equiv="Content-Security-Policy" content="' +
      escape(csp) +
      '">\n<meta name="referrer" content="no-referrer">\n<meta name="viewport" content="width=device-width,initial-scale=1">\n<title>' +
      escape(basename(path)) +
      '</title>\n<style>' +
      css.replace(/<\/style/gi, '<\\/style') +
      '</style>\n</head>\n<body><div id="gp-page" class="file-holder"><main id="gp-content" class="file-content js-markup-content md">' +
      toHtml(tree) +
      '</main></div>\n' +
      inline.map((script) => '<script>' + script + '</script>\n').join('') +
      '</body>\n</html>\n';
    if (output) {
      const target = absolute(output),
        temporary = join(dirname(target), '.' + basename(target) + '.' + randomUUID() + '.tmp');
      try {
        const handle = await open(temporary, 'wx');
        try {
          await handle.writeFile(html, 'utf8');
          await handle.sync();
        } finally {
          await handle.close();
        }
        await rename(temporary, target);
      } catch {
        throw error('HTML を書き込めません');
      } finally {
        await rm(temporary, { force: true });
      }
    }
    const notices = new Map();
    for (const notice of result.notices || []) notices.set(notice.kind, (notices.get(notice.kind) || 0) + 1);
    return { html, notices };
  } catch (cause) {
    if (cause instanceof ExportError) throw cause;
    throw error('HTML への書き出しに失敗しました');
  }
}
