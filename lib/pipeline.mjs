// GitLab 19.5.1 の GfmPipeline の順で、消毒の境界を挟んで処理する。
// 省略するフィルターと理由:
// IframeToImg・Kroki・IframeLink: 外部の図サービスや埋め込みの設定を持たない。
// Plantuml は、手元で描ける context.plantuml がある場合だけ処理する。
// GollumTags・WikiLinkGollum: Wiki のリポジトリとパスの解決を行わない（目次だけは処理する）。
// AssetProxy: 手元のプレビューにはプロキシがない。
// HeadingLocalization: 見出しはエンジンが作った ID をそのまま使う。
// Suggestion: Merge Request の差分と権限の文脈を持たない。
// 参照の 16 個（UserReference・ProjectReference・DesignReference・IssueReference・WorkItemReference・
// ExternalIssueReference・MergeRequestReference・SnippetReference・PersonalSnippetReference・
// CommitRangeReference・LabelReference・MilestoneReference・AlertReference・FeatureFlagReference・
// CommitReference・WikiPageReference）: GitLab のデータベースと権限の解決を行わない。
// ImageLazyLoad: 手元では画像をそのまま読み込む。
// Emoji: gl-emoji を作る代わりに、エンジンの gemojis: true で Unicode の文字にする。
// CustomEmoji: GitLab の独自絵文字の設定を持たない。
// PostProcessPipeline の全フィルター: GitLab の権限・添付・リポジトリ・通知の文脈を持たない。
// Include: Markdown の前に処理する。読み込みは context.readInclude に任せ、URL は取り込まない。
import { renderMarkdown, GITLAB_OPTIONS } from './engine.mjs';
import { toHtml } from 'hast-util-to-html';
import { fragment, elements, content, hasClass } from './tree.mjs';
import { sanitizeDocument, sanitizeLinks } from './sanitize.mjs';
import codeLanguage from './filters/code-language.mjs';
import jsonTable from './filters/json-table.mjs';
import plantuml from './filters/plantuml.mjs';
import spacedLink from './filters/spaced-link.mjs';
import attributes from './filters/attributes.mjs';
import math from './filters/math.mjs';
import color from './filters/color.mjs';
import mermaid from './filters/mermaid.mjs';
import videoLink from './filters/video-link.mjs';
import audioLink from './filters/audio-link.mjs';
import tableOfContentsTag from './filters/table-of-contents-tag.mjs';
import footnote from './filters/footnote.mjs';
import inlineDiff from './filters/inline-diff.mjs';
import imageLink from './filters/image-link.mjs';
import externalLink from './filters/external-link.mjs';
import taskList from './filters/task-list.mjs';
import setDirection from './filters/set-direction.mjs';
import syntaxHighlight from './filters/syntax-highlight.mjs';
import { expandIncludes } from './include.mjs';
import { collectNotices } from './notices.mjs';

// FrontMatter::PATTERN_UNTRUSTED は、開始と終了の区切りが違う場合も一致する。
// Ruby の \s は ASCII の空白、行頭 ^ は LF の直後なので、JS の \s と m は使わず明示する。
const lineBreak = '(?:\\r\\n|[\\n\\v\\f\\r\\u0085\\u2028\\u2029])';
const lineStart = '(?:(?<=\\n)|(?<![\\s\\S]))';
const rubySpace = '[\\t\\n\\v\\f\\r ]';
const frontMatterPattern = new RegExp(
  '^(?<encoding>[^\\r\\n]*coding:[^\\r\\n]*' +
    lineBreak +
    ')?(?<before>' +
    rubySpace +
    '*)' +
    lineStart +
    '(?<delim>---|\\+\\+\\+|;;;)[ \\t]*(?<lang>[^\\t\\n\\v\\f\\r ]*)' +
    lineBreak +
    '(?<front_matter>[\\s\\S]*?)' +
    lineStart +
    '(?:---|\\+\\+\\+|;;;|\\.\\.\\.)[\\t\\v\\f ]*(?:' +
    lineBreak +
    '|$(?![\\s\\S]))',
);
export function frontMatter(text) {
  const match = frontMatterPattern.exec(text);
  if (!match || match.index !== 0) return text;
  const g = match.groups,
    language = g.lang || { '---': 'yaml', '+++': 'toml', ';;;': 'json' }[g.delim];
  const fence = String.fromCharCode(96).repeat(3);
  return (
    (g.encoding ? '\n' : '') +
    g.before +
    fence +
    language +
    ':frontmatter\n' +
    g.front_matter +
    fence +
    '\n' +
    text.slice(match[0].length)
  );
}
// エンジンに渡すオプション。GitLab の OPTIONS (GITLAB_OPTIONS) から、次の 2 つだけを変える。
// - gemojis: GitLab は EmojiFilter で <gl-emoji> にする。手元では、エンジンに :smile: を Unicode の文字にさせる
// - placeholder_detection: GitLab は、プロジェクトかグループで機能フラグ (markdown_placeholders) が有効なときだけ
//   検出する (glfm_markdown.rb の placeholders_disabled?)。手元にはその設定が無いので、無効の側に合わせる
const ENGINE_OPTIONS = Object.freeze({ ...GITLAB_OPTIONS, gemojis: true, placeholder_detection: false });
// 利用者に見せてよい、文書の側の誤り (入れ子が深すぎる、など)。message は日本語の 1 行。
export class DocumentError extends Error {}

/**
 * Markdown を、消毒とフィルターを通した最終の HTML にする。同期の関数で、ファイルにもネットワークにも触らない
 * (読み込みは context の同期の関数に任せる)。
 * @param {string} text
 * @param {object} [context]
 *   file: 文書のパス (ルートからの相対。include の基準)。dir: 文書のフォルダー (ルートからの相対、末尾に /。画像などの基準)。
 *   readInclude(path): ::include のファイルを読む。本文の文字列、無ければ null、
 *     読めなければ { error: 'not readable' | 'too large' } を返す。無いときは include を展開しない。
 *   exists(path): 相対の画像などが手元にあるかを返す。無いときは調べない。
 *   plantuml: 手元で図を描けるときだけ渡すオブジェクト。
 *   readInclude と exists に渡す path は、ルートからの相対の、文字どおりの名前 (区切りは /)。ここでは .. と
 *   絶対パスだけを落とす。パーセントエンコードは解かない (%2e%2e は、その名前のフォルダーを指す)。呼ぶ側は
 *   path を文字どおりに扱い、ルートの内側の実ファイルかどうか (リンクでの脱出、Windows の : や末尾の点など) を
 *   自分で確かめること。
 *   notices の text は原文の抜き出しで、消毒していない。文字として表示する (HTML として入れない)。
 * @returns {{ html: string, headings: Array<{ level: number, id: string, text: string }>,
 *   includes: Array<{ line: number, path: string, lines: number, ok: boolean }>,
 *   diagrams: Array<{ id: string, source: string }>,
 *   notices: Array<{ kind: string, line: number | null, text: string }> }}
 */
export function renderDocument(text, context = {}) {
  if (typeof text !== 'string') throw new TypeError('Markdown は文字列で渡してください。');
  try {
    return render(text, context);
  } catch (error) {
    // 木をたどる処理 (ここのフィルターも、使っている部品も) は再帰なので、数千段の入れ子ではスタックがあふれる。
    // GitLab も深い入れ子は描かない。途中まで作った HTML は返さず、誤りとして知らせる。
    if (error instanceof RangeError)
      throw new DocumentError('入れ子が深すぎるため、描画できません。', { cause: error });
    throw error;
  }
}

function render(text, context) {
  const state = { plantuml: context.plantuml };
  const original = text.replaceAll('\r', '');
  const expansion = expandIncludes(frontMatter(original), context);
  // 取り込んだ本文の CR も、GitLab の MarkdownFilter と同じく消す。
  let tree = fragment(renderMarkdown(expansion.text.replaceAll('\r', ''), ENGINE_OPTIONS).trimEnd());
  for (const filter of [codeLanguage, jsonTable, plantuml, spacedLink]) tree = filter(tree, state);
  tree = sanitizeLinks(sanitizeDocument(tree));
  for (const filter of [
    attributes,
    math,
    color,
    mermaid,
    videoLink,
    audioLink,
    tableOfContentsTag,
    footnote,
    inlineDiff,
    imageLink,
    externalLink,
    taskList,
    setDirection,
    syntaxHighlight,
  ])
    tree = filter(tree, state);
  const headings = elements(tree, (n) => /^h[1-6]$/.test(n.tagName)).map(({ node }) => ({
    level: Number(node.tagName[1]),
    id: String(node.properties.id || '').replace(/^user-content-/, ''),
    text: content({
      type: 'root',
      children: node.children.filter((n) => !(n.tagName === 'a' && hasClass(n, 'anchor'))),
    }),
  }));
  return {
    html: toHtml(tree),
    headings,
    includes: expansion.includes,
    diagrams: [...(state.diagrams || [])].map(([id, source]) => ({ id, source })),
    notices: collectNotices(tree, original, expansion, context),
  };
}
