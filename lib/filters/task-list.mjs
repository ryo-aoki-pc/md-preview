import { elements, content, hasClass, element, text } from '../tree.mjs';
export default function taskList(tree, state) {
  for (const { node: input, parent, ancestors } of elements(tree, n => n.tagName === 'input' && hasClass(n, 'task-list-item-checkbox'))) {
    const index = parent.children.indexOf(input);
    let label;
    if (hasClass(parent, 'task-table-item')) {
      const row = ancestors.at(-2), table = ancestors.findLast(n => n.tagName === 'table');
      const cellIndex = row?.children.filter(n => n.type === 'element').indexOf(parent);
      const firstRow = table && elements(table, n => n.tagName === 'tr')[0]?.node;
      const header = firstRow?.children.filter(n => n.type === 'element')[cellIndex];
      const name = header && content(header).trim();
      if (name) label = 'Check option in column "' + name + '"';
    } else {
      const following = parent.children.slice(index + 1);
      const end = following.findIndex(n => ['ol', 'ul'].includes(n.tagName));
      let name = following.slice(0, end < 0 ? undefined : end).map(content).join('').trim();
      const characters = Array.from(name);
      if (characters.length > 100) {
        const prefix = characters.slice(0, 100).join('');
        const stop = prefix.lastIndexOf(' ');
        name = stop < 0 ? characters.slice(0, 99).join('') + '…' : prefix.slice(0, stop) + '…';
      }
      if (name) label = 'Check option: ' + name;
    }
    input.properties.ariaLabel = label || input.properties.ariaLabel || 'Check option';
    if (input.properties.dataInapplicable === undefined) continue;
    parent.children.splice(index + 1, 0, text(' '));
    function strike(nodes) {
      for (let i = 0; i < nodes.length; i++) {
        const node = nodes[i];
        if (node.type === 'text' && node.value.trim()) nodes[i] = element('s', { className: ['inapplicable'] }, [node]);
        else if (node.children && !['p', 'div', 'ul', 'ol'].includes(node.tagName)) strike(node.children);
      }
    }
    const tail = parent.children.slice(index + 2); strike(tail); parent.children.splice(index + 2, tail.length, ...tail);
  }
  return tree;
}
