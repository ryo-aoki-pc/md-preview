import { elements, addClass } from '../tree.mjs';
export default function mermaid(tree, state) {
  // 手元では AssetProxy を使わないため、プロキシ URL の生成は行わない。
  for (const { node } of elements(tree, (n) => n.tagName === 'pre' && n.properties.dataCanonicalLang === 'mermaid'))
    for (const code of node.children.filter((n) => n.tagName === 'code')) addClass(code, 'js-render-mermaid');
  return tree;
}
