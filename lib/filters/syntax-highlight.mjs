import { elements, element, content, onlyCode, addClass, replace } from '../tree.mjs';
import { highlightCode } from '../highlight.mjs';
export default function syntaxHighlight(tree, state) {
  for (const { node: pre, parent } of elements(tree, n => n.tagName === 'pre' && n.properties.dataKrokiStyle === undefined)) {
    const code = onlyCode(pre);
    if (!code) continue;
    const result = highlightCode(content(code), pre.properties.dataCanonicalLang);
    code.children = result.children;
    pre.children = [code];
    addClass(pre, 'code', 'highlight', 'js-syntax-highlight');
    if (result.language) addClass(pre, 'language-' + result.language);
    pre.properties['v-pre'] = 'true';
    const children = [pre];
    if (result.language !== 'suggestion') children.push(element('copy-code'), element('insert-code-snippet'));
    replace(parent, pre, [element('div', { className: ['gl-relative', 'markdown-code-block', 'js-markdown-code'] }, children)]);
  }
  return tree;
}
