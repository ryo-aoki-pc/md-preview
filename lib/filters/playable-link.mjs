import { elements, element, replace } from '../tree.mjs';
const extensions = { video: ['mp4', 'm4v', 'mov', 'webm', 'ogv'], audio: ['mp3', 'oga', 'ogg', 'spx', 'wav'] };
export default function playableLink(tree, state) {
  const type = state.mediaType;
  for (const { node, parent, ancestors } of elements(tree, (n) => n.tagName === 'img')) {
    if (ancestors.some((n) => n.tagName === 'a')) continue;
    const p = node.properties,
      src = p.dataCanonicalSrc || p.src;
    // File.extname と同じく URL の query を取り除かず、最後のパス要素の拡張子を見る。
    const ext = String(src || '')
      .split('/')
      .pop()
      .match(/\.([^.]+)$/)?.[1]
      .toLowerCase();
    if (!extensions[type].includes(ext)) continue;
    const attrs = { controls: 'true', dataSetup: '{}' };
    if (p.src !== undefined) attrs.src = p.src;
    if (p.title !== undefined || p.alt !== undefined) attrs.dataTitle = p.title ?? p.alt;
    if (p.dataCanonicalSrc !== undefined) attrs.dataCanonicalSrc = p.dataCanonicalSrc;
    if (type === 'video') {
      attrs.preload = 'metadata';
      attrs.className = ['gl-rounded-lg'];
      if (p.height !== undefined) attrs.height = p.height;
      if (p.width !== undefined) attrs.width = p.width;
    }
    replace(parent, node, [
      element('span', { className: ['media-container', type + '-container'] }, [element(type, attrs)]),
    ]);
  }
  return tree;
}
