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

  if (typeof module === "object" && module && module.exports) {
    module.exports = { buildGrid, frozenRegion, rebuildBorders, usableRowBoundaries, usableColBoundaries, stepBoundary, headerRowCount, columnLabels, initialWrap, cellWrapModes, columnMinWidths };
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
    frame: 0, timer: 0, firstMutationAt: null, observer: null, resize: null, notification: null, floating: null,
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
      } else if (action === "wrap-all" || action === "nowrap-all") {
        record.wrap.fill(action === "wrap-all");
      } else if (action === "scroll-x") {
        record.scrollX = record.bar.scrollX.checked;
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
    const separator = document.createElement("span");
    separator.textContent = "|";
    separator.setAttribute("aria-hidden", "true");
    element.appendChild(separator);
    const scrollLabel = document.createElement("label");
    setStyles(scrollLabel, { display: "inline-flex", "align-items": "center", gap: "4px", margin: "0", "font-weight": "normal" });
    const scrollX = document.createElement("input");
    scrollX.type = "checkbox";
    scrollX.setAttribute("data-md-table-sticky-action", "scroll-x");
    scrollX.setAttribute("aria-label", "表を横スクロールする");
    scrollX.title = "表を横スクロールする";
    setStyles(scrollX, { margin: "0" });
    scrollX.addEventListener("change", () => changeCount(record, "scroll-x"));
    scrollLabel.append(scrollX, document.createTextNode("横スクロール"));
    element.append(scrollLabel, button("wrap-all", "すべて折り返す", "すべての列を折り返す"), button("nowrap-all", "すべて折り返さない", "すべての列を折り返さない"), button("float", "全幅で表示", "表を全幅で表示"));
    return { element, buttons, counts, fixed, separator, scrollX };
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
    const anchor = barAnchor(record);
    const next = record.columns ? record.columns.element : anchor;
    if (bar.element.nextSibling !== next) next.before(bar.element);
    const hasFixed = record.region.rows || record.region.cols;
    bar.fixed.style.setProperty("display", hasFixed ? "inline-flex" : "none", "important");
    bar.separator.style.setProperty("display", hasFixed ? "inline" : "none", "important");
    bar.scrollX.checked = record.scrollX;
    const floating = state.floating && state.floating.record === record;
    bar.buttons.float.textContent = floating ? "閉じる" : "全幅で表示";
    const description = floating ? "全幅の表示を閉じる（Esc）" : "表を全幅で表示";
    bar.buttons.float.setAttribute("aria-label", description);
    bar.buttons.float.title = description;
    const rowBoundaries = usableRowBoundaries(record.model);
    const colBoundaries = usableColBoundaries(record.model, rows);
    for (const [axis, value, boundaries] of [["rows", rows, rowBoundaries], ["cols", cols, colBoundaries]]) {
      const text = String(value) + (record.override[axis] === null ? "（自動）" : "");
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
    const anchor = barAnchor(record);
    if (columns.element.nextSibling !== anchor) anchor.before(columns.element);
    if (record.bar && record.bar.element.nextSibling !== columns.element) columns.element.before(record.bar.element);
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
    if (record.columns) panel.appendChild(record.columns.element);
    panel.appendChild(anchor);
    const keydown = (event) => {
      if (event.key !== "Escape") return;
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
      const origin = rects[parts.model.grid[0][0]];
      const rowOffsets = parts.rows.map((row) => row.getBoundingClientRect().top - origin.top);
      const colOffsets = Array(parts.model.colCount).fill(null);
      parts.model.cells.forEach((cell, index) => {
        if (colOffsets[cell.col] === null) colOffsets[cell.col] = rects[index].left - origin.left;
      });
      for (let col = 0; col < colOffsets.length; col += 1) {
        if (colOffsets[col] !== null) continue;
        const index = parts.model.cells.findIndex((cell) => cell.col <= col && col < cell.col + cell.colspan);
        const cell = parts.model.cells[index];
        colOffsets[col] = rects[index].left - origin.left + rects[index].width * (col - cell.col) / cell.colspan;
      }
      const right = parts.elements[0].closest("table").getBoundingClientRect().right - origin.left;
      const widths = colOffsets.map((left, col) => (col + 1 < colOffsets.length ? colOffsets[col + 1] : right) - left);
      const paddings = parts.elements.map((cell) => parseFloat(window.getComputedStyle(cell).paddingLeft) || 0);
      return { rowOffsets, colOffsets, paddings, widths, originLeft: origin.left };
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
    record.model = parts.model;
    record.region = frozenRegion(parts.model);
    record.skipped = parts.model.overlap || (parts.model.cells.length > 0 && parts.model.grid[0][0] === undefined);
    const currentCells = new Set(parts.elements);
    for (const [cell, inner] of record.inners) {
      if (!currentCells.has(cell) || inner.parentNode !== cell || cell.childNodes.length !== 1 || cell.firstChild !== inner) unwrapInner(record, cell);
    }
    const currentInners = new Set(record.inners.values());
    const anchorBefore = barAnchor(record);
    for (const element of record.originals.keys()) {
      if (element === record.table || element === record.wrapper || element === record.scroll || element === anchorBefore || currentCells.has(element) || currentInners.has(element)) continue;
      restoreStyle(element, record.originals.get(element));
      record.applied.delete(element);
      record.originals.delete(element);
      record.base.delete(element);
    }
    if (record.skipped || !parts.model.cells.length) {
      if (state.floating && state.floating.record === record) closeFloating(state.floating.placeholder.isConnected, false);
      for (const [cell] of record.inners) unwrapInner(record, cell);
      apply(record, new Map());
      removeColumns(record);
      unwrapContainer(record);
      record.layout = null;
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
    const desired = new Map();
    const tableStyles = { display: "table", margin: "0", overflow: "visible" };
    if (record.scrollX) tableStyles["max-width"] = "none";
    const scrollStyles = { overflow: "auto" };
    if (record.wrapper) Object.assign(scrollStyles, { width: "fit-content", "max-width": "100%", "max-height": record.printing ? "none" : "70vh", margin: "16px 0" });
    if (floating) Object.assign(scrollStyles, { "max-height": "none", "min-height": "0", flex: "0 1 auto", "align-self": "flex-start", margin: "0", "overscroll-behavior": "contain" });
    desired.set(scroll, scrollStyles);
    const anchor = barAnchor(record);
    if (anchor !== scroll && floating) {
      desired.set(anchor, { display: "flex", "flex-direction": "column", "max-height": "none", "min-height": "0", flex: "0 1 auto", "align-self": "flex-start", margin: "0", "overscroll-behavior": "contain", overflow: "hidden" });
    }
    desired.set(record.table, { ...tableStyles, "border-collapse": "separate", "border-spacing": "0" });
    const modes = cellWrapModes(parts.model, record.wrap);
    const wrapStyles = modes.map((mode) => mode === "nowrap" ? { "white-space": "nowrap" } : { "white-space": "normal", "overflow-wrap": "anywhere" });
    parts.elements.forEach((element, index) => desired.set(element, { ...record.borderCache.styles[index], ...wrapStyles[index] }));
    apply(record, desired);
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
    if (record.override.rows !== null) record.override.rows = lowerBoundary(usableRowBoundaries(parts.model), record.override.rows);
    let rows = record.override.rows === null ? record.region.rows : record.override.rows;
    if (record.override.rows === null && rows && rowOffsets[rows] > scroll.clientHeight / 2) rows = 0;
    let cols;
    if (record.override.cols === null) {
      cols = frozenRegion(parts.model, rows).cols;
    } else {
      cols = lowerBoundary(usableColBoundaries(parts.model, rows), record.override.cols);
      record.override.cols = cols;
    }
    if (record.override.cols === null && cols && colOffsets[cols] > scroll.clientWidth / 2) cols = 0;
    const roles = cellRoles(parts.model, rows, cols);
    const fixedHeight = rows ? rowOffsets[rows] : 0;
    const fixedWidth = cols ? colOffsets[cols] : 0;
    record.layout = { rows, cols, rowOffsets, colOffsets, roles, fixedHeight, fixedWidth };
    if (!rows && !cols) {
      for (const [cell] of record.inners) unwrapInner(record, cell);
      desired.set(record.table, tableStyles);
      parts.model.cells.forEach((cell, index) => {
        const element = parts.elements[index];
        const minimum = desired.get(element)["min-width"];
        const styles = { ...wrapStyles[index], ...nativeOverride(record, cell, element, 0) };
        if (minimum) styles["min-width"] = minimum;
        desired.set(element, styles);
      });
      apply(record, desired);
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
    syncBar(record);
    syncColumns(record, parts, measurement);
    rememberSizes(record);
  }

  function cleanup(record) {
    record.sizes.clear();
    if (state.floating && state.floating.record === record) closeFloating(state.floating.placeholder.isConnected, false);
    removeColumns(record);
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
    if (state.notification) {
      window.clearTimeout(state.notification.timer);
      state.notification.element.remove();
    }
    write(() => {
      if (state.floating) closeFloating(true, false);
      for (const record of state.records.values()) cleanup(record);
    });
    state.records.clear();
    state.pending.clear();
    state.dirty.clear();
  };
  window.addEventListener("beforeprint", beforePrint);
  window.addEventListener("afterprint", afterPrint);
  scan();
  const skipped = Array.from(state.records.values()).filter((record) => record.skipped).length;
  const count = state.records.size - skipped;
  let message = count ? "表の固定: " + count + " 個の表" : "固定する表が見つからない（GitLab の Markdown の表が対象）";
  if (skipped) message += "（" + skipped + " 個は固定しなかった）";
  write(() => { state.notification = notice(message); });
})();
