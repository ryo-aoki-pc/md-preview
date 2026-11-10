export default function setDirection(tree, state) {
  for (const node of tree.children) if (node.type === 'element' && ['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ol', 'ul', 'blockquote', 'table'].includes(node.tagName) && !(node.tagName === 'ul' && node.properties.className?.join(' ') === 'section-nav')) node.properties.dir = 'auto';
  return tree;
}
