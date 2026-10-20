import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { access, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import test from "node:test";
import { setTimeout as delay } from "node:timers/promises";

const root = fileURLToPath(new URL("../", import.meta.url));
const fixtures = path.join(root, "test", "fixtures");

// PATH を調べるだけにし、シェルや外部の探査コマンドは使わない。
async function executable(candidate) {
  try {
    await access(candidate, process.platform === "win32" ? constants.F_OK : constants.X_OK);
    return candidate;
  } catch {
    return null;
  }
}

async function findBrowser() {
  const names = ["chromium", "chromium-browser", "google-chrome", "microsoft-edge"];
  const candidates = [
    process.env.MTS_BROWSER,
    "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
    "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe",
    "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
    "C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
  ].filter(Boolean);
  if (process.env.MTS_BROWSER && !path.isAbsolute(process.env.MTS_BROWSER)) {
    for (const directory of (process.env.PATH || "").split(path.delimiter)) {
      candidates.push(path.join(directory, process.env.MTS_BROWSER));
    }
  }
  if (process.platform !== "win32") {
    for (const directory of (process.env.PATH || "").split(path.delimiter)) {
      for (const name of names) candidates.push(path.join(directory, name));
    }
  }
  for (const candidate of candidates) {
    const found = await executable(candidate);
    if (found) return found;
  }
  return null;
}

class CDP {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 0;
    this.pending = new Map();
    socket.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (!message.id) return;
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      clearTimeout(pending.timeout);
      if (message.error) pending.reject(new Error(message.error.message));
      else pending.resolve(message.result);
    });
    socket.addEventListener("close", () => {
      for (const pending of this.pending.values()) {
        clearTimeout(pending.timeout);
        pending.reject(new Error("ブラウザーとの接続が閉じた"));
      }
      this.pending.clear();
    });
  }

  static async connect(url) {
    const socket = new WebSocket(url);
    await new Promise((resolve, reject) => {
      socket.addEventListener("open", resolve, { once: true });
      socket.addEventListener("error", () => reject(new Error("CDP に接続できない")), { once: true });
    });
    return new CDP(socket);
  }

  send(method, params = {}) {
    const id = ++this.nextId;
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error("CDP が時間内に応答しない: " + method));
      }, 15000);
      this.pending.set(id, { resolve, reject, timeout });
      this.socket.send(JSON.stringify({ id, method, params }));
    });
  }

  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
    if (result.exceptionDetails) {
      throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
    }
    return result.result.value;
  }

  close() {
    this.socket.close();
  }
}

async function removeTemporary(directory) {
  for (let attempt = 0; attempt < 20; attempt++) {
    try {
      await rm(directory, { recursive: true, force: true });
      return;
    } catch (error) {
      if (attempt === 19) throw error;
      await delay(150);
    }
  }
}

async function stopBrowser(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  await new Promise((resolve) => {
    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      resolve();
    }, 3000);
    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
    child.kill();
  });
}

async function launch(browser, directory) {
  let stderr = "";
  const child = spawn(browser, [
    "--user-data-dir=" + directory,
    "--headless=new",
    "--remote-debugging-port=0",
    "--no-first-run",
    "--no-default-browser-check",
    "--window-size=1280,900",
    "about:blank",
  ], { shell: false, windowsHide: true, stdio: ["ignore", "ignore", "pipe"] });
  let failure;
  child.on("error", (error) => { failure = error; });
  child.stderr.on("data", (data) => { stderr = (stderr + data.toString()).slice(-1600); });
  for (let attempt = 0; attempt < 120; attempt++) {
    if (failure) throw Object.assign(failure, { browserChild: child });
    if (child.exitCode !== null) break;
    try {
      const active = await readFile(path.join(directory, "DevToolsActivePort"), "utf8");
      const port = Number(active.split(/\r?\n/)[0]);
      if (port) return { child, port, diagnostics: () => stderr };
    } catch {}
    await delay(100);
  }
  await stopBrowser(child);
  throw new Error("DevToolsActivePort が作られなかった。 " + stderr.trim().slice(-800));
}

async function createPage(directory, version) {
  const [css, sample, mimic] = await Promise.all([
    readFile(path.join(fixtures, "gitlab.css"), "utf8"),
    readFile(path.join(fixtures, "sample.gitlab.html"), "utf8"),
    readFile(path.join(fixtures, "mimic-" + version + ".js"), "utf8"),
  ]);
  // 大きい表では両方向に十分なスクロール量を確保する。
  // 固定の幅や高さの計算自体は、本番と同じ CSS の矩形を使う。
  const prepare = `
    window.__fixtureOriginal = document.querySelector(".file-content").innerHTML;
    if (${JSON.stringify(version)} === "19.5") {
      const original = document.querySelector(".file-content table").cloneNode(true);
      const head = original.tHead;
      const body = document.createElement("tbody");
      body.append(...Array.from(head.rows));
      original.insertBefore(body, original.tBodies[0]);
      head.remove();
      const section = document.createElement("div");
      section.className = "md";
      section.id = "workaround";
      section.append(original);
      document.body.append(section);
      const originalC = Array.from(document.querySelectorAll(".file-content table")).filter(table => table.parentElement.closest("table, .md")?.matches(".file-content"))[2].cloneNode(true);
      const headC = originalC.tHead;
      const bodyC = document.createElement("tbody");
      bodyC.append(...Array.from(headC.rows));
      originalC.insertBefore(bodyC, originalC.tBodies[0]);
      headC.remove();
      const sectionC = document.createElement("div");
      sectionC.className = "md";
      sectionC.id = "workaround-c";
      sectionC.append(originalC);
      document.body.append(sectionC);
    }
  `;
  const finish = `
    const md = document.querySelector(".file-content");
    window.__fixtureTables = Object.fromEntries(Array.from(md.querySelectorAll("table")).filter(table => table.parentElement.closest("table, .md") === md).map((table, index) => [String.fromCharCode(65 + index), table]));
    for (const [name, table] of Object.entries(window.__fixtureTables)) {
      if (name !== "F") table.style.minWidth = "1800px";
    }
    window.__workaroundTable = document.querySelector("#workaround table");
    if (window.__workaroundTable) window.__workaroundTable.style.minWidth = "1800px";
    window.__workaroundC = document.querySelector("#workaround-c table");
    if (window.__workaroundC) window.__workaroundC.style.minWidth = "1800px";
    // 固定する行を増やすと、自動の列が 1 から 2 へ広がる表を用意する。
    const autoColumnsSection = document.createElement("div");
    autoColumnsSection.className = "md";
    autoColumnsSection.id = "auto-columns-case";
    const autoColumnsTable = document.createElement("table");
    autoColumnsTable.style.minWidth = "1800px";
    const autoColumnsBody = autoColumnsTable.createTBody();
    for (let r = 0; r < 22; r++) {
      const row = autoColumnsBody.insertRow();
      for (let c = 0; c < 3; c++) {
        if (r === 1 && c === 1) continue;
        const cell = document.createElement(r === 0 || c === 0 ? "th" : "td");
        if (r === 1 && c === 0) cell.colSpan = 2;
        cell.textContent = r === 1 && c === 0 ? "結合した行見出し" : "自動列の値 " + r + "-" + c;
        if (c === 2) cell.style.minWidth = "1400px";
        else if (cell.colSpan === 1) cell.style.minWidth = "100px";
        row.append(cell);
      }
    }
    autoColumnsSection.append(autoColumnsTable);
    document.body.append(autoColumnsSection);
    window.__autoColumnsTable = autoColumnsTable;
    window.__beforeHtml = Array.from(document.querySelectorAll(".md"), md => md.innerHTML);
    window.__nestedBefore = window.__fixtureTables.G.querySelector("table").outerHTML;
    window.__smallBefore = { text: window.__fixtureTables.F.textContent, rows: window.__fixtureTables.F.rows.length };
    window.__detailsBefore = window.__fixtureTables.H.closest("details").getAttribute("open");
    window.__fixtureReady = true;
  `;
  const html = "<!doctype html>\n<html lang=\"ja\"><meta charset=\"utf-8\"><title>表の固定の試験</title><style>" + css + "</style><div class=\"file-content js-markup-content md\">" + sample + "</div><script>" + prepare + "</script><script>" + mimic + "</script><script>" + finish + "</script></html>\n";
  const filename = path.join(directory, "fixture-" + version + ".html");
  await writeFile(filename, html, "utf8");
  return pathToFileURL(filename).href;
}

// 一時的に static へ戻して、実際の DOM の開始位置を独立に測る。
// 見出しの範囲は標本ごとの期待値を指定し、中核の計算結果には依存しない。
function prepareMeasurements(regions) {
  const measurements = {};
  for (const [name, region] of Object.entries(regions)) {
    const table = name === "回避形" ? window.__workaroundTable : window.__fixtureTables[name];
    const scroll = table.parentElement;
    scroll.scrollTop = 0;
    scroll.scrollLeft = 0;
    const rows = Array.from(table.rows);
    const cells = rows.flatMap(row => Array.from(row.cells));
    const styles = cells.map(cell => cell.getAttribute("style"));
    cells.forEach(cell => cell.style.setProperty("position", "static", "important"));
    const grid = [];
    const placed = [];
    let start = 0;
    for (const section of Array.from(table.children).filter(el => /^(THEAD|TBODY|TFOOT)$/.test(el.tagName))) {
      const sectionRows = Array.from(section.rows);
      sectionRows.forEach((row, localRow) => {
        const r = start + localRow;
        grid[r] ||= [];
        let col = 0;
        for (const cell of Array.from(row.cells)) {
          while (grid[r][col]) col++;
          const rowspan = Math.min(cell.rowSpan || sectionRows.length - localRow, sectionRows.length - localRow);
          const item = { cell, row: r, col, rowspan, colspan: cell.colSpan };
          placed.push(item);
          for (let rr = r; rr < r + rowspan; rr++) {
            grid[rr] ||= [];
            for (let cc = col; cc < col + cell.colSpan; cc++) grid[rr][cc] = item;
          }
          col += cell.colSpan;
        }
      });
      start += sectionRows.length;
    }
    const origin = grid[0][0].cell.getBoundingClientRect();
    const rowOffsets = rows.map(row => row.getBoundingClientRect().top - rows[0].getBoundingClientRect().top);
    const colOffsets = [];
    for (const item of placed) {
      colOffsets[item.col] ??= item.cell.getBoundingClientRect().left - origin.left;
      const rect = item.cell.getBoundingClientRect();
      item.before = { top: rect.top, left: rect.left };
    }
    cells.forEach((cell, index) => {
      if (styles[index] === null) cell.removeAttribute("style");
      else cell.setAttribute("style", styles[index]);
    });
    measurements[name] = { table, scroll, placed, rowOffsets, colOffsets, R: region[0], C: region[1] };
  }
  window.__measurements = measurements;
  return Object.fromEntries(Object.entries(measurements).map(([name, m]) => [name, { rows: m.table.rows.length, clientWidth: m.scroll.clientWidth, clientHeight: m.scroll.clientHeight, width: m.table.getBoundingClientRect().width }]));
}

function scrollMeasurements() {
  for (const measurement of Object.values(window.__measurements)) {
    measurement.scroll.scrollTop = 200;
    measurement.scroll.scrollLeft = 300;
  }
  return new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
}

function checkMeasurements() {
  return Object.fromEntries(Object.entries(window.__measurements).map(([name, m]) => {
    const wrap = m.scroll.getBoundingClientRect();
    const topOrigin = wrap.top + m.scroll.clientTop;
    const leftOrigin = wrap.left + m.scroll.clientLeft;
    const top = [];
    const left = [];
    const normal = [];
    for (const item of m.placed) {
      const rect = item.cell.getBoundingClientRect();
      const isTop = item.row + item.rowspan <= m.R;
      const isLeft = item.col + item.colspan <= m.C;
      const cross = item.col < m.C && m.C < item.col + item.colspan;
      if (isTop) top.push({ actual: rect.top - topOrigin, expected: m.rowOffsets[item.row], text: item.cell.textContent.slice(0, 30) });
      if (isLeft) left.push({ actual: rect.left - leftOrigin, expected: m.colOffsets[item.col], text: item.cell.textContent.slice(0, 30) });
      if (!isTop && !isLeft && !cross && item.row >= m.R + 8) {
        normal.push({ topDelta: rect.top - item.before.top, leftDelta: rect.left - item.before.left });
      }
    }
    return [name, { top, left, normal, scrollTop: m.scroll.scrollTop, scrollLeft: m.scroll.scrollLeft }];
  }));
}

// elementFromPoint は画面の中だけを調べるので、表を順に表示して確かめる。
async function checkStickyDetails(headerName) {
  const result = { topEdges: [], leftEdges: [], vertical: [], horizontal: [] };
  for (const name of [headerName, "B"]) {
    const m = window.__measurements[name];
    m.scroll.scrollIntoView({ block: "start", inline: "nearest" });
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const wrap = m.scroll.getBoundingClientRect();
    const topOrigin = wrap.top + m.scroll.clientTop;
    const leftOrigin = wrap.left + m.scroll.clientLeft;
    const bounds = {
      top: Math.max(0, topOrigin), bottom: Math.min(window.innerHeight, topOrigin + m.scroll.clientHeight),
      left: Math.max(0, leftOrigin), right: Math.min(window.innerWidth, leftOrigin + m.scroll.clientWidth),
    };
    const frozenBottom = topOrigin + (m.R ? m.rowOffsets[m.R] : 0);
    const frozenRight = leftOrigin + (m.C ? m.colOffsets[m.C] : 0);
    for (const item of m.placed) {
      const cell = item.cell;
      const rect = cell.getBoundingClientRect();
      const isTop = item.row + item.rowspan <= m.R;
      const isLeft = item.col + item.colspan <= m.C;
      if (name === headerName && isTop && item.row > 0) {
        const x1 = Math.max(rect.left, bounds.left, frozenRight);
        const x2 = Math.min(rect.right, bounds.right);
        const y = rect.top + 0.5;
        if (x2 - x1 > 2 && y >= bounds.top && y < bounds.bottom) {
          const hit = document.elementFromPoint((x1 + x2) / 2, y);
          result.topEdges.push({ text: cell.textContent.trim(), covered: hit === cell || cell.contains(hit) });
        }
      }
      if (name === "B" && isLeft && !isTop && item.col > 0) {
        const y1 = Math.max(rect.top, bounds.top, frozenBottom);
        const y2 = Math.min(rect.bottom, bounds.bottom);
        const x = rect.left + 0.5;
        if (y2 - y1 > 2 && x >= bounds.left && x < bounds.right) {
          const hit = document.elementFromPoint(x, (y1 + y2) / 2);
          result.leftEdges.push({ text: cell.textContent.trim(), covered: hit === cell || cell.contains(hit) });
        }
      }
      if (name !== headerName) continue;
      const inner = cell.querySelector(":scope > [data-md-table-sticky-inner]");
      if (isLeft && !isTop && item.rowspan > 1 && rect.top < frozenBottom && rect.bottom > frozenBottom + 40) {
        if (!inner) {
          result.vertical.push({ text: cell.textContent.trim(), wrapped: false });
          continue;
        }
        const content = inner.getBoundingClientRect();
        const range = document.createRange();
        range.selectNodeContents(inner);
        const text = range.getBoundingClientRect();
        result.vertical.push({
          text: inner.textContent.trim(), wrapped: true, top: content.top - topOrigin, expected: frozenBottom - topOrigin,
          visible: content.top >= bounds.top - 1 && content.bottom <= bounds.bottom + 1 && content.left >= bounds.left - 1 && content.right <= bounds.right + 1,
          // 文字の行の箱は、字の上下の余白の分だけ入れ物より少し大きいので、縦の中心で見る。
          textVisible: text.width > 0 && text.height > 0 && (text.top + text.bottom) / 2 >= frozenBottom && (text.top + text.bottom) / 2 <= bounds.bottom && text.right > bounds.left && text.left < bounds.right,
        });
      }
      if (isTop && !isLeft && item.colspan > 1 && /202[56] 年/.test(cell.textContent)) {
        const content = inner?.getBoundingClientRect();
        // セルの終わりに押し戻される見出しを除き、文字が残るだけの幅がある見出しを見る。
        if (rect.right > frozenRight + (content?.width || 0) + 1 && rect.left < bounds.right && rect.right > bounds.left) {
          result.horizontal.push({
            text: cell.textContent.trim(), wrapped: !!inner,
            left: content ? content.left - leftOrigin : null, expected: frozenRight - leftOrigin,
            visible: !!content && content.right > bounds.left && content.left < bounds.right && content.top >= bounds.top - 1 && content.bottom <= bounds.bottom + 1,
          });
        }
      }
    }
    if (name === "B") {
      const first = m.placed.find(item => item.col === 0 && item.row >= m.R && item.colspan === 1).cell;
      result.borders = { right: parseFloat(getComputedStyle(first).borderRightWidth), collapse: getComputedStyle(m.table).borderCollapse };
    }
  }
  return result;
}

async function checkMutationScope(cdp) {
  // 寸法を読む回数を一時的に数え、表の外の更新で測り直していないことを確かめる。
  await delay(400);
  const result = await cdp.evaluate(`(async () => {
    const tables = { A: window.__fixtureTables.A, D: window.__fixtureTables.D };
    const counts = { A: 0, D: 0 };
    const original = Element.prototype.getBoundingClientRect;
    const cache = window.__mdTableSticky.records.get(tables.D).borderCache;
    const outside = document.createElement("div");
    outside.hidden = true;
    Element.prototype.getBoundingClientRect = function () {
      for (const [name, table] of Object.entries(tables)) {
        if (this === table || table.contains(this)) counts[name]++;
      }
      return original.call(this);
    };
    try {
      document.body.append(outside);
      outside.textContent = "時刻の表示の代わり";
      await new Promise(resolve => setTimeout(resolve, 450));
      const unrelated = { ...counts };
      counts.A = 0;
      counts.D = 0;
      const cell = tables.D.tBodies[0].rows[0].cells[1];
      cell.textContent = cell.textContent;
      await new Promise(resolve => setTimeout(resolve, 450));
      return {
        unrelated, related: { ...counts }, hasCache: !!cache,
        sameCache: cache === window.__mdTableSticky.records.get(tables.D).borderCache,
      };
    } finally {
      Element.prototype.getBoundingClientRect = original;
      outside.remove();
    }
  })()`);
  assert.deepEqual(result.unrelated, { A: 0, D: 0 }, "表の外の更新では表を測り直さない");
  assert.equal(result.related.A, 0, "変化の無い表は測り直さない");
  assert.ok(result.related.D > 0, "中身が変わった表だけを測り直す");
  assert.equal(result.hasCache, true, "付け直した罫線を記録する");
  assert.equal(result.sameCache, true, "セルの並びが変わらなければ罫線を読み直さない");
}

async function checkNativeBodyHeader(cdp) {
  const result = await cdp.evaluate(`(async () => {
    const table = window.__fixtureTables.I;
    const scroll = table.parentElement;
    const row = table.tBodies[0].rows[0];
    const header = row.cells[1];
    const ordinary = row.cells[2];
    scroll.scrollTop = 0;
    const before = { header: header.getBoundingClientRect().top, ordinary: ordinary.getBoundingClientRect().top };
    scroll.scrollTop = 200;
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    return {
      headerTag: header.tagName, ordinaryTag: ordinary.tagName, position: getComputedStyle(header).position,
      priority: header.style.getPropertyPriority("position"), scrollTop: scroll.scrollTop,
      headerDelta: header.getBoundingClientRect().top - before.header, ordinaryDelta: ordinary.getBoundingClientRect().top - before.ordinary,
    };
  })()`);
  assert.equal(result.headerTag, "TH", "本文の先頭行の 2 列目は th にする");
  assert.equal(result.ordinaryTag, "TD", "動きを比べる本文のセルは td にする");
  assert.equal(result.position, "static", "固定する範囲の外の th は GitLab 自身の固定を外す");
  assert.equal(result.priority, "important", "GitLab の CSS より強く static を入れる");
  assert.equal(result.scrollTop, 200, "本文の th を縦へスクロールする");
  near(result.headerDelta, -200, "固定しない本文の th は縦へ動く");
  near(result.ordinaryDelta, -200, "比べる本文の td は縦へ動く");
  near(result.headerDelta, result.ordinaryDelta, "本文の th はほかの本文のセルと一緒に動く");
  await checkNativeFallbacks(cdp);
}

async function checkNativeFallbacks(cdp) {
  await cdp.evaluate(`(() => {
    const table = window.__fixtureTables.I;
    const scroll = table.parentElement;
    window.__nativeWrapperStyle = scroll.getAttribute("style");
    scroll.style.setProperty("width", "80px", "important");
    scroll.style.setProperty("max-height", "60px", "important");
  })()`);
  try {
    await waitFor(cdp, "(() => { const layout = window.__mdTableSticky.records.get(window.__fixtureTables.I).layout; return layout && layout.rows === 0 && layout.cols === 0; })()", "狭い範囲では行と列の固定を外す");
    const narrow = await cdp.evaluate(`(() => {
      const table = window.__fixtureTables.I;
      return { header: getComputedStyle(table.tHead.rows[0].cells[1]).position, body: getComputedStyle(table.tBodies[0].rows[0].cells[1]).position, collapse: getComputedStyle(table).borderCollapse };
    })()`);
    assert.deepEqual(narrow, { header: "sticky", body: "static", collapse: "collapse" }, "両方向を外しても行 0 の固定は残し、本文の固定だけを外す");
  } finally {
    await cdp.evaluate(`(() => {
      const scroll = window.__fixtureTables.I.parentElement;
      if (window.__nativeWrapperStyle === null) { scroll.getAttribute("style"); scroll.removeAttribute("style"); }
      else scroll.setAttribute("style", window.__nativeWrapperStyle);
    })()`);
  }
  await waitFor(cdp, "(() => { const layout = window.__mdTableSticky.records.get(window.__fixtureTables.I).layout; return layout && layout.rows === 1 && layout.cols === 1; })()", "範囲を戻した後に固定も戻す");
  await cdp.evaluate(`(() => {
    const wrapper = document.createElement("div");
    wrapper.setAttribute("data-sticky-header", "");
    const table = document.createElement("table");
    const addHeader = section => { const cell = document.createElement("th"); cell.textContent = "小さい見出し"; section.insertRow().append(cell); };
    addHeader(table.createTHead());
    addHeader(table.createTBody());
    addHeader(table.createTBody());
    wrapper.append(table);
    document.querySelector(".file-content").append(wrapper);
    window.__unfrozenNativeTable = table;
    window.__unfrozenNativeWrapper = wrapper;
  })()`);
  try {
    await waitFor(cdp, "window.__mdTableSticky.records.has(window.__unfrozenNativeTable)", "全行・全列になる表も記録する");
    const unfrozen = await cdp.evaluate(`(() => {
      const table = window.__unfrozenNativeTable;
      const record = window.__mdTableSticky.records.get(table);
      return { rows: record.region.rows, cols: record.region.cols, borders: record.borderCache, header: getComputedStyle(table.tHead.rows[0].cells[0]).position, bodies: Array.from(table.tBodies, body => getComputedStyle(body.rows[0].cells[0]).position), collapse: getComputedStyle(table).borderCollapse };
    })()`);
    assert.deepEqual(unfrozen, { rows: 0, cols: 0, borders: null, header: "sticky", bodies: ["static", "static"], collapse: "collapse" }, "全行・全列になる表は罫線を付け直さず、行 0 以外の固定を外す");
  } finally {
    await cdp.evaluate("window.__unfrozenNativeWrapper.remove()");
  }
}

async function waitFor(cdp, expression, message) {
  for (let attempt = 0; attempt < 40; attempt++) {
    if (await cdp.evaluate(expression)) return;
    await delay(50);
  }
  assert.fail(message);
}

async function checkContinuousMutations(cdp) {
  const result = await cdp.evaluate(`(async () => {
    const outside = document.createElement("div");
    outside.hidden = true;
    const table = document.createElement("table");
    table.style.minWidth = "1800px";
    const body = table.createTBody();
    for (let r = 0; r < 6; r++) {
      const row = body.insertRow();
      for (let c = 0; c < 4; c++) {
        const cell = document.createElement(r === 0 || c === 0 ? "th" : "td");
        cell.textContent = r === 0 ? "見出し " + c : "値 " + r + "-" + c;
        row.append(cell);
      }
    }
    let timer;
    let updates = 0;
    try {
      document.body.append(outside);
      // 表の外の変化が 300 ms の待ちを延ばし続ける形にする。
      await new Promise(resolve => {
        timer = setInterval(() => {
          outside.textContent = "時刻 " + (++updates);
          if (updates === 1) resolve();
        }, 100);
      });
      const started = performance.now();
      document.querySelector(".file-content").append(table);
      while (performance.now() - started < 1500) {
        const cell = table.rows[0].cells[0];
        if (window.__mdTableSticky.records.has(table) && cell.style.position === "sticky" && cell.style.getPropertyPriority("position") === "important") {
          return { fixed: true, elapsed: performance.now() - started, updates };
        }
        await new Promise(resolve => setTimeout(resolve, 20));
      }
      return { fixed: false, elapsed: performance.now() - started, updates };
    } finally {
      clearInterval(timer);
      const wrapper = table.parentElement;
      table.remove();
      if (wrapper?.hasAttribute("data-md-table-sticky-wrap")) wrapper.remove();
      outside.remove();
    }
  })()`);
  assert.equal(result.fixed, true, "表の外が 100 ms ごとに変わり続けても新しい表を 1.5 秒以内に固定する");
  assert.ok(result.elapsed < 1500, "新しい表を固定するまでの待ちを制限する");
  assert.ok(result.updates >= 3, "固定を確かめるまで表の外の更新を続ける");
}

async function checkDynamic(cdp, bookmarklet) {
  await cdp.evaluate(bookmarklet);
  assert.equal(await cdp.evaluate("Array.from(window.__mdTableSticky.records.values()).every(record => record.override.rows === null && record.override.cols === null)"), true, "解除して再実行すると選んだ数を覚えず自動から始める");
  await cdp.evaluate(`(() => {
    const table = document.createElement("table");
    table.style.minWidth = "1800px";
    const body = table.createTBody();
    const header = body.insertRow();
    for (let c = 0; c < 12; c++) {
      const cell = document.createElement("th");
      cell.textContent = "見出し " + c;
      header.append(cell);
    }
    for (let r = 1; r <= 30; r++) {
      const row = body.insertRow();
      for (let c = 0; c < 12; c++) {
        const cell = document.createElement(c === 0 ? "th" : "td");
        cell.textContent = c === 0 ? "行 " + r : "値 " + r + "-" + c;
        row.append(cell);
      }
    }
    window.__dynamicTable = table;
    document.querySelector(".file-content").append(table);
  })()`);
  await waitFor(cdp, "window.__mdTableSticky.records.has(window.__dynamicTable) && window.__dynamicTable.rows[0].cells[0].style.position === 'sticky'", "後から出た表が固定されない");
  assert.equal(await cdp.evaluate("window.__dynamicTable.parentElement.hasAttribute('data-md-table-sticky-wrap')"), true, "新しい表を自分の包みで囲む");
  await checkMutationScope(cdp);
  await checkContinuousMutations(cdp);
  const print = await cdp.evaluate(`(() => {
    const wrapper = window.__dynamicTable.parentElement;
    window.dispatchEvent(new Event("beforeprint"));
    const before = { value: wrapper.style.maxHeight, priority: wrapper.style.getPropertyPriority("max-height") };
    window.dispatchEvent(new Event("afterprint"));
    return { before, after: wrapper.style.maxHeight };
  })()`);
  assert.deepEqual(print, { before: { value: "none", priority: "important" }, after: "70vh" }, "印刷の前後で自作の包みの高さを戻す");

  await cdp.evaluate(`(() => {
    const table = window.__fixtureTables.D;
    window.__sortRows = Array.from(table.tBodies[0].rows);
    window.__sortBorderCache = window.__mdTableSticky.records.get(table).borderCache;
    window.__sortLastText = window.__sortRows.at(-1).cells[0].textContent.trim();
    window.__sortArias = Array.from(table.tHead.rows[0].cells, cell => cell.getAttribute("aria-sort"));
    const button = table.querySelector("thead th button");
    button.click();
    button.click();
  })()`);
  await waitFor(cdp, "window.__fixtureTables.D.tBodies[0].rows[0].cells[0].textContent.trim() === window.__sortLastText && window.__fixtureTables.D.tBodies[0].rows[0].cells[0].style.getPropertyPriority('left') === 'important'", "並べ替えた行の先頭列が固定されない");
  await delay(350);
  const sorted = await cdp.evaluate(`(() => {
    const table = window.__fixtureTables.D;
    const scroll = table.parentElement;
    scroll.scrollTop = 200;
    scroll.scrollLeft = 300;
    const cell = table.tBodies[0].rows[0].cells[0];
    return new Promise(resolve => requestAnimationFrame(() => {
      resolve({ left: cell.getBoundingClientRect().left - scroll.getBoundingClientRect().left - scroll.clientLeft, top: cell.style.top, text: cell.textContent.trim(), expected: window.__sortLastText, cacheChanged: window.__sortBorderCache !== window.__mdTableSticky.records.get(table).borderCache });
    }));
  })()`);
  near(sorted.left, 0, "並べ替えた後も先頭列の位置を保つ");
  assert.equal(sorted.top, "auto", "本文のセルを上へ固定しない");
  assert.equal(sorted.text, sorted.expected, "行の並べ替えを再現する");
  assert.equal(sorted.cacheChanged, true, "行の並びが変わったら罫線を作り直す");
  await cdp.evaluate(`(() => {
    const table = window.__fixtureTables.D;
    table.tBodies[0].append(...window.__sortRows);
    Array.from(table.tHead.rows[0].cells).forEach((cell, index) => {
      if (window.__sortArias[index] === null) cell.removeAttribute("aria-sort");
      else cell.setAttribute("aria-sort", window.__sortArias[index]);
    });
  })()`);

  await cdp.evaluate(`(() => {
    const table = window.__dynamicTable;
    window.__dynamicOwnWrapper = table.parentElement;
    window.__dynamicBar = window.__mdTableSticky.records.get(table).bar.element;
    const outer = document.createElement("div");
    outer.className = "gl-table-shadow-overlay gl-my-5";
    const scroll = document.createElement("div");
    scroll.className = "gl-max-h-[70vh] gl-w-fit gl-max-w-full gl-overflow-x-auto gl-overflow-y-auto";
    scroll.setAttribute("data-sticky-header", "");
    outer.append(scroll);
    table.before(outer);
    scroll.append(table);
    window.__dynamicGitLabWrapper = outer;
  })()`);
  await waitFor(cdp, "!window.__dynamicOwnWrapper.isConnected && window.__dynamicTable.parentElement.hasAttribute('data-sticky-header')", "後からできた GitLab の包みへ移れない");
  assert.equal(await cdp.evaluate("window.__mdTableSticky.records.get(window.__dynamicTable).wrapper === null"), true, "自作の包みの記録も外す");
  assert.equal(await cdp.evaluate("window.__dynamicGitLabWrapper.previousElementSibling === window.__dynamicBar"), true, "GitLab の包みへ移した後も帯を外の前へ移す");
  await cdp.evaluate(`(() => {
    const record = window.__mdTableSticky.records.get(window.__dynamicTable);
    record.bar.element.querySelector('[data-md-table-sticky-action="cols-minus"]').click();
  })()`);
  assert.equal(await cdp.evaluate("window.__mdTableSticky.records.get(window.__dynamicTable).override.cols"), 0, "手動の数を選んだ表を対象外にする");
  await cdp.evaluate(`(() => {
    const table = window.__dynamicTable;
    table.setAttribute("data-table-fields", "");
    const marker = document.createTextNode(" ");
    table.append(marker);
    window.__dynamicMarker = marker;
  })()`);
  await waitFor(cdp, "!window.__mdTableSticky.records.has(window.__dynamicTable)", "対象外になった表の記録を外す");
  assert.equal(await cdp.evaluate("window.__dynamicBar.isConnected"), false, "対象外になった表の帯も消す");
  await cdp.evaluate("window.__dynamicTable.removeAttribute('data-table-fields'); window.__dynamicMarker.remove()");
  await waitFor(cdp, "!!window.__mdTableSticky.records.get(window.__dynamicTable)?.bar", "再び対象になった表へ帯を戻す");
  assert.equal(await cdp.evaluate("(() => { const selected = window.__mdTableSticky.records.get(window.__dynamicTable).override; return selected.rows === null && selected.cols === null; })()"), true, "新しく処理する表は自動から始める");
  await cdp.evaluate("window.__dynamicBar = window.__mdTableSticky.records.get(window.__dynamicTable).bar.element");
  await cdp.evaluate("window.__dynamicTable.remove(); window.__dynamicGitLabWrapper.remove()");
  await waitFor(cdp, "!window.__mdTableSticky.records.has(window.__dynamicTable)", "切り離した表の記録が残る");
  assert.equal(await cdp.evaluate("window.__dynamicBar.isConnected"), false, "切り離した表の帯も消す");
  await cdp.evaluate(bookmarklet);
  assert.deepEqual(
    await cdp.evaluate("Array.from(document.querySelectorAll('.md'), md => md.innerHTML)"),
    await cdp.evaluate("window.__beforeHtml"),
    "動的な処理の後も元の Markdown の DOM へ戻す",
  );
}

// 帯の操作は実際のボタンを押し、当てた値とスクロール中の位置を確かめる。
async function exerciseControls(version) {
  const checks = [];
  const check = (pass, message) => checks.push({ pass: !!pass, message });
  const frame = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const waitLayout = async predicate => {
    const started = performance.now();
    while (!predicate() && performance.now() - started < 1500) {
      await new Promise(resolve => setTimeout(resolve, 20));
    }
    return predicate();
  };
  const state = window.__mdTableSticky;
  const barOf = record => record.bar?.element || null;
  const action = (record, name) => barOf(record).querySelector('[data-md-table-sticky-action="' + name + '"]');
  const count = (record, name) => barOf(record).querySelector('[data-md-table-sticky-count="' + name + '"]').textContent;
  const records = Array.from(state.records.values());
  const bars = Array.from(document.querySelectorAll('[data-md-table-sticky-bar]'));
  const expected = records.filter(record => !record.skipped && (record.region.rows || record.region.cols));
  check(bars.length === expected.length, '固定の対象になる表ごとに帯を 1 つ置く');
  for (const record of records) {
    const bar = barOf(record);
    const needed = expected.includes(record);
    check(!!bar === needed, '固定の対象かどうかで帯の有無を決める: ' + record.table.rows[0]?.textContent.trim());
    if (!bar) continue;
    const scroll = record.table.parentElement;
    const anchor = record.wrapper || scroll.closest('.gl-table-shadow-overlay') || scroll;
    check(bar.nextElementSibling === anchor && bar.parentElement === anchor.parentElement && !scroll.contains(bar), '帯をスクロールの範囲の外のすぐ前に置く');
    check(bars.filter(candidate => candidate === bar).length === 1, '同じ表の帯を重ねない');
    check(bar.textContent.includes('固定') && bar.textContent.includes('行') && bar.textContent.includes('列'), '帯に固定する向きを表示する');
    for (const button of bar.querySelectorAll('button')) {
      check(button.type === 'button' && !!button.getAttribute('aria-label') && button.title === button.getAttribute('aria-label'), 'ボタンに種類と日本語の説明を付ける');
    }
  }
  const inspect = record => {
    const rows = record.layout?.rows || 0;
    const cols = record.layout?.cols || 0;
    const rowBoundaries = [];
    const colBoundaries = [];
    for (let k = 0; k < record.model.rowCount; k++) {
      if (!record.model.cells.some(cell => cell.row < k && k < cell.row + cell.rowspan)) rowBoundaries.push(k);
    }
    for (let k = 0; k < record.model.colCount; k++) {
      if (!record.model.cells.some(cell => cell.row < rows && cell.col < k && k < cell.col + cell.colspan)) colBoundaries.push(k);
    }
    for (const [axis, value, boundaries] of [['rows', rows, rowBoundaries], ['cols', cols, colBoundaries]]) {
      const automatic = record.override[axis] === null;
      check(count(record, axis) === String(value) + (automatic ? '（自動）' : ''), '数の表示を実際に当てた数と自動の状態に合わせる: ' + axis);
      check(action(record, axis + '-minus').disabled === !boundaries.some(k => k < value), '減らすボタンの端を実際の数に合わせる: ' + axis);
      check(action(record, axis + '-plus').disabled === !boundaries.some(k => k > value), '増やすボタンの端を実際の数に合わせる: ' + axis);
    }
    check(action(record, 'auto').disabled === (record.override.rows === null && record.override.cols === null), '自動のときだけ自動ボタンを押せなくする');
  };
  for (const record of expected) inspect(record);
  const tableA = version === '19.5' ? window.__workaroundTable : window.__fixtureTables.A;
  const a = state.records.get(tableA);
  const barA = barOf(a);
  const scrollA = tableA.parentElement;
  scrollA.scrollIntoView({ block: 'start', inline: 'nearest' });
  scrollA.scrollTop = 0;
  scrollA.scrollLeft = 0;
  await frame();
  const beforeBar = barA.getBoundingClientRect();
  const cacheA = a.borderCache;
  check(a.layout.rows === 2 && count(a, 'rows') === '2（自動）', '表 A は自動の 2 行から始める');
  action(a, 'rows-plus').click();
  await frame();
  // 本文の「東」は 6 行を結合しているので、2 の次に使える境目は 8 になる。
  check(a.layout.rows === 8 && a.override.rows === 8, '表 A の行を増やすと rowspan をまたぐ 3〜7 を飛ばして 8 にする');
  inspect(a);
  scrollA.scrollTop = 200;
  scrollA.scrollLeft = 300;
  await frame();
  const third = Array.from(tableA.rows[2].cells).find(cell => cell.rowSpan === 1);
  const thirdIndex = Array.from(tableA.rows).flatMap(row => Array.from(row.cells)).indexOf(third);
  const thirdRect = third.getBoundingClientRect();
  const scrollRect = scrollA.getBoundingClientRect();
  check(a.layout.roles[thirdIndex].top && getComputedStyle(third).position === 'sticky' && Math.abs(thirdRect.top - scrollRect.top - scrollA.clientTop - a.layout.rowOffsets[2]) <= 1, '行を増やすと 3 行目のセルも上に固定する');
  const afterBar = barA.getBoundingClientRect();
  check(Math.abs(afterBar.top - beforeBar.top) <= 1 && Math.abs(afterBar.left - beforeBar.left) <= 1, '表を縦横にスクロールしても帯は動かない');
  action(a, 'auto').click();
  await frame();
  check(a.layout.rows === 2 && a.override.rows === null && a.override.cols === null, '自動を押すと表 A の 2 行へ戻す');
  action(a, 'rows-minus').click();
  await frame();
  check(a.layout.rows === 0 && a.override.rows === 0, '表 A の行を減らすと境目 1 を飛ばして 0 にする');
  inspect(a);
  const second = tableA.rows[1].cells[0];
  scrollA.scrollTop = 0;
  await frame();
  const secondTop = second.getBoundingClientRect().top;
  scrollA.scrollTop = 200;
  await frame();
  check(!a.layout.roles.some(role => role.top) && getComputedStyle(second).position !== 'sticky' && Math.abs(second.getBoundingClientRect().top - secondTop + 200) <= 1, '行 0 を選ぶと 2 行目の見出しは本文と一緒に動く');
  action(a, 'auto').click();
  await frame();
  check(a.layout.rows === 2 && a.borderCache === cacheA, '自動に戻しても罫線の計算は使い回す');
  inspect(a);
  const tableC = version === '19.5' ? window.__workaroundC : window.__fixtureTables.C;
  const c = state.records.get(tableC);
  check(c.layout.rows === 2 && c.layout.cols === 2, '表 C は結合した角に合わせて 2 行 2 列から始める');
  action(c, 'cols-minus').click();
  await frame();
  check(c.layout.cols === 0 && c.override.cols === 0 && !c.layout.roles.some(role => role.left), '表 C の列を減らすと境目 1 を飛ばして 0 にし、左の固定を外す');
  inspect(c);
  action(c, 'rows-minus').click();
  action(c, 'cols-plus').click();
  await frame();
  check(c.layout.rows === 0 && c.layout.cols === 1, '行を固定しないときは表 C の列 1 の境目も選べる');
  action(c, 'rows-plus').click();
  await frame();
  check(c.layout.rows === 2 && c.override.cols === 0 && c.layout.cols === 0, '行を増やして列の境目が使えなくなったら選んだ数以下へ戻す');
  inspect(c);
  action(c, 'auto').click();
  await frame();
  check(c.layout.cols === 2, '自動を押すと表 C の 2 列へ戻す');
  inspect(c);
  const autoColumns = state.records.get(window.__autoColumnsTable);
  check(autoColumns.layout.rows === 1 && autoColumns.layout.cols === 1 && autoColumns.override.cols === null, '自動列の標本は 1 行 1 列から始める');
  action(autoColumns, 'rows-plus').click();
  await frame();
  const merged = window.__autoColumnsTable.rows[1].cells[0];
  check(autoColumns.layout.rows === 2 && autoColumns.layout.cols === 2 && autoColumns.override.cols === null, '行を手動で増やすと結合セルに合わせて自動の列を 2 へ広げる');
  check(getComputedStyle(merged).position === 'sticky' && merged.style.left === '0px', '新しく固定した結合セルを左にも固定する');
  inspect(autoColumns);
  action(autoColumns, 'rows-minus').click();
  await frame();
  check(autoColumns.layout.rows === 1 && autoColumns.layout.cols === 1 && autoColumns.override.cols === null, '行を減らすと自動の列も 1 へ戻す');
  inspect(autoColumns);
  action(autoColumns, 'rows-plus').click();
  await frame();
  action(autoColumns, 'cols-minus').click();
  await frame();
  check(autoColumns.layout.rows === 2 && autoColumns.layout.cols === 0 && autoColumns.override.cols === 0 && !autoColumns.layout.roles.some(role => role.left), '広がった自動の列を減らすと使えない境目 1 を飛ばして 0 にする');
  inspect(autoColumns);
  action(autoColumns, 'auto').click();
  await frame();
  check(autoColumns.layout.rows === 1 && autoColumns.layout.cols === 1 && autoColumns.override.rows === null && autoColumns.override.cols === null, '自動列の標本も自動へ戻す');
  inspect(autoColumns);
  const f = state.records.get(window.__fixtureTables.F);
  check(f.layout.cols === 0 && f.layout.colOffsets[1] > f.scroll.clientWidth / 2, '小さい表 F は半分の上限で列を外す');
  check(!!barOf(f), '行を固定する表 F にも帯を出す');
  action(f, 'cols-plus').click();
  await frame();
  check(f.layout.cols === 1 && f.override.cols === 1 && f.layout.roles.some(role => role.left), '利用者が選んだ列の数を半分の上限より優先する');
  inspect(f);
  action(f, 'rows-minus').click();
  action(f, 'cols-minus').click();
  await frame();
  check(f.layout.rows === 0 && f.layout.cols === 0 && !!barOf(f)?.isConnected, '行列とも 0 を選んでも帯を残す');
  check(getComputedStyle(f.table).borderCollapse === 'collapse', '行列とも 0 を選んだ表の罫線を元に戻す');
  inspect(f);
  action(f, 'auto').click();
  await frame();
  inspect(f);
  const originalSmallScrollStyle = f.scroll.getAttribute('style');
  try {
    f.scroll.style.setProperty('max-height', '30px', 'important');
    check(await waitLayout(() => f.layout?.rows === 0 && f.layout?.cols === 0), '小さい表の高さを狭めると両方向の自動固定を外す');
    const keptBar = !!barOf(f)?.isConnected;
    check(keptBar, '両方向とも上限で 0 になっても帯を残す');
    check(f.override.rows === null && f.override.cols === null, '両方向とも上限で外した表も自動の状態を保つ');
    if (keptBar) {
      inspect(f);
      check(!action(f, 'rows-plus').disabled && !action(f, 'cols-plus').disabled, '両方向とも自動で 0 の帯から数を増やせる');
      action(f, 'rows-plus').click();
      action(f, 'cols-plus').click();
      await frame();
      check(f.layout.rows === 1 && f.layout.cols === 1 && f.override.rows === 1 && f.override.cols === 1, '両方向の + で半分の上限より選んだ数を優先して固定し直す');
      check(f.layout.roles.some(role => role.top && role.left) && getComputedStyle(f.table).borderCollapse === 'separate', '両方向を選び直すと角の固定と罫線の付け直しを戻す');
      inspect(f);
    }
  } finally {
    if (barOf(f) && (f.override.rows !== null || f.override.cols !== null)) action(f, 'auto').click();
    if (originalSmallScrollStyle === null) {
      f.scroll.getAttribute('style');
      f.scroll.removeAttribute('style');
    } else {
      f.scroll.setAttribute('style', originalSmallScrollStyle);
    }
  }
  check(await waitLayout(() => f.layout?.rows === 1 && f.layout?.cols === 0), '小さい表の包みを元に戻すと自動の行 1・列 0 へ戻す');
  inspect(f);
  const displays = new Map(bars.map(bar => [bar, getComputedStyle(bar).display]));
  window.dispatchEvent(new Event('beforeprint'));
  check(bars.every(bar => getComputedStyle(bar).display === 'none'), '印刷の前はすべての帯を隠す');
  window.dispatchEvent(new Event('afterprint'));
  check(bars.every(bar => getComputedStyle(bar).display === displays.get(bar)), '印刷の後は帯の表示を戻す');
  return checks;
}

async function checkControls(cdp, version) {
  const checks = await cdp.evaluate('(' + exerciseControls.toString() + ')(' + JSON.stringify(version) + ')');
  for (const result of checks) assert.equal(result.pass, true, result.message);
}

function near(actual, expected, message) {
  assert.ok(Math.abs(actual - expected) <= 1, message + ": " + actual + " / " + expected);
}

test("生成したブックマークレットを GitLab の DOM で確かめる", { timeout: 120000 }, async (t) => {
  const browser = await findBrowser();
  if (!browser) {
    t.skip("Edge、Chrome、Chromium が見つからない。MTS_BROWSER で実行ファイルを指定できる");
    return;
  }
  if (typeof WebSocket !== "function") {
    t.skip("Node.js の組み込み WebSocket がない。Node.js 22 以上が必要");
    return;
  }
  const directory = await mkdtemp(path.join(root, ".mts-browser-"));
  let child;
  let cdp;
  try {
    let launched;
    try {
      launched = await launch(browser, directory);
    } catch (error) {
      child = error.browserChild;
      t.skip("ブラウザーを起動できない: " + error.message);
      return;
    }
    child = launched.child;
    try {
      const targets = await fetch("http://127.0.0.1:" + launched.port + "/json/list").then(response => response.json());
      const target = targets.find(entry => entry.type === "page");
      assert.ok(target?.webSocketDebuggerUrl, "CDP のページがある");
      cdp = await CDP.connect(target.webSocketDebuggerUrl);
      await cdp.send("Page.enable");
    } catch (error) {
      await delay(100);
      const diagnostic = launched.diagnostics();
      if (child.exitCode !== null || /FATAL:.*GPU|GPU process isn.t usable/.test(diagnostic)) {
        t.skip("ブラウザーが起動中に終了した（終了コード " + child.exitCode + "）: " + diagnostic.trim().split(/\r?\n/).at(-1));
        return;
      }
      throw error;
    }
    const url = (await readFile(path.join(root, "dist", "bookmarklet.txt"), "utf8")).trim();
    assert.ok(url.startsWith("javascript:"));
    const bookmarklet = decodeURIComponent(url.slice("javascript:".length));
    for (const version of ["master", "19.5"]) {
      await t.test(version + " の表", async () => {
        const pageUrl = await createPage(directory, version);
        await cdp.send("Page.navigate", { url: pageUrl });
        let ready = false;
        for (let attempt = 0; attempt < 100; attempt++) {
          ready = await cdp.evaluate("location.href === " + JSON.stringify(pageUrl) + " && window.__fixtureReady === true");
          if (ready) break;
          await delay(50);
        }
        assert.equal(ready, true, "確かめ用のページが開く");
        const shapes = await cdp.evaluate("({ a: window.__fixtureTables.A.tHead.rows.length, span: window.__fixtureTables.A.tHead.rows[0].cells[0].rowSpan, bWrapped: window.__fixtureTables.B.parentElement.hasAttribute('data-sticky-header'), dButton: !!window.__fixtureTables.D.querySelector('thead th > div > button > span') })");
        assert.equal(shapes.dButton, true, "並べ替えの見出しの DOM を再現する");
        assert.equal(shapes.a, version === "master" ? 2 : 1, "版ごとの thead の行数");
        assert.equal(shapes.span, version === "master" ? 2 : 1, "19.5.0 の見出しの結合の欠落も再現する");
        assert.equal(shapes.bWrapped, version === "master", "thead の無い表を包む版の違い");
        if (version === "master") {
          const nativeBodyHeader = await cdp.evaluate(`(() => {
            const header = window.__fixtureTables.I.tBodies[0].rows[0].cells[1];
            const style = getComputedStyle(header);
            return { position: style.position, top: style.top };
          })()`);
          assert.deepEqual(nativeBodyHeader, { position: "sticky", top: "0px" }, "本文の先頭行の th に GitLab 自身の固定がある形を再現する");
        }
        await cdp.evaluate(bookmarklet);
        await cdp.evaluate("new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
        assert.equal(await cdp.evaluate("!!window.__mdTableSticky"), true, "固定が始まる");
        if (version === "master") await checkNativeBodyHeader(cdp);
        const regions = { A: [version === "master" ? 2 : 1, 1], B: [2, 2], C: [version === "master" ? 2 : 1, 2], D: [1, 1], E: [1, 1] };
        if (version === "19.5") regions["回避形"] = [2, 1];
        const dimensions = await cdp.evaluate("(" + prepareMeasurements.toString() + ")(" + JSON.stringify(regions) + ")");
        for (const [name, dimension] of Object.entries(dimensions)) {
          assert.ok(dimension.clientHeight > 0 && dimension.clientWidth > 0, name + " が測れる");
          assert.ok(dimension.width > dimension.clientWidth + 300, name + " は横へスクロールできる");
        }
        await cdp.evaluate("(" + scrollMeasurements.toString() + ")()");
        const positions = await cdp.evaluate("(" + checkMeasurements.toString() + ")()");
        for (const [name, position] of Object.entries(positions)) {
          assert.equal(position.scrollTop, 200, name + " の縦のスクロール量");
          assert.equal(position.scrollLeft, 300, name + " の横のスクロール量");
          assert.ok(position.top.length, name + " に上へ固定するセルがある");
          assert.ok(position.left.length, name + " に左へ固定するセルがある");
          assert.ok(position.normal.length, name + " に動くセルがある");
          for (const cell of position.top) near(cell.actual, cell.expected, name + " の上の位置 " + cell.text);
          for (const cell of position.left) near(cell.actual, cell.expected, name + " の左の位置 " + cell.text);
          for (const cell of position.normal) {
            near(cell.topDelta, -200, name + " の本文は縦へ動く");
            near(cell.leftDelta, -300, name + " の本文は横へ動く");
          }
        }
        const second = await cdp.evaluate(`(() => {
          const m = window.__measurements[${JSON.stringify(version === "master" ? "A" : "回避形")}];
          const cell = m.placed.find(item => item.row === 1).cell;
          return { top: parseFloat(cell.style.top), first: m.rowOffsets[1] };
        })()`);
        near(second.top, second.first, "2 行目の見出しは 1 行目の高さだけ下へ置く");
        const stickyDetails = await cdp.evaluate("(" + checkStickyDetails.toString() + ")(" + JSON.stringify(version === "master" ? "A" : "回避形") + ")");
        assert.ok(stickyDetails.topEdges.length, "2 行目以降の見出しの上端を調べる");
        assert.ok(stickyDetails.leftEdges.length, "2 列目以降の見出しの左端を調べる");
        for (const edge of stickyDetails.topEdges) assert.equal(edge.covered, true, "見出しの上端の帯をセルが覆う: " + edge.text);
        for (const edge of stickyDetails.leftEdges) assert.equal(edge.covered, true, "見出しの左端の帯をセルが覆う: " + edge.text);
        assert.equal(stickyDetails.borders.collapse, "separate", "固定する表は separate にする");
        assert.ok(stickyDetails.borders.right > 0, "固定した 2 列の間の罫線を残す");
        assert.ok(stickyDetails.vertical.length, "見出しの下まで続く縦の結合セルがある");
        for (const inner of stickyDetails.vertical) {
          assert.equal(inner.wrapped, true, "縦の結合セルの中身を span に移す: " + inner.text);
          near(inner.top, inner.expected, "縦の結合セルの中身を見出しの下に残す: " + inner.text);
          assert.equal(inner.visible, true, "縦の結合セルの入れ物が見えている: " + inner.text);
          assert.equal(inner.textVisible, true, "縦の結合セルの文字が見えている: " + inner.text);
        }
        assert.ok(stickyDetails.horizontal.length, "横に結合した年の見出しが見える");
        for (const inner of stickyDetails.horizontal) {
          assert.equal(inner.wrapped, true, "横の結合セルの中身を span に移す: " + inner.text);
          assert.ok(inner.left >= inner.expected - 1, "年の見出しの中身を固定した列の右へ残す: " + inner.text);
          assert.equal(inner.visible, true, "年の見出しの文字が見えている: " + inner.text);
        }
        const dividers = await cdp.evaluate(`(() => {
          const m = window.__measurements.B;
          const wrap = m.scroll.getBoundingClientRect();
          return Array.from(m.table.querySelectorAll("[data-md-table-sticky-inner]")).filter(span => span.textContent.startsWith("区切り:")).map(span => {
            const rect = span.getBoundingClientRect();
            return { left: rect.left - wrap.left, top: rect.top - wrap.top, bottom: rect.bottom - wrap.top, height: m.scroll.clientHeight };
          });
        })()`);
        assert.ok(dividers.length, "区切りのセルの子を span に移す");
        const visibleDivider = dividers.find(divider => divider.bottom > 0 && divider.top < divider.height);
        assert.ok(visibleDivider, "スクロールした範囲に区切りの行がある");
        assert.ok(visibleDivider.left >= -1 && visibleDivider.left <= 40, "区切りの中身は見えている範囲の左へ残る");
        assert.equal(await cdp.evaluate("window.__fixtureTables.G.querySelector('table').outerHTML === window.__nestedBefore"), true, "入れ子の表には手を加えない");
        assert.equal(await cdp.evaluate("window.__fixtureTables.F.textContent === window.__smallBefore.text && window.__fixtureTables.F.rows.length === window.__smallBefore.rows"), true, "小さい表の中身を保つ");
        await checkControls(cdp, version);
        assert.equal(await cdp.evaluate("window.__fixtureTables.H.closest('details').open"), false, "details はまだ閉じている");
        await cdp.evaluate("window.__fixtureTables.H.closest('details').open = true");
        let expanded = false;
        for (let attempt = 0; attempt < 30; attempt++) {
          expanded = await cdp.evaluate("Array.from(window.__fixtureTables.H.rows[0].cells).some(cell => cell.style.getPropertyPriority('position') === 'important' && cell.style.position === 'sticky')");
          if (expanded) break;
          await delay(50);
        }
        assert.equal(expanded, true, "details を開いた後に見出しを固定する");
        const hMeasurements = await cdp.evaluate("(" + prepareMeasurements.toString() + ")({ H: [1, 1] })");
        assert.ok(hMeasurements.H.clientHeight > 0);
        await cdp.evaluate("(" + scrollMeasurements.toString() + ")()");
        const hPosition = await cdp.evaluate("(" + checkMeasurements.toString() + ")()");
        for (const cell of hPosition.H.top) near(cell.actual, cell.expected, "H の上の位置");
        for (const cell of hPosition.H.left) near(cell.actual, cell.expected, "H の左の位置");
        await cdp.evaluate("window.__fixtureTables.H.closest('details').removeAttribute('open')");
        await cdp.evaluate(`(() => {
          const record = window.__mdTableSticky.records.get(window.__fixtureTables.A);
          record.bar.element.querySelector('[data-md-table-sticky-action="rows-minus"]').click();
        })()`);
        assert.equal(await cdp.evaluate("window.__mdTableSticky.records.get(window.__fixtureTables.A).override.rows"), 0, "手動の数を選んだ状態で解除する");
        await cdp.evaluate(bookmarklet);
        assert.equal(await cdp.evaluate("typeof window.__mdTableSticky"), "undefined", "固定の状態を消す");
        assert.equal(await cdp.evaluate("document.querySelectorAll('[data-md-table-sticky-bar]').length"), 0, "固定を外すと帯も消す");
        const restored = await cdp.evaluate("Array.from(document.querySelectorAll('.md'), md => md.innerHTML)");
        const before = await cdp.evaluate("window.__beforeHtml");
        assert.deepEqual(restored, before, "もう一度押すと Markdown の DOM を元へ戻す");
        await checkDynamic(cdp, bookmarklet);
      });
    }
  } finally {
    if (cdp) cdp.close();
    await stopBrowser(child);
    await removeTemporary(directory);
  }
});
