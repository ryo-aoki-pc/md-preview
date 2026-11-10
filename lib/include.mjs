// GitLab IncludeFilter と Gsub.gsub_with_limit の文字列処理。ファイルとネットワークは扱わない。
import { includeBase, resolvePath } from './paths.mjs';
// GitLab 19.5.1 の application_setting_implementation.rb の asciidoc_max_includes の既定値。
export const MAX_INCLUDES = 32;
export const MAX_EXPANDED_BYTES = 16 * 1024 * 1024;
const encoder = new TextEncoder();
const bytes = (value) => encoder.encode(value).length;
const newlines = (value) => value.split('\n').length - 1;
const lineCount = (value) => (value ? newlines(value) + (value.endsWith('\n') ? 0 : 1) : 0);
const pattern =
  /(?:(?<=\n)|(?<![\s\S]))::include\{file=(?<include>[^\n]{1,1024})\}(?=\r\n|[\n\v\f\r\u0085\u2028\u2029]|$(?![\s\S]))/gu;
// Comrak 0.56.0 の cm::escape_inline と cm::escape_link_destination の写し。
// 取り込んだ本文そのものにはエスケープを掛けず、後ろの Markdown と消毒を必ず通す。
const inlineEscapes = new Set('*_[]#<>\\~!&-+=.()"' + String.fromCharCode(96));
export function escapeInline(value) {
  return Array.from(value, (character) => {
    const code = character.codePointAt(0);
    if (code < 0x20) return '&#' + code + ';';
    return inlineEscapes.has(character) ? '\\' + character : character;
  }).join('');
}
export function escapeLinkDestination(value) {
  return (
    '<' +
    Array.from(value, (character) => {
      if (character === '<' || character === '>') return '\\' + character;
      if (character === '\n') return '%0A';
      if (character === '\r') return '%0D';
      return character;
    }).join('') +
    '>'
  );
}
const markdownLink = (value) => '[' + escapeInline(value) + '](' + escapeLinkDestination(value) + ')';
const errorMessage = (filename, reason) => "**Error including '" + markdownLink(filename) + "': " + reason + '**';
function isHttp(target) {
  if (!/^https?:\/\//i.test(target)) return false;
  // URI.parse の無効な文字・不正な % を先に検査する。ネットワークは呼ばない。
  if (/[^\x21-\x7e]|%(?![a-f\d]{2})/i.test(target)) return false;
  try {
    return ['http:', 'https:'].includes(new URL(target).protocol);
  } catch {
    return false;
  }
}
export function expandIncludes(source, context = {}) {
  const includes = [],
    spans = [],
    parts = [],
    cache = new Map();
  const reader = typeof context.readInclude === 'function' ? context.readInclude : null;
  const base = includeBase(context.file);
  let cursor = 0,
    sourceLine = 1,
    size = bytes(source),
    count = 0;
  for (const match of source.matchAll(pattern)) {
    sourceLine += newlines(source.slice(cursor, match.index));
    const filename = match.groups.include,
      http = isHttp(filename);
    const resolved = http ? { path: filename, safe: false } : resolvePath(filename, base);
    const item = { line: sourceLine, path: resolved.path, lines: 0, ok: false };
    includes.push(item);
    let replacement = match[0];
    if (reader && count < MAX_INCLUDES) {
      count++;
      if (http) {
        // wiki_asciidoc_allow_uri_includes の既定 false と同じく、URL はリンクにするだけ。
        replacement = markdownLink(filename);
      } else {
        let result = null;
        if (resolved.safe) {
          // Ruby と同じく、生の指定名をキーに文書内だけで読み込み結果を共有する。
          if (!cache.has(filename)) {
            try {
              cache.set(filename, reader.call(context, resolved.path));
            } catch {
              cache.set(filename, { error: 'not readable' });
            }
          }
          result = cache.get(filename);
        }
        if (typeof result === 'string') {
          // TruncateSourceFilter の代わりに、展開後の本文の UTF-8 の大きさを 16 MiB に制限する。
          // これは手元だけの差で、超える取り込みは GitLab の too large の文にする。
          if (size - bytes(match[0]) + bytes(result) > MAX_EXPANDED_BYTES)
            replacement = errorMessage(filename, 'too large');
          else {
            replacement = result;
            item.ok = true;
            item.lines = lineCount(result);
          }
        } else {
          const reason =
            result === null || result === undefined
              ? 'not found'
              : result.error === 'too large'
                ? 'too large'
                : 'not readable';
          replacement = errorMessage(filename, reason);
        }
      }
    }
    // reader がない API 相当の場合と、32 回より後の一致は、文字を一切変えない。
    // 空の本文は空文字にする。置換結果にある include は再検索しない。
    parts.push(source.slice(cursor, match.index), replacement);
    size += bytes(replacement) - bytes(match[0]);
    const addedLines = newlines(replacement);
    if (item.ok || addedLines) spans.push({ item, newlines: addedLines });
    cursor = match.index + match[0].length;
  }
  parts.push(source.slice(cursor));
  return {
    text: parts.join(''),
    includes,
    // 公開する lines は本文の行数。末尾改行がない本文でも正しく戻せるよう、実際に
    // 増えた LF の数を各 include と一緒に保持して、取り込んだ行は指示行へ戻す。
    originalLine(expandedLine) {
      if (!Number.isSafeInteger(expandedLine) || expandedLine < 1) return null;
      let shift = 0;
      for (const span of spans) {
        const start = span.item.line + shift;
        if (expandedLine < start) break;
        if (expandedLine <= start + span.newlines) return span.item.line;
        shift += span.newlines;
      }
      return expandedLine - shift;
    },
    isIncludedLine(expandedLine) {
      let shift = 0;
      for (const span of spans) {
        const start = span.item.line + shift;
        if (expandedLine < start) return false;
        if (expandedLine <= start + span.newlines) return span.item.ok;
        shift += span.newlines;
      }
      return false;
    },
  };
}
