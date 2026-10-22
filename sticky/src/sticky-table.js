(function () {
  "use strict";

  // DOM とは切り離して、行のまとまりごとに格子を作る。
  function spanValue(value, maximum, allowZero) {
    if (typeof value === "number") {
      if (!Number.isInteger(value) || value < 0) return 1;
      return Math.min(maximum, Math.max(allowZero ? 0 : 1, value));
    }
    const text = String(value == null ? 1 : value).trim();
    if (!/^\+?\d+$/.test(text)) return 1;
    const number = Number(text);
    if (!Number.isFinite(number)) return maximum;
    return Math.min(maximum, Math.max(allowZero ? 0 : 1, number));
  }

  function buildGrid(groups) {
    const grid = [];
    const cells = [];
    const sections = [];
    let rowCount = 0;
    let colCount = 0;
    let overlap = false;
    for (const group of groups) {
      const start = rowCount;
      const end = start + group.rows.length;
      sections.push({ kind: group.kind, start, rowCount: group.rows.length });
      for (const row of group.rows) {
        if (!grid[rowCount]) grid[rowCount] = [];
        let col = 0;
        for (const input of row) {
          while (grid[rowCount][col] !== undefined) col += 1;
          const colspan = spanValue(input.colspan, 1000, false);
          const parsed = spanValue(input.rowspan, 65534, true);
          const rowspan = Math.max(1, Math.min(parsed === 0 ? end - rowCount : parsed, end - rowCount));
          const index = cells.length;
          cells.push({ row: rowCount, col, rowspan, colspan, header: input.header === true, kind: group.kind });
          for (let r = rowCount; r < rowCount + rowspan; r += 1) {
            if (!grid[r]) grid[r] = [];
            for (let c = col; c < col + colspan; c += 1) {
              if (grid[r][c] !== undefined) overlap = true;
              else grid[r][c] = index;
            }
          }
          col += colspan;
          colCount = Math.max(colCount, col);
        }
        rowCount += 1;
      }
    }
    return { rowCount, colCount, cells, grid, overlap, groups: sections };
  }

  function cellRoles(model, rows, cols) {
    return model.cells.map((cell) => ({
      top: rows > 0 && cell.row + cell.rowspan <= rows,
      left: cols > 0 && cell.col + cell.colspan <= cols,
      cross: cols > 0 && cell.col < cols && cols < cell.col + cell.colspan,
    }));
  }

  function headerRowCount(model) {
    let leading = 0;
    for (let r = 0; r < model.rowCount; r += 1) {
      const anchors = model.cells.filter((cell) => cell.row === r);
      if (!anchors.length || !anchors.every((cell) => cell.header)) break;
      leading += 1;
    }
    const heads = model.groups.filter((group) => group.kind === "thead").reduce((sum, group) => sum + group.rowCount, 0);
    return Math.max(leading, heads);
  }

  function columnLabels(model, texts) {
    const count = headerRowCount(model);
    return Array.from({ length: model.colCount }, (_, col) => {
      const labels = model.cells.filter((cell) => cell.row < count && cell.col <= col && col < cell.col + cell.colspan)
        .map((cell) => String(texts[model.cells.indexOf(cell)] || "").replace(/\s+/g, " ").trim()).filter(Boolean);
      const text = labels.join(" / ");
      return Array.from(text).length > 40 ? Array.from(text).slice(0, 40).join("") + "…" : text;
    });
  }

  function initialWrap(model, nowrapFlags) {
    const headers = headerRowCount(model);
    return Array.from({ length: model.colCount }, (_, col) => {
      const candidates = model.cells.map((cell, index) => cell.col === col && cell.colspan === 1 ? index : -1).filter((index) => index >= 0);
      const body = candidates.filter((index) => model.cells[index].row >= headers);
      const indices = body.length ? body : candidates;
      return !indices.length || !indices.every((index) => nowrapFlags[index]);
    });
  }

  function cellWrapModes(model, wrap) {
    return model.cells.map((cell) => wrap.slice(cell.col, cell.col + cell.colspan).every((value) => value === false) ? "nowrap" : "wrap");
  }

  function columnMinWidths(maxWidths, cap) {
    return maxWidths.map((width) => Math.min(width, cap));
  }

  function frozenRegion(model, selectedRows = null) {
    const { rowCount, colCount, cells, grid } = model;
    if (!rowCount || !colCount || model.overlap) {
      return { rows: 0, cols: 0, headerCols: 0, roles: cellRoles(model, 0, 0) };
    }
    let rows = selectedRows;
    if (rows === null) {
      rows = Math.max(1, headerRowCount(model));
      let changed = true;
      while (changed) {
        changed = false;
        for (const cell of cells) {
          if (cell.row < rows && cell.row + cell.rowspan > rows) {
            rows = cell.row + cell.rowspan;
            changed = true;
          }
        }
      }
      if (rows >= rowCount) rows = 0;
    }
    let headerCols = colCount - 1;
    let bodyRows = 0;
    for (let r = rows; r < rowCount; r += 1) {
      const first = cells[grid[r][0]];
      if (first && first.col === 0 && first.colspan === colCount) continue;
      bodyRows += 1;
      let count = 0;
      while (count < colCount - 1) {
        const cell = cells[grid[r][count]];
        if (!cell || !cell.header) break;
        count += 1;
      }
      headerCols = Math.min(headerCols, count);
    }
    if (!bodyRows) headerCols = 0;
    let cols = Math.max(1, headerCols);
    let changed = true;
    while (changed) {
      changed = false;
      for (const cell of cells) {
        const end = cell.col + cell.colspan;
        if (cell.row < rows && cell.col < cols && cols < end && end < colCount) {
          cols = end;
          changed = true;
        }
      }
    }
    if (cols >= colCount) cols = 0;
    return { rows, cols, headerCols, roles: cellRoles(model, rows, cols) };
  }

  // hidden は共有する線を消し、それ以外は太い線を選ぶ。同じ太さなら上・左を優先する。
  function rebuildBorders(model, borders) {
    const empty = () => ({ width: 0, style: "none", color: "transparent" });
    function line(edge) {
      if (edge && edge.style === "hidden") return { width: 0, style: "hidden", color: "transparent" };
      if (!edge || edge.style === "none" || !(Number(edge.width) > 0)) return empty();
      return { width: Number(edge.width), style: edge.style, color: edge.color };
    }
    function wider(first, second) {
      if (first.style === "hidden") return first;
      if (second.style === "hidden") return second;
      return second.width > first.width ? second : first;
    }
    function visible(edge) {
      return edge.style === "hidden" ? empty() : { ...edge };
    }
    function missing(row, col) {
      return !model.grid[row] || model.grid[row][col] === undefined;
    }
    return model.cells.map((cell, index) => {
      const own = borders[index] || {};
      const right = cell.col + cell.colspan;
      const bottom = cell.row + cell.rowspan;
      let topExposed = true;
      let leftExposed = true;
      let rightLine = empty();
      let bottomLine = empty();
      for (let r = cell.row; r < bottom; r += 1) {
        if (!missing(r, cell.col - 1)) leftExposed = false;
        const neighbor = model.grid[r] && model.grid[r][right];
        const adjacent = neighbor === undefined ? null : borders[neighbor];
        rightLine = wider(rightLine, wider(line(own.right), line(adjacent && adjacent.left)));
      }
      for (let c = cell.col; c < right; c += 1) {
        if (!missing(cell.row - 1, c)) topExposed = false;
        const neighbor = model.grid[bottom] && model.grid[bottom][c];
        const adjacent = neighbor === undefined ? null : borders[neighbor];
        bottomLine = wider(bottomLine, wider(line(own.bottom), line(adjacent && adjacent.top)));
      }
      return {
        top: topExposed ? visible(line(own.top)) : empty(),
        right: visible(rightLine),
        bottom: visible(bottomLine),
        left: leftExposed ? visible(line(own.left)) : empty(),
      };
    });
  }

  function usableRowBoundaries(model) {
    const boundaries = [];
    for (let k = 0; k < model.rowCount; k += 1) {
      if (!model.cells.some((cell) => cell.row < k && k < cell.row + cell.rowspan)) boundaries.push(k);
    }
    return boundaries;
  }

  function usableColBoundaries(model, rows) {
    const boundaries = [];
    for (let k = 0; k < model.colCount; k += 1) {
      if (!model.cells.some((cell) => cell.row < rows && cell.col < k && k < cell.col + cell.colspan)) boundaries.push(k);
    }
    return boundaries;
  }

  function stepBoundary(boundaries, current, direction) {
    if (direction > 0) return boundaries.find((boundary) => boundary > current) ?? null;
    for (let i = boundaries.length - 1; i >= 0; i -= 1) {
      if (boundaries[i] < current) return boundaries[i];
    }
    return null;
  }

  function lowerBoundary(boundaries, current) {
    for (let i = boundaries.length - 1; i >= 0; i -= 1) {
      if (boundaries[i] <= current) return boundaries[i];
    }
    return 0;
  }

  function pickBoundaries(model, cellIndex) {
    const cell = model.cells[cellIndex];
    if (!cell) return { rows: 0, cols: 0 };
    const rows = lowerBoundary(usableRowBoundaries(model), cell.row);
    const cols = lowerBoundary(usableColBoundaries(model, rows), cell.col);
    return { rows, cols };
  }

  function crossCells(model, cellIndex) {
    const selected = model.cells[cellIndex];
    if (!selected) return [];
    const indices = [];
    model.cells.forEach((cell, index) => {
      const row = cell.row < selected.row + selected.rowspan && selected.row < cell.row + cell.rowspan;
      const col = cell.col < selected.col + selected.colspan && selected.col < cell.col + cell.colspan;
      if (row || col) indices.push(index);
    });
    return indices;
  }

  function normalizeSearch(value) {
    return String(value == null ? "" : value).normalize("NFKC").toLowerCase().replace(/\s+/g, " ").trim();
  }

  function searchMatches(texts, query) {
    const needle = normalizeSearch(query);
    if (!needle) return [];
    return texts.map((text, index) => normalizeSearch(text).includes(needle) ? index : -1).filter((index) => index >= 0);
  }

  function filterRows(model, matched, keepRows) {
    const protectedRows = Array.from({ length: model.rowCount }, (_, row) => row < keepRows);
    const kinds = Array(model.rowCount).fill("tbody");
    for (const group of model.groups) {
      for (let row = group.start; row < group.start + group.rowCount; row += 1) {
        kinds[row] = group.kind;
        if (group.kind === "thead" || group.kind === "tfoot") protectedRows[row] = true;
      }
    }
    const matchedRows = new Set(matched.filter((index) => model.cells[index]).map((index) => model.cells[index].row));
    const anchors = Array.from({ length: model.rowCount }, () => []);
    const ends = Array.from({ length: model.rowCount }, (_, row) => row + 1);
    for (const cell of model.cells) {
      anchors[cell.row].push(cell);
      ends[cell.row] = Math.max(ends[cell.row], cell.row + cell.rowspan);
    }
    const visible = Array(model.rowCount).fill(false);
    for (let start = 0; start < model.rowCount;) {
      let end = ends[start];
      for (let row = start; row < end; row += 1) end = Math.max(end, ends[row]);
      let keep = false;
      for (let row = start; row < end; row += 1) {
        if (protectedRows[row] || matchedRows.has(row)) keep = true;
      }
      for (let row = start; row < end; row += 1) visible[row] = keep;
      start = end;
    }
    let following = false;
    for (let row = model.rowCount - 1; row >= 0; row -= 1) {
      if (kinds[row] === "thead" || kinds[row] === "tfoot") continue;
      const cells = anchors[row];
      const divider = cells.length === 1 && cells[0].col === 0 && cells[0].colspan === model.colCount && cells[0].rowspan === 1;
      if (divider) {
        visible[row] = protectedRows[row] || matchedRows.has(row) || following;
        following = false;
      } else if (visible[row]) {
        following = true;
      }
    }
    return visible.map((keep, row) => keep ? -1 : row).filter((row) => row >= 0);
  }

  function tableMatrix(model, texts, hiddenRows) {
    const hidden = new Set(hiddenRows);
    const rows = [];
    for (let row = 0; row < model.rowCount; row += 1) {
      if (!hidden.has(row)) rows.push(row);
    }
    const positions = new Map(rows.map((row, index) => [row, index]));
    const grid = rows.map(() => Array(model.colCount).fill(""));
    const cells = [];
    model.cells.forEach((cell, index) => {
      if (hidden.has(cell.row)) return;
      const row = positions.get(cell.row);
      const text = String(texts[index] == null ? "" : texts[index]);
      cells.push({ row, col: cell.col, rowspan: cell.rowspan, colspan: cell.colspan, header: cell.header, text });
      grid[row][cell.col] = text;
    });
    return { rowCount: rows.length, colCount: model.colCount, cells, grid };
  }

  function toTsv(matrix) {
    const quote = (text) => /[\t\r\n"]/.test(text) ? '"' + text.replace(/"/g, '""') + '"' : text;
    return matrix.grid.map((row) => row.map(quote).join("\t")).join("\r\n") + (matrix.grid.length ? "\r\n" : "");
  }

  function toHtml(matrix) {
    const escape = (text) => text.replace(/[&<>"]/g, (value) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[value]).replace(/\r\n?|\n/g, "<br>");
    const rows = Array.from({ length: matrix.rowCount }, () => []);
    for (const cell of matrix.cells) {
      const tag = cell.header ? "th" : "td";
      const rowspan = cell.rowspan > 1 ? ' rowspan="' + cell.rowspan + '"' : "";
      const colspan = cell.colspan > 1 ? ' colspan="' + cell.colspan + '"' : "";
      rows[cell.row].push("<" + tag + rowspan + colspan + ">" + escape(cell.text) + "</" + tag + ">");
    }
    return "<table>" + rows.map((row) => "<tr>" + row.join("") + "</tr>").join("") + "</table>";
  }

  if (typeof module === "object" && module && module.exports) {
    module.exports = { buildGrid, frozenRegion, rebuildBorders, usableRowBoundaries, usableColBoundaries, stepBoundary, pickBoundaries, crossCells, headerRowCount, columnLabels, initialWrap, cellWrapModes, columnMinWidths, searchMatches, filterRows, tableMatrix, toTsv, toHtml };
  }
  if (typeof document === "undefined") return;

  const previous = window.__mdTableSticky;
  if (previous) {
    previous.stop();
    delete window.__mdTableSticky;
    notice("表の固定を外した");
    return;
  }

  // style 属性を文字列のまま戻して、元の書き方も残す。
  function restoreStyle(element, original) {
    if (original === null) {
      // Chromium は、CSSOM で書いた直後に属性を消すと、後で空の style="" を書き戻す。先に属性へ書き戻させてから消す。
      element.getAttribute("style");
      element.removeAttribute("style");
    } else {
      element.setAttribute("style", original);
    }
  }

  function setStyles(element, styles) {
    for (const [name, value] of Object.entries(styles)) {
      element.style.setProperty(name, value, "important");
    }
  }

  function notice(message) {
    const element = document.createElement("div");
    element.setAttribute("role", "status");
    element.textContent = message;
    setStyles(element, {
      position: "fixed", right: "16px", bottom: "16px", "z-index": "2147483647",
      padding: "10px 16px", "border-radius": "4px", background: "#242424", color: "#fff",
      "font-size": "14px", "line-height": "1.5", "max-width": "calc(100vw - 32px)",
    });
    document.body.appendChild(element);
    const timer = window.setTimeout(() => {
      const current = window.__mdTableSticky;
      if (current) current.write(() => element.remove());
      else element.remove();
    }, 2500);
    return { element, timer };
  }

  const state = {
    records: new Map(), processed: new WeakSet(), pending: new Set(), dirty: new Set(), active: true,
    frame: 0, timer: 0, firstMutationAt: null, observer: null, resize: null, notification: null, floating: null, picking: null, pickHandlers: null,
  };
  window.__mdTableSticky = state;

  function markChanged(node, descendants) {
    const element = node.nodeType === 1 ? node : node.parentElement;
    if (!element) return;
    let table = element.closest("table");
    while (table) {
      if (state.records.has(table)) state.dirty.add(table);
      table = table.parentElement && table.parentElement.closest("table");
    }
    if (descendants) {
      for (const child of element.querySelectorAll("table")) {
        if (state.records.has(child)) state.dirty.add(child);
      }
    }
  }

  function scheduleScan(mutations) {
    if (!state.active || !mutations.length) return;
    for (const mutation of mutations) {
      markChanged(mutation.target, false);
      for (const node of mutation.addedNodes) markChanged(node, true);
      for (const node of mutation.removedNodes) markChanged(node, true);
    }
    const now = window.performance.now();
    if (state.firstMutationAt === null) state.firstMutationAt = now;
    const remaining = Math.max(0, 1000 - (now - state.firstMutationAt));
    window.clearTimeout(state.timer);
    state.timer = window.setTimeout(scan, Math.min(300, remaining));
  }

  // 子の移動を自分の変更として扱い、次の見張りを呼び続けない。
  function write(action) {
    if (state.observer) {
      const mutations = state.observer.takeRecords();
      if (mutations.length) scheduleScan(mutations);
      state.observer.disconnect();
    }
    try {
      return action();
    } finally {
      if (state.active && state.observer) {
        state.observer.observe(document.body, { childList: true, subtree: true });
      }
    }
  }
  state.write = write;

  function remember(record, element) {
    if (!record.originals.has(element)) record.originals.set(element, element.getAttribute("style"));
  }

  function transparent(color) {
    if (color === "transparent") return true;
    const rgba = color.match(/^rgba\([^,]+,[^,]+,[^,]+,\s*([\d.]+)\s*\)$/);
    if (rgba) return Number(rgba[1]) === 0;
    return /\/\s*0(?:\.0*)?%?\s*\)$/.test(color);
  }

  function ancestorBackground(element) {
    for (let node = element; node; node = node.parentElement) {
      const color = window.getComputedStyle(node).backgroundColor;
      if (!transparent(color)) return color;
    }
    return "white";
  }

  function tableParts(table) {
    const sections = Array.from(table.children).filter((element) => /^(THEAD|TBODY|TFOOT)$/.test(element.tagName));
    sections.sort((a, b) => ["THEAD", "TBODY", "TFOOT"].indexOf(a.tagName) - ["THEAD", "TBODY", "TFOOT"].indexOf(b.tagName));
    const rows = [];
    const elements = [];
    const groups = sections.map((section) => ({
      kind: section.tagName.toLowerCase(),
      rows: Array.from(section.rows, (row) => {
        rows.push(row);
        return Array.from(row.cells, (cell) => {
          elements.push(cell);
          return { rowspan: cell.rowSpan, colspan: cell.colSpan, header: cell.tagName === "TH" };
        });
      }),
    }));
    return { model: buildGrid(groups), rows, elements };
  }

  function unwrapInner(record, cell) {
    const inner = record.inners.get(cell);
    if (!inner) return;
    if (inner.parentNode) {
      while (inner.firstChild) inner.parentNode.insertBefore(inner.firstChild, inner);
      inner.remove();
    }
    record.inners.delete(cell);
    record.originals.delete(inner);
    record.applied.delete(inner);
  }

  function unwrapContainer(record) {
    const wrapper = record.wrapper;
    if (!wrapper) return;
    if (state.resize) state.resize.unobserve(wrapper);
    if (wrapper.parentNode) {
      while (wrapper.firstChild) wrapper.parentNode.insertBefore(wrapper.firstChild, wrapper);
      wrapper.remove();
    }
    record.originals.delete(wrapper);
    record.applied.delete(wrapper);
    record.wrapper = null;
  }

  function scrollContainer(record) {
    const gitlab = record.table.parentElement && record.table.parentElement.closest("[data-sticky-header]");
    if (record.wrapper && gitlab && record.wrapper.contains(gitlab)) unwrapContainer(record);
    const parent = record.table.parentElement;
    if (parent && parent.hasAttribute("data-sticky-header")) return parent;
    if (!record.wrapper) {
      const wrapper = document.createElement("div");
      wrapper.setAttribute("data-md-table-sticky-wrap", "");
      record.table.before(wrapper);
      wrapper.appendChild(record.table);
      record.wrapper = wrapper;
    }
    return record.wrapper;
  }

  function structureOf(parts) {
    return {
      rows: parts.rows.slice(), rowCells: parts.rows.map((row) => Array.from(row.cells)),
      elements: parts.elements.slice(), cells: parts.model.cells,
    };
  }

  function sameStructure(cache, parts) {
    if (!cache || cache.rows.length !== parts.rows.length || cache.elements.length !== parts.elements.length) return false;
    for (let r = 0; r < parts.rows.length; r += 1) {
      if (cache.rows[r] !== parts.rows[r]) return false;
      const cells = Array.from(parts.rows[r].cells);
      if (cells.length !== cache.rowCells[r].length || cells.some((cell, c) => cell !== cache.rowCells[r][c])) return false;
    }
    return parts.model.cells.every((cell, index) => {
      const old = cache.cells[index];
      return old.row === cell.row && old.col === cell.col && old.rowspan === cell.rowspan && old.colspan === cell.colspan && old.kind === cell.kind;
    });
  }

  function readBase(record, cell, style) {
    record.base.set(cell, {
      transparent: transparent(style.backgroundColor),
      nativeTop: style.position === "sticky" && style.top !== "auto", top: style.top, nowrap: style.whiteSpace === "nowrap",
    });
  }

  // 行やセルの組が変わったときだけ、元の collapse の罫線を読む。
  function ensureBorders(record, parts) {
    if (sameStructure(record.borderCache, parts)) return false;
    remember(record, record.table);
    for (const cell of parts.elements) remember(record, cell);
    const tableStyle = record.table.getAttribute("style");
    const saved = parts.elements.map((cell) => cell.getAttribute("style"));
    let borders;
    try {
      restoreStyle(record.table, record.originals.get(record.table));
      for (const cell of parts.elements) restoreStyle(cell, record.originals.get(cell));
      borders = parts.elements.map((cell) => {
        const style = window.getComputedStyle(cell);
        readBase(record, cell, style);
        const sides = {};
        for (const side of ["top", "right", "bottom", "left"]) {
          sides[side] = {
            width: parseFloat(style.getPropertyValue("border-" + side + "-width")) || 0,
            style: style.getPropertyValue("border-" + side + "-style"),
            color: style.getPropertyValue("border-" + side + "-color"),
          };
        }
        return sides;
      });
    } finally {
      restoreStyle(record.table, tableStyle);
      parts.elements.forEach((cell, index) => restoreStyle(cell, saved[index]));
    }
    const values = rebuildBorders(parts.model, borders);
    const styles = values.map((sides) => {
      const result = {};
      for (const side of ["top", "right", "bottom", "left"]) {
        const edge = sides[side];
        result["border-" + side + "-width"] = edge.width + "px";
        result["border-" + side + "-style"] = edge.style;
        result["border-" + side + "-color"] = edge.color;
      }
      return result;
    });
    record.borderCache = { ...structureOf(parts), values, styles };
    return true;
  }

  // 後から GitLab の包みができた場合も、罫線は読み直さず背景と元の固定だけを調べる。
  function updateBase(record, elements) {
    const saved = elements.map((cell) => cell.getAttribute("style"));
    try {
      for (const cell of elements) restoreStyle(cell, record.originals.get(cell));
      for (const cell of elements) readBase(record, cell, window.getComputedStyle(cell));
    } finally {
      elements.forEach((cell, index) => restoreStyle(cell, saved[index]));
    }
  }

  function updateNativeFull(record, floating) {
    const scroll = record.scroll;
    if (scroll === record.wrapper || !scroll.hasAttribute("data-sticky-header")) {
      record.nativeFull = false;
    } else if (!floating && !record.printing) {
      remember(record, scroll);
      const saved = scroll.getAttribute("style");
      try {
        restoreStyle(scroll, record.originals.get(scroll));
        record.nativeFull = window.getComputedStyle(scroll).maxHeight === "none";
      } finally {
        restoreStyle(scroll, saved);
      }
    }
    // 全幅の表示と印刷の間は、元の枠で測れるまで選択を保つ。
    if (!record.nativeFull) record.fitHeight = false;
  }

  // 検索やフォーカスで移った先が固定した部分の下に隠れないようにする。枠のほかの style は当て直さない。
  function setScrollPadding(scroll, top, left) {
    for (const [name, value] of [["scroll-padding-top", top], ["scroll-padding-left", left]]) {
      if (value === null) scroll.style.removeProperty(name);
      else scroll.style.setProperty(name, value + "px", "important");
    }
  }

  function applyOne(record, element, styles) {
    const signature = JSON.stringify(styles);
    if (record.applied.get(element) === signature) return;
    remember(record, element);
    restoreStyle(element, record.originals.get(element));
    setStyles(element, styles);
    record.applied.set(element, signature);
  }

  function apply(record, desired) {
    for (const [element] of record.applied) {
      if (!desired.has(element)) {
        restoreStyle(element, record.originals.get(element));
        record.applied.delete(element);
      }
    }
    for (const [element, styles] of desired) applyOne(record, element, styles);
  }

  function removeBar(record) {
    if (!record.bar) return;
    record.bar.element.remove();
    record.bar = null;
  }

  function barAnchor(record) {
    if (record.wrapper) return record.wrapper;
    const parent = record.table.parentElement;
    if (parent && parent.hasAttribute("data-sticky-header")) {
      return parent.closest("div.gl-table-shadow-overlay") || parent;
    }
    return record.table;
  }

  function stopPicking(record, remeasure = true) {
    if (state.picking !== record) return;
    if (state.pickHandlers) {
      window.removeEventListener("click", state.pickHandlers.onClick, true);
      window.removeEventListener("keydown", state.pickHandlers.onKeydown, true);
    }
    state.pickHandlers = null;
    state.picking = null;
    record.picking = false;
    if (remeasure && state.active && record.table.isConnected) refresh(record);
  }

  function startPicking(record) {
    if (state.picking) stopPicking(state.picking);
    record.picking = true;
    state.picking = record;
    const onClick = (event) => {
      const target = event.target;
      if (record.bar && record.bar.buttons.pick.contains(target)) return;
      const cell = target && target.closest ? target.closest("td, th") : null;
      if (cell && cell.closest("table") === record.table) {
        event.preventDefault();
        event.stopPropagation();
        event.stopImmediatePropagation();
        write(() => {
          const parts = tableParts(record.table);
          const boundary = pickBoundaries(parts.model, parts.elements.indexOf(cell));
          record.override.rows = boundary.rows;
          record.override.cols = boundary.cols;
          stopPicking(record, false);
          refresh(record);
        });
      } else {
        write(() => stopPicking(record));
      }
    };
    const onKeydown = (event) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      write(() => stopPicking(record));
    };
    state.pickHandlers = { onClick, onKeydown };
    window.addEventListener("click", onClick, true);
    window.addEventListener("keydown", onKeydown, true);
  }

  function removeSearch(record) {
    window.clearTimeout(record.searchTimer);
    record.searchTimer = 0;
    const searchUi = record.searchUi;
    if (!searchUi) return;
    for (const [element, type, handler] of searchUi.handlers) element.removeEventListener(type, handler);
    searchUi.element.remove();
    record.searchUi = null;
  }

  function closeSearch(record) {
    record.search = { open: false, query: "", index: 0, filter: false, composing: false };
    removeSearch(record);
    refresh(record);
    if (record.bar) record.bar.buttons.search.focus({ preventScroll: true });
  }

  function updateSearchQuery(record) {
    window.clearTimeout(record.searchTimer);
    record.searchTimer = 0;
    if (!record.searchUi || record.search.composing) return;
    const query = record.searchUi.input.value;
    if (record.search.query !== query) {
      record.search.query = query;
      record.search.index = 0;
    }
  }

  function scheduleSearch(record) {
    window.clearTimeout(record.searchTimer);
    record.searchTimer = window.setTimeout(() => {
      record.searchTimer = 0;
      if (!state.active || !record.table.isConnected || !record.searchUi || record.search.composing) return;
      updateSearchQuery(record);
      record.search.index = 0;
      write(() => refresh(record));
    }, 150);
  }

  function moveSearch(record, direction) {
    if (!state.active || !record.table.isConnected || record.search.composing) return;
    updateSearchQuery(record);
    write(() => {
      const parts = tableParts(record.table);
      const matched = searchMatches(parts.elements.map((cell) => cell.textContent), record.search.query);
      const index = record.search.index < matched.length ? record.search.index : 0;
      record.search.index = matched.length ? (index + direction + matched.length) % matched.length : 0;
      refresh(record);
      const current = record.matches[record.search.index];
      const cell = record.elements[current];
      if (!cell || !record.layout || !record.scroll) return;
      const role = record.layout.roles[current];
      const target = role.top || role.left ? record.scroll : cell;
      target.scrollIntoView({ block: "nearest", inline: "nearest" });
    });
  }

  function createSearch(record) {
    const element = document.createElement("div");
    element.setAttribute("data-md-table-sticky-search", "");
    element.setAttribute("role", "group");
    element.setAttribute("aria-label", "表の中を検索");
    setStyles(element, {
      display: "flex", "align-items": "center", "flex-wrap": "wrap", gap: "8px", "flex-shrink": "0",
      width: "fit-content", "max-width": "100%", margin: "4px 0", padding: "2px 6px",
      "box-sizing": "border-box", "font-size": "12px", "line-height": "1.5", "font-family": "inherit",
      color: "var(--gl-text-color-subtle, #626168)", background: "var(--gl-background-color-default, #fff)",
      border: "1px solid var(--gl-border-color-default, #dcdcde)", "border-radius": "4px",
    });
    const handlers = [];
    function listen(node, type, handler) {
      node.addEventListener(type, handler);
      handlers.push([node, type, handler]);
    }
    const input = document.createElement("input");
    input.type = "search";
    input.setAttribute("data-md-table-sticky-search-input", "");
    input.setAttribute("aria-label", "表の中を検索");
    input.placeholder = "表の中を検索";
    input.value = record.search.query;
    setStyles(input, {
      "font-size": "12px", "font-family": "inherit", "line-height": "1.5", "max-width": "100%", "min-width": "0",
      color: "var(--gl-text-color-default, #333238)", background: "var(--gl-background-color-default, #fff)",
      border: "1px solid var(--gl-border-color-default, #dcdcde)", "border-radius": "3px", padding: "0 5px", margin: "0",
    });
    listen(input, "input", (event) => {
      if (event.isComposing || record.search.composing) {
        window.clearTimeout(record.searchTimer);
        record.searchTimer = 0;
        return;
      }
      scheduleSearch(record);
    });
    listen(input, "compositionstart", () => {
      record.search.composing = true;
      window.clearTimeout(record.searchTimer);
      record.searchTimer = 0;
    });
    listen(input, "compositionend", () => {
      record.search.composing = false;
      scheduleSearch(record);
    });
    function button(action, text, description, handler) {
      const node = document.createElement("button");
      node.type = "button";
      node.textContent = text;
      node.setAttribute("data-md-table-sticky-action", action);
      node.setAttribute("aria-label", description);
      node.title = description;
      setStyles(node, {
        "font-size": "12px", "font-family": "inherit", "line-height": "1.5", padding: "0 5px",
        "min-width": "22px", "min-height": "22px", margin: "0", "border-radius": "3px",
        border: "1px solid var(--gl-border-color-default, #dcdcde)",
        color: "var(--gl-text-color-default, #333238)", background: "var(--gl-background-color-default, #fff)",
      });
      listen(node, "click", handler);
      return node;
    }
    const previous = button("search-prev", "前", "前の一致へ移る（Shift+Enter）", () => moveSearch(record, -1));
    const next = button("search-next", "次", "次の一致へ移る（Enter）", () => moveSearch(record, 1));
    const count = document.createElement("span");
    count.setAttribute("data-md-table-sticky-search-count", "");
    count.setAttribute("aria-live", "polite");
    const label = document.createElement("label");
    setStyles(label, { display: "inline-flex", "align-items": "center", gap: "4px", margin: "0", "font-weight": "normal" });
    const filter = document.createElement("input");
    filter.type = "checkbox";
    filter.setAttribute("data-md-table-sticky-action", "search-filter");
    filter.setAttribute("aria-label", "一致するセルのある行だけを表示する");
    filter.title = "一致するセルのある行だけを表示する";
    setStyles(filter, { margin: "0" });
    listen(filter, "change", () => {
      updateSearchQuery(record);
      record.search.filter = filter.checked;
      write(() => refresh(record));
    });
    label.append(filter, document.createTextNode("一致する行だけ"));
    const close = button("search-close", "閉じる", "検索を閉じる（Esc）", () => write(() => closeSearch(record)));
    element.append(input, previous, next, count, label, close);
    listen(element, "keydown", (event) => {
      if (event.key === "Escape" && !state.picking) {
        event.preventDefault();
        event.stopPropagation();
        write(() => closeSearch(record));
      } else if (event.key === "Enter" && event.target === input && !event.isComposing && event.keyCode !== 229 && !record.search.composing) {
        event.preventDefault();
        event.stopPropagation();
        moveSearch(record, event.shiftKey ? -1 : 1);
      }
    });
    return { element, input, count, filter, handlers };
  }

  function syncSearch(record) {
    if (!record.search.open) {
      removeSearch(record);
      return;
    }
    if (!record.searchUi) record.searchUi = createSearch(record);
    const { element, count, filter } = record.searchUi;
    const total = record.matches.length;
    const text = !normalizeSearch(record.search.query) ? "" : total ? (record.search.index + 1) + " / " + total + " 件" : "0 件";
    if (count.textContent !== text) count.textContent = text;
    filter.checked = record.search.filter;
    element.style.setProperty("display", record.printing ? "none" : "flex", "important");
    element.style.setProperty("align-self", state.floating && state.floating.record === record ? "flex-start" : "auto", "important");
  }

  function syncControlOrder(record) {
    let next = barAnchor(record);
    const elements = [record.bar && record.bar.element, record.searchUi && record.searchUi.element, record.columns && record.columns.element].filter(Boolean);
    for (let index = elements.length - 1; index >= 0; index -= 1) {
      const element = elements[index];
      if (element.nextSibling !== next) next.before(element);
      next = element;
    }
  }

  function highlightImage(selected) {
    const color = selected ? "rgba(31, 117, 254, 0.22)" : "rgba(31, 117, 254, 0.10)";
    return "linear-gradient(" + color + ", " + color + ")";
  }

  function highlightStyles(record, index, crossed, matched) {
    const images = [];
    const styles = {};
    if (matched.has(index)) {
      const color = "rgba(255, 196, 0, 0.35)";
      images.push("linear-gradient(" + color + ", " + color + ")");
      if (record.matches[record.search.index] === index) {
        styles.outline = "2px solid rgba(255, 140, 0, 0.9)";
        styles["outline-offset"] = "-2px";
      }
    }
    if (crossed.has(index)) images.push(highlightImage(index === record.hover.index));
    if (images.length) styles["background-image"] = images.join(", ");
    return styles;
  }

  function addHighlights(record, desired) {
    const crossed = new Set(record.hover ? crossCells(record.model, record.hover.index) : []);
    const matched = new Set(record.matches);
    record.elements.forEach((element, index) => {
      const styles = desired.get(element);
      if (styles) Object.assign(styles, highlightStyles(record, index, crossed, matched));
    });
  }

  function updateHover(record, index) {
    const hover = record.hover;
    if (!hover || hover.index === index) return;
    const previous = crossCells(record.model, hover.index);
    const next = new Set(crossCells(record.model, index));
    hover.index = index;
    const matched = new Set(record.matches);
    for (const changed of new Set([...previous, ...next])) {
      const element = record.elements[changed];
      const styles = record.desired && record.desired.get(element);
      if (!styles) continue;
      delete styles["background-image"];
      delete styles.outline;
      delete styles["outline-offset"];
      Object.assign(styles, highlightStyles(record, changed, next, matched));
      applyOne(record, element, styles);
    }
  }

  function stopHover(record, reapply = true) {
    if (!record.hover) return;
    record.table.removeEventListener("pointerover", record.hover.onOver);
    record.table.removeEventListener("pointerleave", record.hover.onLeave);
    if (reapply) updateHover(record, -1);
    record.hover = null;
  }

  function syncHover(record) {
    if (!record.highlight) {
      stopHover(record, false);
      return;
    }
    if (record.hover) return;
    const onOver = (event) => {
      const cell = event.target && event.target.closest ? event.target.closest("td, th") : null;
      if (!cell || cell.closest("table") !== record.table) return;
      const index = record.elements.indexOf(cell);
      if (record.hover.index !== index) write(() => updateHover(record, index));
    };
    const onLeave = () => write(() => updateHover(record, -1));
    record.hover = { index: -1, onOver, onLeave };
    record.table.addEventListener("pointerover", onOver);
    record.table.addEventListener("pointerleave", onLeave);
  }

  function copyText(cell) {
    let text = "";
    function boundary() {
      if (text && !/\n[^\S\n]*$/.test(text)) text += "\n";
    }
    function visit(node) {
      if (node.nodeType === 3) {
        text += node.nodeValue;
        return;
      }
      if (node.nodeType !== 1 || node.tagName.toLowerCase() === "svg" || node.getAttribute("aria-hidden") === "true") return;
      if (node.tagName === "BR") {
        text += "\n";
        return;
      }
      const block = /^(P|DIV|LI)$/.test(node.tagName);
      if (block) boundary();
      for (const child of node.childNodes) visit(child);
      if (block) boundary();
    }
    for (const child of cell.childNodes) visit(child);
    const lines = text.replace(/\r\n?/g, "\n").split("\n").map((line) => line.replace(/[^\S\n]+/g, " ").trim());
    while (lines.length && !lines[0]) lines.shift();
    while (lines.length && !lines[lines.length - 1]) lines.pop();
    return lines.join("\n");
  }

  function copyFallback(text) {
    const focused = document.activeElement;
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.setAttribute("aria-label", "表を写すための一時の欄");
    setStyles(textarea, { position: "fixed", left: "0", top: "0", opacity: "0", "pointer-events": "none" });
    try {
      return write(() => {
        document.body.appendChild(textarea);
        textarea.focus({ preventScroll: true });
        textarea.select();
        return document.execCommand("copy");
      });
    } finally {
      write(() => textarea.remove());
      if (focused && focused.isConnected && focused.focus) focused.focus({ preventScroll: true });
    }
  }

  async function copyTable(record) {
    const parts = tableParts(record.table);
    const hidden = parts.rows.map((row, index) => window.getComputedStyle(row).display === "none" ? index : -1).filter((row) => row >= 0);
    const matrix = tableMatrix(parts.model, parts.elements.map(copyText), hidden);
    const tsv = toTsv(matrix);
    let copied = false;
    let html = false;
    try {
      if (navigator.clipboard && navigator.clipboard.write && typeof ClipboardItem !== "undefined") {
        await navigator.clipboard.write([new ClipboardItem({
          "text/html": new Blob([toHtml(matrix)], { type: "text/html" }),
          "text/plain": new Blob([tsv], { type: "text/plain" }),
        })]);
        copied = true;
        html = true;
      }
    } catch (error) {
      // HTML を写せないときは文字だけで試す。
    }
    if (!state.active || state.records.get(record.table) !== record || !record.table.isConnected) return;
    if (!copied) {
      try {
        if (navigator.clipboard && navigator.clipboard.writeText) {
          await navigator.clipboard.writeText(tsv);
          copied = true;
        }
      } catch (error) {
        // 文字だけでも写せないときは一時の欄で試す。
      }
    }
    if (!state.active || state.records.get(record.table) !== record || !record.table.isConnected) return;
    if (!copied) {
      try {
        copied = copyFallback(tsv);
      } catch (error) {
        // どの手段でも写せなかったことを知らせる。
      }
    }
    const size = "（" + matrix.rowCount + " 行 × " + matrix.colCount + " 列）";
    const message = copied ? (html ? "表を写した" : "表を文字だけ写した") + size : "表を写せなかった";
    write(() => {
      if (state.notification) {
        window.clearTimeout(state.notification.timer);
        state.notification.element.remove();
      }
      state.notification = notice(message);
    });
  }

  function changeCount(record, action) {
    if (!state.active || !record.table.isConnected) return;
    write(() => {
      if (action === "auto") {
        record.override.rows = null;
        record.override.cols = null;
      } else if (action === "float") {
        if (state.floating && state.floating.record === record) closeFloating();
        else openFloating(record);
        return;
      } else if (action === "search") {
        if (record.search.open) closeSearch(record);
        else {
          record.search.open = true;
          refresh(record);
          if (record.searchUi) record.searchUi.input.focus({ preventScroll: true });
        }
        return;
      } else if (action === "copy") {
        copyTable(record);
        return;
      } else if (action === "wrap-all" || action === "nowrap-all") {
        record.wrap.fill(action === "wrap-all");
      } else if (action === "scroll-x") {
        record.scrollX = record.bar.scrollX.checked;
      } else if (action === "fit-height") {
        record.fitHeight = record.bar.fitHeight.checked;
      } else if (action === "pick") {
        if (record.picking) stopPicking(record, false);
        else startPicking(record);
      } else if (action === "highlight") {
        record.highlight = record.bar.highlight.checked;
      } else if (action === "compact") {
        record.compact = record.bar.compact.checked;
      } else {
        const axis = action.startsWith("rows-") ? "rows" : "cols";
        const current = record.layout ? record.layout[axis] : 0;
        const rows = record.layout ? record.layout.rows : 0;
        const boundaries = axis === "rows" ? usableRowBoundaries(record.model) : usableColBoundaries(record.model, rows);
        const next = stepBoundary(boundaries, current, action.endsWith("plus") ? 1 : -1);
        if (next === null) return;
        record.override[axis] = next;
      }
      refresh(record);
    });
  }

  function createBar(record) {
    const element = document.createElement("div");
    element.setAttribute("data-md-table-sticky-bar", "");
    element.setAttribute("role", "group");
    element.setAttribute("aria-label", "表の表示設定");
    setStyles(element, {
      display: "flex", "align-items": "center", "flex-wrap": "wrap", gap: "8px",
      width: "fit-content", "max-width": "100%", margin: "4px 0", padding: "2px 6px",
      "font-size": "12px", "line-height": "1.5", "font-family": "inherit",
      color: "var(--gl-text-color-subtle, #626168)", background: "var(--gl-background-color-default, #fff)",
      border: "1px solid var(--gl-border-color-default, #dcdcde)", "border-radius": "4px",
    });
    const fixed = document.createElement("span");
    fixed.setAttribute("data-md-table-sticky-fixed", "");
    setStyles(fixed, { display: "inline-flex", "align-items": "center", gap: "8px" });
    const label = document.createElement("span");
    label.textContent = "固定";
    fixed.appendChild(label);
    element.appendChild(fixed);
    const buttons = {};
    const counts = {};
    function button(action, text, description) {
      const node = document.createElement("button");
      node.type = "button";
      node.textContent = text;
      node.setAttribute("data-md-table-sticky-action", action);
      node.setAttribute("aria-label", description);
      node.title = description;
      setStyles(node, {
        "font-size": "12px", "font-family": "inherit", "line-height": "1.5", padding: "0 5px",
        "min-width": "22px", "min-height": "22px", margin: "0", "border-radius": "3px",
        border: "1px solid var(--gl-border-color-default, #dcdcde)",
        color: "var(--gl-text-color-default, #333238)", background: "var(--gl-background-color-default, #fff)",
      });
      node.addEventListener("click", () => {
        if (!node.disabled) changeCount(record, action);
      });
      buttons[action] = node;
      return node;
    }
    for (const [axis, text] of [["rows", "行"], ["cols", "列"]]) {
      const group = document.createElement("span");
      setStyles(group, { display: "inline-flex", "align-items": "center", gap: "4px" });
      const name = document.createElement("span");
      name.textContent = text;
      const count = document.createElement("span");
      count.setAttribute("data-md-table-sticky-count", axis);
      counts[axis] = count;
      group.append(name, button(axis + "-minus", "−", "固定する" + text + "を 1 つ減らす"), count, button(axis + "-plus", "+", "固定する" + text + "を 1 つ増やす"));
      fixed.appendChild(group);
    }
    fixed.appendChild(button("auto", "自動", "固定する行と列を自動に戻す"));
    fixed.appendChild(button("pick", "セルで固定", "セルを選び、その上の行と左の列を固定する（Esc でやめる）"));
    const separator = document.createElement("span");
    separator.textContent = "|";
    separator.setAttribute("aria-hidden", "true");
    element.appendChild(separator);
    function checkbox(action, text, description) {
      const label = document.createElement("label");
      setStyles(label, { display: "inline-flex", "align-items": "center", gap: "4px", margin: "0", "font-weight": "normal" });
      const input = document.createElement("input");
      input.type = "checkbox";
      input.setAttribute("data-md-table-sticky-action", action);
      input.setAttribute("aria-label", description);
      input.title = description;
      setStyles(input, { margin: "0" });
      input.addEventListener("change", () => changeCount(record, action));
      label.append(input, document.createTextNode(text));
      element.appendChild(label);
      return { input, label };
    }
    const scrollX = checkbox("scroll-x", "横スクロール", "表を横スクロールする").input;
    const fitHeight = checkbox("fit-height", "枠に収める", "この表だけ枠の高さを画面の 70% に収めて行を固定する（GitLab の Show full table の設定は変えない）");
    const highlight = checkbox("highlight", "強調", "マウスを載せたセルの行と列を色付けする").input;
    const compact = checkbox("compact", "詰める", "セルの余白と文字を小さくして詰めて表示する").input;
    element.append(button("wrap-all", "すべて折り返す", "すべての列を折り返す"), button("nowrap-all", "すべて折り返さない", "すべての列を折り返さない"), button("search", "検索", "表の中を検索する"), button("copy", "写す", "表を Excel に貼れる形でクリップボードへ写す（見えている行だけ）"), button("float", "全幅で表示", "表を全幅で表示"));
    return { element, buttons, counts, fixed, separator, scrollX, fitHeight: fitHeight.input, fitHeightLabel: fitHeight.label, highlight, compact };
  }

  function syncBar(record) {
    const rows = record.layout ? record.layout.rows : 0;
    const cols = record.layout ? record.layout.cols : 0;
    const manual = record.override.rows !== null || record.override.cols !== null;
    if (record.skipped || !record.model.cells.length) {
      removeBar(record);
      return;
    }
    if (!record.bar) record.bar = createBar(record);
    const bar = record.bar;
    syncSearch(record);
    syncControlOrder(record);
    const hasFixed = record.region.rows || record.region.cols;
    bar.fixed.style.setProperty("display", hasFixed ? "inline-flex" : "none", "important");
    bar.separator.style.setProperty("display", hasFixed ? "inline" : "none", "important");
    bar.scrollX.checked = record.scrollX;
    bar.fitHeight.checked = record.fitHeight;
    bar.fitHeightLabel.style.setProperty("display", record.nativeFull ? "inline-flex" : "none", "important");
    bar.highlight.checked = record.highlight;
    bar.compact.checked = record.compact;
    bar.buttons.pick.setAttribute("aria-pressed", record.picking ? "true" : "false");
    bar.buttons.search.setAttribute("aria-pressed", record.search.open ? "true" : "false");
    const floating = state.floating && state.floating.record === record;
    bar.buttons.float.textContent = floating ? "閉じる" : "全幅で表示";
    const description = floating ? "全幅の表示を閉じる（Esc）" : "表を全幅で表示";
    bar.buttons.float.setAttribute("aria-label", description);
    bar.buttons.float.title = description;
    const rowBoundaries = usableRowBoundaries(record.model);
    const colBoundaries = usableColBoundaries(record.model, rows);
    for (const [axis, value, boundaries] of [["rows", rows, rowBoundaries], ["cols", cols, colBoundaries]]) {
      const full = axis === "rows" && record.nativeFull && !record.fitHeight;
      const suffix = record.override[axis] === null ? (full ? "（自動・全高）" : "（自動）") : (full ? "（全高）" : "");
      const text = String(value) + suffix;
      if (bar.counts[axis].textContent !== text) bar.counts[axis].textContent = text;
      bar.buttons[axis + "-minus"].disabled = stepBoundary(boundaries, value, -1) === null;
      bar.buttons[axis + "-plus"].disabled = stepBoundary(boundaries, value, 1) === null;
    }
    bar.buttons.auto.disabled = !manual;
    for (const node of Object.values(bar.buttons)) {
      setStyles(node, { opacity: node.disabled ? "0.45" : "1", cursor: node.disabled ? "default" : "pointer" });
    }
    bar.element.style.setProperty("display", record.printing ? "none" : "flex", "important");
    bar.element.style.setProperty("align-self", floating ? "flex-start" : "auto", "important");
  }

  function setScroll(record, scroll) {
    if (record.scroll === scroll) return false;
    if (record.scroll) {
      if (state.resize) state.resize.unobserve(record.scroll);
      if (record.onScroll) record.scroll.removeEventListener("scroll", record.onScroll);
    }
    record.scroll = scroll;
    record.onScroll = () => {
      if (record.columns) record.columns.element.scrollLeft = scroll.scrollLeft;
    };
    if (scroll) {
      scroll.addEventListener("scroll", record.onScroll, { passive: true });
      if (state.resize) state.resize.observe(scroll);
    }
    return true;
  }

  function removeColumns(record) {
    if (record.columns) record.columns.element.remove();
    record.columns = null;
    setScroll(record, null);
  }

  function syncColumns(record, parts, measurement) {
    if (!record.columns || record.columns.inputs.length !== parts.model.colCount) {
      if (record.columns) record.columns.element.remove();
      const element = document.createElement("div");
      element.setAttribute("data-md-table-sticky-columns", "");
      setStyles(element, { overflow: "hidden", "box-sizing": "border-box", "max-width": "100%", "flex-shrink": "0" });
      const inner = document.createElement("div");
      setStyles(inner, { display: "flex", "box-sizing": "content-box", position: "relative", "min-height": "24px" });
      const inputs = [];
      const labels = [];
      const texts = [];
      for (let col = 0; col < parts.model.colCount; col += 1) {
        const label = document.createElement("label");
        const input = document.createElement("input");
        const text = document.createElement("span");
        input.type = "checkbox";
        input.setAttribute("data-md-table-sticky-column", String(col));
        setStyles(input, { margin: "0", "flex-shrink": "0" });
        text.textContent = "折り返す";
        label.append(input, text);
        inner.appendChild(label);
        labels.push(label);
        inputs.push(input);
        texts.push(text);
        input.addEventListener("change", () => {
          if (!state.active || !record.table.isConnected) return;
          write(() => {
            record.wrap[col] = input.checked;
            refresh(record);
          });
        });
      }
      element.appendChild(inner);
      record.columns = { element, inner, inputs, labels, texts };
    }
    const columns = record.columns;
    syncControlOrder(record);
    const names = columnLabels(parts.model, parts.elements.map((cell) => cell.textContent));
    columns.inputs.forEach((input, col) => {
      const name = "列 " + (col + 1) + (names[col] ? "（" + names[col] + "）を折り返す" : " を折り返す");
      input.checked = record.wrap[col];
      input.setAttribute("aria-label", name);
      input.title = name;
      columns.labels[col].title = name;
    });
    const visible = measurement && !record.printing;
    columns.element.style.setProperty("display", visible ? "block" : "none", "important");
    columns.element.style.setProperty("align-self", state.floating && state.floating.record === record ? "flex-start" : "auto", "important");
    if (!visible) return;
    columns.element.style.setProperty("width", record.scroll.clientWidth + "px", "important");
    const left = columns.element.getBoundingClientRect().left;
    const scrollLeft = record.scroll.getBoundingClientRect().left + record.scroll.clientLeft;
    const padding = measurement.originLeft + record.scroll.scrollLeft - left;
    const widths = measurement.widths;
    setStyles(columns.inner, { width: widths.reduce((sum, width) => sum + width, 0) + "px", "padding-left": Math.max(0, padding) + "px" });
    columns.labels.forEach((label, col) => {
      const sticky = record.layout && col < record.layout.cols;
      setStyles(label, {
        display: "flex", "align-items": "center", gap: "4px", margin: "0", padding: "2px 0",
        "font-size": "12px", "font-weight": "normal", "line-height": "20px", "box-sizing": "border-box",
        flex: "0 0 " + widths[col] + "px", overflow: "hidden", position: sticky ? "sticky" : "relative",
        left: sticky ? scrollLeft - left + measurement.colOffsets[col] + "px" : "auto",
        "z-index": sticky ? "1" : "0", background: "var(--gl-background-color-default, " + ancestorBackground(record.scroll) + ")",
      });
      columns.texts[col].style.setProperty("display", widths[col] >= 64 ? "inline" : "none", "important");
    });
    columns.element.scrollLeft = record.scroll.scrollLeft;
  }

  function openFloating(record) {
    if (state.floating) closeFloating();
    const anchor = barAnchor(record);
    const barRect = record.bar.element.getBoundingClientRect();
    const anchorRect = anchor.getBoundingClientRect();
    const placeholder = document.createElement("div");
    placeholder.setAttribute("data-md-table-sticky-placeholder", "");
    placeholder.textContent = "この表は全幅で表示している（Esc で戻す）";
    setStyles(placeholder, {
      height: Math.max(0, anchorRect.bottom - barRect.top) + "px", "box-sizing": "border-box",
      margin: window.getComputedStyle(record.bar.element).marginTop + " 0 " + window.getComputedStyle(anchor).marginBottom,
      display: "flex", "align-items": "center", "justify-content": "center", "font-size": "12px",
      border: "1px dashed var(--gl-border-color-default, #dcdcde)",
    });
    record.bar.element.before(placeholder);
    const overlay = document.createElement("div");
    overlay.setAttribute("data-md-table-sticky-float", "");
    overlay.setAttribute("role", "dialog");
    overlay.setAttribute("aria-modal", "true");
    overlay.setAttribute("aria-label", "表を全幅で表示");
    setStyles(overlay, { position: "fixed", inset: "0", "z-index": "2147483646", background: "rgba(0, 0, 0, 0.4)" });
    const panel = document.createElement("div");
    panel.className = "md";
    panel.setAttribute("data-md-table-sticky-float-panel", "");
    setStyles(panel, {
      position: "absolute", left: "8px", right: "8px", top: "8px", width: "auto", "max-width": "none", margin: "0", "max-height": "calc(100vh - 16px)",
      "box-sizing": "border-box", display: "flex", "flex-direction": "column", padding: "8px", overflow: "hidden",
      background: "var(--gl-background-color-default, " + ancestorBackground(record.scroll) + ")",
      color: "var(--gl-text-color-default, #333238)", border: "1px solid var(--gl-border-color-default, #dcdcde)",
      "border-radius": "6px", "box-shadow": "0 8px 32px rgba(0, 0, 0, 0.3)",
    });
    overlay.appendChild(panel);
    document.body.appendChild(overlay);
    panel.appendChild(record.bar.element);
    if (record.searchUi) panel.appendChild(record.searchUi.element);
    if (record.columns) panel.appendChild(record.columns.element);
    panel.appendChild(anchor);
    const keydown = (event) => {
      if (event.key !== "Escape" || state.picking) return;
      if (record.searchUi && record.searchUi.element.contains(event.target)) return;
      event.preventDefault();
      event.stopPropagation();
      write(() => closeFloating());
    };
    state.floating = { record, placeholder, overlay, panel, keydown };
    window.addEventListener("keydown", keydown, true);
    overlay.addEventListener("click", (event) => {
      if (event.target === overlay) write(() => closeFloating());
    });
    refresh(record);
    record.bar.buttons.float.focus({ preventScroll: true });
  }

  function closeFloating(restore = true, remeasure = true) {
    const floating = state.floating;
    if (!floating) return;
    const { record, placeholder, overlay, keydown } = floating;
    state.floating = null;
    window.removeEventListener("keydown", keydown, true);
    if (restore && placeholder.isConnected) {
      const anchor = barAnchor(record);
      placeholder.before(record.bar.element);
      if (record.searchUi) placeholder.before(record.searchUi.element);
      if (record.columns) placeholder.before(record.columns.element);
      placeholder.before(anchor);
    }
    placeholder.remove();
    overlay.remove();
    if (restore && record.table.isConnected) {
      if (remeasure) refresh(record);
      if (record.bar) record.bar.buttons.float.focus({ preventScroll: true });
    }
  }

  function measureLayout(parts) {
    const saved = parts.elements.map((cell) => cell.getAttribute("style"));
    try {
      // 固定を外した同じ配置から、行・列とチェックボックスの位置を測る。
      for (const cell of parts.elements) cell.style.setProperty("position", "static", "important");
      const rects = parts.elements.map((cell) => cell.getBoundingClientRect());
      const tableRect = parts.elements[0].closest("table").getBoundingClientRect();
      const shown = rects.map((rect) => rect.width > 0 && rect.height > 0);
      const first = rects.findIndex((rect, index) => shown[index]);
      const origin = first >= 0 ? rects[first] : tableRect;
      const rowRects = parts.rows.map((row) => row.getBoundingClientRect());
      const rowOffsets = rowRects.map((rect) => rect.top - origin.top);
      const rowEnds = rowRects.map((rect) => rect.bottom - origin.top);
      const colOffsets = Array(parts.model.colCount).fill(null);
      parts.model.cells.forEach((cell, index) => {
        if (shown[index] && colOffsets[cell.col] === null) colOffsets[cell.col] = rects[index].left - origin.left;
      });
      const right = tableRect.right - origin.left;
      for (let col = 0; col < colOffsets.length; col += 1) {
        if (colOffsets[col] !== null) continue;
        const index = parts.model.cells.findIndex((cell, cellIndex) => shown[cellIndex] && cell.col <= col && col < cell.col + cell.colspan);
        if (index >= 0) {
          const cell = parts.model.cells[index];
          colOffsets[col] = rects[index].left - origin.left + rects[index].width * (col - cell.col) / cell.colspan;
        } else {
          // 表示中のセルがない列だけ、前後の境目から補間する。
          let next = col + 1;
          while (next < colOffsets.length && colOffsets[next] === null) next += 1;
          const left = col > 0 ? colOffsets[col - 1] : 0;
          const end = next < colOffsets.length ? colOffsets[next] : right;
          colOffsets[col] = left + (end - left) / (next - col + 1);
        }
      }
      const widths = colOffsets.map((left, col) => (col + 1 < colOffsets.length ? colOffsets[col + 1] : right) - left);
      const paddings = parts.elements.map((cell) => parseFloat(window.getComputedStyle(cell).paddingLeft) || 0);
      return { rowOffsets, rowEnds, colOffsets, paddings, widths, originLeft: origin.left };
    } finally {
      parts.elements.forEach((cell, index) => restoreStyle(cell, saved[index]));
    }
  }

  function measureMaxWidths(record, parts) {
    const position = { left: record.scroll.scrollLeft, top: record.scroll.scrollTop };
    const elements = Array.from(new Set([record.table, record.scroll, barAnchor(record)]));
    const saved = elements.map((element) => element.getAttribute("style"));
    try {
      for (const element of elements) setStyles(element, { width: "max-content", "max-width": "none" });
      return measureLayout(parts).widths;
    } finally {
      elements.forEach((element, index) => restoreStyle(element, saved[index]));
      record.scroll.scrollLeft = position.left;
      record.scroll.scrollTop = position.top;
    }
  }

  function nativeOverride(record, cell, element, rows) {
    const base = record.base.get(element);
    if (base && base.nativeTop && (rows || cell.row !== 0)) return { position: "static" };
    return null;
  }

  function rememberSizes(record) {
    for (const element of [record.table, record.scroll]) {
      const rect = element.getBoundingClientRect();
      record.sizes.set(element, { width: rect.width, height: rect.height });
    }
  }

  function refresh(record) {
    record.sizes.clear();
    if (!state.active || !record.table.isConnected) return;
    if (!eligible(record.table)) {
      cleanup(record);
      state.records.delete(record.table);
      state.processed.delete(record.table);
      return;
    }
    const parts = tableParts(record.table);
    const hovered = record.hover && record.elements ? record.elements[record.hover.index] : null;
    record.model = parts.model;
    record.elements = parts.elements;
    if (record.hover) record.hover.index = hovered ? parts.elements.indexOf(hovered) : -1;
    record.region = frozenRegion(parts.model);
    if (record.override.rows !== null) record.override.rows = lowerBoundary(usableRowBoundaries(parts.model), record.override.rows);
    record.matches = record.search.open ? searchMatches(parts.elements.map((cell) => cell.textContent), record.search.query) : [];
    if (record.search.index >= record.matches.length) record.search.index = 0;
    const keepRows = Math.max(headerRowCount(parts.model), record.override.rows === null ? record.region.rows : record.override.rows);
    record.hiddenRows = record.search.open && record.search.filter && normalizeSearch(record.search.query) ? filterRows(parts.model, record.matches, keepRows) : [];
    record.skipped = parts.model.overlap || (parts.model.cells.length > 0 && parts.model.grid[0][0] === undefined);
    const currentCells = new Set(parts.elements);
    const currentRows = new Set(parts.rows);
    for (const [cell, inner] of record.inners) {
      if (!currentCells.has(cell) || inner.parentNode !== cell || cell.childNodes.length !== 1 || cell.firstChild !== inner) unwrapInner(record, cell);
    }
    const currentInners = new Set(record.inners.values());
    const anchorBefore = barAnchor(record);
    for (const element of record.originals.keys()) {
      if (element === record.table || element === record.wrapper || element === record.scroll || element === anchorBefore || currentCells.has(element) || currentRows.has(element) || currentInners.has(element)) continue;
      restoreStyle(element, record.originals.get(element));
      record.applied.delete(element);
      record.originals.delete(element);
      record.base.delete(element);
    }
    if (record.skipped || !parts.model.cells.length) {
      stopPicking(record, false);
      stopHover(record, false);
      record.desired = new Map();
      if (state.floating && state.floating.record === record) closeFloating(state.floating.placeholder.isConnected, false);
      for (const [cell] of record.inners) unwrapInner(record, cell);
      apply(record, new Map());
      removeColumns(record);
      unwrapContainer(record);
      record.layout = null;
      removeSearch(record);
      removeBar(record);
      return;
    }
    const scroll = scrollContainer(record);
    const scrollChanged = setScroll(record, scroll);
    const scrollPosition = { left: scroll.scrollLeft, top: scroll.scrollTop };
    const rebuilt = ensureBorders(record, parts);
    if (scrollChanged && !rebuilt) updateBase(record, parts.elements);
    if (!record.wrap || record.wrap.length !== parts.model.colCount) {
      record.wrap = initialWrap(parts.model, parts.elements.map((cell) => record.base.get(cell).nowrap));
    }
    const floating = state.floating && state.floating.record === record;
    updateNativeFull(record, floating);
    syncHover(record);
    const desired = new Map();
    const tableStyles = { display: "table", margin: "0", overflow: "visible" };
    if (record.picking) tableStyles.cursor = "crosshair";
    if (record.scrollX) tableStyles["max-width"] = "none";
    const scrollStyles = { overflow: "auto" };
    if (record.nativeFull && record.fitHeight && !record.printing && !floating) scrollStyles["max-height"] = "70vh";
    if (record.wrapper) Object.assign(scrollStyles, { width: "fit-content", "max-width": "100%", "max-height": record.printing ? "none" : "70vh", margin: "16px 0" });
    if (floating) Object.assign(scrollStyles, { "max-height": "none", "min-height": "0", flex: "0 1 auto", "align-self": "flex-start", margin: "0", "overscroll-behavior": "contain" });
    desired.set(scroll, scrollStyles);
    const anchor = barAnchor(record);
    if (anchor !== scroll && floating) {
      desired.set(anchor, { display: "flex", "flex-direction": "column", "max-height": "none", "min-height": "0", flex: "0 1 auto", "align-self": "flex-start", margin: "0", "overscroll-behavior": "contain", overflow: "hidden" });
    }
    desired.set(record.table, { ...tableStyles, "border-collapse": "separate", "border-spacing": "0" });
    for (const row of record.hiddenRows) desired.set(parts.rows[row], { display: "none" });
    const modes = cellWrapModes(parts.model, record.wrap);
    const wrapStyles = modes.map((mode) => mode === "nowrap" ? { "white-space": "nowrap" } : { "white-space": "normal", "overflow-wrap": "anywhere" });
    const compactStyles = record.compact ? { padding: "2px 6px", "line-height": "1.3", "font-size": "0.875em" } : {};
    parts.elements.forEach((element, index) => desired.set(element, { ...record.borderCache.styles[index], ...wrapStyles[index], ...compactStyles }));
    addHighlights(record, desired);
    apply(record, desired);
    record.desired = desired;
    if (record.scrollX) {
      const maxWidths = measureMaxWidths(record, parts);
      const cap = (parseFloat(window.getComputedStyle(record.table).fontSize) || 16) * 12;
      const minimums = columnMinWidths(maxWidths, cap);
      parts.model.cells.forEach((cell, index) => {
        if (cell.colspan !== 1 || modes[index] !== "wrap") return;
        const element = parts.elements[index];
        const computed = window.getComputedStyle(element);
        const extras = ["padding-left", "padding-right", "border-left-width", "border-right-width"].reduce((sum, name) => sum + (parseFloat(computed.getPropertyValue(name)) || 0), 0);
        desired.get(element)["min-width"] = Math.max(0, minimums[cell.col] - (computed.boxSizing === "border-box" ? 0 : extras)) + "px";
      });
      apply(record, desired);
    }
    scroll.scrollLeft = scrollPosition.left;
    scroll.scrollTop = scrollPosition.top;
    const visible = record.table.getBoundingClientRect();
    if (!visible.width || !visible.height) {
      record.layout = null;
      syncBar(record);
      syncColumns(record, parts, null);
      return;
    }
    let measurement = measureLayout(parts);
    let { rowOffsets, colOffsets, paddings } = measurement;
    let rows = record.override.rows === null ? record.region.rows : record.override.rows;
    if (record.override.rows === null && rows && measurement.rowEnds[rows - 1] > scroll.clientHeight / 2) rows = 0;
    let cols;
    if (record.override.cols === null) {
      cols = frozenRegion(parts.model, rows).cols;
    } else {
      cols = lowerBoundary(usableColBoundaries(parts.model, rows), record.override.cols);
      record.override.cols = cols;
    }
    if (record.override.cols === null && cols && colOffsets[cols] > scroll.clientWidth / 2) cols = 0;
    const roles = cellRoles(parts.model, rows, cols);
    const fixedHeight = rows ? measurement.rowEnds[rows - 1] : 0;
    const fixedWidth = cols ? colOffsets[cols] : 0;
    record.layout = { rows, cols, rowOffsets, colOffsets, roles, fixedHeight, fixedWidth };
    if (!rows && !cols) {
      for (const [cell] of record.inners) unwrapInner(record, cell);
      desired.set(record.table, tableStyles);
      parts.model.cells.forEach((cell, index) => {
        const element = parts.elements[index];
        const minimum = desired.get(element)["min-width"];
        const styles = { ...wrapStyles[index], ...compactStyles, ...nativeOverride(record, cell, element, 0) };
        if (minimum) styles["min-width"] = minimum;
        desired.set(element, styles);
      });
      addHighlights(record, desired);
      apply(record, desired);
      record.desired = desired;
      setScrollPadding(scroll, null, null);
      scroll.scrollLeft = scrollPosition.left;
      scroll.scrollTop = scrollPosition.top;
      measurement = measureLayout(parts);
      Object.assign(record.layout, { rowOffsets: measurement.rowOffsets, colOffsets: measurement.colOffsets });
      syncBar(record);
      syncColumns(record, parts, measurement);
      rememberSizes(record);
      return;
    }
    const background = "var(--gl-background-color-default, " + ancestorBackground(scroll) + ")";
    parts.model.cells.forEach((cell, index) => {
      const element = parts.elements[index];
      const role = roles[index];
      const styles = desired.get(element);
      if (role.top || role.left) {
        styles.position = "sticky";
        styles["z-index"] = role.top ? (role.left ? "3" : "2") : "1";
        const base = record.base.get(element);
        if (role.top) styles.top = rowOffsets[cell.row] + "px";
        if (role.left) {
          styles.left = colOffsets[cell.col] + "px";
          if (!role.top) {
            // 高さの上限で行を外したときも、GitLab 自身の先頭行は残す。
            const nativeHeader = !rows && cell.row === 0 && base.nativeTop;
            styles.top = nativeHeader ? base.top : "auto";
            if (nativeHeader) styles["z-index"] = "3";
          }
        }
        if (base.transparent) styles["background-color"] = background;
        // GitLab は、上下の罫線が 0 になる境目の線を box-shadow で描く。付け直した下の罫線があるときだけ消す。
        if (record.borderCache.values[index].bottom.width > 0) styles["box-shadow"] = "none";
      } else {
        // 本文の th に GitLab が付けた固定を外す。行 0 を残す扱いは変えない。
        Object.assign(styles, nativeOverride(record, cell, element, rows));
      }
      const vertical = !role.top && (role.left || role.cross) && cell.rowspan > 1;
      const horizontal = role.top && !role.left && cell.colspan > 1;
      if (role.cross || vertical || horizontal) {
        let inner = record.inners.get(element);
        if (!inner) {
          inner = document.createElement("span");
          inner.setAttribute("data-md-table-sticky-inner", "");
          while (element.firstChild) inner.appendChild(element.firstChild);
          element.appendChild(inner);
          record.inners.set(element, inner);
        }
        const innerStyles = { display: "inline-block", position: "sticky" };
        if (vertical) innerStyles.top = fixedHeight + "px";
        if (role.cross) innerStyles.left = colOffsets[cell.col] + paddings[index] + "px";
        else if (horizontal) innerStyles.left = fixedWidth + paddings[index] + "px";
        desired.set(inner, innerStyles);
      } else {
        unwrapInner(record, element);
      }
    });
    apply(record, desired);
    record.desired = desired;
    setScrollPadding(scroll, rows ? fixedHeight : null, cols ? fixedWidth : null);
    scroll.scrollLeft = scrollPosition.left;
    scroll.scrollTop = scrollPosition.top;
    syncBar(record);
    syncColumns(record, parts, measurement);
    rememberSizes(record);
  }

  function cleanup(record) {
    record.sizes.clear();
    stopPicking(record, false);
    stopHover(record, false);
    record.desired = null;
    if (state.floating && state.floating.record === record) closeFloating(state.floating.placeholder.isConnected, false);
    removeColumns(record);
    removeSearch(record);
    removeBar(record);
    if (state.resize) {
      state.resize.unobserve(record.table);
      if (record.scroll) state.resize.unobserve(record.scroll);
    }
    for (const [cell] of record.inners) unwrapInner(record, cell);
    for (const [element, original] of record.originals) restoreStyle(element, original);
    unwrapContainer(record);
    state.pending.delete(record);
    state.dirty.delete(record.table);
  }

  function eligible(table) {
    const nearest = table.parentElement && table.parentElement.closest("table, .md");
    return nearest && nearest.matches(".md") && !table.closest(".code") && !table.hasAttribute("data-table-fields");
  }

  function scan() {
    if (!state.active) return;
    write(() => {
      if (state.floating && !state.floating.placeholder.isConnected) {
        const record = state.floating.record;
        cleanup(record);
        state.records.delete(record.table);
        state.processed.delete(record.table);
      }
      window.clearTimeout(state.timer);
      state.timer = 0;
      state.firstMutationAt = null;
      const dirty = new Set(state.dirty);
      state.dirty.clear();
      for (const [table, record] of state.records) {
        if (!table.isConnected || !eligible(table)) {
          cleanup(record);
          state.records.delete(table);
          state.processed.delete(table);
        }
      }
      for (const table of document.querySelectorAll(".md table")) {
        if (!eligible(table)) continue;
        if (!state.processed.has(table)) {
          const record = {
            table, wrapper: null, scroll: null, sizes: new Map(), originals: new Map(), applied: new Map(),
            inners: new Map(), base: new Map(), borderCache: null, override: { rows: null, cols: null }, bar: null, columns: null, wrap: null, scrollX: false, onScroll: null, printing: false, skipped: false,
            nativeFull: false, fitHeight: false, picking: false, highlight: false, compact: false, hover: null, elements: [], desired: null,
            search: { open: false, query: "", index: 0, filter: false, composing: false }, searchUi: null, searchTimer: 0, matches: [], hiddenRows: [],
          };
          state.records.set(table, record);
          state.processed.add(table);
          if (state.resize) state.resize.observe(table);
          dirty.add(table);
        }
        if (dirty.has(table)) refresh(state.records.get(table));
      }
    });
  }

  function requestRefresh(record) {
    state.pending.add(record);
    if (state.frame) return;
    state.frame = window.requestAnimationFrame(() => {
      state.frame = 0;
      const records = Array.from(state.pending);
      state.pending.clear();
      write(() => records.forEach(refresh));
    });
  }

  function printMode(printing) {
    write(() => {
      if (printing && state.floating) closeFloating(true, false);
      for (const record of state.records.values()) {
        record.printing = printing;
        refresh(record);
      }
    });
  }
  const beforePrint = () => printMode(true);
  const afterPrint = () => printMode(false);
  const tableExpanded = () => {
    for (const record of state.records.values()) requestRefresh(record);
  };

  state.observer = new MutationObserver(scheduleScan);
  if (typeof ResizeObserver !== "undefined") {
    state.resize = new ResizeObserver((entries) => {
      const targets = new Map(entries.map((entry) => [entry.target, entry]));
      for (const record of state.records.values()) {
        for (const element of [record.table, record.scroll]) {
          const entry = targets.get(element);
          if (!entry) continue;
          const size = record.sizes.get(element);
          const box = entry.borderBoxSize && entry.borderBoxSize[0];
          const width = box ? box.inlineSize : entry.contentRect.width;
          const height = box ? box.blockSize : entry.contentRect.height;
          const same = size && Math.abs(width - size.width) <= 0.5 && Math.abs(height - size.height) <= 0.5;
          if (!same) {
            requestRefresh(record);
            break;
          }
        }
      }
    });
  }
  state.stop = () => {
    state.active = false;
    state.observer.disconnect();
    if (state.resize) state.resize.disconnect();
    window.clearTimeout(state.timer);
    state.timer = 0;
    state.firstMutationAt = null;
    window.cancelAnimationFrame(state.frame);
    window.removeEventListener("beforeprint", beforePrint);
    window.removeEventListener("afterprint", afterPrint);
    window.removeEventListener("gl-markdown-table-expanded", tableExpanded);
    if (state.notification) {
      window.clearTimeout(state.notification.timer);
      state.notification.element.remove();
    }
    write(() => {
      if (state.picking) stopPicking(state.picking, false);
      if (state.floating) closeFloating(true, false);
      for (const record of state.records.values()) cleanup(record);
    });
    state.records.clear();
    state.pending.clear();
    state.dirty.clear();
  };
  window.addEventListener("beforeprint", beforePrint);
  window.addEventListener("afterprint", afterPrint);
  window.addEventListener("gl-markdown-table-expanded", tableExpanded);
  scan();
  const skipped = Array.from(state.records.values()).filter((record) => record.skipped).length;
  const count = state.records.size - skipped;
  let message = count ? "表の固定: " + count + " 個の表" : "固定する表が見つからない（GitLab の Markdown の表が対象）";
  if (skipped) message += "（" + skipped + " 個は固定しなかった）";
  write(() => { state.notification = notice(message); });
})();
