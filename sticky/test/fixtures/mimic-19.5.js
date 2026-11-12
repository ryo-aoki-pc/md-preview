(function () {
  "use strict";
  // GitLab 19.5.0 の資料にある描き直しを再現する。
  // thead の先頭行を新しい th に移し、tbody は先頭のまとまりだけを使う。
  function rowsInto(section, rows) {
    for (const row of rows) {
      const next = section.insertRow();
      next.append(...Array.from(row.cells));
    }
  }
  function render(table) {
    if (table.dataset.markdownTableApplied === "true" || !table.parentElement) return;
    const header = table.tHead?.rows[0];
    if (!header || !header.cells.length) return;
    const bodies = Array.from(table.tBodies[0]?.rows || []);
    const feet = Array.from(table.tFoot?.rows || []);
    const next = document.createElement("table");
    next.setAttribute("data-markdown-table-applied", "true");
    next.setAttribute("data-print-scale-target", "");
    next.className = "gl-min-w-full gl-overflow-y-hidden";
    if (table.caption) next.createCaption().append(...Array.from(table.caption.childNodes));
    const head = next.createTHead().insertRow();
    const sortable = bodies.length > 1 && bodies.length <= 1000;
    Array.from(header.cells).forEach((cell, index) => {
      const th = document.createElement("th");
      for (const name of ["align", "style"]) {
        if (cell.hasAttribute(name)) th.setAttribute(name, cell.getAttribute(name));
      }
      th.setAttribute("aria-sort", "none");
      th.setAttribute("scope", "col");
      th.className = "gl-group/markdown-table-header";
      const holder = document.createElement("div");
      holder.className = "gl-flex gl-items-center gl-gap-2";
      const content = document.createElement("span");
      content.append(...Array.from(cell.childNodes));
      if (sortable) {
        const button = document.createElement("button");
        button.type = "button";
        button.append(content);
        holder.append(button);
        let descending = false;
        button.addEventListener("click", () => {
          const rows = Array.from(next.tBodies[0].rows);
          rows.sort((a, b) => (a.cells[index]?.textContent || "").localeCompare(b.cells[index]?.textContent || "", "ja", { numeric: true }));
          if (descending) rows.reverse();
          descending = !descending;
          th.setAttribute("aria-sort", descending ? "ascending" : "descending");
          next.tBodies[0].append(...rows);
        });
      } else {
        holder.append(content);
      }
      th.append(holder);
      head.append(th);
    });
    rowsInto(next.createTBody(), bodies);
    if (feet.length) rowsInto(next.createTFoot(), feet);
    const outer = document.createElement("div");
    outer.className = "gl-table-shadow-overlay gl-my-5";
    outer.setAttribute("data-testid", "table-shadow-overlay");
    const scroll = document.createElement("div");
    scroll.className = "gl-max-h-[70vh] gl-w-fit gl-max-w-full gl-overflow-x-auto gl-overflow-y-auto print:gl-max-h-none";
    scroll.setAttribute("data-sticky-header", "");
    scroll.setAttribute("data-print-scale-container", "");
    outer.append(scroll);
    scroll.append(next);
    table.replaceWith(outer);
  }
  window.mimicMarkdownTables = function () {
    Array.from(document.querySelectorAll(".md table:not(.code)")).forEach(render);
  };
  window.mimicMarkdownTables();
})();
