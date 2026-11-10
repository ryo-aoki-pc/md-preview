import { content, element, elements, replace } from '../tree.mjs';
import { plantumlId } from '../plantuml-source.mjs';

export default function plantuml(tree, state) {
  if (!state.plantuml) return tree;
  state.diagrams ||= new Map();
  for (const { node: pre, parent } of elements(tree, (node) => node.tagName === 'pre')) {
    const code = pre.children.find(
      (node) =>
        node.tagName === 'code' &&
        (pre.properties.dataCanonicalLang === 'plantuml' || node.properties.dataCanonicalLang === 'plantuml'),
    );
    if (!code) continue;
    // Ruby の chomp と同じく、末尾の改行を一つだけ落とす。
    const source = content(code).replace(/(?:\r\n|\n|\r)$/, '');
    if (Buffer.byteLength(source, 'utf8') > 256 * 1024) continue;
    const id = plantumlId(source);
    const properties = {
      className: ['plantuml'],
      src: '/_/plantuml/' + id + '.svg',
      dataDiagram: 'plantuml',
      dataDiagramSrc: 'data:text/plain;base64,' + Buffer.from(source, 'utf8').toString('base64'),
    };
    // GitLab との差: 行と図の対応を残し、スクロールの同期とダブルクリックでの移動に使う。
    if (pre.properties.dataSourcepos !== undefined) properties.dataSourcepos = pre.properties.dataSourcepos;
    replace(parent, pre, [element('img', properties)]);
    state.diagrams.set(id, source);
  }
  return tree;
}
