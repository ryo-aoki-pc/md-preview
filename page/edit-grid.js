(() => {
  'use strict';

  const TYPE = 'application/x-gp-table';
  const LIMITS = Object.freeze({ rows: 1000, cols: 1000, cells: 10000, bytes: 1572864, chars: 1572864 });
  const kinds = ['', 'pipe', 'html'];
  const shapes = ['cells', 'rows', 'cols', 'table'];
  const hex32 = /^[0-9a-f]{32}$/i;
  const hex64 = /^[0-9a-f]{64}$/i;
  const integer = (value, min, max = Number.MAX_SAFE_INTEGER) => Number.isSafeInteger(value) && value >= min && value <= max;
  const object = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
  const strings = (values) => Array.isArray(values) && values.every((value) => typeof value === 'string');

  function emptyPipeTable(cols, rows) {
    if (!integer(cols, 1, 20) || !integer(rows, 1, 100)) throw new RangeError('列は 1〜20、本文の行は 1〜100 で指定してください');
    const line = (cells) => '| ' + cells.join(' | ') + ' |';
    return [line(Array.from({ length: cols }, (_, i) => '列' + (i + 1))),
      line(Array(cols).fill('---')), ...Array(rows).fill(line(Array(cols).fill('')))].join('\n');
  }

  function emptyAlert(kind) {
    if (!['note', 'tip', 'important', 'warning', 'caution'].includes(kind)) throw new RangeError('注記の種類を選んでください');
    return '> [!' + kind + ']\n> ';
  }

  function emptyCodeBlock(language) {
    if (!['sh', 'bash', 'powershell'].includes(language)) throw new RangeError('コードの言語を選んでください');
    return '```' + language + '\n\n```';
  }

  function detailsBlock() {
    return '<details>\n<summary>題</summary>\n\n本文\n\n</details>';
  }

  function dimensions(model) {
    return {
      rows: Array.isArray(model.rows) ? model.rows.length : model.rows ?? model.slots?.length ?? 0,
      cols: model.cols ?? model.slots?.reduce((width, row) => Math.max(width, row.length), 0) ?? 0,
    };
  }

  function slotsOf(info) {
    const slots = Array.from({ length: info.rows }, () => []);
    for (const cell of info.cells) {
      for (let row = cell.row; row < cell.row + cell.rowspan; row++) {
        for (let col = cell.col; col < cell.col + cell.colspan; col++) slots[row][col] = cell;
      }
    }
    return slots;
  }

  function axisRect(model, rect, axis) {
    const size = dimensions(model);
    if (axis === 'rows') return { row: rect.row, col: 0, rows: rect.rows, cols: size.cols };
    if (axis === 'cols') return { row: 0, col: rect.col, rows: size.rows, cols: rect.cols };
    return null;
  }

  function rectOf(model, sel) {
    if (!sel?.anchor || !sel.focus) return null;
    const { anchor: a, focus: f, kind = 'cells' } = sel;
    const size = dimensions(model);
    if (![a, f].every((p) => integer(p.row, 0, size.rows - 1) && integer(p.col, 0, size.cols - 1))) return null;
    const rect = { row: Math.min(a.row, f.row), col: Math.min(a.col, f.col), rows: Math.abs(a.row - f.row) + 1, cols: Math.abs(a.col - f.col) + 1 };
    if (kind === 'rows' || kind === 'cols') return axisRect(model, rect, kind);
    if (kind !== 'cells') return null;
    const start = model.slots[a.row]?.[a.col], end = model.slots[f.row]?.[f.col];
    if (!start || !end) return null;
    const row = Math.min(start.row, end.row), col = Math.min(start.col, end.col);
    // 端点の箱だけを一度囲む。途中の結合へ広げる閉包は取らない。
    return { row, col, rows: Math.max(start.row + start.rowspan, end.row + end.rowspan) - row, cols: Math.max(start.col + start.colspan, end.col + end.colspan) - col };
  }

  function contains(rect, cell) {
    return cell.row >= rect.row && cell.col >= rect.col && cell.row + cell.rowspan <= rect.row + rect.rows && cell.col + cell.colspan <= rect.col + rect.cols;
  }

  function crossing(model, rect) {
    return model.cells.find((cell) => cell.row < rect.row + rect.rows && cell.row + cell.rowspan > rect.row && cell.col < rect.col + rect.cols && cell.col + cell.colspan > rect.col && !contains(rect, cell)) ?? null;
  }

  function cellsIn(model, rect) {
    return model.cells.filter((cell) => contains(rect, cell));
  }

  function step(model, point, dir) {
    const cell = model.slots[point.row]?.[point.col];
    if (!cell) return null;
    if (dir === 'next' || dir === 'previous') return model.cells[model.cells.indexOf(cell) + (dir === 'next' ? 1 : -1)] ?? null;
    if (!['up', 'down', 'left', 'right'].includes(dir)) return null;
    const row = dir === 'up' ? cell.row - 1 : dir === 'down' ? cell.row + cell.rowspan : cell.row;
    const col = dir === 'left' ? cell.col - 1 : dir === 'right' ? cell.col + cell.colspan : cell.col;
    return model.slots[row]?.[col] ?? null;
  }

  function validRect(info, rect) {
    return object(rect) && integer(rect.row, 0) && integer(rect.col, 0) && integer(rect.rows, 1) && integer(rect.cols, 1) && rect.row + rect.rows <= info.rows && rect.col + rect.cols <= info.cols;
  }

  function holesIn(info, rect) {
    if (!validRect(info, rect)) return [];
    const slots = slotsOf(info), holes = [];
    for (let row = rect.row; row < rect.row + rect.rows; row++)
      for (let col = rect.col; col < rect.col + rect.cols; col++)
        if (!slots[row]?.[col]) holes.push({ row, col });
    return holes;
  }

  function crossesSection(info, rect) {
    if (info.kind === 'pipe') return rect.row === 0 && rect.rows > 1;
    return !(info.sections ?? []).some((section) => rect.row >= section.first_row && rect.row + rect.rows <= section.first_row + section.rows);
  }

  function mergeReason(info, rect) {
    if (info.cellsLocked) return info.cellsLocked;
    if (!validRect(info, rect) || !limited(rect.rows, rect.cols)) return '結合する範囲を選び直してください';
    if (crossing(info, rect)) return '結合セルの途中で範囲が切れています。結合セル全体を含むように選び直してください';
    const cells = cellsIn(info, rect);
    if (cells.length < 2) return '1 セルだけの範囲は結合できません';
    if (holesIn(info, rect).length) return 'セルの無い位置を含む範囲は結合できません';
    if (crossesSection(info, rect)) return info.kind === 'pipe'
      ? 'パイプの表の見出し行と本文をまたいでは結合できません' : 'thead・tbody・tfoot の区画をまたいでは結合できません';
    const locked = cells.find((cell) => (cell.row !== rect.row || cell.col !== rect.col) && cell.mergeLocked);
    if (locked) return locked.mergeLocked;
    return info.kind === 'pipe' ? info.convertLocked?.html || '' : '';
  }

  function unmergeReason(info, point, rect) {
    if (info.cellsLocked) return info.cellsLocked;
    const cell = info.cells.find((cell) => cell.row === point.row && cell.col === point.col);
    if (!cell || cell.rowspan === 1 && cell.colspan === 1) return '結合していないセルは解除できません';
    if (!rect || rect.row !== cell.row || rect.col !== cell.col || rect.rows !== cell.rowspan || rect.cols !== cell.colspan)
      return '結合を解除するセルを1つ選んでください';
    // サーバーの UnmergeHTMLCell と同じ。下の行の左に穴があると、空のセルを覆っていた列に置けない。
    if (cell.rowspan > 1 && cell.col > 0 &&
      holesIn(info, { row: cell.row + 1, col: 0, rows: cell.rowspan - 1, cols: cell.col }).length)
      return '結合の下の行の左にセルの無い位置があるので、結合を解除できません';
    return '';
  }

  function matrix(info, rect) {
    if (!validRect(info, rect)) return null;
    const values = Array.from({ length: rect.rows }, () => Array(rect.cols).fill(''));
    for (const cell of cellsIn(info, rect)) values[cell.row - rect.row][cell.col - rect.col] = cell.raw ? cell.source : cell.text;
    return values;
  }

  function toTSV(values) {
    const quote = (value) => /[\t\r\n"]/.test(value) ? '"' + value.replace(/"/g, '""') + '"' : value;
    return values.map((row) => row.map(quote).join('\t')).join('\r\n') + (values.length && (values.length > 1 || values[0].length > 1) ? '\r\n' : '');
  }

  // 引用符内の改行・タブと二重の引用符を含む TSV を読む。
  function parseTSV(text) {
    const rows = [];
    let row = [], value = '', quoted = false, start = true, finalNewline = false;
    for (let i = 0; i < text.length; i++) {
      const char = text[i];
      finalNewline = false;
      if (quoted) {
        if (char === '"' && text[i + 1] === '"') { value += '"'; i++; }
        else if (char === '"') quoted = false;
        else value += char;
      } else if (char === '"' && start) { quoted = true; start = false; }
      else if (char === '\t' || char === '\r' || char === '\n') {
        row.push(value); value = ''; start = true;
        if (char !== '\t') {
          if (char === '\r' && text[i + 1] === '\n') i++;
          rows.push(row); row = []; finalNewline = true;
        }
      } else { value += char; start = false; }
    }
    if (quoted) throw new Error('貼り付ける値の引用符が閉じていません');
    // 引用符の外の最後の改行が作った行だけを除く。値の空行・空列は残す。
    if (!finalNewline) { row.push(value); rows.push(row); }
    return rows;
  }

  function toHTML(info, rect) {
    if (!validRect(info, rect) || crossing(info, rect)) return '';
    const slots = slotsOf(info), rows = [];
    let count = 0;
    const escape = (value) => value.replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]).replace(/\r\n?|\n/g, '<br style="mso-data-placement:same-cell">');
    for (let row = rect.row; row < rect.row + rect.rows; row++) {
      let html = '<tr>';
      for (let col = rect.col; col < rect.col + rect.cols; col++) {
        const cell = slots[row]?.[col];
        if (cell && (cell.row !== row || cell.col !== col)) continue;
        count++;
        if (!cell) { html += '<td></td>'; continue; }
        const tag = cell.header ? 'th' : 'td';
        const span = (cell.rowspan > 1 ? ` rowspan="${cell.rowspan}"` : '') + (cell.colspan > 1 ? ` colspan="${cell.colspan}"` : '');
        html += `<${tag}${span}>${escape(cell.raw ? cell.source : cell.text)}</${tag}>`;
      }
      rows.push(html + '</tr>');
    }
    return count < 2 ? '' : '<table>' + rows.join('') + '</table>';
  }

  function limited(rows, cols) {
    return integer(rows, 1, LIMITS.rows) && integer(cols, 1, LIMITS.cols) && rows * cols <= LIMITS.cells;
  }

  function fits(text) {
    if (text.length > LIMITS.chars) return false;
    let bytes = 0;
    for (const char of text) {
      const code = char.codePointAt(0);
      bytes += code <= 0x7f ? 1 : code <= 0x7ff ? 2 : code <= 0xffff ? 3 : 4;
      if (bytes > LIMITS.bytes) return false;
    }
    return true;
  }

  function fragment(info, rect, shape = 'cells') {
    if (!validRect(info, rect) || !limited(rect.rows, rect.cols) || !['html', 'pipe'].includes(info.kind) || !shapes.includes(shape) || info.copyLocked || crossing(info, rect)) return null;
    if (cellsIn(info, rect).some((cell) => cell.copyLocked)) return null;
    const cells = cellsIn(info, rect).map((cell) => ({ row: cell.row - rect.row, col: cell.col - rect.col, rowspan: cell.rowspan, colspan: cell.colspan, text: '', tag: info.kind === 'html' ? cell.tag : '', source: cell.source }));
    const rowTags = info.kind === 'html' && rect.col === 0 && rect.cols === info.cols ? info.rowTags?.slice(rect.row, rect.row + rect.rows) ?? [] : [];
    const delimiters = info.kind === 'pipe' ? info.delimiters?.slice(rect.col, rect.col + rect.cols) ?? [] : [];
    const result = { version: 1, kind: info.kind, shape, rows: rect.rows, cols: rect.cols, cells, row_tags: rowTags, delimiters };
    return fits(JSON.stringify(result)) ? result : null;
  }

  function valuesFragment(values) {
    if (!Array.isArray(values) || !limited(values.length, values[0]?.length) || !values.every((row) => strings(row) && row.length === values[0].length)) return null;
    const cells = values.flatMap((row, y) => row.map((text, x) => ({ row: y, col: x, rowspan: 1, colspan: 1, text, tag: '', source: '' })));
    const result = { version: 1, kind: '', shape: 'cells', rows: values.length, cols: values[0].length, cells, row_tags: [], delimiters: [] };
    return fits(JSON.stringify(result)) ? result : null;
  }

  // 型と上限だけを検査する。原文・タグ・結合の意味はサーバーと tableedit が検査する。
  function readFragment(value) {
    if (!object(value) || value.version !== 1 || !kinds.includes(value.kind) || !shapes.includes(value.shape) || !limited(value.rows, value.cols) || !Array.isArray(value.cells) || value.cells.length > value.rows * value.cols || !strings(value.row_tags) || ![0, value.rows].includes(value.row_tags.length) || !strings(value.delimiters) || ![0, value.cols].includes(value.delimiters.length)) return null;
    const cells = [];
    for (const cell of value.cells) {
      if (!object(cell) || !integer(cell.row, 0, value.rows - 1) || !integer(cell.col, 0, value.cols - 1) || !integer(cell.rowspan, 1, LIMITS.rows) || !integer(cell.colspan, 1, LIMITS.cols) || typeof cell.text !== 'string' || typeof cell.tag !== 'string' || typeof cell.source !== 'string') return null;
      cells.push({ row: cell.row, col: cell.col, rowspan: cell.rowspan, colspan: cell.colspan, text: cell.text, tag: cell.tag, source: cell.source });
    }
    return { version: 1, kind: value.kind, shape: value.shape, rows: value.rows, cols: value.cols, cells, row_tags: [...value.row_tags], delimiters: [...value.delimiters] };
  }

  function readCut(cut) {
    if (!object(cut) || typeof cut.id !== 'string' || !hex32.test(cut.id) || !integer(cut.table, 0) || !integer(cut.render, 1) || typeof cut.hash !== 'string' || !hex64.test(cut.hash) || !integer(cut.row, 0) || !integer(cut.col, 0) || !limited(cut.rows, cut.cols)) return null;
    return { id: cut.id, table: cut.table, render: cut.render, hash: cut.hash, row: cut.row, col: cut.col, rows: cut.rows, cols: cut.cols };
  }

  function parseEnvelope(text) {
    if (typeof text !== 'string' || text.length > LIMITS.chars || !fits(text)) return null;
    try {
      const value = JSON.parse(text, (key, item) => {
        if (['__proto__', 'constructor', 'prototype'].includes(key)) throw new Error('写しに使えない項目があります');
        return item;
      });
      if (!object(value) || value.format !== 'md-preview-table' || value.version !== 1 || typeof value.origin !== 'string' || !hex32.test(value.origin)) return null;
      const fragment = readFragment(value.fragment), cut = value.cut === null ? null : readCut(value.cut);
      if (!fragment || (value.cut !== null && !cut)) return null;
      return { format: 'md-preview-table', version: 1, origin: value.origin, fragment, cut };
    } catch { return null; }
  }

  function envelope({ origin, fragment, cut = null }) {
    return parseEnvelope(JSON.stringify({ format: 'md-preview-table', version: 1, origin, fragment, cut }));
  }

  // info は送信前の宛先の表情報（index・rows・cols）。戻り値は反映後の矩形。
  function placed(op, info) {
    const size = dimensions(info);
    if (['mergeCells', 'mergeCellsAsHTML', 'setAlign', 'setHeader'].includes(op.type))
      return validRect(info, op) ? { row: op.row, col: op.col, rows: op.rows, cols: op.cols } : null;
    if (op.type === 'unmergeCells') {
      const cell = info.cells.find((cell) => cell.row === op.row && cell.col === op.col);
      return cell ? { row: cell.row, col: cell.col, rows: cell.rowspan, cols: cell.colspan } : null;
    }
    const source = op.fragment ?? (op.values ? { rows: op.values.length, cols: op.values[0]?.length } : op.from);
    if (['paste', 'pasteFragment', 'moveCells'].includes(op.type)) {
      return source && integer(op.row, 0) && integer(op.col, 0) && limited(source.rows, source.cols) ? { row: op.row, col: op.col, rows: source.rows, cols: source.cols } : null;
    }
    const axis = ['duplicateRows', 'insertRowsFrom', 'moveRows'].includes(op.type) ? 'rows' : ['duplicateColumns', 'insertColumnsFrom', 'moveColumns'].includes(op.type) ? 'cols' : null;
    if (!axis) return null;
    const key = axis === 'rows' ? 'row' : 'col';
    let start = op[key], count;
    if (!integer(start, 0)) return null;
    if (op.type.startsWith('duplicate')) { count = op.count; start += count; }
    else {
      if (!['before', 'after'].includes(op.side)) return null;
      start += op.side === 'after' ? 1 : 0;
      if (op.type.startsWith('insert')) count = source?.[axis];
      else {
        count = op.from?.count;
        if (!integer(op.from?.[key], 0)) return null;
        if (op.from.table === info.index) {
          if (start > op.from[key] && start < op.from[key] + count) return null;
          if (start > op.from[key]) start -= count;
        }
      }
    }
    if (!integer(count, 1, axis === 'rows' ? LIMITS.rows : LIMITS.cols) || !integer(start, 0)) return null;
    return axisRect(size, axis === 'rows' ? { row: start, rows: count } : { col: start, cols: count }, axis);
  }

  function foreignHints(fragment) {
    return fragment.cells.some((cell) => [cell.text, cell.tag, cell.source].some((text) => /\]\(|\]\[|\[\^|\b(?:href|src)\s*=/i.test(text)));
  }

  window.gpEditGrid = Object.freeze({ TYPE, LIMITS, emptyPipeTable, emptyAlert, emptyCodeBlock, detailsBlock, slotsOf, rectOf, crossing, cellsIn, holesIn, crossesSection, mergeReason, unmergeReason, step, axisRect, matrix, toTSV, parseTSV, toHTML, fragment, valuesFragment, envelope, parseEnvelope, placed, foreignHints });
})();
