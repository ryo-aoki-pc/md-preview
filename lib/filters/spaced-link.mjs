import { renderMarkdown } from '../engine.mjs';
import { entries, fragment, replace } from '../tree.mjs';
export default function spacedLink(tree, state) {
  const ignored = new Set(['a', 'code', 'kbd', 'pre', 'script', 'style']);
  for (const { node, parent, ancestors } of entries(tree, (n) => n.type === 'text' && n.value.includes(']('))) {
    if (
      ancestors.some(
        (n) => ignored.has(n.tagName) || (n.tagName === 'span' && n.properties.dataMathStyle !== undefined),
      )
    )
      continue;
    const pattern = /(!)?\[([^\n]+?)\]\(([^\n]+?)( ".+?")?\)/g;
    const children = [];
    let last = 0;
    for (const match of node.value.matchAll(pattern)) {
      if (match.index > last) children.push({ type: 'text', value: node.value.slice(last, match.index) });
      const source =
        (match[1] || '') + '[' + match[2] + '](' + match[3].replaceAll(' ', '%20') + (match[4] || '') + ')';
      const rendered = fragment(renderMarkdown(source, { gemojis: true }).trimEnd());
      const paragraph = rendered.children.find((n) => n.tagName === 'p');
      children.push(...(paragraph ? paragraph.children : rendered.children));
      last = match.index + match[0].length;
    }
    if (!last) continue;
    if (last < node.value.length) children.push({ type: 'text', value: node.value.slice(last) });
    replace(parent, node, children);
  }
  return tree;
}
