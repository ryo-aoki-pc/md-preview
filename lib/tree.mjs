// 各フィルターが使う hast の操作。文字列を HTML として挿入せず、文字ノードで組み立てる。
import { fromHtml } from 'hast-util-from-html';
import { toString } from 'hast-util-to-string';
export const text = value => ({ type: 'text', value: String(value) });
export const element = (tagName, properties = {}, children = []) => ({ type: 'element', tagName, properties, children });
export const fragment = html => fromHtml(html, { fragment: true });
export const content = toString;
export const classes = node => Array.isArray(node.properties?.className) ? node.properties.className : String(node.properties?.className || '').split(/\s+/).filter(Boolean);
export const hasClass = (node, name) => classes(node).includes(name);
export function addClass(node, ...names) { node.properties.className = [...new Set([...classes(node), ...names.flatMap(n => n.split(/\s+/))])]; }
export function entries(tree, predicate = () => true) {
  const found = [];
  function visit(node, parent, ancestors) {
    if (predicate(node)) found.push({ node, parent, ancestors });
    for (const child of node.children || []) visit(child, node, [...ancestors, node]);
  }
  visit(tree, null, []);
  return found;
}
export const elements = (tree, predicate = () => true) => entries(tree, n => n.type === 'element' && predicate(n));
export function replace(parent, node, children) {
  const index = parent.children.indexOf(node);
  if (index >= 0) parent.children.splice(index, 1, ...children);
}
export const onlyCode = pre => {
  const children = pre.children.filter(n => n.type === 'element');
  return children.length === 1 && children[0].tagName === 'code' ? children[0] : null;
};
