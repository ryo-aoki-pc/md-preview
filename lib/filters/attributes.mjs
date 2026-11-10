import { elements, classes } from '../tree.mjs';
export default function attributes(tree, state) {
  for (const { node: img, parent } of elements(tree, n => n.tagName === 'img')) {
    const sibling = parent.children[parent.children.indexOf(img) + 1];
    const match = sibling?.type === 'text' && sibling.value.match(/^(\{([^\n]{1,100})\})/);
    if (!match) continue;
    for (const attr of match[2].trim().split(/\s+/)) {
      const value = attr.match(/^([\w-]+)="?([^"\s]+)"?$/);
      if (value && ['width', 'height'].includes(value[1]) && /^\d{1,4}(%|px)?$/.test(value[2])) img.properties[value[1]] = value[2];
      const name = attr.match(/^\.(float-right|float-left|float-none)$/)?.[1];
      if (name) img.properties.className = [...classes(img), 'glfm-' + name];
    }
    sibling.value = sibling.value.slice(match[1].length);
  }
  return tree;
}
