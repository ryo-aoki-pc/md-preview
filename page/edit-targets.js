// 編集とコメントで、画面のブロック・生の HTML・表のセルを同じ方法で見分ける。
'use strict';

(() => {
  const preview = window.gpPreview;
  if (!preview) return;
  const rangePattern = /^[1-9][0-9]*:[1-9][0-9]*-[1-9][0-9]*:[0-9]+$/;
  const blockTags = new Set(['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'li', 'dt', 'dd', 'blockquote', 'pre', 'ul', 'ol', 'dl', 'hr']);
  const excludedBlock = 'table, .section-nav';
  function blockOf(element) {
    if (!element?.closest || !element.closest('#gp-content') || element.closest('table')) return null;
    if (element.closest('section[data-footnotes]')) {
      const definition = element.closest('section[data-footnotes] > ol > li[data-sourcepos]');
      return definition && rangePattern.test(definition.getAttribute('data-sourcepos') || '') ? definition : null;
    }
    if (element.closest(excludedBlock)) return null;
    for (let node = element; node && node.id !== 'gp-content'; node = node.parentElement) {
      const target = preview.sourceOf(node);
      if (!target || !rangePattern.test(target.range || '')) continue;
      const live = preview.findSource(target.tag, target.range);
      if (live && (blockTags.has(target.tag) ||
        (target.tag === 'img' && live.matches('a[data-diagram="plantuml"] > img, img[data-diagram="plantuml"], .gp-plantuml-error')) ||
        (target.tag === 'div' && live.matches('div.markdown-alert')))) return live;
    }
    return null;
  }
  const findBlock = (target) => target?.tag === 'html' ? preview.findHTML(target)[0] : target && preview.findSource(target.tag, target.range);
  function rawBlockOf(element) {
    if (!element?.closest || !element.closest('#gp-content') || element.closest('table, .section-nav, .markdown-code-block, section[data-footnotes]')) return null;
    // 生の HTML の表（位置を持たない table）は、表のセルの編集が扱うので、ここでは開かない。
    // 位置を持つ祖先の内側は、今までどおりそのブロックの原文で扱う。
    if (element.closest('[data-sourcepos]')) return null;
    return preview.htmlOf(element);
  }
  // サーバーと同じく、区画を木の順にたどる。table.rows は tfoot の順序が変わる。
  function grid(table, info) {
    const groups = [];
    let direct = [];
    const flush = () => { if (direct.length) groups.push(direct); direct = []; };
    for (const child of table.children) {
      if (child.localName === 'tr') { direct.push(child); continue; }
      flush();
      if (['thead', 'tbody', 'tfoot'].includes(child.localName))
        groups.push(Array.from(child.children).filter((row) => row.localName === 'tr'));
    }
    flush();
    if (groups.reduce((n, rows) => n + rows.length, 0) !== info.rows) return null;
    const cells = [];
    const slots = [];
    let row = 0, width = 0;
    for (const group of groups) {
      const occupied = new Map();
      for (let local = 0; local < group.length; local++, row++) {
        let col = 0;
        for (const element of group[local].children) {
          if (!element.matches('th, td')) continue;
          while ((occupied.get(col) || 0) > local) col++;
          const colspan = element.colSpan;
          let rowspan = element.rowSpan;
          if (!rowspan || rowspan > group.length - local) rowspan = group.length - local;
          const cell = info.cells[cells.length];
          if (!cell || col + colspan > info.cols || cell.row !== row || cell.col !== col ||
            cell.rowspan !== rowspan || cell.colspan !== colspan || cell.header !== (element.localName === 'th'))
            return null;
          const item = { ...cell, element, table: info.index };
          for (let x = col; x < col + colspan; x++) {
            if ((occupied.get(x) || 0) > local) return null;
            occupied.set(x, local + rowspan);
            for (let y = row; y < row + rowspan; y++) {
              slots[y] ??= [];
              slots[y][x] = item;
            }
          }
          cells.push(item);
          col += colspan;
          width = Math.max(width, col);
        }
      }
    }
    return cells.length === info.cells.length && width === info.cols ? { cells, slots, rows: groups.flat() } : null;
  }

  window.gpEditTargets = { rangePattern, blockTags, excludedBlock, blockOf, findBlock, rawBlockOf, grid };
})();
