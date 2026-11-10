import { elements, element, replace } from '../tree.mjs';
export default function imageLink(tree, state) {
  for (const { node: img, parent, ancestors } of elements(tree, n => n.tagName === 'img')) {
    // ImageLazyLoad を省いた手元では、空の data-src の代わりに空の src も見る。
    // これにより GitLab と同じく、URL が空の画像にはリンクを作らない。
    if (ancestors.some(n => n.tagName === 'a') || img.properties.dataSrc === '' || img.properties.src === '') continue;
    const p = img.properties;
    const attrs = { className: ['no-attachment-icon'], target: '_blank', rel: ['noopener', 'noreferrer'] };
    const href = p.dataSrc ?? p.src;
    if (href !== undefined) attrs.href = href;
    if (p.dataCanonicalSrc !== undefined) attrs.dataCanonicalSrc = p.dataCanonicalSrc;
    if (p.dataDiagram !== undefined && p.dataDiagramSrc !== undefined) {
      attrs.dataDiagram = p.dataDiagram; attrs.dataDiagramSrc = p.dataDiagramSrc;
      delete p.dataDiagram; delete p.dataDiagramSrc;
    }
    replace(parent, img, [element('a', attrs, [img])]);
  }
  return tree;
}
