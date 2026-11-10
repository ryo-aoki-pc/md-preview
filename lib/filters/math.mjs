import { elements } from '../tree.mjs';
export default function math(tree, state) {
  const nodes = elements(
    tree,
    (n) =>
      ['pre', 'code', 'span'].includes(n.tagName) &&
      (n.properties.dataMathStyle !== undefined || (n.tagName === 'pre' && n.properties.dataCanonicalLang === 'math')),
  );
  for (const { node } of nodes.slice(0, 1000)) {
    if (
      node.tagName === 'pre' &&
      node.properties.dataCanonicalLang === 'math' &&
      node.properties.dataMathStyle === undefined
    )
      node.properties.dataMathStyle = 'display';
    node.properties.className = ['js-render-math'];
  }
  return tree;
}
