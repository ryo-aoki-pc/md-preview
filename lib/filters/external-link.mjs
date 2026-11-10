import { elements, entries, addClass } from '../tree.mjs';
// 依存を増やさず標準 URL を使うため、Ruby の URI・Addressable の全入力での互換性は未確認。
function parseStrict(source) {
  // Ruby URI.parse は ASCII の構文を検査し、Addressable が IDN を正規化する。
  if (/[^\x21-\x7e]|%(?![a-f\d]{2})/i.test(source)) return null;
  try { return new URL(source, 'https://markdown.invalid/'); } catch { return null; }
}
export default function externalLink(tree, state) {
  for (const { node } of elements(tree, n => n.tagName === 'a' && n.properties.href !== undefined && n.properties.href !== '')) {
    const p = node.properties, key = p.dataCanonicalSrc !== undefined ? 'dataCanonicalSrc' : 'href';
    const source = String(p[key]);
    if (source.startsWith('#')) continue;
    const uri = parseStrict(source);
    const scheme = source.match(/^([a-z][a-z\d+.-]*):/i)?.[1]?.toLowerCase();
    // 手元に内部ホストはない。scheme と hostname のない相対 URL だけを内部として扱う。
    if (uri && !scheme && !source.startsWith('//')) continue;
    for (const { node: child } of entries(node, n => n !== node)) {
      if (child.type === 'text') child.value = child.value.replaceAll('\u202e', '%E2%80%AE');
      else if (child.properties) for (const [key, value] of Object.entries(child.properties)) {
        if (typeof value === 'string') child.properties[key] = value.replaceAll('\u202e', '%E2%80%AE');
        else if (Array.isArray(value)) child.properties[key] = value.map(v => typeof v === 'string' ? v.replaceAll('\u202e', '%E2%80%AE') : v);
      }
    }
    if (uri && (uri.hostname.startsWith('xn--') || source.includes('%E2%80%AE'))) {
      addClass(node, 'has-tooltip'); p.title = uri.href;
    }
    if (!uri || scheme === undefined || ['http', 'https'].includes(scheme)) {
      const license = String(p.rel) === 'license';
      p.rel = ['nofollow', 'noreferrer', 'noopener', ...(license ? ['license'] : [])];
      p.target = '_blank';
    }
  }
  return tree;
}
