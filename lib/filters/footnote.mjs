import { elements, addClass } from '../tree.mjs';
export default function footnote(tree, state) {
  const links = elements(tree, n => n.tagName === 'a' && n.properties.dataFootnoteRef !== undefined).filter(e => e.parent.tagName === 'sup');
  if (links.length > 1000) return tree;
  const sections = elements(tree, n => n.tagName === 'section' && n.properties.dataFootnotes !== undefined);
  if (!sections.length) return tree;
  addClass(sections[0].node, 'footnotes');
  // プレビューは一文書なので、GitLab の rand_suffix を空にして ID を安定させる。
  for (const { node: link, parent } of links) {
    if (!link.properties.id) continue;
    const target = String(link.properties.href || '').replace(/^#/, '');
    const item = sections.flatMap(s => elements(s.node, n => n.tagName === 'li' && n.properties.id === target))[0]?.node;
    if (!item) continue;
    // 19.5.1 の参照ソースは id から注を引くが、19.6 の実出力は再参照も復元する。
    // fnref-1-2 は fn-1 を指すので、href から注を探して個々の戻りリンクを処理する。
    addClass(parent, 'footnote-ref');
    const back = elements(item, n => n.tagName === 'a' && n.properties.href === '#' + link.properties.id)[0]?.node;
    if (back) {
      back.properties.title = 'Back to reference ' + (back.properties.dataFootnoteBackrefIdx ?? '');
      back.properties.ariaLabel = back.properties.title;
      addClass(back, 'footnote-backref');
    }
  }
  return tree;
}
