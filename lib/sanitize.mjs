// html-pipeline 2.14.3 の ALLOWLIST → BaseSanitizationFilter → SanitizationFilter の順で組み立てる。
// defaultSchema を流用すると name・id・class・プロトコルの条件が GitLab と変わる。
import { sanitize } from 'hast-util-sanitize';
import { elements, fragment, classes, replace } from './tree.mjs';
const words = (source) => source.trim().split(/\s+/);
// HTML の属性名から hast の属性名へ変換する。固定の許可リストだけを解析する。
const props = (source) =>
  words(source).map((name) => Object.keys(fragment('<i ' + name + '=""></i>').children[0].properties)[0]);
const baseAttributes = {
  a: props('href'),
  img: props('src longdesc'),
  div: props('itemscope itemtype'),
  blockquote: props('cite'),
  del: props('cite'),
  ins: props('cite'),
  q: props('cite'),
  '*': props(
    'abbr accept accept-charset accesskey action align alt aria-describedby aria-hidden aria-label aria-labelledby axis border cellpadding cellspacing char charoff charset checked clear cols colspan color compact coords datetime dir disabled enctype for frame headers height hreflang hspace ismap label lang maxlength media method multiple name nohref noshade nowrap open progress prompt readonly rel rev role rows rowspan rules scope selected shape size span start summary tabindex target title type usemap valign value vspace width itemprop',
  ),
};
const allowlist = {
  tagNames: words(
    'h1 h2 h3 h4 h5 h6 h7 h8 br b i strong em a pre code img tt div ins del sup sub p ol ul table thead tbody tfoot blockquote dl dt dd kbd q samp var hr ruby rt rp li tr td th s strike summary details caption figure figcaption abbr bdo cite dfn mark small span time wbr',
  ),
  strip: ['script'],
  attributes: baseAttributes,
  protocols: {
    href: words('http https mailto xmpp github-windows github-mac irc ircs'),
    cite: ['http', 'https'],
    src: ['http', 'https'],
    longDesc: ['http', 'https'],
  },
  ancestors: {
    li: ['ul', 'ol'],
    thead: ['table'],
    tbody: ['table'],
    tfoot: ['table'],
    tr: ['table'],
    td: ['table'],
    th: ['table'],
  },
  allowComments: false,
  allowDoctypes: false,
  clobber: [],
  clobberPrefix: '',
  required: {},
};
const attributes = structuredClone(allowlist.attributes);
attributes.span = props('data-math-style data-placeholder');
attributes.code = props('data-math-style');
attributes.pre = props('data-canonical-lang data-lang-params data-math-style data-mermaid-style data-kroki-style');
attributes.abbr = props('title');
attributes['*'] = attributes['*'].filter((name) => name !== 'name');
attributes.a.push(...props('name data-wikilink data-placeholder'));
attributes.img.push(...props('data-diagram data-diagram-src data-placeholder'));
const protocols = { ...allowlist.protocols };
delete protocols.href;
attributes.th = props('style');
attributes.td = props('style');
attributes.table = props('data-table-fields data-table-filter data-table-markdown');
attributes['*'].push(...props('data-sourcepos data-escaped-char'));
attributes.a.push('id');
for (const name of words('li h1 h2 h3 h4 h5 h6')) attributes[name] = ['id'];
attributes.a.push('className');
for (const name of ['div', 'p', 'ul', 'ol', 'input']) attributes[name] = ['className'];
for (const name of ['span', 'code', 'li', 'th', 'td', 'img']) attributes[name].push('className');
attributes.section = props('data-footnotes');
attributes.a.push(...props('data-footnote-ref data-footnote-backref data-footnote-backref-idx data-heading-content'));
attributes.input.push(...props('data-inapplicable data-indeterminate data-checkbox-sourcepos'));
export const gitlabSchema = {
  ...allowlist,
  tagNames: [...allowlist.tagNames, 'section', 'input'],
  attributes,
  protocols,
};
const exactClasses = {
  a: ['anchor'],
  div: ['note', 'tip', 'important', 'warning', 'caution'].map((s) => 'markdown-alert markdown-alert-' + s),
  p: ['markdown-alert-title'],
  code: ['idiff'],
  ul: ['task-list'],
  ol: ['task-list'],
  li: ['task-list-item', 'inapplicable task-list-item'],
  th: ['task-table-item'],
  td: ['task-table-item'],
  input: ['task-list-item-checkbox'],
};
function transform(tree) {
  for (const { node, parent, ancestors } of elements(tree)) {
    const p = node.properties;
    for (const key of Object.keys(p)) if (key.includes(':') || /^(xLink|xml|xmlns)/.test(key)) delete p[key];
    if (node.tagName === 'a') {
      if (String(p.rel) !== 'license') delete p.rel;
      if (ancestors.some((n) => n.tagName === 'a')) {
        replace(parent, node, node.children);
        continue;
      }
    }
    // Sanitize のプロトコル検査は大文字小文字・周囲の空白を区別しない。
    for (const key of ['cite', 'src', 'longDesc'])
      if (typeof p[key] === 'string') {
        const m = p[key].trim().match(/^([a-z][a-z\d+.-]*):/i);
        if (m && ['http', 'https'].includes(m[1].toLowerCase()))
          p[key] = p[key].replace(m[1] + ':', m[1].toLowerCase() + ':').trim();
      }
    if (['th', 'td'].includes(node.tagName) && p.style !== undefined) {
      const match = String(p.style).match(/text-align: (center|left|right)/);
      if (match) p.style = 'text-align: ' + match[1];
      else delete p.style;
    }
    if (p.dataTableFields !== undefined) {
      let fields;
      try {
        fields = JSON.parse(p.dataTableFields);
      } catch {
        /* 不正な JSON は属性を落とす。 */
      }
      if (Array.isArray(fields) && fields.every((f) => f !== null && typeof f === 'object' && !Array.isArray(f))) {
        p.dataTableFields = JSON.stringify(
          fields.map((f) =>
            Object.fromEntries(Object.entries(f).filter(([k]) => ['key', 'label', 'sortable'].includes(k))),
          ),
        );
      } else delete p.dataTableFields;
    }
    if (p.id !== undefined) {
      const prefix =
        node.tagName === 'a'
          ? 'fnref-'
          : node.tagName === 'li'
            ? 'fn-'
            : /^h[1-6]$/.test(node.tagName)
              ? 'user-content-'
              : null;
      if (prefix && !String(p.id).startsWith(prefix)) delete p.id;
    }
    if (p.className !== undefined) {
      const list = classes(node),
        value = list.join(' ');
      const allowed =
        node.tagName === 'span'
          ? value.includes('idiff') && list.every((s) => ['idiff', 'left', 'right', 'deletion', 'addition'].includes(s))
          : node.tagName === 'img'
            ? list.every((s) => ['glfm-float-right', 'glfm-float-left', 'glfm-float-none'].includes(s))
            : Object.hasOwn(exactClasses, node.tagName) && exactClasses[node.tagName].includes(value);
      if (!allowed) delete p.className;
    }
    if (
      node.tagName === 'input' &&
      !(
        p.type === 'checkbox' &&
        classes(node).join(' ') === 'task-list-item-checkbox' &&
        ['li', 'td', 'th'].includes(parent.tagName)
      )
    )
      replace(parent, node, []);
  }
}
export function sanitizeDocument(tree) {
  transform(tree);
  const safe = sanitize(tree, gitlabSchema);
  // 許可されない外側の要素がほどかれた場合にも入れ子のリンクを残さない。
  transform(safe);
  return safe;
}

// SanitizeNodeLink.safe_protocol? と同じく、スキームから数字・空白・制御文字等を除いて照合する。
export function safeProtocol(scheme) {
  return (
    typeof scheme === 'string' &&
    !['data', 'javascript', 'vbscript'].includes(
      scheme
        .trim()
        .toLowerCase()
        .replace(/[^a-z+.-]+/g, ''),
    )
  );
}
export function permitUrl(value) {
  const source = String(value).trim();
  if (/[\uD800-\uDFFF]/u.test(source)) return false;
  const scheme = source.match(/^([^/?#]*):/);
  if (scheme && !safeProtocol(scheme[1])) return false;
  // Addressable の代わりに Node の URL で構文を検証する。相対 URL の文字列は書き換えない。
  // WHATWG と Addressable の構文検査には差があるため、標準 URL で不正と判定された値は安全側で拒否する。
  try {
    new URL(source, 'https://markdown.invalid/');
  } catch {
    return false;
  }
  if (scheme && !/^[a-z][a-z\d+.-]*$/i.test(scheme[1])) return false;
  return true;
}
export function sanitizeLinks(tree) {
  const nodes = elements(tree, (n) =>
    ['href', 'src', 'dataSrc', 'dataCanonicalSrc'].some((key) => n.properties[key] !== undefined),
  );
  if (nodes.length > 10000)
    return fragment(
      '<p>Timeout while sanitizing links - rendering aborted. Please reduce the number of links if possible.</p>\n',
    );
  for (const { node } of nodes)
    for (const key of ['href', 'src', 'dataSrc', 'dataCanonicalSrc']) {
      if (node.properties[key] === undefined) continue;
      const value = String(node.properties[key]).trim();
      if (permitUrl(value)) node.properties[key] = value;
      else delete node.properties[key];
    }
  return tree;
}
