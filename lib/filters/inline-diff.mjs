import { toHtml } from 'hast-util-to-html';
import { entries, fragment, replace } from '../tree.mjs';
export default function inlineDiff(tree, state) {
  for (const { node, parent, ancestors } of entries(tree, (n) => n.type === 'text')) {
    if (ancestors.some((n) => ['pre', 'code', 'tt'].includes(n.tagName))) continue;
    // Ruby と同じく削除の後に追加を置換する。追加が削除をまたぐ入れ子もこの順序で扱う。
    // 入力は消毒済みの文字ノードをエスケープしたものなので、新しい HTML は固定の span だけになる。
    const before = toHtml(node);
    const after = before
      .replace(
        /(?:\[\-([^\n]*?)\-\]|\{\-([^\n]*?)\-\})/g,
        (_, a, b) => '<span class="idiff left right deletion">' + (a || '') + (b || '') + '</span>',
      )
      .replace(
        /(?:\[\+([^\n]*?)\+\]|\{\+([^\n]*?)\+\})/g,
        (_, a, b) => '<span class="idiff left right addition">' + (a || '') + (b || '') + '</span>',
      );
    if (before !== after) replace(parent, node, fragment(after).children);
  }
  return tree;
}
