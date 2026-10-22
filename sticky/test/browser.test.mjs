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

async function createPage(directory, version, mimicTables = true) {
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
  const finish = `(async () => {
    const md = document.querySelector(".file-content");
    window.__fixtureTables = Object.fromEntries(Array.from(md.querySelectorAll("table")).filter(table => table.parentElement.closest("table, .md") === md).map((table, index) => [String.fromCharCode(65 + index), table]));
    for (const [name, table] of Object.entries(window.__fixtureTables)) {
      if (!["F", "J", "K"].includes(name)) table.style.minWidth = "1800px";
    }
    // 描き直す前の API の HTML で、元の nowrap の指定も確かめる。
    const nowrapTable = window.__fixtureTables.K;
    window.__kInitialWrap = nowrapTable ? Array.from(nowrapTable.rows[0].cells, (_, col) => !Array.from(nowrapTable.rows).every(row => getComputedStyle(row.cells[col]).whiteSpace === "nowrap")) : null;
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
    // 全行・全列になる小さい表でも、折り返しと全幅の操作を出す。
    const noFreezeSection = document.createElement("div");
    noFreezeSection.className = "md";
    noFreezeSection.id = "no-freeze-case";
    const noFreezeTable = document.createElement("table");
    noFreezeTable.createTBody().insertRow().insertCell().textContent = "固定しない表の内容";
    noFreezeSection.append(noFreezeTable);
    document.body.append(noFreezeSection);
    window.__noFreezeTable = noFreezeTable;
    // 先頭の空の tr は、測る基準のセルが無いので対象から外す。
    const emptyFirstRowSection = document.createElement("div");
    emptyFirstRowSection.className = "md";
    emptyFirstRowSection.id = "empty-first-row-case";
    const emptyFirstRowTable = document.createElement("table");
    const emptyFirstRowBody = emptyFirstRowTable.createTBody();
    emptyFirstRowBody.insertRow();
    const emptyFirstRowHeader = emptyFirstRowBody.insertRow();
    for (const text of ["見出し", "値"]) {
      const cell = document.createElement("th");
      cell.textContent = text;
      emptyFirstRowHeader.append(cell);
    }
    const emptyFirstRowData = emptyFirstRowBody.insertRow();
    emptyFirstRowData.insertCell().textContent = "空の先頭行の後の内容";
    emptyFirstRowData.insertCell().textContent = "測る基準の無い表";
    emptyFirstRowSection.append(emptyFirstRowTable);
    document.body.append(emptyFirstRowSection);
    window.__emptyFirstRowTable = emptyFirstRowTable;
    // 「セルを選んで固定」がリンクの既定の遷移を止めることを、外部へ移動しないリンクで確かめる。
    const pickLinkCell = (window.__workaroundTable || window.__fixtureTables.A).rows[4].cells[0];
    const pickLink = document.createElement("a");
    pickLink.href = "#md-table-sticky-pick-probe";
    pickLink.append(...Array.from(pickLinkCell.childNodes));
    pickLinkCell.append(pickLink);
    window.__pickLink = pickLink;
    // mimic の高さのボタンが、標本の幅を変えた後の寸法へ追従してから元の DOM を控える。
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    window.__beforeHtml = Array.from(document.querySelectorAll(".md"), md => md.innerHTML);
    window.__nestedBefore = window.__fixtureTables.G.querySelector("table").outerHTML;
    window.__smallBefore = { text: window.__fixtureTables.F.textContent, rows: window.__fixtureTables.F.rows.length, width: window.__fixtureTables.F.getBoundingClientRect().width };
    window.__detailsBefore = window.__fixtureTables.H.closest("details").getAttribute("open");
    window.__fixtureReady = true;
  })();`;
  const html = "<!doctype html>\n<html lang=\"ja\"><meta charset=\"utf-8\"><title>表の固定の試験</title><style>" + css + "</style><div class=\"file-content js-markup-content md\">" + sample + "</div><script>" + prepare + "</script><script>" + (mimicTables ? mimic : "") + "</script><script>" + finish + "</script></html>\n";
  const filename = path.join(directory, "fixture-" + version + (mimicTables ? "" : "-raw") + ".html");
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

// headless の Edge は、開いて約 20 秒後などにページを一時に隠し、その間のタイマーを遅らせる。
// 時間を測る確かめは、測る間に隠れなかった回の結果を使い、隠れたらやり直す（隠れる間は数秒続くことがある）。
async function whileVisible(cdp, run) {
  let result;
  for (let attempt = 0; attempt < 10; attempt++) {
    await waitFor(cdp, 'document.visibilityState === "visible"', "ページが表示に戻らない");
    await cdp.evaluate('(() => { window.__hiddenDuring = false; if (!window.__visibilityWatch) { window.__visibilityWatch = true; document.addEventListener("visibilitychange", () => { if (document.visibilityState !== "visible") window.__hiddenDuring = true; }); } })()');
    result = await run();
    if (!(await cdp.evaluate("window.__hiddenDuring"))) return result;
  }
  return result;
}

async function checkMutationScope(cdp) {  // 寸法を読む回数を一時的に数え、表の外の更新で測り直していないことを確かめる。
  await delay(400);
  const result = await whileVisible(cdp, () => cdp.evaluate(`(async () => {
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
  })()`));
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
      return { rows: record.region.rows, cols: record.region.cols, hasBorders: !!record.borderCache, header: getComputedStyle(table.tHead.rows[0].cells[0]).position, bodies: Array.from(table.tBodies, body => getComputedStyle(body.rows[0].cells[0]).position), collapse: getComputedStyle(table).borderCollapse };
    })()`);
    assert.deepEqual(unfrozen, { rows: 0, cols: 0, hasBorders: true, header: "sticky", bodies: ["static", "static"], collapse: "collapse" }, "全行・全列になる表は罫線を付け直さず、行 0 以外の固定を外す");
  } finally {
    await cdp.evaluate(`(() => {
      const bar = window.__mdTableSticky.records.get(window.__unfrozenNativeTable)?.bar?.element;
      window.__unfrozenNativeBar = bar;
      window.__unfrozenNativeColumns = bar?.nextElementSibling;
      window.__unfrozenNativeWrapper.remove();
    })()`);
  }
  await waitFor(cdp, "!window.__mdTableSticky.records.has(window.__unfrozenNativeTable) && !window.__unfrozenNativeBar?.isConnected && !window.__unfrozenNativeColumns?.isConnected", "切り離した表の記録・ツールバー・列のチェックボックスが消えない");
}

async function waitFor(cdp, expression, message) {
  for (let attempt = 0; attempt < 40; attempt++) {
    if (await cdp.evaluate(expression)) return;
    await delay(50);
  }
  assert.fail(message);
}

async function checkContinuousMutations(cdp) {
  const result = await whileVisible(cdp, () => cdp.evaluate(`(async () => {
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
  })()`));
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
  assert.equal(await cdp.evaluate("window.__dynamicGitLabWrapper.previousElementSibling?.hasAttribute('data-md-table-sticky-columns') && window.__dynamicGitLabWrapper.previousElementSibling.previousElementSibling === window.__dynamicBar"), true, "GitLab の包みへ移した後もツールバーを外の前へ移す");
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
  assert.equal(await cdp.evaluate("window.__dynamicBar.isConnected"), false, "対象外になった表のツールバーも消す");
  await cdp.evaluate("window.__dynamicTable.removeAttribute('data-table-fields'); window.__dynamicMarker.remove()");
  await waitFor(cdp, "!!window.__mdTableSticky.records.get(window.__dynamicTable)?.bar", "再び対象になった表へツールバーを戻す");
  assert.equal(await cdp.evaluate("(() => { const selected = window.__mdTableSticky.records.get(window.__dynamicTable).override; return selected.rows === null && selected.cols === null; })()"), true, "新しく処理する表は自動から始める");
  await cdp.evaluate("window.__dynamicBar = window.__mdTableSticky.records.get(window.__dynamicTable).bar.element");
  await cdp.evaluate("window.__dynamicTable.remove(); window.__dynamicGitLabWrapper.remove()");
  await waitFor(cdp, "!window.__mdTableSticky.records.has(window.__dynamicTable)", "切り離した表の記録が残る");
  assert.equal(await cdp.evaluate("window.__dynamicBar.isConnected"), false, "切り離した表のツールバーも消す");
  await cdp.evaluate(bookmarklet);
  // mimic の高さのボタンは ResizeObserver で付け外しするので、表の寸法が戻って知らせが届くまで待つ。
  await cdp.evaluate("new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
  assert.deepEqual(
    await cdp.evaluate("Array.from(document.querySelectorAll('.md'), md => md.innerHTML)"),
    await cdp.evaluate("window.__beforeHtml"),
    "動的な処理の後も元の Markdown の DOM へ戻す",
  );
}

// ツールバーの操作は実際のボタンを押し、当てた値とスクロール中の位置を確かめる。
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
  const expected = records.filter(record => !record.skipped && record.model.cells.length);
  check(bars.length === expected.length, 'セルがある対象の表ごとにツールバーを 1 つ置く');
  for (const record of records) {
    const bar = barOf(record);
    const needed = expected.includes(record);
    check(!!bar === needed, '対象の表かどうかでツールバーの有無を決める: ' + record.table.rows[0]?.textContent.trim());
    if (!bar) continue;
    const scroll = record.table.parentElement;
    const anchor = record.wrapper || scroll.closest('.gl-table-shadow-overlay') || scroll;
    const columns = bar.nextElementSibling;
    check(columns?.hasAttribute('data-md-table-sticky-columns') && columns.nextElementSibling === anchor && bar.parentElement === anchor.parentElement && !scroll.contains(bar) && !scroll.contains(columns), 'ツールバーと列のチェックボックスをスクロールの範囲の外のすぐ前に置く');
    check(bars.filter(candidate => candidate === bar).length === 1, '同じ表のツールバーを重ねない');
    if (record.region.rows || record.region.cols) check(bar.textContent.includes('固定') && bar.textContent.includes('行') && bar.textContent.includes('列'), 'ツールバーに固定する向きを表示する');
    else check(['rows-minus', 'rows-plus', 'cols-minus', 'cols-plus', 'auto'].every(name => action(record, name).getClientRects().length === 0), '固定しない表では固定する向きと操作を隠す');
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
  const cacheA = a.borderCache;
  check(a.layout.rows === 2 && count(a, 'rows') === '2（自動）', '表 A は自動の 2 行から始める');
  action(a, 'rows-plus').click();
  await frame();
  // 本文の「東」は 6 行を結合しているので、2 の次に使える境目は 8 になる。
  check(a.layout.rows === 8 && a.override.rows === 8, '表 A の行を増やすと rowspan をまたぐ 3〜7 を飛ばして 8 にする');
  inspect(a);
  // 数の文字が変わるとツールバーの折り返しが変わることがあるので、スクロールする直前の位置と比べる。
  const beforeBar = barA.getBoundingClientRect();
  scrollA.scrollTop = 200;
  scrollA.scrollLeft = 300;
  await frame();
  const third = Array.from(tableA.rows[2].cells).find(cell => cell.rowSpan === 1);
  const thirdIndex = Array.from(tableA.rows).flatMap(row => Array.from(row.cells)).indexOf(third);
  const thirdRect = third.getBoundingClientRect();
  const scrollRect = scrollA.getBoundingClientRect();
  check(a.layout.roles[thirdIndex].top && getComputedStyle(third).position === 'sticky' && Math.abs(thirdRect.top - scrollRect.top - scrollA.clientTop - a.layout.rowOffsets[2]) <= 1, '行を増やすと 3 行目のセルも上に固定する');
  const afterBar = barA.getBoundingClientRect();
  check(Math.abs(afterBar.top - beforeBar.top) <= 1 && Math.abs(afterBar.left - beforeBar.left) <= 1, '表を縦横にスクロールしてもツールバーは動かない');
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
  check(!!barOf(f), '行を固定する表 F にもツールバーを出す');
  action(f, 'cols-plus').click();
  await frame();
  check(f.layout.cols === 1 && f.override.cols === 1 && f.layout.roles.some(role => role.left), '利用者が選んだ列の数を半分の上限より優先する');
  inspect(f);
  action(f, 'rows-minus').click();
  action(f, 'cols-minus').click();
  await frame();
  check(f.layout.rows === 0 && f.layout.cols === 0 && !!barOf(f)?.isConnected, '行列とも 0 を選んでもツールバーを残す');
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
    check(keptBar, '両方向とも上限で 0 になってもツールバーを残す');
    check(f.override.rows === null && f.override.cols === null, '両方向とも上限で外した表も自動の状態を保つ');
    if (keptBar) {
      inspect(f);
      check(!action(f, 'rows-plus').disabled && !action(f, 'cols-plus').disabled, '両方向とも自動で 0 のツールバーから数を増やせる');
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
  check(bars.every(bar => getComputedStyle(bar).display === 'none'), '印刷の前はすべてのツールバーを隠す');
  window.dispatchEvent(new Event('afterprint'));
  check(bars.every(bar => getComputedStyle(bar).display === displays.get(bar)), '印刷の後はツールバーの表示を戻す');
  return checks;
}

async function checkControls(cdp, version) {
  const checks = await cdp.evaluate('(' + exerciseControls.toString() + ')(' + JSON.stringify(version) + ')');
  for (const result of checks) assert.equal(result.pass, true, result.message);
}


// 折り返しと全幅の操作を DOM と計算値で確かめる。
async function exerciseDisplayControls() {
  const checks = [];
  const check = (pass, message) => checks.push({ pass: !!pass, message });
  const frame = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const state = window.__mdTableSticky;
  const barOf = record => record.bar.element;
  const columnsOf = record => barOf(record).nextElementSibling;
  const action = (record, name) => barOf(record).querySelector('[data-md-table-sticky-action="' + name + '"]');
  const inputsOf = record => Array.from(columnsOf(record).querySelectorAll('input[data-md-table-sticky-column]'));
  const elementsOf = record => Array.from(record.table.rows).flatMap(row => Array.from(row.cells));
  const records = Array.from(state.records.values()).filter(record => !record.skipped && record.model.cells.length);
  for (const record of records) {
    const columns = columnsOf(record);
    const inputs = inputsOf(record);
    check(columns.hasAttribute('data-md-table-sticky-columns') && inputs.length === record.model.colCount, '対象の表に列の数だけ折り返しのチェックボックスを置く');
    check(inputs.every((input, col) => input.type === 'checkbox' && Number(input.dataset.mdTableStickyColumn) === col && !input.classList.contains('task-list-item-checkbox') && input.getAttribute('aria-label') === input.title && input.title.startsWith('列 ' + (col + 1))), '列のチェックボックスに番号と日本語の説明を付け、GitLab のタスクのクラスは付けない');
    if (!record.table.checkVisibility()) {
      check(!columns.checkVisibility(), '閉じた details の中では列のチェックボックスを隠す');
      continue;
    }
    const elements = elementsOf(record);
    const previousScroll = record.scroll.scrollLeft;
    for (const distance of [0, 200]) {
      record.scroll.scrollLeft = distance;
      await frame();
      check(Math.abs(columns.scrollLeft - record.scroll.scrollLeft) <= 1, '列のチェックボックスの横スクロール量を表に合わせる');
      for (let col = 0; col < record.model.colCount; col++) {
        const index = record.model.cells.findIndex(cell => cell.col === col && cell.colspan === 1);
        if (index < 0) continue;
        const label = inputs[col].closest('label');
        const rect = label.getBoundingClientRect();
        const cell = elements[index].getBoundingClientRect();
        check(Math.abs(rect.left - cell.left) <= 1, '横スクロール ' + distance + 'px で列 ' + (col + 1) + ' のラベルとセルの左端をそろえる');
        const text = label.querySelector('span');
        check((getComputedStyle(text).display !== 'none') === (rect.width >= 64), '列の幅が 64px 以上のときだけ折り返すの文字を出す');
        if (col < (record.layout?.cols || 0)) check(getComputedStyle(label).position === 'sticky', '固定した列のラベルも左へ固定する');
      }
    }
    if (record.scroll.scrollWidth - record.scroll.clientWidth >= 200) check(record.scroll.scrollLeft === 200, '横に広い表では 200px の位置まで動かして列の位置を調べる');
    record.scroll.scrollLeft = previousScroll;
  }
  await frame();
  const a = state.records.get(window.__workaroundTable || window.__fixtureTables.A);
  const aInputs = inputsOf(a);
  check(aInputs[2].title === '列 3（2025 年 / 1 月）を折り返す' && aInputs[2].getAttribute('aria-label') === aInputs[2].title, '複数行の結合した見出しから列の説明を作る');
  const aElements = elementsOf(a);
  const col = 2;
  const cellIndex = a.model.cells.findIndex(cell => cell.col === col && cell.colspan === 1);
  const cell = aElements[cellIndex];
  const originalWrap = aInputs[col].checked;
  if (inputsOf(a)[col].checked) inputsOf(a)[col].click();
  await frame();
  check(getComputedStyle(cell).whiteSpace === 'nowrap', '列のチェックを外すとセルを折り返さない');
  inputsOf(a)[col].click();
  await frame();
  check(getComputedStyle(cell).whiteSpace !== 'nowrap' && getComputedStyle(cell).overflowWrap === 'anywhere', '列のチェックを戻すと長い単語も折り返す');
  const mergedIndex = a.model.cells.findIndex(item => item.colspan > 1);
  action(a, 'nowrap-all').click();
  await frame();
  check(inputsOf(a).every(input => !input.checked) && aElements.every(element => getComputedStyle(element).whiteSpace === 'nowrap'), 'すべて折り返さないで結合セルも含めて nowrap にする');
  inputsOf(a)[a.model.cells[mergedIndex].col].click();
  await frame();
  check(getComputedStyle(aElements[mergedIndex]).whiteSpace !== 'nowrap', '結合セルが覆う列の一つを折り返すと結合セルも折り返す');
  action(a, 'wrap-all').click();
  await frame();
  check(inputsOf(a).every(input => input.checked) && aElements.every(element => getComputedStyle(element).whiteSpace !== 'nowrap'), 'すべて折り返すで列と結合セルを折り返す');
  if (!originalWrap) inputsOf(a)[col].click();
  const emptyFirstRow = state.records.get(window.__emptyFirstRowTable);
  check(emptyFirstRow?.skipped === true && emptyFirstRow.model.overlap === false && emptyFirstRow.model.grid[0][0] === undefined, 'セルが重ならなくても先頭行が空の表は対象から外す');
  check(!emptyFirstRow?.bar && !emptyFirstRow?.columns && !document.querySelector('#empty-first-row-case [data-md-table-sticky-bar], #empty-first-row-case [data-md-table-sticky-columns]'), '先頭行が空の表にはツールバーも列のチェックボックスも出さない');
  const noFreeze = state.records.get(window.__noFreezeTable);
  check(noFreeze.region.rows === 0 && noFreeze.region.cols === 0 && !!noFreeze.bar && inputsOf(noFreeze).length === 1, '固定しない表にもツールバーと列のチェックボックスを置く');
  check(['rows-minus', 'rows-plus', 'cols-minus', 'cols-plus', 'auto'].every(name => action(noFreeze, name).getClientRects().length === 0), '固定しない表は固定の操作だけを隠す');
  check(['scroll-x', 'wrap-all', 'nowrap-all', 'float'].every(name => action(noFreeze, name).getClientRects().length > 0), '固定しない表でも折り返し・横スクロール・全幅の操作を使える');
  const j = state.records.get(window.__fixtureTables.J);
  const k = state.records.get(window.__fixtureTables.K);
  // mimic が見出しの nowrap を落としても、本文のセルから初めの状態を決める。
  check(inputsOf(k).map(input => input.checked).join(',') === 'false,false,true', '描き直し後も表 K の先頭 2 列はチェックなし、3 列目はチェックありにする');
  check(Array.from(k.table.tHead.rows).every(row => Array.from(row.cells).slice(0, 2).every(cell => getComputedStyle(cell).whiteSpace === 'nowrap')), '表 K のチェックなしの先頭 2 列は作り直された見出しにも nowrap を当てる');
  const scrollX = action(j, 'scroll-x');
  check(scrollX.type === 'checkbox' && !scrollX.checked && !scrollX.classList.contains('task-list-item-checkbox'), '横スクロールはチェックボックスで既定はなしにする');
  action(j, 'wrap-all').click();
  // 実装の測定値を使わず、同じ CSS の複製を max-content で測る。
  const maxWidths = state.write(() => {
    const holder = document.createElement('div');
    holder.className = 'md';
    holder.style.cssText = 'position:fixed;left:0;top:0;visibility:hidden;width:max-content;max-width:none';
    const clone = j.table.cloneNode(true);
    clone.setAttribute('data-table-fields', '');
    for (const [name, value] of Object.entries({ display: 'table', width: 'max-content', 'min-width': '0', 'max-width': 'none', 'table-layout': 'auto' })) clone.style.setProperty(name, value, 'important');
    for (const cell of clone.querySelectorAll('th,td')) {
      for (const name of ['width', 'min-width', 'max-width']) cell.style.removeProperty(name);
      cell.style.setProperty('white-space', 'normal', 'important');
      cell.style.setProperty('overflow-wrap', 'normal', 'important');
    }
    holder.append(clone);
    document.body.append(holder);
    try {
      return Array.from(clone.tBodies[0].rows[0].cells, cell => cell.getBoundingClientRect().width);
    } finally {
      holder.remove();
    }
  });
  const md = document.querySelector('.file-content');
  const mdStyle = md.getAttribute('style');
  try {
    md.style.setProperty('width', '360px', 'important');
    await new Promise(resolve => setTimeout(resolve, 400));
    await frame();
    check(j.scroll.scrollWidth <= j.scroll.clientWidth + 1, '横スクロールなしでは長い URL を含む表 J を枠の幅に収める');
    scrollX.click();
    await frame();
    const cap = parseFloat(getComputedStyle(j.table).fontSize) * 12;
    const widths = Array.from(j.table.tBodies[0].rows[0].cells, cell => cell.getBoundingClientRect().width);
    check(widths.every((width, index) => width >= Math.min(maxWidths[index], cap) - 1), '横スクロールありでは折り返す各列を最大の内容の幅と 12em の小さい方より狭くしない');
    check(j.scroll.scrollWidth > j.scroll.clientWidth + 1, '横スクロールありでは狭い枠を超えた表 J を横にスクロールできる');
    const reference = j.table.tBodies[0].rows[0].cells[4];
    check(getComputedStyle(reference).overflowWrap === 'anywhere', '横スクロールありでも表 J の長い参照 URL を途中で折り返す');
    check(widths[4] <= cap + 1, '枠を超えた横スクロールありの表 J でも参照列の幅を 12em + 1px 以下にする');
    j.scroll.scrollLeft = 200;
    await frame();
    check(j.scroll.scrollLeft > 0, '横スクロールありの表 J が実際に横へ動く');
    const keptScrollLeft = j.scroll.scrollLeft;
    inputsOf(j)[3].click();
    await frame();
    check(j.scroll.scrollLeft === keptScrollLeft, '横スクロールありで列を折り返さなくしても横の位置を保つ');
    inputsOf(j)[3].click();
    await frame();
    check(j.scroll.scrollLeft === keptScrollLeft, '列を折り返すへ戻して最大の内容の幅を測り直しても横の位置を保つ');
    action(j, 'rows-minus').click();
    await frame();
    check(j.scroll.scrollLeft === keptScrollLeft, '横スクロールありで固定する行を変えても横の位置を保つ');
    action(j, 'auto').click();
    await frame();
    check(j.scroll.scrollLeft === keptScrollLeft, '固定の自動へ戻す測り直しでも横の位置を保つ');
    scrollX.click();
    await frame();
    check(j.scroll.scrollWidth <= j.scroll.clientWidth + 1, '横スクロールをなしへ戻すと表 J を再び枠の幅に収める');
  } finally {
    if (scrollX.checked) scrollX.click();
    if (mdStyle === null) md.removeAttribute('style');
    else md.setAttribute('style', mdStyle);
  }
  await new Promise(resolve => setTimeout(resolve, 400));
  await frame();
  const columnDisplays = new Map(records.map(record => [columnsOf(record), getComputedStyle(columnsOf(record)).display]));
  window.dispatchEvent(new Event('beforeprint'));
  check(Array.from(columnDisplays.keys()).every(columns => getComputedStyle(columns).display === 'none'), '印刷の前はすべての列のチェックボックスを隠す');
  window.dispatchEvent(new Event('afterprint'));
  await frame();
  check(Array.from(columnDisplays).every(([columns, display]) => getComputedStyle(columns).display === display), '印刷の後は列のチェックボックスの表示を戻す');
  return checks;
}

async function exerciseFloating() {
  const checks = [];
  const check = (pass, message) => checks.push({ pass: !!pass, message });
  const frame = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const state = window.__mdTableSticky;
  const a = state.records.get(window.__fixtureTables.A);
  const d = state.records.get(window.__fixtureTables.D);
  const barOf = record => record.bar.element;
  const action = record => barOf(record).querySelector('[data-md-table-sticky-action="float"]');
  const anchorOf = record => record.wrapper || record.scroll.closest('.gl-table-shadow-overlay') || record.scroll;
  barOf(a).scrollIntoView({ block: 'start', inline: 'nearest' });
  await frame();
  const bar = barOf(a);
  const columns = bar.nextElementSibling;
  const anchor = anchorOf(a);
  const before = { top: bar.getBoundingClientRect().top, height: anchor.getBoundingClientRect().bottom - bar.getBoundingClientRect().top, scrollY: window.scrollY };
  const open = async record => { action(record).click(); await frame(); };
  const closed = () => !document.querySelector('[data-md-table-sticky-float]') && !document.querySelector('[data-md-table-sticky-placeholder]');
  await open(a);
  const overlay = document.querySelector('[data-md-table-sticky-float]');
  const panel = overlay.querySelector('[data-md-table-sticky-float-panel]');
  const placeholder = document.querySelector('[data-md-table-sticky-placeholder]');
  const tolerance = window.innerWidth - document.documentElement.clientWidth + 1;
  check(overlay.parentElement === document.body && overlay.getAttribute('role') === 'dialog' && overlay.getAttribute('aria-modal') === 'true' && overlay.getAttribute('aria-label') === '表の全幅表示', '全幅の背景を body の直下のモーダルとして置く');
  check(panel.classList.contains('md') && Math.abs(panel.getBoundingClientRect().width - (window.innerWidth - 16)) <= tolerance && panel.contains(a.table), '表をブラウザーの幅から左右 8px ずつ引いたパネルへ移す');
  check(panel.children[0] === bar && panel.children[1] === columns && panel.children[2] === anchor, '全幅のパネルにツールバー・列のチェックボックス・表の枠の順で移す');
  check(Math.abs(placeholder.getBoundingClientRect().top - before.top) <= 2 && Math.abs(placeholder.getBoundingClientRect().height - before.height) <= 2, '元の場所にツールバーから表の下までと同じ位置と高さの代わりの要素を置く');
  check(window.scrollY === before.scrollY, '全幅で開いてもページの縦スクロール量を変えない');
  check(action(a).textContent === '閉じる' && action(a).title === '全幅表示を閉じる（Esc）' && action(a).getAttribute('aria-label') === action(a).title && document.activeElement === action(a), '全幅の閉じるボタンへフォーカスを置く');
  const floatedInputs = Array.from(columns.querySelectorAll('input[data-md-table-sticky-column]'));
  const floatedElements = Array.from(a.table.rows).flatMap(row => Array.from(row.cells));
  const floatedIndex = a.model.cells.findIndex(cell => cell.col === 2 && cell.colspan === 1);
  const wasChecked = floatedInputs[2].checked;
  if (wasChecked) floatedInputs[2].click();
  await frame();
  check(getComputedStyle(floatedElements[floatedIndex]).whiteSpace === 'nowrap', '全幅のパネルの中でも列の折り返しを切り替えられる');
  if (wasChecked) floatedInputs[2].click();
  const floatedScrollX = bar.querySelector('[data-md-table-sticky-action="scroll-x"]');
  floatedScrollX.click();
  await frame();
  check(floatedScrollX.checked && a.scrollX, '全幅のパネルの中でも横スクロールを切り替えられる');
  a.scroll.scrollTop = 200;
  a.scroll.scrollLeft = 200;
  await frame();
  check(Math.abs(columns.scrollLeft - a.scroll.scrollLeft) <= 1, '全幅のパネルの中でも表と列のチェックボックスを一緒に横へ動かす');
  const fixedIndex = a.model.cells.findIndex((cell, index) => a.layout.roles[index].top && a.layout.roles[index].left);
  const fixedCell = floatedElements[fixedIndex];
  const scrollRect = a.scroll.getBoundingClientRect();
  check(fixedIndex >= 0 && getComputedStyle(fixedCell).position === 'sticky' && Math.abs(fixedCell.getBoundingClientRect().top - scrollRect.top - a.scroll.clientTop - a.layout.rowOffsets[a.model.cells[fixedIndex].row]) <= 1, '全幅のパネルの中でも見出しを上へ固定する');
  floatedScrollX.click();
  await frame();
  let escaped = 0;
  const receiveEscape = event => { if (event.key === 'Escape') escaped++; };
  document.addEventListener('keydown', receiveEscape);
  const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
  action(a).dispatchEvent(escape);
  document.removeEventListener('keydown', receiveEscape);
  await frame();
  check(escape.defaultPrevented && escaped === 0 && closed(), 'Esc をほかへ伝えず全幅の背景と代わりの要素を消す');
  check(document.activeElement === action(a) && bar.nextElementSibling === columns && columns.nextElementSibling === anchor, 'Esc で元の位置へ戻し全幅のボタンへフォーカスを戻す');
  await open(a);
  document.querySelector('[data-md-table-sticky-float]').click();
  await frame();
  check(closed() && document.activeElement === action(a), '背景のクリックでも元へ戻す');
  await open(a);
  action(a).click();
  await frame();
  check(closed() && document.activeElement === action(a), '閉じるボタンでも元へ戻す');
  await open(a);
  await open(d);
  check(document.querySelectorAll('[data-md-table-sticky-float]').length === 1 && document.querySelectorAll('[data-md-table-sticky-placeholder]').length === 1 && document.querySelector('[data-md-table-sticky-float-panel]').contains(d.table) && !document.querySelector('[data-md-table-sticky-float-panel]').contains(a.table), '別の表を全幅で開くと前の表を戻し一つだけ表示する');
  window.dispatchEvent(new Event('beforeprint'));
  check(closed(), '印刷の前は全幅表示を閉じる');
  window.dispatchEvent(new Event('afterprint'));
  await frame();
  const j = state.records.get(window.__fixtureTables.J);
  const jWidthBefore = j.table.getBoundingClientRect().width;
  check(j.scroll.scrollWidth <= j.scroll.clientWidth + 1, 'その場の表 J は横スクロールなしで元の枠に収まる');
  await open(j);
  const jPanel = document.querySelector('[data-md-table-sticky-float-panel]');
  check(jPanel.contains(j.table) && j.table.getBoundingClientRect().width > jWidthBefore + 1, '長い内容の表 J は全幅のボタンを押す前より広く表示する');
  check(j.scroll.scrollWidth <= j.scroll.clientWidth + 1 && j.table.getBoundingClientRect().width <= j.scroll.clientWidth + 1, '全幅の表 J もパネルの中の枠に収めて折り返す');
  action(j).click();
  await frame();
  check(closed(), '表 J の全幅表示を閉じる');
  const f = state.records.get(window.__fixtureTables.F);
  const fWidthBefore = f.table.getBoundingClientRect().width;
  await open(f);
  check(Math.abs(f.table.getBoundingClientRect().width - fWidthBefore) <= 1, '小さい表 F は全幅のパネルの中でも表の幅を 1px 以内に保つ');
  action(f).click();
  await frame();
  check(closed(), '小さい表 F の全幅表示を閉じる');
  return checks;
}

async function checkDisplayControls(cdp) {
  const present = await cdp.evaluate('!!window.__fixtureTables.J && !!window.__fixtureTables.K');
  assert.equal(present, true, '表 J・K が sample.gitlab.html に必要。Claude Code が tools/render-gitlab.mjs で API の HTML を再生成してからブラウザー試験を実行する');
  for (const exercise of [exerciseDisplayControls, exerciseFloating]) {
    const checks = await cdp.evaluate('(' + exercise.toString() + ')()');
    for (const result of checks) assert.equal(result.pass, true, result.message);
  }
}


// 1 回目の追加機能を、固定の計算値と実際の表示の両方で確かめる。
async function exerciseFirstRoundControls(version) {
  const checks = [];
  const check = (pass, message) => checks.push({ pass: !!pass, message });
  const frame = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const state = window.__mdTableSticky;
  const action = (record, name) => record.bar.element.querySelector('[data-md-table-sticky-action="' + name + '"]');
  const elementsOf = record => Array.from(record.table.rows).flatMap(row => Array.from(row.cells));
  const a = state.records.get(window.__workaroundTable || window.__fixtureTables.A);
  const records = Array.from(state.records.values()).filter(record => record.bar);
  for (const record of records) {
    for (const name of ['fit-height', 'highlight', 'compact']) {
      const input = action(record, name);
      check(input?.type === 'checkbox' && !input.checked && input.title === input.getAttribute('aria-label') && !input.classList.contains('task-list-item-checkbox'), '新しいチェックボックスを既定でオフにし日本語の説明を付ける: ' + name);
    }
    check(action(record, 'pick')?.getAttribute('aria-pressed') === 'false', '「セルを選んで固定」は選択中でないことを表示する');
    check(!action(record, 'fit-height').closest('label').checkVisibility(), '全体表示でない表では「高さを制限」を隠す');
  }
  if (version === 'master') {
    const native = state.records.get(window.__fixtureTables.A);
    const toggle = native.scroll.closest('.gl-table-shadow-overlay').parentElement.querySelector('[data-testid="markdown-table-height-toggle"]');
    check(!!toggle, '窓の 70% より高い表に GitLab の全体表示のボタンを置く');
    toggle.click();
    await frame();
    const fit = action(native, 'fit-height');
    check(native.nativeFull && fit.closest('label').checkVisibility() && !fit.checked, 'GitLab を全体表示へ変えると「高さを制限」を出す');
    check(native.bar.element.querySelector('[data-md-table-sticky-count="rows"]').textContent.endsWith('（自動・全体表示中）'), '全体表示で高さを制限していない表では行の数に「全体表示中」を添える');
    check(getComputedStyle(native.scroll).maxHeight === 'none', 'GitLab の全体表示は高さの上限を外す');
    fit.click();
    await frame();
    check(native.fitHeight && Math.abs(parseFloat(getComputedStyle(native.scroll).maxHeight) - innerHeight * 0.7) <= 1, '「高さを制限」でこの表の高さを画面の 70% にする');
    check(getComputedStyle(window.__fixtureTables.D.parentElement).maxHeight === 'none', '「高さを制限」は別の表の全体表示を変えない');
    native.scroll.scrollIntoView({ block: 'start', inline: 'nearest' });
    native.scroll.scrollTop = 200;
    await frame();
    const first = elementsOf(native)[0].getBoundingClientRect();
    const wrap = native.scroll.getBoundingClientRect();
    check(native.scroll.scrollTop === 200 && Math.abs(first.top - wrap.top - native.scroll.clientTop) <= 1, '全体表示の表の高さを制限すると縦にスクロールしても見出しを止める');
    fit.click();
    await frame();
    check(!native.fitHeight && getComputedStyle(native.scroll).maxHeight === 'none', '「高さを制限」のチェックを外すと GitLab の全体表示へ戻す');
    fit.click();
    await frame();
    toggle.click();
    await frame();
    check(!native.nativeFull && !native.fitHeight && !fit.checked && !fit.closest('label').checkVisibility(), 'Collapse table で「高さを制限」の状態を戻して隠す');
  } else {
    check(!!a.wrapper && !action(a, 'fit-height').closest('label').checkVisibility(), '19.5 の自分の包みには「高さを制限」を出さない');
  }
  action(a, 'auto').click();
  await frame();
  const padding = getComputedStyle(a.scroll);
  check(Math.abs(parseFloat(padding.scrollPaddingTop) - a.layout.fixedHeight) <= 1 && Math.abs(parseFloat(padding.scrollPaddingLeft) - a.layout.fixedWidth) <= 1, '枠の scroll-padding を固定した行の高さと列の幅に合わせる');
  a.scroll.scrollIntoView({ block: 'start', inline: 'nearest' });
  a.scroll.scrollLeft = 0;
  a.scroll.scrollTop = 0;
  const target = elementsOf(a).find(cell => cell.textContent.trim() === '東の店 3');
  const range = document.createRange();
  range.selectNodeContents(target);
  const bounds = a.scroll.getBoundingClientRect();
  const top = bounds.top + a.scroll.clientTop;
  a.scroll.scrollTop += range.getBoundingClientRect().top - top - Math.max(1, a.layout.fixedHeight / 3);
  await frame();
  check(range.getBoundingClientRect().top < top + a.layout.fixedHeight - 1, '本文の検索対象を固定した見出しの下に隠してから検索する');
  const selection = window.getSelection();
  selection.removeAllRanges();
  const start = range.cloneRange();
  start.collapse(true);
  selection.addRange(start);
  check(window.find(target.textContent.trim(), true, false, false, false, false, false), 'window.find で隠した本文の文字を見つける');
  await frame();
  const found = selection.rangeCount ? selection.getRangeAt(0).getBoundingClientRect() : null;
  const afterFind = a.scroll.getBoundingClientRect();
  check(found && target.contains(selection.anchorNode) && found.top >= afterFind.top + a.scroll.clientTop + a.layout.fixedHeight - 1 && found.bottom <= afterFind.top + a.scroll.clientTop + a.scroll.clientHeight + 1, 'window.find が本文の文字を見出しの下に隠れない位置へ動かす');
  selection.removeAllRanges();
  action(a, 'rows-minus').click();
  action(a, 'cols-minus').click();
  await frame();
  check(a.layout.rows === 0 && a.layout.cols === 0 && !a.scroll.style.getPropertyValue('scroll-padding-top') && !a.scroll.style.getPropertyValue('scroll-padding-left'), '行列とも固定しないときは scroll-padding を外す');
  action(a, 'auto').click();
  await frame();
  const elements = elementsOf(a);
  const original = elements.map(cell => {
    const style = getComputedStyle(cell);
    return { padding: style.padding, fontSize: parseFloat(style.fontSize), lineHeight: style.lineHeight };
  });
  a.scroll.scrollTop = 150;
  a.scroll.scrollLeft = 200;
  await frame();
  const position = { top: a.scroll.scrollTop, left: a.scroll.scrollLeft };
  action(a, 'compact').click();
  await frame();
  check(a.compact && elements.every((cell, index) => {
    const style = getComputedStyle(cell);
    return style.paddingTop === '2px' && style.paddingBottom === '2px' && style.paddingLeft === '6px' && style.paddingRight === '6px' && Math.abs(parseFloat(style.fontSize) - original[index].fontSize * 0.875) <= 0.1 && Math.abs(parseFloat(style.lineHeight) - parseFloat(style.fontSize) * 1.3) <= 0.1;
  }), 'コンパクトは全セルの余白・文字の大きさ・行の高さを変える');
  check(a.scroll.scrollTop === position.top && a.scroll.scrollLeft === position.left, '詰めた寸法を測り直しても縦横のスクロール位置を保つ');
  const styles = elements.map(cell => cell.getAttribute('style'));
  elements.forEach(cell => cell.style.setProperty('position', 'static', 'important'));
  const firstTop = a.table.rows[0].getBoundingClientRect().top;
  const rowOffsets = Array.from(a.table.rows, row => row.getBoundingClientRect().top - firstTop);
  elements.forEach((cell, index) => {
    if (styles[index] === null) cell.removeAttribute('style');
    else cell.setAttribute('style', styles[index]);
  });
  check(a.model.cells.some((cell, index) => cell.row > 0 && a.layout.roles[index].top) && a.model.cells.every((cell, index) => !a.layout.roles[index].top || Math.abs(parseFloat(elements[index].style.top) - rowOffsets[cell.row]) <= 1), '固定した 2 行目以降の top はコンパクトにした後の行の上端に合わせる');
  action(a, 'compact').click();
  await frame();
  check(elements.every((cell, index) => {
    const style = getComputedStyle(cell);
    return style.padding === original[index].padding && Math.abs(parseFloat(style.fontSize) - original[index].fontSize) <= 0.1 && style.lineHeight === original[index].lineHeight;
  }), 'コンパクトのチェックを外すと余白と文字を元へ戻す');
  action(a, 'pick').click();
  await frame();
  check(a.picking && state.picking === a && action(a, 'pick').getAttribute('aria-pressed') === 'true' && getComputedStyle(a.table).cursor === 'crosshair', '「セルを選んで固定」で選択中の状態と十字のカーソルを出す');
  action(a, 'pick').click();
  await frame();
  check(!state.picking && !a.picking && action(a, 'pick').getAttribute('aria-pressed') === 'false', '「セルを選んで固定」をもう一度押すと選択をやめる');
  action(a, 'pick').click();
  const other = state.records.get(window.__fixtureTables.D);
  action(other, 'pick').click();
  await frame();
  check(!a.picking && other.picking && state.picking === other, '別の表を選ぶと前の表の選ぶ状態をやめる');
  const outside = new MouseEvent('click', { bubbles: true, cancelable: true });
  document.body.dispatchEvent(outside);
  await frame();
  check(!state.picking && !outside.defaultPrevented, '表の外のクリックを止めずに選ぶ状態だけをやめる');
  action(a, 'float').click();
  await frame();
  action(a, 'pick').click();
  const firstEscape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
  window.dispatchEvent(firstEscape);
  await frame();
  check(firstEscape.defaultPrevented && !state.picking && !!document.querySelector('[data-md-table-sticky-float]'), '全幅の中の Esc 1 回目ではセルを選ぶ状態だけをやめる');
  const secondEscape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
  window.dispatchEvent(secondEscape);
  await frame();
  check(secondEscape.defaultPrevented && !document.querySelector('[data-md-table-sticky-float]'), '全幅の中の Esc 2 回目で全幅表示を閉じる');
  return checks;
}

async function prepareFirstRoundPointer(kind) {
  const frame = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const state = window.__mdTableSticky;
  const a = state.records.get(window.__workaroundTable || window.__fixtureTables.A);
  const record = kind === 'sort' ? state.records.get(window.__fixtureTables.D) : a;
  const elements = Array.from(record.table.rows).flatMap(row => Array.from(row.cells));
  const action = name => record.bar.element.querySelector('[data-md-table-sticky-action="' + name + '"]');
  let index;
  let target;
  if (kind === 'link') {
    target = window.__pickLink;
    index = elements.indexOf(target.closest('td, th'));
  } else if (kind === 'sort') {
    target = record.table.querySelector('thead th button');
    index = elements.indexOf(target.closest('th'));
  } else {
    index = record.model.cells.findIndex(cell => kind === 'merged' ? cell.row === 2 && cell.col === 0 && cell.rowspan > 1 : cell.row === 4 && cell.col === 3);
    target = elements[index];
  }
  if (kind === 'pick' || kind === 'link' || kind === 'sort') action('pick').click();
  else if (!action('highlight').checked) action('highlight').click();
  record.scroll.scrollTop = 0;
  record.scroll.scrollLeft = 0;
  target.scrollIntoView({ block: 'center', inline: 'center' });
  record.scroll.scrollIntoView({ block: 'start', inline: 'nearest' });
  await frame();
  const cell = target.closest('td, th');
  const rect = target.getBoundingClientRect();
  const wrap = record.scroll.getBoundingClientRect();
  const left = Math.max(rect.left, wrap.left + record.scroll.clientLeft, 0);
  const right = Math.min(rect.right, wrap.right, innerWidth);
  const top = Math.max(rect.top, wrap.top + record.scroll.clientTop, 0);
  const bottom = Math.min(rect.bottom, wrap.bottom, innerHeight);
  const x = (left + right) / 2;
  const y = (top + bottom) / 2;
  const hit = document.elementFromPoint(x, y);
  window.__firstRoundPointer = { record, index, backgrounds: elements.map(element => getComputedStyle(element).backgroundColor), rows: Array.from(record.table.tBodies[0].rows, row => row.textContent), sort: record.table.querySelector('thead th')?.getAttribute('aria-sort'), hash: location.hash };
  return { x, y, hit: right > left && bottom > top && (hit === target || target.contains(hit) || (kind !== 'link' && kind !== 'sort' && cell.contains(hit))), pressed: action('pick').getAttribute('aria-pressed') };
}

function inspectFirstRoundPointer(kind) {
  const { record, index, backgrounds, rows, sort, hash } = window.__firstRoundPointer;
  const elements = Array.from(record.table.rows).flatMap(row => Array.from(row.cells));
  if (kind === 'pick' || kind === 'link' || kind === 'sort') {
    return { rows: record.layout.rows, cols: record.layout.cols, manualRows: record.override.rows, manualCols: record.override.cols, picking: !!record.picking, pressed: record.bar.element.querySelector('[data-md-table-sticky-action="pick"]').getAttribute('aria-pressed'), hashUnchanged: location.hash === hash, sortUnchanged: record.table.querySelector('thead th')?.getAttribute('aria-sort') === sort && Array.from(record.table.tBodies[0].rows, row => row.textContent).every((text, i) => text === rows[i]) };
  }
  const hovered = record.model.cells[index];
  const expected = record.model.cells.map((cell, i) => ({ cell, i })).filter(({ cell }) => cell.row < hovered.row + hovered.rowspan && hovered.row < cell.row + cell.rowspan || cell.col < hovered.col + hovered.colspan && hovered.col < cell.col + cell.colspan).map(({ i }) => i);
  const actual = elements.flatMap((element, i) => getComputedStyle(element).backgroundImage.includes('linear-gradient') ? [i] : []);
  return { expected, actual, self: getComputedStyle(elements[index]).backgroundImage, backgroundUnchanged: elements.every((element, i) => getComputedStyle(element).backgroundColor === backgrounds[i]) };
}

async function checkFirstRoundControls(cdp, version) {
  const checks = await cdp.evaluate('(' + exerciseFirstRoundControls.toString() + ')(' + JSON.stringify(version) + ')');
  for (const result of checks) assert.equal(result.pass, true, result.message);
  const frame = () => cdp.evaluate('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))');
  const point = async kind => {
    const position = await cdp.evaluate('(' + prepareFirstRoundPointer.toString() + ')(' + JSON.stringify(kind) + ')');
    assert.equal(position.hit, true, 'CDP で押すセルまたは載せるセルを画面内へ出す: ' + kind);
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: position.x, y: position.y });
    if (['pick', 'link', 'sort'].includes(kind)) {
      assert.equal(position.pressed, 'true', 'CDP のクリックの前にセルを選ぶ状態にする');
      await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x: position.x, y: position.y, button: 'left', clickCount: 1 });
      await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: position.x, y: position.y, button: 'left', clickCount: 1 });
    }
    await frame();
    return cdp.evaluate('(' + inspectFirstRoundPointer.toString() + ')(' + JSON.stringify(kind) + ')');
  };
  for (const kind of ['pick', 'link', ...(version === 'master' ? ['sort'] : [])]) {
    const result = await point(kind);
    if (kind === 'pick') assert.deepEqual([result.rows, result.cols, result.manualRows, result.manualCols], [2, 2, 2, 2], '本文の rowspan の中のセルを選ぶと行 2・列 2 の使える境目へ丸める');
    assert.equal(result.picking, false, 'セルを選ぶと選ぶ状態をやめる: ' + kind);
    assert.equal(result.pressed, 'false', 'セルを選んだ後はボタンの押下状態を戻す');
    if (kind === 'link') assert.equal(result.hashUnchanged, true, 'リンクを選んでも既定の遷移を行わない');
    if (kind === 'sort') assert.equal(result.sortUnchanged, true, '並べ替えのボタンを選んでも行の順と aria-sort を変えない');
  }
  await cdp.evaluate('(() => { for (const record of window.__mdTableSticky.records.values()) record.bar?.element.querySelector(\'[data-md-table-sticky-action="auto"]\').click(); })()');
  await frame();
  for (const kind of ['merged', 'normal']) {
    const result = await point(kind);
    assert.deepEqual(result.actual, result.expected, '載ったセルの覆う行または列と交わるセルだけを強調する: ' + kind);
    assert.ok(result.self.includes('linear-gradient'), '載ったセル自身も強調する');
    assert.equal(result.backgroundUnchanged, true, '強調しても固定セルの不透明な background-color を変えない');
  }
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: 0, y: 0 });
  await frame();
  assert.equal(await cdp.evaluate('Array.from(window.__firstRoundPointer.record.table.querySelectorAll("td, th")).every(cell => !cell.style.backgroundImage)'), true, '表の外へマウスを出すと強調を消す');
  await point('normal');
  await cdp.evaluate('window.__firstRoundPointer.record.bar.element.querySelector(\'[data-md-table-sticky-action="highlight"]\').click()');
  await frame();
  assert.equal(await cdp.evaluate('Array.from(window.__firstRoundPointer.record.table.querySelectorAll("td, th")).every(cell => !cell.style.backgroundImage)'), true, '強調のチェックを外すとすべての強調を消す');
}


// 2 回目の検索・絞り込み・コピーを、実際の入力とクリップボードの読み戻しで確かめる。
function prepareSearchCase(version) {
  const state = window.__mdTableSticky;
  const table = version === '19.5' ? window.__workaroundTable : window.__fixtureTables.A;
  const record = state.records.get(table);
  const bar = record.bar.element;
  const action = name => bar.querySelector('[data-md-table-sticky-action="' + name + '"]');
  action('auto').click();
  if (!action('scroll-x').checked) action('scroll-x').click();
  const actions = Array.from(bar.querySelectorAll('[data-md-table-sticky-action]'), element => element.dataset.mdTableStickyAction);
  action('search').click();
  const search = bar.nextElementSibling;
  const input = search.querySelector('[data-md-table-sticky-search-input]');
  const anchor = record.wrapper || record.scroll.closest('.gl-table-shadow-overlay') || record.scroll;
  window.__searchCase = { record, bar, search, input, anchor };
  return {
    order: actions.indexOf('nowrap-all') < actions.indexOf('search') && actions.indexOf('search') < actions.indexOf('copy') && actions.indexOf('copy') < actions.indexOf('float'),
    pressed: action('search').getAttribute('aria-pressed'),
    searchLabel: action('search').getAttribute('aria-label'),
    copyLabel: action('copy').getAttribute('aria-label'),
    line: search.hasAttribute('data-md-table-sticky-search') && search.nextElementSibling === record.columns.element && record.columns.element.nextElementSibling === anchor && search.parentElement === anchor.parentElement && !anchor.contains(search),
    focused: document.activeElement === input,
    input: input.type === 'search' && input.getAttribute('aria-label') === '表の中を検索' && input.placeholder === '表の中を検索',
    count: search.querySelector('[data-md-table-sticky-search-count]').textContent,
    live: search.querySelector('[data-md-table-sticky-search-count]').getAttribute('aria-live'),
    labels: [['search-prev', '前の一致へ（Shift+Enter）'], ['search-next', '次の一致へ（Enter）'], ['search-filter', '一致するセルのある行だけを表示する'], ['search-close', '検索を閉じる（Esc）']].every(([name, label]) => {
      const element = search.querySelector('[data-md-table-sticky-action="' + name + '"]');
      return element && element.title === label && element.getAttribute('aria-label') === label && !element.classList.contains('task-list-item-checkbox');
    }),
  };
}

function inspectSearchCase() {
  const { record, bar } = window.__searchCase;
  const search = bar.nextElementSibling;
  const input = search.querySelector('[data-md-table-sticky-search-input]');
  const elements = Array.from(record.table.rows).flatMap(row => Array.from(row.cells));
  const normalize = text => text.normalize('NFKC').toLowerCase().replace(/\s+/g, ' ').trim();
  const query = normalize(input.value);
  const matched = elements.flatMap((element, index) => query && normalize(element.textContent).includes(query) ? [index] : []);
  const yellow = elements.flatMap((element, index) => /255,\s*196,\s*0/.test(getComputedStyle(element).backgroundImage) ? [index] : []);
  const current = elements.flatMap((element, index) => getComputedStyle(element).outlineStyle === 'solid' && /255,\s*140,\s*0/.test(getComputedStyle(element).outlineColor) ? [index] : []);
  const cell = elements[current[0]];
  const rect = cell?.getBoundingClientRect();
  const wrap = record.scroll.getBoundingClientRect();
  const style = getComputedStyle(record.scroll);
  const role = record.layout.roles[current[0]];
  return {
    count: search.querySelector('[data-md-table-sticky-search-count]').textContent,
    matched, yellow, current,
    visible: !!rect && (role?.top || rect.top >= wrap.top + record.scroll.clientTop + parseFloat(style.scrollPaddingTop || 0) - 1) && (role?.left || rect.left >= wrap.left + record.scroll.clientLeft + parseFloat(style.scrollPaddingLeft || 0) - 1) && rect.top < wrap.top + record.scroll.clientHeight && rect.left < wrap.left + record.scroll.clientWidth,
    visibleRows: Array.from(record.table.rows).flatMap((row, index) => getComputedStyle(row).display === 'none' ? [] : [index]),
    filter: search.querySelector('[data-md-table-sticky-action="search-filter"]').checked,
    background: cell ? getComputedStyle(cell).backgroundImage : '',
  };
}

function copyExpectation() {
  const { record } = window.__searchCase;
  const rows = Array.from(record.table.rows).filter(row => getComputedStyle(row).display !== 'none');
  const grid = Array.from({ length: rows.length }, () => []);
  const cells = [];
  let cols = 0;
  rows.forEach((row, r) => {
    let col = 0;
    for (const cell of row.cells) {
      while (grid[r][col] !== undefined) col++;
      const text = cell.textContent.replace(/\s+/g, ' ').trim();
      const rowspan = cell.rowSpan || rows.length - r;
      cells.push({ row: r, col, rowspan, colspan: cell.colSpan, header: cell.tagName === 'TH', text });
      for (let rr = r; rr < r + rowspan; rr++) {
        for (let cc = col; cc < col + cell.colSpan; cc++) grid[rr][cc] = rr === r && cc === col ? text : '';
      }
      col += cell.colSpan;
      cols = Math.max(cols, col);
    }
  });
  const quote = text => /[\t\r\n"]/.test(text) ? '"' + text.replace(/"/g, '""') + '"' : text;
  return { rows: rows.length, cols, cells, tsv: grid.map(row => Array.from({ length: cols }, (_, col) => quote(row[col] || '')).join('\t')).join('\r\n') + '\r\n' };
}


// 実際のクリップボードを使えない環境でも、文字の取り出しと代替の順番を確かめる。
async function exerciseCopyFallbacks() {
  const checks = [];
  const check = (pass, message) => checks.push({ pass: !!pass, message });
  const state = window.__mdTableSticky;
  const record = state.records.get(window.__fixtureTables.F);
  const table = record.table;
  const cell = table.rows[table.rows.length - 1].cells[1];
  const savedNodes = Array.from(cell.childNodes);
  const saved = [
    [navigator, 'clipboard', Object.getOwnPropertyDescriptor(navigator, 'clipboard')],
    [window, 'ClipboardItem', Object.getOwnPropertyDescriptor(window, 'ClipboardItem')],
    [document, 'execCommand', Object.getOwnPropertyDescriptor(document, 'execCommand')],
  ];
  const define = (owner, name, value) => Object.defineProperty(owner, name, { configurable: true, writable: true, value });
  const inputCount = document.querySelectorAll('textarea[aria-label="コピー用の一時的な欄"]').length;
  try {
    try {
      define(navigator, 'clipboard', {});
      define(window, 'ClipboardItem', class { constructor(data) { this.data = data; } });
      define(document, 'execCommand', () => false);
    } catch (error) {
      return { unsupported: 'コピーの API を試験用に差し替えられない: ' + error.message, checks };
    }
    cell.innerHTML = '\n<p>  先頭   の文<br> 次  の行 </p><div>まとまり</div><ul><li> 項目  1 </li><li>項目 2<svg><text>除く svg</text></svg><span aria-hidden="true">除く aria</span></li></ul>\n';
    const richText = '先頭 の文\n次 の行\nまとまり\n項目 1\n項目 2';
    const quote = text => /[\t\r\n"]/.test(text) ? '"' + text.replace(/"/g, '""') + '"' : text;
    const expected = Array.from(table.rows, row => Array.from(row.cells, item => quote(item === cell ? richText : item.textContent.replace(/\s+/g, ' ').trim())).join('\t')).join('\r\n') + '\r\n';
    const size = '（' + table.rows.length + ' 行 × 2 列）';
    const textareaValue = expected.replace(/\r\n/g, '\n');
    const run = async (clipboard, command) => {
      define(navigator, 'clipboard', clipboard);
      define(document, 'execCommand', command);
      const before = state.notification;
      record.bar.element.querySelector('[data-md-table-sticky-action="copy"]').click();
      for (let attempt = 0; attempt < 50 && state.notification === before; attempt++) {
        await new Promise(resolve => setTimeout(resolve, 10));
      }
      return state.notification?.element.textContent;
    };
    const primary = {};
    const richNotice = await run({ write: async items => {
      for (const [type, blob] of Object.entries(items[0].data)) primary[type] = await blob.text();
    } }, () => { throw new Error('HTML をコピーできたら代替へ進めない'); });
    check(primary['text/plain'] === expected, 'br・p・div・li の境目を改行にし、各行の空白と前後の空行を整えて TSV にする');
    const htmlCell = new DOMParser().parseFromString(primary['text/html'] || '', 'text/html').querySelector('table')?.rows[table.rows.length - 1]?.cells[1];
    check(htmlCell?.innerHTML === '先頭 の文<br>次 の行<br>まとまり<br>項目 1<br>項目 2', 'HTML に改行を br で残し、svg と aria-hidden の子の文字をコピーしない');
    check(richNotice === '表をコピーした' + size, 'HTML と文字をコピーできたときは通常の知らせを出す');
    const calls = [];
    let written = '';
    const plainNotice = await run({
      write: async () => { calls.push('write'); throw new Error('試験用の HTML の失敗'); },
      writeText: async text => { calls.push('writeText'); written = text; },
    }, () => { calls.push('execCommand'); return true; });
    check(calls.join(',') === 'write,writeText' && written === expected, 'HTML の書き込みが失敗したら同じ TSV を writeText でコピーする');
    check(plainNotice === '表を文字だけコピーした' + size, 'writeText でコピーできたときは文字だけの知らせを出す');
    let temporary = null;
    let temporaryValid = false;
    calls.length = 0;
    const fallbackNotice = await run({
      write: async () => { calls.push('write'); throw new Error('試験用の HTML の失敗'); },
      writeText: async () => { calls.push('writeText'); throw new Error('試験用の文字の失敗'); },
    }, command => {
      calls.push(command);
      temporary = document.activeElement;
      temporaryValid = temporary?.tagName === 'TEXTAREA' && temporary.parentElement === document.body && !temporary.closest('.md') && temporary.value === textareaValue && temporary.selectionStart === 0 && temporary.selectionEnd === textareaValue.length;
      return true;
    });
    check(calls.join(',') === 'write,writeText,copy' && temporaryValid, '両方の API が失敗したら body 直下の一時の欄で TSV 全体を選んで copy する');
    check(!temporary?.isConnected && document.querySelectorAll('textarea[aria-label="コピー用の一時的な欄"]').length === inputCount, '代替でコピーした後は一時の欄を残さない');
    check(fallbackNotice === '表を文字だけコピーした' + size, '一時の欄でコピーできたときも文字だけの知らせを出す');
    const failedNotice = await run({}, () => false);
    check(failedNotice === '表をコピーできなかった', '両方の API がなく代替の copy も失敗したら失敗を知らせる');
    check(document.querySelectorAll('textarea[aria-label="コピー用の一時的な欄"]').length === inputCount, '失敗した後も一時の欄を残さない');
    return { checks };
  } finally {
    cell.replaceChildren(...savedNodes);
    for (const [owner, name, descriptor] of saved) {
      if (descriptor) Object.defineProperty(owner, name, descriptor);
      else delete owner[name];
    }
  }
}

async function checkSearchControls(cdp, t, version) {
  const frame = () => cdp.evaluate('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))');
  const inspect = () => cdp.evaluate('(' + inspectSearchCase.toString() + ')()');
  const type = async query => {
    await cdp.evaluate('(() => { const input = window.__searchCase.bar.nextElementSibling.querySelector("[data-md-table-sticky-search-input]"); input.focus(); input.select(); })()');
    await cdp.send('Input.insertText', { text: query });
    await delay(220);
    await frame();
  };
  const key = async (name, shift = false) => {
    const keyCode = name === 'Enter' ? 13 : 27;
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: name, code: name, windowsVirtualKeyCode: keyCode, nativeVirtualKeyCode: keyCode, modifiers: shift ? 8 : 0 });
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: name, code: name, windowsVirtualKeyCode: keyCode, nativeVirtualKeyCode: keyCode, modifiers: shift ? 8 : 0 });
    await frame();
  };
  const setup = await cdp.evaluate('(' + prepareSearchCase.toString() + ')(' + JSON.stringify(version) + ')');
  assert.equal(setup.order, true, 'ツールバーに折り返さない・検索・コピー・全幅表示の順で並べる');
  assert.equal(setup.pressed, 'true', '検索を開くと aria-pressed を true にする');
  assert.equal(setup.searchLabel, 'この表の中を検索する');
  assert.equal(setup.copyLabel, '表示中の行を、Excel に貼り付けられる形でコピーする');
  assert.equal(setup.line && setup.focused && setup.input && setup.labels, true, '検索行をツールバーと列のチェックボックスの間の Vue の包みの外へ置き、日本語の説明と欄のフォーカスを付ける');
  assert.equal(setup.count, '');
  assert.equal(setup.live, 'polite');
  // 見出しの「店舗」は含めず、本文の 30 店だけに一致させる。
  await type('の店');
  let result = await inspect();
  assert.equal(result.matched.length, 30, '標本の 30 店のセルが検索に一致する');
  assert.equal(result.count, '1 / 30 件', '150 ms を過ぎると最初の一致と総数を表示する');
  assert.deepEqual(result.yellow, result.matched, '一致したセルだけに黄色を重ねる');
  assert.deepEqual(result.current, [result.matched[0]], '今の一致だけに輪郭を付ける');
  await key('Enter', true);
  result = await inspect();
  assert.equal(result.count, '30 / 30 件', '最初の一致の Shift+Enter で最後へ回る');
  assert.deepEqual(result.current, [result.matched.at(-1)]);
  assert.equal(result.visible, true, '最後の本文の一致へ移しても固定した見出しと列の下に隠さない');
  await key('Enter');
  assert.equal((await inspect()).count, '1 / 30 件', '最後の一致の Enter で最初へ回る');
  await key('Enter');
  assert.equal((await inspect()).count, '2 / 30 件', 'Enter で次の一致へ移る');
  await cdp.evaluate('(() => { const input = window.__searchCase.input; input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true })); input.value = "一致しない文字列"; input.dispatchEvent(new InputEvent("input", { bubbles: true, isComposing: true })); })()');
  await delay(220);
  assert.equal((await inspect()).count, '2 / 30 件', 'IME の変換中は入力の一致を取り直さない');
  await key('Enter');
  assert.equal((await inspect()).count, '2 / 30 件', '変換中の Enter で一致を進めない');
  await cdp.evaluate('window.__searchCase.input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true }))');
  await delay(220);
  result = await inspect();
  assert.equal(result.count, '0 件', '変換が終わると文字を取り直す');
  assert.deepEqual(result.yellow, []);
  await type('地域');
  const stationary = await cdp.evaluate('(() => { const scroll = window.__searchCase.record.scroll; scroll.scrollTop = 150; scroll.scrollLeft = 200; return [scroll.scrollTop, scroll.scrollLeft]; })()');
  await key('Enter');
  assert.deepEqual(await cdp.evaluate('[window.__searchCase.record.scroll.scrollTop, window.__searchCase.record.scroll.scrollLeft]'), stationary, '固定した一致へ移るときは枠の中をスクロールしない');
  await type('西の店 2');
  await cdp.evaluate('window.__searchCase.bar.nextElementSibling.querySelector(\'[data-md-table-sticky-action="search-filter"]\').click()');
  await frame();
  result = await inspect();
  assert.deepEqual(result.visibleRows, [0, 1, 8, 9, 10, 11, 12, 13], '店の 2 行目だけが一致しても地域の rowspan=6 の全行と見出しだけを残す');
  const secondHeader = await cdp.evaluate('(() => { const { record } = window.__searchCase; const rows = record.table.rows; return { top: parseFloat(rows[1].cells[0].style.top), height: rows[0].getBoundingClientRect().height }; })()');
  near(secondHeader.top, secondHeader.height, '絞り込んだ後も 2 行目の見出しを 1 行目の高さの下へ固定する');
  await cdp.evaluate('(() => { const { record, bar } = window.__searchCase; const highlight = bar.querySelector(\'[data-md-table-sticky-action="highlight"]\'); if (!highlight.checked) highlight.click(); const cell = Array.from(record.table.rows).flatMap(row => Array.from(row.cells)).find(cell => cell.textContent.trim() === "西の店 2"); cell.scrollIntoView({ block: "nearest", inline: "nearest" }); record.scroll.scrollIntoView({ block: "start", inline: "nearest" }); window.__searchHoverCell = cell; })()');
  await frame();
  const point = await cdp.evaluate('(() => { const rect = window.__searchHoverCell.getBoundingClientRect(); return { x: (rect.left + rect.right) / 2, y: (rect.top + rect.bottom) / 2 }; })()');
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', ...point });
  await frame();
  assert.equal(await cdp.evaluate('(getComputedStyle(window.__searchHoverCell).backgroundImage.match(/linear-gradient/g) || []).length'), 2, '一致したセルに載ると検索と行・列の強調を 2 枚重ねる');
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: 0, y: 0 });
  await frame();
  result = await inspect();
  assert.deepEqual(result.yellow, result.matched, 'マウスを外しても検索の黄色を残す');
  await t.test(version + ' のクリップボード', async clip => {
    try {
      await cdp.send('Browser.grantPermissions', { permissions: ['clipboardReadWrite', 'clipboardSanitizedWrite'] });
      const available = await cdp.evaluate('!!(navigator.clipboard?.read && navigator.clipboard?.write && window.ClipboardItem)');
      if (!available) {
        clip.skip('headless のクリップボードで HTML の書き込みまたは読み戻しの API が使えない');
        return;
      }
    } catch (error) {
      clip.skip('headless のクリップボードの権限を与えられない: ' + error.message);
      return;
    }
    const expected = await cdp.evaluate('(' + copyExpectation.toString() + ')()');
    const position = await cdp.evaluate('(() => { const button = window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="copy"]\'); button.scrollIntoView({ block: "center", inline: "nearest" }); const rect = button.getBoundingClientRect(); return { x: (rect.left + rect.right) / 2, y: (rect.top + rect.bottom) / 2 }; })()');
    await cdp.evaluate("window.__noticeBefore = Array.from(document.querySelectorAll('body > [role=status]')).at(-1) || null");
    await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', ...position, button: 'left', clickCount: 1 });
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', ...position, button: 'left', clickCount: 1 });
    // 書き込みが終わってから出る知らせを待ってから読み戻す（途中で読むと Clipboard data has changed になる）。
    await waitFor(cdp, '(() => { const notice = Array.from(document.querySelectorAll("body > [role=status]")).at(-1); return !!notice && notice !== window.__noticeBefore && /コピー/.test(notice.textContent); })()', 'コピーの知らせが出ない');
    let copied;
    try {
      copied = await cdp.evaluate('(async () => { const result = {}; for (const item of await navigator.clipboard.read()) for (const type of item.types) result[type] = await (await item.getType(type)).text(); return result; })()');
    } catch (error) {
      clip.skip('headless のクリップボードを読み戻せない: ' + error.message);
      return;
    }
    assert.equal(copied['text/plain'], expected.tsv, '見えている行の TSV は結合セルの左上だけに値を持ち、今の DOM の順でコピーする');
    assert.ok(copied['text/html'], 'HTML も同時にコピーする');
    const html = await cdp.evaluate('(' + ((text) => {
      const table = new DOMParser().parseFromString(text, 'text/html').querySelector('table');
      return { rows: table?.rows.length, cells: table ? Array.from(table.rows).flatMap(row => Array.from(row.cells, cell => ({ text: cell.textContent, rowspan: cell.rowSpan, colspan: cell.colSpan, header: cell.tagName === 'TH' }))) : [] };
    }).toString() + ')(' + JSON.stringify(copied['text/html']) + ')');
    assert.equal(html.rows, expected.rows, 'HTML は見出しと見えている 6 行だけを持つ');
    assert.deepEqual(html.cells, expected.cells.map(({ text, rowspan, colspan, header }) => ({ text, rowspan, colspan, header })), 'HTML は見えている行の結合と th・td を残す');
    assert.equal(await cdp.evaluate('Array.from(document.querySelectorAll("body > [role=status]")).at(-1)?.textContent'), '表をコピーした（' + expected.rows + ' 行 × ' + expected.cols + ' 列）', 'HTML と TSV をコピーした行数・列数を知らせる');
    await cdp.evaluate('(() => { const table = window.__fixtureTables.D; const rows = Array.from(table.tBodies[0].rows); window.__copySortCase = { table, rows, arias: Array.from(table.tHead.rows[0].cells, cell => cell.getAttribute("aria-sort")) }; const button = table.querySelector("thead th button"); button.click(); if (table.tBodies[0].rows[0] !== rows.at(-1)) button.click(); })()');
    try {
      await waitFor(cdp, 'window.__copySortCase.table.tBodies[0].rows[0] === window.__copySortCase.rows.at(-1)', 'コピーする前に並べ替えのボタンで本文を降順へ変える');
      await delay(220);
      await frame();
      const sortedExpected = await cdp.evaluate('(() => { const { table, rows } = window.__copySortCase; const text = cell => cell.textContent.replace(/\\s+/g, " ").trim(); return { firstColumn: [text(table.rows[0].cells[0]), ...rows.slice().reverse().map(row => text(row.cells[0]))], tsv: Array.from(table.rows, row => Array.from(row.cells, text).join("\\t")).join("\\r\\n") + "\\r\\n" }; })()');
      const sortedPosition = await cdp.evaluate('(() => { const record = window.__mdTableSticky.records.get(window.__copySortCase.table); const button = record.bar.element.querySelector(\'[data-md-table-sticky-action="copy"]\'); button.scrollIntoView({ block: "center", inline: "nearest" }); const rect = button.getBoundingClientRect(); return { x: (rect.left + rect.right) / 2, y: (rect.top + rect.bottom) / 2 }; })()');
      await cdp.evaluate("window.__noticeBefore = Array.from(document.querySelectorAll('body > [role=status]')).at(-1) || null");
      await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', ...sortedPosition, button: 'left', clickCount: 1 });
      await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', ...sortedPosition, button: 'left', clickCount: 1 });
      await waitFor(cdp, '(() => { const notice = Array.from(document.querySelectorAll("body > [role=status]")).at(-1); return !!notice && notice !== window.__noticeBefore && /コピー/.test(notice.textContent); })()', '並べ替え後のコピーの知らせが出ない');
      let sortedCopied;
      try {
        sortedCopied = await cdp.evaluate('(async () => { const result = {}; for (const item of await navigator.clipboard.read()) for (const type of item.types) result[type] = await (await item.getType(type)).text(); return result; })()');
      } catch (error) {
        clip.skip('headless の並べ替え後のクリップボードを読み戻せない: ' + error.message);
        return;
      }
      assert.equal(sortedCopied['text/plain'], sortedExpected.tsv, '並べ替えのボタンで変えた現在の DOM の順で TSV をコピーする');
      const firstColumn = await cdp.evaluate('(' + (text => {
        const table = new DOMParser().parseFromString(text, 'text/html').querySelector('table');
        return table ? Array.from(table.rows, row => row.cells[0].textContent) : [];
      }).toString() + ')(' + JSON.stringify(sortedCopied['text/html'] || '') + ')');
      assert.deepEqual(firstColumn, sortedExpected.firstColumn, 'HTML の本文も並べ替える前の逆順になり、見出しは先頭へ残る');
    } finally {
      await cdp.evaluate('(() => { const { table, rows, arias } = window.__copySortCase; table.tBodies[0].append(...rows); Array.from(table.tHead.rows[0].cells).forEach((cell, index) => { if (arias[index] === null) cell.removeAttribute("aria-sort"); else cell.setAttribute("aria-sort", arias[index]); }); })()');
      await frame();
    }

  });
  await t.test(version + ' の文字の取り出しと代替コピー', async fallback => {
    const result = await cdp.evaluate('(' + exerciseCopyFallbacks.toString() + ')()');
    if (result.unsupported) {
      fallback.skip(result.unsupported);
      return;
    }
    for (const check of result.checks) assert.equal(check.pass, true, check.message);
  });
  await cdp.evaluate('window.__searchCase.bar.nextElementSibling.querySelector(\'[data-md-table-sticky-action="search-filter"]\').click()');
  await frame();
  assert.equal((await inspect()).visibleRows.length, 32, '絞り込みを外すとすべての行を戻す');
  await cdp.evaluate('window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="float"]\').click()');
  await frame();
  assert.equal(await cdp.evaluate('(() => { const { bar, anchor, record } = window.__searchCase; const search = bar.nextElementSibling; const panel = document.querySelector("[data-md-table-sticky-float-panel]"); return panel.children[0] === bar && panel.children[1] === search && search.hasAttribute("data-md-table-sticky-search") && panel.children[2] === record.columns.element && panel.children[3] === anchor; })()'), true, '全幅のパネルへ検索行もツールバー・検索・列のチェックボックス・枠の順で移す');
  await cdp.evaluate('window.__searchCase.bar.nextElementSibling.querySelector("[data-md-table-sticky-search-input]").focus()');
  await key('Escape');
  assert.equal(await cdp.evaluate('!!document.querySelector("[data-md-table-sticky-float]") && window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="search"]\').getAttribute("aria-pressed") === "false"'), true, '検索欄の Esc 1 回目は検索だけを閉じる');
  await key('Escape');
  assert.equal(await cdp.evaluate('(() => { const { bar, anchor, record } = window.__searchCase; return !document.querySelector("[data-md-table-sticky-float]") && bar.nextElementSibling === record.columns.element && record.columns.element.nextElementSibling === anchor; })()'), true, 'Esc 2 回目で全幅を閉じて元の並びへ戻す');
  await cdp.evaluate('window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="search"]\').click()');
  assert.equal(await cdp.evaluate('(() => { const { bar } = window.__searchCase; const search = bar.nextElementSibling; return search.querySelector("input[type=search]").value === "" && search.querySelector("[data-md-table-sticky-search-count]").textContent === "" && !search.querySelector(\'[data-md-table-sticky-action="search-filter"]\').checked; })()'), true, '検索を閉じると欄・件数・絞り込みを空へ戻す');
  // 検索の行を開いたまま、要素ではない window に Esc が届いても全幅表示を閉じる。
  await cdp.evaluate('window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="float"]\').click()');
  await frame();
  assert.equal(await cdp.evaluate('(() => { const event = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }); window.dispatchEvent(event); return event.defaultPrevented && !document.querySelector("[data-md-table-sticky-float]"); })()'), true, '検索の行を開いたまま window に届いた Esc でも全幅表示を閉じる');
  await cdp.evaluate('window.dispatchEvent(new Event("beforeprint"))');
  assert.equal(await cdp.evaluate('window.__searchCase.bar.nextElementSibling.getClientRects().length === 0'), true, '印刷の前は検索行を隠す');
  await cdp.evaluate('window.dispatchEvent(new Event("afterprint"))');
  await frame();
  assert.equal(await cdp.evaluate('window.__searchCase.bar.nextElementSibling.hasAttribute("data-md-table-sticky-search") && window.__searchCase.bar.nextElementSibling.getClientRects().length > 0'), true, '印刷の後に検索行を戻す');
  await cdp.evaluate('window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="search"]\').click()');
  assert.equal(await cdp.evaluate('window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="search"]\').getAttribute("aria-pressed") === "false" && window.__searchCase.bar.nextElementSibling === window.__searchCase.record.columns.element'), true, '検索ボタンをもう一度押しても検索を閉じる');
  await cdp.evaluate('window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="search"]\').click()');
  await cdp.evaluate('window.__searchCase.bar.nextElementSibling.querySelector(\'[data-md-table-sticky-action="search-close"]\').click()');
  assert.equal(await cdp.evaluate('document.activeElement === window.__searchCase.bar.querySelector(\'[data-md-table-sticky-action="search"]\')'), true, '閉じるボタンで検索を閉じるとツールバーの検索へフォーカスを戻す');
}

async function checkOwnedMutationScope(cdp, bookmarklet) {
  // 自分のツールバー・列のチェックボックス・パネルの追加と削除から、遅れて表を測り直さないことを確かめる。
  const result = await whileVisible(cdp, () => cdp.evaluate('(async () => {\n' +
    'const run = ' + JSON.stringify(bookmarklet) + ';\n' +
    'const tables = { A: window.__fixtureTables.A, D: window.__fixtureTables.D };\n' +
    'const counts = { A: 0, D: 0 };\n' +
    'const original = Element.prototype.getBoundingClientRect;\n' +
    'const frame = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));\n' +
    'Element.prototype.getBoundingClientRect = function () { for (const [name, table] of Object.entries(tables)) if (this === table || table.contains(this)) counts[name]++; return original.call(this); };\n' +
    'const delayed = [];\n' +
    'const settle = async () => { await frame(); counts.A = counts.D = 0; await new Promise(resolve => setTimeout(resolve, 450)); delayed.push({ ...counts }); };\n' +
    'try {\n' +
    '(0, eval)(run); (0, eval)(run); await settle();\n' +
    'const bar = window.__mdTableSticky.records.get(tables.A).bar.element;\n' +
    'bar.querySelector(\'[data-md-table-sticky-action="nowrap-all"]\').click(); await settle();\n' +
    'bar.querySelector(\'[data-md-table-sticky-action="wrap-all"]\').click(); await settle();\n' +
    'bar.querySelector(\'[data-md-table-sticky-action="float"]\').click(); await settle();\n' +
    'bar.querySelector(\'[data-md-table-sticky-action="float"]\').click(); await settle();\n' +
    'return delayed;\n' +
    '} finally { Element.prototype.getBoundingClientRect = original; }\n' +
    '})()'));
  assert.deepEqual(result, Array.from({ length: 5 }, () => ({ A: 0, D: 0 })), 'ツールバー・列のチェックボックス・パネルを作る操作の後に見張りが余分な測り直しを走らせない');
}


async function checkRawNowrap(cdp, directory, version, bookmarklet) {
  const pageUrl = await createPage(directory, version, false);
  await cdp.send('Page.navigate', { url: pageUrl });
  await waitFor(cdp, 'location.href === ' + JSON.stringify(pageUrl) + ' && window.__fixtureReady === true', '描き直す前の確かめ用のページが開かない');
  assert.equal(await cdp.evaluate('!!window.__fixtureTables.J && !!window.__fixtureTables.K'), true, '表 J・K が sample.gitlab.html に必要。Claude Code が tools/render-gitlab.mjs で API の HTML を再生成してからブラウザー試験を実行する');
  assert.deepEqual(await cdp.evaluate('window.__kInitialWrap'), [false, false, true], 'API の HTML では表 K の先頭 2 列が見出しも本文も nowrap、3 列目は折り返す');
  await cdp.evaluate(bookmarklet);
  await cdp.evaluate('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))');
  const wrap = await cdp.evaluate('(() => { const bar = window.__mdTableSticky.records.get(window.__fixtureTables.K).bar.element; return Array.from(bar.nextElementSibling.querySelectorAll("input[data-md-table-sticky-column]"), input => input.checked); })()');
  assert.deepEqual(wrap, [false, false, true], 'GitLab の描き直し前の Markdown の nowrap を初めのチェックに生かす');
  await cdp.evaluate(bookmarklet);
  assert.deepEqual(await cdp.evaluate('Array.from(document.querySelectorAll(".md"), md => md.innerHTML)'), await cdp.evaluate('window.__beforeHtml'), 'nowrap の初期値を適用して解除した後も API の HTML に戻す');
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
  let keepVisible;
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
      // headless の Edge は、起動から十数秒後に窓の中の高さを 30px ほど縮めることがある。表の高さ（70vh）と GitLab の全体表示のボタンを揃えるため、表示の大きさを固定する。
      await cdp.send("Emulation.setDeviceMetricsOverride", { width: 1248, height: 804, deviceScaleFactor: 1, mobile: false });
      // Windows の環境によっては、headless の Edge が開いてから約 20 秒でページを隠し、requestAnimationFrame が止まる。
      // 1 秒ごとに前面へ出し、隠れても 2 秒以内に戻す。
      const page = cdp;
      keepVisible = setInterval(() => { page.send("Page.bringToFront").catch(() => {}); }, 1000);
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
      await t.test(version + " の表", async (versionTest) => {
        await checkRawNowrap(cdp, directory, version, bookmarklet);
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
        assert.equal(await cdp.evaluate("Math.abs(window.__fixtureTables.F.getBoundingClientRect().width - window.__smallBefore.width) <= 1"), true, "小さい表 F の幅を押す前と 1px 以内に保つ");
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
        for (const edge of stickyDetails.topEdges) assert.equal(edge.covered, true, "見出しの上端のすき間をセルが覆う: " + edge.text);
        for (const edge of stickyDetails.leftEdges) assert.equal(edge.covered, true, "見出しの左端のすき間をセルが覆う: " + edge.text);
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
        await checkDisplayControls(cdp);
        await checkOwnedMutationScope(cdp, bookmarklet);
        await checkFirstRoundControls(cdp, version);
        await checkSearchControls(cdp, versionTest, version);
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
        await cdp.evaluate('window.__mdTableSticky.records.get(window.__fixtureTables.A).bar.element.querySelector(\'[data-md-table-sticky-action="float"]\').click()');
        assert.equal(await cdp.evaluate("!!document.querySelector('[data-md-table-sticky-float]')"), true, "全幅表示を開いたまま解除する");
        await cdp.evaluate('(() => { const record = window.__mdTableSticky.records.get(window.__fixtureTables.A); for (const name of ["highlight", "compact", "pick"]) record.bar.element.querySelector(\'[data-md-table-sticky-action="\' + name + \'"]\').click(); window.__cleanupPickCell = record.table.rows[2].cells[1]; })()');
        assert.equal(await cdp.evaluate('!!window.__mdTableSticky.picking'), true, 'セルを選ぶ状態のまま解除する');
        await cdp.evaluate('(() => { const record = window.__mdTableSticky.records.get(window.__fixtureTables.A); record.bar.element.querySelector(\'[data-md-table-sticky-action="search"]\').click(); const input = record.bar.element.nextElementSibling.querySelector("[data-md-table-sticky-search-input]"); window.__cleanupSearch = { record, input }; input.focus(); })()');
        await cdp.send('Input.insertText', { text: '西の店 2' });
        await delay(220);
        await cdp.evaluate('window.__cleanupSearch.record.bar.element.nextElementSibling.querySelector(\'[data-md-table-sticky-action="search-filter"]\').click()');
        await cdp.evaluate("new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
        assert.equal(await cdp.evaluate('Array.from(window.__cleanupSearch.record.table.rows).some(row => getComputedStyle(row).display === "none")'), true, '検索行を開き、絞り込んだまま解除する');
        await cdp.evaluate('(() => { const input = window.__cleanupSearch.input; input.value = "解除前の入力"; input.dispatchEvent(new InputEvent("input", { bubbles: true })); })()');
        await cdp.evaluate(bookmarklet);
        assert.equal(await cdp.evaluate("document.querySelectorAll('[data-md-table-sticky-float], [data-md-table-sticky-placeholder], [data-md-table-sticky-columns], [data-md-table-sticky-search]').length"), 0, "解除すると全幅の背景・代わりの要素・列のチェックボックス・検索行を消す");
        assert.equal(await cdp.evaluate("typeof window.__mdTableSticky"), "undefined", "固定の状態を消す");
        assert.equal(await cdp.evaluate("document.querySelectorAll('[data-md-table-sticky-bar]').length"), 0, "解除するとツールバーも消す");
        // mimic の高さのボタンは ResizeObserver で付け外しするので、表の寸法が戻って知らせが届くまで待つ。
        await cdp.evaluate("new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
        const restored = await cdp.evaluate("Array.from(document.querySelectorAll('.md'), md => md.innerHTML)");
        const before = await cdp.evaluate("window.__beforeHtml");
        assert.deepEqual(restored, before, "もう一度押すと Markdown の DOM を元へ戻す");
        await cdp.evaluate('(() => { const { record, input } = window.__cleanupSearch; window.__cleanupSearch.after = JSON.stringify(record.search); input.value = "解除後の入力"; input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true })); input.dispatchEvent(new InputEvent("input", { bubbles: true })); input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true })); input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true })); })()');
        await delay(220);
        await cdp.evaluate("new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
        assert.equal(await cdp.evaluate('JSON.stringify(window.__cleanupSearch.record.search) === window.__cleanupSearch.after'), true, '解除した検索欄に入力・変換・Enter を送っても受け手が残っていない');
        assert.deepEqual(await cdp.evaluate("Array.from(document.querySelectorAll('.md'), md => md.innerHTML)"), before, '解除前の入力タイマーの期限が過ぎても元の DOM を保つ');
        assert.equal(await cdp.evaluate('(() => { let received = false; const cell = window.__cleanupPickCell; cell.addEventListener("click", event => { received = !event.defaultPrevented; event.preventDefault(); }, { once: true }); cell.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true })); return received; })()'), true, '選ぶ状態で解除しても、その後のセルのクリックを止めない');
        await checkDynamic(cdp, bookmarklet);
      });
    }
  } finally {
    clearInterval(keepVisible);
    if (cdp) cdp.close();
    await stopBrowser(child);
    await removeTemporary(directory);
  }
});
