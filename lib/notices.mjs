// 木は読むだけにし、GitLab との表示の差を原文の短い抜き出しとして返す。
import { DIAGRAM_LANGUAGES } from './highlight.mjs';
import { content } from './tree.mjs';
import { resolvePath } from './paths.mjs';
const diagrams = new Set(DIAGRAM_LANGUAGES.filter((name) => name !== 'mermaid'));
const blocks = new Set([
  'p',
  'div',
  'section',
  'blockquote',
  'li',
  'td',
  'th',
  'tr',
  'table',
  'pre',
  'ul',
  'ol',
  'dl',
  'dt',
  'dd',
  'caption',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
]);
const excerpt = (value) => Array.from(String(value)).slice(0, 80).join('');
const countNewlines = (value) => value.split('\n').length - 1;
const slug = '[A-Za-z0-9_](?:[A-Za-z0-9_.-]*[A-Za-z0-9_])?';
const project = '(?:' + slug + '/)+' + slug;
const named = '(?:"[^"\\n]+"|[\\p{L}_](?:[\\p{L}\\p{N}_.-]*[\\p{L}\\p{N}_])?)';
// 参照の通知はサーバーの参照解決の代わりではない。保守しやすい控えめな候補だけを拾う。
// - 語やメールの途中を避け、課題・MR・エピックの正の番号、既知の角括弧記法を拾う。
// - 裸の $ は金額との区別が曖昧なので 3 桁以上だけ。小数・桁区切り・通貨名が続くものは除く。
// - 利用者・プロジェクトの名は ASCII の slug、ラベル・マイルストーンは文字で始まる名か引用名。
// - SHA、数字だけの名、壊れた引用、語に埋まった記号は拾わない。コード・リンク・数式・
//   data-escaped-char の中は、木をたどる時点で除く。
const referencePattern = new RegExp(
  '(?:' +
    project +
    '(?:[#!&$][1-9]\\d*|[~%]' +
    named +
    ')|[#!&][1-9]\\d*|\\$[1-9]\\d{2,}|@' +
    slug +
    '(?:/' +
    slug +
    ')*|[~%]' +
    named +
    '|\\[(?:issue|merge_request|epic|snippet):[1-9]\\d*\\])',
  'gu',
);
const beforeWord = /[\p{L}\p{N}_./%+@-]/u;
const afterWord = /[\p{L}\p{N}_/@]/u;
const emojiPattern = /:[a-z_][a-z0-9_+-]*:/gi;
const schemes = /^[a-z][a-z0-9+.-]*:/i;
const characterAt = (value, index) => (index < value.length ? String.fromCodePoint(value.codePointAt(index)) : '');
function characterBefore(value, index) {
  const last = value.charCodeAt(index - 1);
  return value.slice(index > 1 && last >= 0xdc00 && last <= 0xdfff ? index - 2 : index - 1, index);
}

export function collectNotices(tree, original, expansion, context = {}) {
  const sourceLines = original.split('\n'),
    seen = new Set(),
    kept = [],
    existence = new Map();
  const add = (kind, line, value) => {
    const notice = { kind, line, text: excerpt(value) };
    const key = JSON.stringify([kind, line, notice.text]);
    if (seen.has(key)) return;
    seen.add(key);
    // 全候補の重いオブジェクトは持たず、原文の順で先頭200件と重複判定のキーだけを保つ。
    const rank = line ?? Infinity;
    let low = 0,
      high = kept.length;
    while (low < high) {
      const middle = (low + high) >>> 1;
      if ((kept[middle].line ?? Infinity) <= rank) low = middle + 1;
      else high = middle;
    }
    if (low < 200) {
      kept.splice(low, 0, notice);
      if (kept.length > 200) kept.pop();
    }
  };
  function addAt(kind, expandedLine, value, wholeLine = false) {
    const mapped = expansion.originalLine(expandedLine);
    const line = mapped !== null && mapped <= sourceLines.length ? mapped : null;
    const source = line === null ? null : sourceLines[line - 1];
    // 取り込み元の文は主文書に無いので、指示した原文の include 行を示す。
    const snippet =
      source !== null && (wholeLine || expansion.isIncludedLine(expandedLine) || !source.includes(value))
        ? source.trim()
        : value;
    add(kind, line, snippet);
  }
  for (const include of expansion.includes)
    if (!include.ok) add('include', include.line, sourceLines[include.line - 1]?.trim() || include.path);
  function textNotices(value, line, previous) {
    let cursor = 0,
      currentLine = line;
    for (const match of value.matchAll(referencePattern)) {
      if (currentLine !== null) currentLine += countNewlines(value.slice(cursor, match.index));
      cursor = match.index;
      const before = match.index ? characterBefore(value, match.index) : previous;
      const next = match.index + match[0].length,
        after = characterAt(value, next);
      if ((before && beforeWord.test(before)) || (after && afterWord.test(after))) continue;
      if (match[0].startsWith('$') && /^(?:[.,]\d|(?:円|USD|EUR|JPY)\b)/u.test(value.slice(next, next + 8))) continue;
      addAt('reference', currentLine, match[0]);
    }
    cursor = 0;
    currentLine = line;
    for (const match of value.matchAll(emojiPattern)) {
      if (currentLine !== null) currentLine += countNewlines(value.slice(cursor, match.index));
      cursor = match.index;
      const before = match.index ? characterBefore(value, match.index) : previous;
      const after = characterAt(value, match.index + match[0].length);
      // 12:30:45、URL のパス、単語の途中の : は候補にしない。
      if ((before && /[\p{L}\p{N}_:/]/u.test(before)) || (after && /[\p{L}\p{N}_:]/u.test(after))) continue;
      addAt('emoji', currentLine, match[0]);
    }
  }
  function missing(node, line) {
    if (typeof context.exists !== 'function') return;
    const value = node.properties.dataCanonicalSrc ?? node.properties.dataSrc ?? node.properties.src;
    if (typeof value !== 'string' || value.startsWith('#') || value.startsWith('//') || schemes.test(value)) return;
    const pathPart = value.split(/[?#]/, 1)[0];
    if (!pathPart) return;
    let decoded;
    try {
      decoded = decodeURIComponent(pathPart);
    } catch {
      addAt('missing', line, value, true);
      return;
    }
    if (decoded.startsWith('//') || schemes.test(decoded)) return;
    const resolved = resolvePath(decoded, typeof context.dir === 'string' ? context.dir : '');
    let exists = false;
    if (resolved.safe) {
      if (!existence.has(resolved.path)) {
        try {
          existence.set(resolved.path, context.exists(resolved.path) === true);
        } catch {
          existence.set(resolved.path, false);
        }
      }
      exists = existence.get(resolved.path);
    }
    if (!exists) addAt('missing', line, value, true);
  }
  function visit(node, inheritedLine = null, excluded = false, previous = '') {
    if (node.type === 'text') {
      if (!excluded) textNotices(node.value, inheritedLine, previous);
      return {
        line: inheritedLine === null ? null : inheritedLine + countNewlines(node.value),
        last: Array.from(node.value).at(-1) || previous,
      };
    }
    const position = String(node.properties?.dataSourcepos || '').match(/^(\d+):\d+-(\d+):\d+$/);
    const start = position && Number.isSafeInteger(Number(position[1])) ? Number(position[1]) : inheritedLine;
    let cursor = start,
      last = blocks.has(node.tagName) ? '' : previous;
    if (node.type === 'element') {
      if (node.tagName === 'pre') {
        const language = node.properties.dataCanonicalLang;
        if (diagrams.has(language)) addAt('diagram', start, content(node).split('\n')[0], true);
        if (language === 'suggestion') addAt('suggestion', start, content(node).split('\n')[0], true);
      }
      if (['img', 'video', 'audio'].includes(node.tagName)) missing(node, start);
    }
    const ignore =
      excluded ||
      ['pre', 'code', 'a'].includes(node.tagName) ||
      node.properties?.dataEscapedChar !== undefined ||
      node.properties?.dataMathStyle !== undefined;
    for (const child of node.children || []) {
      const result = visit(child, cursor, ignore, last);
      // 隣のブロックの sourcepos を、位置の分からない生 HTML に引き継がない。
      if (cursor !== null) cursor = result.line;
      last = result.last;
    }
    return { line: position ? Number(position[2]) : cursor, last: blocks.has(node.tagName) ? '\n' : last };
  }
  visit(tree);
  // more 自身も全体の 200 件の枠に含める。
  return seen.size <= 200 ? kept : [...kept.slice(0, 199), { kind: 'more', line: null, text: String(seen.size - 199) }];
}
