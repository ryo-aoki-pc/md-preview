import { elements, element, text, content, hasClass, replace } from '../tree.mjs';
function buildToc(tree) {
  const root = { level: 0, children: [] }; let current = root;
  for (const { node } of elements(tree, n => /^h[1-6]$/.test(n.tagName))) {
    const anchor = elements(node, n => n.tagName === 'a' && hasClass(n, 'anchor'))[0]?.node;
    if (!anchor) continue;
    const level = Number(node.tagName[1]);
    while (current.level >= level) current = current.parent;
    const next = { level, parent: current, children: [], href: String(anchor.properties.href || '').slice(1), text: content(node).trim() };
    current.children.push(next); current = next;
  }
  function list(children, top = false) {
    return element('ul', top ? { className: ['section-nav'] } : {}, children.map(child => element('li', {}, [element('a', { href: '#' + child.href }, [text(child.text)]), ...(child.children.length ? [list(child.children)] : [])])));
  }
  return root.children.length ? list(root.children, true) : null;
}
export default function tableOfContentsTag(tree, state) {
  // キャッシュは文書内だけで共有し、複数の目次には別々の木を挿入する。
  const toc = buildToc(tree);
  for (const { node, parent } of elements(tree, n => n.tagName === 'p')) {
    const standard = node.children.length === 1 && node.children[0].type === 'text' && /^\[toc\]/i.test(node.children[0].value) && node.children[0].value.trim().toLowerCase() === '[toc]';
    const gollum = content(node).toLowerCase() === '_toc_' && node.children.some(n => n.tagName === 'a' && n.properties.dataWikilink === 'true' && n.children.some(c => c.type === 'text' && c.value.toLowerCase().startsWith('_toc_')));
    if (standard || gollum) replace(parent, node, toc ? [structuredClone(toc)] : []);
  }
  return tree;
}
