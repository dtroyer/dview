// dView web UI: two images side by side with their metadata compared below.

const TOP = "/"; // the top of the file tree (the virtual root)

// log sends a message to the server's log when dview runs with -x.
const log = (level, msg, data) => window.dviewLog?.(level, msg, data);

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

function el(tag, props = {}, ...children) {
  const e = document.createElement(tag);
  Object.assign(e, props);
  for (const c of children) e.append(c);
  return e;
}

async function getJSON(url) {
  const resp = await fetch(url);
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(body.error || resp.statusText);
  return body;
}

const q = (path) => encodeURIComponent(path);

// ---- State ----------------------------------------------------------------

// Each Open button remembers its own directory, also across reloads.
function loadDir(i) {
  try { return localStorage.getItem(`dview.dir.${i}`) || TOP; } catch { return TOP; }
}
function saveDir(i, dir) {
  try { localStorage.setItem(`dview.dir.${i}`, dir); } catch { /* not available */ }
}

const state = {
  images: [null, null], // {path, display} for image 1 (left) and image 2 (right)
  dirs: [loadDir(0), loadDir(1)],
  last: null, // the last /api/compare response
  // Show only changed lines: on unless turned off earlier.
  onlyChanges: (() => {
    try { return localStorage.getItem("dview.onlyChanges") !== "0"; } catch { return true; }
  })(),
};

// ---- File dialog ------------------------------------------------------------

const picker = (() => {
  const dialog = $("#picker");
  const list = $(".picker-list", dialog);
  const pathEl = $(".picker-path", dialog);
  const count = $(".picker-count", dialog);
  const openBtn = $('[data-action="open"]', dialog);
  const cancelBtn = $('[data-action="cancel"]', dialog);

  let listing = null;
  let selected = new Set();
  let resolve = null;

  function files() {
    // Selected files in the order the dialog lists them.
    return listing ? listing.entries.filter((e) => !e.dir && selected.has(e.path)) : [];
  }

  function updateSelection() {
    for (const li of $$("li[data-path]", list)) {
      if (li.dataset.dir !== "true") li.setAttribute("aria-selected", selected.has(li.dataset.path));
    }
    const n = selected.size;
    count.textContent = n === 0 ? "" : n === 1 ? "1 file selected" : `${n} files selected` + (n > 2 ? " (first 2 used)" : "");
    openBtn.disabled = n === 0;
  }

  function row(name, path, isDir, icon) {
    const li = el("li", { className: isDir ? "dir" : "file", role: "option" },
      el("span", { className: "icon", textContent: icon, ariaHidden: "true" }),
      el("span", { className: "name", textContent: name }));
    li.dataset.path = path;
    li.dataset.dir = isDir;
    return li;
  }

  async function go(dir) {
    log("debug", "dialog: listing directory", { dir });
    try {
      listing = await getJSON(`/api/list?path=${q(dir)}`);
    } catch (err) {
      log("warn", "dialog: listing failed", { dir, error: err.message });
      if (dir !== TOP) return go(TOP); // the remembered directory is gone
      listing = null;
      pathEl.textContent = "";
      list.replaceChildren(el("li", { className: "error", textContent: `Can't list files: ${err.message}` }));
      return;
    }
    selected = new Set();
    pathEl.textContent = listing.display;
    pathEl.title = listing.display;
    const rows = [];
    if (listing.parent) rows.push(row("..", listing.parent, true, "↑"));
    for (const e of listing.entries) rows.push(row(e.name, e.path, e.dir, e.dir ? "▸" : "▫"));
    if (rows.length === 0) rows.push(el("li", { className: "empty", textContent: "No folders or images here" }));
    list.replaceChildren(...rows);
    list.scrollTop = 0;
    updateSelection();
  }

  function finish(chosen) {
    log("debug", "dialog: closed", { dir: listing ? listing.path : TOP, files: chosen.map((f) => f.path) });
    const done = resolve;
    resolve = null;
    if (dialog.open) dialog.close();
    done?.({ files: chosen, dir: listing ? listing.path : TOP });
  }

  list.addEventListener("click", (ev) => {
    const li = ev.target.closest("li[data-path]");
    if (!li) return;
    if (li.dataset.dir === "true") { go(li.dataset.path); return; }
    const p = li.dataset.path;
    if (selected.has(p)) selected.delete(p); else selected.add(p);
    updateSelection();
  });
  list.addEventListener("dblclick", (ev) => {
    const li = ev.target.closest("li[data-path]");
    if (!li || li.dataset.dir === "true") return;
    selected = new Set([li.dataset.path]);
    finish(files());
  });
  dialog.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && selected.size > 0) { ev.preventDefault(); finish(files()); }
  });
  openBtn.addEventListener("click", () => finish(files()));
  cancelBtn.addEventListener("click", () => finish([]));
  dialog.addEventListener("close", () => { if (resolve) finish([]); }); // Escape

  return {
    // open shows the dialog at dir and resolves to {files, dir}, where dir is
    // the directory the dialog was in when it closed.
    open(dir) {
      log("debug", "dialog: opening", { dir });
      return new Promise((res) => {
        resolve = res;
        list.replaceChildren();
        dialog.showModal();
        list.focus();
        go(dir);
      });
    },
  };
})();

// ---- Rendering ----------------------------------------------------------------

function field(label, ...content) {
  return el("div", { className: "field" },
    el("div", { className: "label", textContent: label }), ...content);
}

function plainText(text) {
  return el("div", { className: "text", textContent: text });
}

function segmentNode(s) {
  return s.op === "equal" ? document.createTextNode(s.text)
    : el("span", { className: s.op === "insert" ? "ins" : "del", textContent: s.text });
}

// splitLines turns diff segments into lines: an array of lines, each an
// array of segments, so lines can be shown or hidden on their own.
function splitLines(segments) {
  const lines = [[]];
  for (const s of segments) {
    s.text.split("\n").forEach((part, n) => {
      if (n > 0) lines.push([]);
      if (part !== "") lines[lines.length - 1].push({ op: s.op, text: part });
    });
  }
  return lines;
}

// diffText shows a word diff. With onlyChanges, only the lines that contain
// a change are shown, with a marker where unchanged lines were left out.
function diffText(segments, onlyChanges) {
  const div = el("div", { className: "text" });
  if (!onlyChanges) {
    for (const s of segments) div.append(segmentNode(s));
    return div;
  }
  const lines = splitLines(segments);
  const changed = lines.map((line) => line.some((s) => s.op !== "equal"));
  if (!changed.includes(true)) return note("No changes.");
  let first = true;
  let skipped = false; // unchanged lines were left out since the last shown line
  const addLine = (nodes) => {
    if (!first) div.append("\n");
    div.append(...nodes);
    first = false;
  };
  const gap = () => el("span", { className: "gap", textContent: "⋯" });
  lines.forEach((line, n) => {
    if (!changed[n]) { skipped = true; return; }
    if (skipped) addLine([gap()]);
    skipped = false;
    addLine(line.map(segmentNode));
  });
  if (skipped) addLine([gap()]);
  return div;
}

function plainSettings(settings) {
  const pre = el("pre", { className: "settings" });
  for (const s of settings) pre.append(el("div", { className: "line", textContent: `${s.key}: ${s.value}` }));
  return pre;
}

function diffSettings(lines, onlyChanges) {
  if (onlyChanges) {
    lines = lines.filter((l) => l.op !== "equal");
    if (lines.length === 0) return note("No changes.");
  }
  const pre = el("pre", { className: "settings" });
  const sign = { equal: " ", insert: "+", delete: "-" };
  const cls = { equal: "line", insert: "line ins", delete: "line del" };
  for (const l of lines) {
    pre.append(el("div", { className: cls[l.op], textContent: `${sign[l.op]} ${l.key}: ${l.value}` }));
  }
  return pre;
}

function note(text) {
  return el("p", { className: "note", textContent: text });
}

// renderMeta fills one metadata panel. diff is set only for image 2 when both
// images have metadata.
function renderMeta(panel, side, diff, onlyChanges = false) {
  if (!side) { panel.replaceChildren(note("No image selected.")); return; }
  if (side.status === "missing") { panel.replaceChildren(note("This image has no generation metadata.")); return; }
  if (side.status !== "ok") {
    panel.replaceChildren(note(`This image's metadata couldn't be read: ${side.error || "unknown error"}`));
    return;
  }
  const p = side.params;
  const parts = [];
  if (diff) {
    parts.push(field("Prompt", diffText(diff.prompt, onlyChanges)));
    if (diff.negative.length) parts.push(field("Negative prompt", diffText(diff.negative, onlyChanges)));
    if (diff.settings.length) parts.push(field("Settings", diffSettings(diff.settings, onlyChanges)));
  } else {
    parts.push(field("Prompt", plainText(p.prompt)));
    if (p.negative) parts.push(field("Negative prompt", plainText(p.negative)));
    if (p.settings.length) parts.push(field("Settings", plainSettings(p.settings)));
  }
  panel.replaceChildren(...parts);
}

function renderImage(i) {
  const img = state.images[i];
  const name = $(`.filename[data-side="${i}"]`);
  name.textContent = img ? img.display : "No image";
  name.title = img ? img.display : "";
  name.classList.toggle("set", !!img);

  const frame = $(`.frame[data-side="${i}"]`);
  if (!img) {
    frame.replaceChildren(el("div", { className: "placeholder", textContent: `Image ${i + 1}` }));
    return;
  }
  const tag = el("img", { src: `/api/image?path=${q(img.path)}`, alt: img.display });
  tag.addEventListener("error", () => {
    frame.replaceChildren(el("div", { className: "placeholder", textContent: "Couldn't load this image" }));
  });
  frame.replaceChildren(tag);
}

let refreshSeq = 0;

const panels = [$('.meta[data-side="0"]'), $('.meta[data-side="1"]')];
const onlyChangesBox = $("#only-changes");

// renderComparison shows the last comparison, honoring the checkbox. The
// checkbox only applies when there is a diff to filter.
function renderComparison() {
  const resp = state.last;
  onlyChangesBox.checked = state.onlyChanges;
  onlyChangesBox.disabled = !(resp && resp.diff);
  if (!resp) {
    panels.forEach((p) => renderMeta(p, null));
    return;
  }
  renderMeta(panels[0], resp.a, null);
  renderMeta(panels[1], resp.b, resp.diff, state.onlyChanges);
}

onlyChangesBox.addEventListener("change", () => {
  state.onlyChanges = onlyChangesBox.checked;
  try { localStorage.setItem("dview.onlyChanges", state.onlyChanges ? "1" : "0"); } catch { /* not available */ }
  log("debug", "only changed lines", { on: state.onlyChanges });
  renderComparison();
});

async function refresh() {
  renderImage(0);
  renderImage(1);
  const [a, b] = state.images;
  if (!a && !b) {
    state.last = null;
    renderComparison();
    return;
  }
  const seq = ++refreshSeq;
  const params = new URLSearchParams();
  if (a) params.set("a", a.path);
  if (b) params.set("b", b.path);
  log("debug", "comparing", { a: a ? a.path : "", b: b ? b.path : "" });
  try {
    const resp = await getJSON(`/api/compare?${params}`);
    if (seq !== refreshSeq) return; // a newer request superseded this one
    state.last = resp;
    renderComparison();
  } catch (err) {
    if (seq !== refreshSeq) return;
    log("error", "compare failed", { error: err.message });
    state.last = null;
    onlyChangesBox.disabled = true;
    panels.forEach((p) => p.replaceChildren(note(`Couldn't load metadata: ${err.message}`)));
  }
}

// ---- Open buttons -------------------------------------------------------------

for (const btn of $$("button.open")) {
  const i = Number(btn.dataset.side);
  btn.addEventListener("click", async () => {
    log("debug", "open button clicked", { side: i, dir: state.dirs[i] });
    const { files, dir } = await picker.open(state.dirs[i]);
    state.dirs[i] = dir;
    saveDir(i, dir);
    if (files.length === 0) return;
    // The first file goes to this button's image, a second to the other one.
    state.images[i] = files[0];
    if (files.length > 1) state.images[1 - i] = files[1];
    refresh();
  });
}

refresh();
log("debug", "app ready", { openButtons: $$("button.open").length });
