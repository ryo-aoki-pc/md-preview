import { renderMarkdown } from '../engine.mjs';
import { elements, element, text, fragment, content, onlyCode, replace } from '../tree.mjs';
const hash = (x) => x !== null && typeof x === 'object' && !Array.isArray(x);
// Ruby の真偽では空文字と 0 も真になる。
const truthy = (x) => x !== undefined && x !== null && x !== false;
const inspect = (x) =>
  x === undefined || x === null
    ? 'nil'
    : typeof x === 'string'
      ? JSON.stringify(x)
      : Array.isArray(x)
        ? '[' + x.map(inspect).join(', ') + ']'
        : hash(x)
          ? '{' +
            Object.entries(x)
              .map(([k, v]) => inspect(k) + '=>' + inspect(v))
              .join(', ') +
            '}'
          : String(x);
const string = (x) => (x === undefined || x === null ? '' : typeof x === 'object' ? inspect(x) : String(x));
const present = (x) =>
  truthy(x) &&
  (typeof x === 'string'
    ? Boolean(x.trim())
    : Array.isArray(x)
      ? x.length > 0
      : hash(x)
        ? Object.keys(x).length > 0
        : true);
const markdown = (value) =>
  fragment(renderMarkdown(string(value).replaceAll('\r', ''), { sourcepos: false, gemojis: true }).trimEnd()).children;
export default function jsonTable(tree, state) {
  for (const { node: pre, parent } of elements(
    tree,
    (n) =>
      n.properties.dataCanonicalLang === 'json' &&
      String(n.properties.dataLangParams || '')
        .split(/\s+/)
        .includes('table'),
  )) {
    const code = onlyCode(pre);
    if (!code) continue;
    let data;
    try {
      data = JSON.parse(content(code));
    } catch {
      continue;
    }
    if (
      !hash(data) ||
      !truthy(data.markdown) ||
      !Array.isArray(data.items) ||
      !data.items.length ||
      !data.items.every(hash)
    )
      continue;
    if (truthy(data.fields) && (!Array.isArray(data.fields) || !data.fields.every(hash))) continue;
    const fields = truthy(data.fields) ? data.fields : Object.keys(data.items[0]).map((key) => ({ key }));
    const table = element('table', { dataTableFields: JSON.stringify(fields), dataTableMarkdown: 'true' });
    if (truthy(data.filter)) table.properties.dataTableFilter = 'true';
    if (present(data.caption)) table.children.push(element('caption', {}, markdown(data.caption)));
    table.children.push(
      element('thead', {}, [
        element(
          'tr',
          {},
          fields.map((f) => element('th', {}, [text(string(truthy(f.label) ? f.label : f.key))])),
        ),
      ]),
    );
    table.children.push(
      element(
        'tbody',
        {},
        data.items.map((item) =>
          element(
            'tr',
            {},
            fields.map((f) => {
              let children = markdown(Object.hasOwn(item, f.key) ? item[f.key] : undefined);
              if (children.length === 1 && children[0].tagName === 'p') children = children[0].children;
              return element('td', {}, children);
            }),
          ),
        ),
      ),
    );
    replace(parent, pre, [element('div', {}, [table])]);
  }
  return tree;
}
