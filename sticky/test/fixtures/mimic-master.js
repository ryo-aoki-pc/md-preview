(function () {
  "use strict";
  // GitLab master の資料にある描き直しを、Vue を使わずに再現する。
  // 元のセルを移し、行と表の包みは新しく作る。
  const renderedTables = new Set();
  const tableRecords = new WeakMap();
  let isExpanded = false;
  let nextTableId = 0;
  // Vue の :class と同じく、全高でないときだけ高さの上限の class を先頭に置く。
  const scrollClasses = () => (isExpanded ? "" : "gl-max-h-[70vh] ") + "gl-w-fit gl-max-w-full gl-overflow-x-auto gl-overflow-y-auto print:gl-max-h-none";
  function updateHeightToggle(record) {
    const clipped = record.table.offsetHeight > window.innerHeight * 0.7;
    if (clipped === Boolean(record.toggle)) return;
    if (!clipped) {
      record.toggle.remove();
      record.toggle = null;
      record.button = null;
      return;
    }
    const toggle = document.createElement("div");
    toggle.className = "gl-mt-2 gl-flex gl-justify-center print:gl-hidden";
    const button = document.createElement("button");
    button.type = "button";
    button.setAttribute("data-testid", "markdown-table-height-toggle");
    button.setAttribute("aria-controls", record.scroll.id);
    button.setAttribute("aria-expanded", String(isExpanded));
    button.textContent = isExpanded ? "Collapse table" : "Show full table";
    button.addEventListener("click", () => {
      window.dispatchEvent(new CustomEvent("gl-markdown-table-expanded", { detail: { isExpanded: !isExpanded } }));
    });
    toggle.append(button);
    record.outer.append(toggle);
    record.toggle = toggle;
    record.button = button;
  }
  window.addEventListener("gl-markdown-table-expanded", (event) => {
    isExpanded = event.detail.isExpanded;
    for (const scroll of document.querySelectorAll("[data-sticky-header]")) {
      scroll.className = scrollClasses();
    }
    for (const record of renderedTables) {
      if (!record.button) continue;
      record.button.textContent = isExpanded ? "Collapse table" : "Show full table";
      record.button.setAttribute("aria-expanded", String(isExpanded));
    }
  });
  const heightObserver = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const record = tableRecords.get(entry.target);
      if (record) updateHeightToggle(record);
    }
  });
  window.addEventListener("resize", () => {
    for (const record of renderedTables) updateHeightToggle(record);
  });
  const allHeader = (row) => row.cells.length > 0 && Array.from(row.cells).every((cell) => cell.tagName === "TH");
  const hasSpan = (row) => Array.from(row.cells).some((cell) => cell.rowSpan !== 1 || cell.colSpan !== 1);
  function rowsInto(section, rows) {
    for (const row of rows) {
      const next = section.insertRow();
      next.append(...Array.from(row.cells));
    }
  }
  function makeHeader(table, row, bodyRows) {
    const head = table.createTHead().insertRow();
    Array.from(row.cells).forEach((cell, index) => {
      const th = document.createElement("th");
      for (const name of ["align", "style"]) {
        if (cell.hasAttribute(name)) th.setAttribute(name, cell.getAttribute(name));
      }
      th.setAttribute("aria-sort", "none");
      th.setAttribute("scope", "col");
      th.className = "gl-group/markdown-table-header";
      const holder = document.createElement("div");
      holder.className = "gl-flex gl-items-center gl-gap-2";
      const button = document.createElement("button");
      button.type = "button";
      const content = document.createElement("span");
      content.append(...Array.from(cell.childNodes));
      button.append(content);
      holder.append(button);
      th.append(holder);
      head.append(th);
      let descending = false;
      button.addEventListener("click", () => {
        const sorted = Array.from(table.tBodies[0].rows).sort((a, b) => a.cells[index].textContent.localeCompare(b.cells[index].textContent, "ja", { numeric: true }));
        if (descending) sorted.reverse();
        descending = !descending;
        th.setAttribute("aria-sort", descending ? "ascending" : "descending");
        table.tBodies[0].append(...sorted);
      });
    });
  }
  function render(table) {
    if (table.hasAttribute("data-markdown-table-applied") || table.hasAttribute("data-table-fields")) return;
    if (!table.parentElement || table.parentElement.closest("table, .md")?.matches(".md") !== true) return;
    const heads = Array.from(table.tHead?.rows || []);
    const bodies = Array.from(table.tBodies, (body) => Array.from(body.rows));
    const feet = Array.from(table.tFoot?.rows || []);
    const first = bodies[0] || [];
    let leading = 0;
    while (leading < first.length && allHeader(first[leading])) leading++;
    const headerRows = [...heads, ...first.slice(0, leading)];
    const sortable = headerRows.length === 1 && bodies.length === 1 && first.length - leading >= 2 && first.length - leading <= 1000 && ![...heads, ...first, ...feet].some(hasSpan);
    const next = document.createElement("table");
    for (const attribute of table.attributes) next.setAttribute(attribute.name, attribute.value);
    next.setAttribute("data-markdown-table-applied", "true");
    next.setAttribute("data-print-scale-target", "");
    next.classList.add("gl-min-w-full", "gl-overflow-y-hidden");
    if (table.caption) {
      const caption = next.createCaption();
      caption.append(...Array.from(table.caption.childNodes));
    }
    if (sortable) {
      const rows = first.slice(leading);
      makeHeader(next, headerRows[0], rows);
      const body = next.createTBody();
      rowsInto(body, rows);
    } else {
      if (heads.length) rowsInto(next.createTHead(), heads);
      bodies.forEach((rows) => rowsInto(next.createTBody(), rows));
    }
    if (feet.length) rowsInto(next.createTFoot(), feet);
    const outer = document.createElement("div");
    outer.className = "gl-my-5 gl-w-fit gl-max-w-full";
    const overlay = document.createElement("div");
    overlay.className = "gl-table-shadow-overlay";
    overlay.setAttribute("data-testid", "table-shadow-overlay");
    if (table.hasAttribute("dir")) overlay.setAttribute("dir", table.getAttribute("dir"));
    const scroll = document.createElement("div");
    scroll.className = scrollClasses();
    scroll.id = "fixture-markdown-table-" + (++nextTableId) + "-scroll";
    scroll.setAttribute("data-sticky-header", "");
    scroll.setAttribute("data-print-scale-container", "");
    outer.append(overlay);
    overlay.append(scroll);
    scroll.append(next);
    table.replaceWith(outer);
    const record = { outer, scroll, table: next, toggle: null, button: null };
    renderedTables.add(record);
    tableRecords.set(next, record);
    updateHeightToggle(record);
    heightObserver.observe(next);
  }
  window.mimicMarkdownTables = function () {
    Array.from(document.querySelectorAll(".md table:not(.code)")).forEach(render);
  };
  window.mimicMarkdownTables();
})();
