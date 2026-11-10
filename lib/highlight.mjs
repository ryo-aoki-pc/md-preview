import { createRequire } from 'node:module';
import { fragment, classes, text, element } from './tree.mjs';
const require = createRequire(import.meta.url);
let highlighter;
// Rouge は JavaScript に移植されていないため、字句分割だけ highlight.js で代える。
// 対応先は fallback.css が配色している Rouge の短いクラス名に限る。
export const SCOPE_TO_ROUGE = Object.freeze({
  keyword: 'k',
  built_in: 'nb',
  type: 'kt',
  literal: 'kc',
  number: 'm',
  operator: 'o',
  punctuation: null,
  property: 'na',
  attr: 'na',
  attribute: 'na',
  variable: 'nv',
  'variable.language': 'bp',
  'variable.constant': 'no',
  title: 'nf',
  'title.function': 'nf',
  'title.function.invoke': 'nf',
  'title.class': 'nc',
  'title.class.inherited': 'nc',
  params: null,
  function: null,
  class: null,
  string: 's',
  'string.escape': 'se',
  char: 'sc',
  regexp: 'sr',
  symbol: 'ss',
  bullet: 'ss',
  subst: 'si',
  'template-variable': 'nv',
  'template-tag': 'nt',
  comment: 'c',
  doctag: 'cs',
  quote: 'c',
  meta: 'cp',
  'meta.keyword': 'cp',
  'meta.string': 's',
  tag: 'nt',
  name: 'nt',
  selector: 'nt',
  'selector-tag': 'nt',
  'selector-id': 'na',
  'selector-class': 'nc',
  'selector-attr': 'na',
  'selector-pseudo': 'nd',
  section: 'gh',
  addition: 'gi',
  deletion: 'gd',
  emphasis: 'ge',
  strong: 'gs',
  code: 's',
  link: 'na',
});
export const DIAGRAM_LANGUAGES = Object.freeze([
  'actdiag',
  'blockdiag',
  'bpmn',
  'bytefield',
  'c4plantuml',
  'd2',
  'dbml',
  'ditaa',
  'erd',
  'excalidraw',
  'graphviz',
  'mermaid',
  'nomnoml',
  'nwdiag',
  'packetdiag',
  'pikchr',
  'plantuml',
  'rackdiag',
  'seqdiag',
  'svgbob',
  'symbolator',
  'umlet',
  'vega',
  'vegalite',
  'wavedrom',
  'structurizr',
  'diagramsnet',
  'wireviz',
]);
const plainLanguages = new Set(['math', 'suggestion', ...DIAGRAM_LANGUAGES]);
const aliases = { sh: 'bash', shell: 'bash', text: 'plaintext', txt: 'plaintext' };
function lexer(language) {
  if (plainLanguages.has(language)) return { language, lexer: null };
  highlighter ??= require('highlight.js');
  const requested = Object.hasOwn(aliases, language) ? aliases[language] : language;
  const definition = requested && highlighter.getLanguage(requested);
  if (!definition) return { language: 'plaintext', lexer: null };
  const canonical = highlighter.listLanguages().find((name) => highlighter.getLanguage(name) === definition);
  // Rouge と highlight.js で統合先の違う言語名は、GitLab の表示名に戻す。
  const tag =
    canonical === 'bash'
      ? 'shell'
      : canonical === 'xml' && ['html', 'xhtml'].includes(requested)
        ? 'html'
        : canonical === 'ini' && requested === 'toml'
          ? 'toml'
          : canonical;
  return { language: tag, lexer: canonical };
}
export function tokensFromTree(tree) {
  const tokens = [];
  function visit(node, inherited = null) {
    if (node.type === 'text') {
      if (node.value) tokens.push({ scope: inherited, value: node.value });
      return;
    }
    const names = node.tagName === 'span' ? classes(node) : [];
    const root = names.find((name) => name.startsWith('hljs-'));
    let scope = inherited;
    if (root) {
      const full = [root.slice(5), ...names.filter((n) => n !== root).map((n) => n.replace(/_+$/, ''))].join('.');
      const base = root.slice(5);
      scope = Object.hasOwn(SCOPE_TO_ROUGE, full)
        ? SCOPE_TO_ROUGE[full]
        : Object.hasOwn(SCOPE_TO_ROUGE, base)
          ? SCOPE_TO_ROUGE[base]
          : null;
    }
    for (const child of node.children || []) visit(child, scope);
  }
  visit(tree);
  return tokens;
}
// HTMLGitlab.token_lines: 最後の改行では余分な空行を増やさず、改行をまたぐ字句は行ごとに分ける。
export function tokenLines(tokens) {
  const lines = [];
  let line = [];
  for (const token of tokens) {
    const parts = token.value.split('\n');
    const last = parts.pop();
    for (const part of parts) {
      if (part) line.push({ scope: token.scope, value: part });
      lines.push(line);
      line = [];
    }
    if (last) line.push({ scope: token.scope, value: last });
  }
  if (line.length) lines.push(line);
  return lines;
}
function visibleCharacters(value) {
  const normalized = value.replace(/[\u00a0\u1680\u2000-\u200a\u202f\u205f\u3000]/g, ' ');
  const result = [];
  let last = 0;
  for (const match of normalized.matchAll(/[\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069]/g)) {
    if (match.index > last) result.push(text(normalized.slice(last, match.index)));
    result.push(
      element(
        'span',
        {
          className: ['unicode-bidi', 'has-tooltip'],
          dataToggle: 'tooltip',
          title: 'Potentially unwanted character detected: Unicode BiDi Control',
        },
        [text(match[0])],
      ),
    );
    last = match.index + 1;
  }
  if (last < normalized.length) result.push(text(normalized.slice(last)));
  return result;
}
export function highlightCode(source, requested) {
  let selected = lexer(requested),
    tokens;
  try {
    tokens = selected.lexer
      ? tokensFromTree(
          fragment(highlighter.highlight(source, { language: selected.lexer, ignoreIllegals: true }).value),
        )
      : [{ scope: null, value: source }];
  } catch {
    // GitLab と同じく、字句解析器の失敗時にも本文を失わず平文に戻す。
    selected = { language: null, lexer: null };
    tokens = [{ scope: null, value: source }];
  }
  const children = [];
  for (const [index, line] of tokenLines(tokens).entries()) {
    if (index) children.push(text('\n'));
    const parts = line.flatMap((token) => {
      const value = token.value.replace(/(?:\r\n|\r|\n)$/, '');
      if (!value) return [];
      const contents = visibleCharacters(value);
      return token.scope ? [element('span', { className: [token.scope] }, contents)] : contents;
    });
    children.push(
      element(
        'span',
        { id: 'LC' + (index + 1), className: ['line'], dataLang: selected.language || 'plaintext' },
        parts,
      ),
    );
  }
  return { language: selected.language, children };
}
