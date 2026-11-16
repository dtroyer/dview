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
  updateSendButtons();
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
    updateSendButtons();
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
    updateSendButtons();
    panels.forEach((p) => p.replaceChildren(note(`Couldn't load metadata: ${err.message}`)));
  }
}

// ---- Messages -------------------------------------------------------------------

const toastEl = $("#toast");
let toastTimer = 0;

function toast(text, isError = false) {
  toastEl.textContent = text;
  toastEl.classList.toggle("error", isError);
  toastEl.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastEl.hidden = true; }, 10000);
}
toastEl.addEventListener("click", () => { toastEl.hidden = true; });

// ---- Back end URL -----------------------------------------------------------

const MAX_URLS = 4;
const urlInput = $("#backend-url");
const urlToggle = $(".combo-toggle");
const urlMenu = $("#backend-history");

function validURL(s) {
  try {
    const u = new URL(s);
    return (u.protocol === "http:" || u.protocol === "https:") && u.host !== "";
  } catch { return false; }
}

// The last few URLs used, most recent first, kept in the browser.
state.urls = (() => {
  try {
    const v = JSON.parse(localStorage.getItem("dview.urls") || "[]");
    return Array.isArray(v) ? v.filter((u) => typeof u === "string" && validURL(u)).slice(0, MAX_URLS) : [];
  } catch { return []; }
})();

function rememberURL(url) {
  if (!validURL(url)) return;
  state.urls = [url, ...state.urls.filter((u) => u !== url)].slice(0, MAX_URLS);
  try { localStorage.setItem("dview.urls", JSON.stringify(state.urls)); } catch { /* not available */ }
}

function checkURL() {
  const v = urlInput.value.trim();
  urlInput.classList.toggle("invalid", v !== "" && !validURL(v));
}

function closeMenu() {
  urlMenu.hidden = true;
  urlToggle.setAttribute("aria-expanded", "false");
}

function openMenu() {
  const items = state.urls.map((u) => {
    const li = el("li", { role: "option", textContent: u, title: u });
    li.setAttribute("aria-selected", u === urlInput.value.trim());
    li.addEventListener("click", () => {
      urlInput.value = u;
      checkURL();
      rememberURL(u);
      closeMenu();
      log("debug", "back end URL chosen from history", { url: u });
    });
    return li;
  });
  if (items.length === 0) items.push(el("li", { className: "empty", textContent: "No recent URLs" }));
  urlMenu.replaceChildren(...items);
  urlMenu.hidden = false;
  urlToggle.setAttribute("aria-expanded", "true");
}

urlToggle.addEventListener("click", () => (urlMenu.hidden ? openMenu() : closeMenu()));
document.addEventListener("click", (ev) => { if (!ev.target.closest(".combo")) closeMenu(); });
urlInput.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape") closeMenu();
  if (ev.key === "ArrowDown") { ev.preventDefault(); openMenu(); }
});
urlInput.addEventListener("input", checkURL);
// "change" fires when an edit is finished: Enter or leaving the box.
urlInput.addEventListener("change", () => {
  const v = urlInput.value.trim();
  if (validURL(v)) {
    rememberURL(v);
    log("debug", "back end URL set", { url: v });
  }
});

// The URL from --url wins; otherwise start with the last one used.
(async () => {
  let fromServer = "";
  try { fromServer = (await getJSON("/api/config")).url || ""; } catch { /* use history */ }
  urlInput.value = fromServer || state.urls[0] || "";
  if (fromServer) rememberURL(fromServer);
  checkURL();
})();

// ---- Send buttons -----------------------------------------------------------

const sendButtons = $$("button.send");

// sideData returns image i's entry from the last comparison.
function sideData(i) {
  return state.last ? (i === 0 ? state.last.a : state.last.b) : null;
}

function updateSendButtons() {
  for (const btn of sendButtons) {
    const side = sideData(Number(btn.dataset.side));
    btn.disabled = !(side && side.raw);
    btn.title = btn.disabled ? "This image has no generation metadata to send"
      : "Copy this image's generation parameters and switch to Forge";
  }
}

// copyText puts text on the clipboard. The Clipboard API needs a secure
// context (https or localhost) and permission; without them, fall back to
// the older copy command.
async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch (err) {
      log("warn", "Clipboard API refused, using the copy command", { error: err.message });
    }
  }
  const ta = el("textarea", { value: text });
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.append(ta);
  ta.select();
  const ok = document.execCommand("copy");
  ta.remove();
  if (!ok) throw new Error("the browser refused to copy");
}

// openBackend brings up the Forge tab that dView opened earlier, or opens a
// new one. An existing tab is not reloaded, so nothing in it is lost.
function openBackend(url) {
  const w = window.open("", "dview-backend");
  if (!w) return false; // blocked by the browser
  let isNew;
  try { isNew = w.location.href === "about:blank"; } catch { isNew = false; } // a Forge page: other origin
  if (isNew) {
    w.opener = null;
    w.location.href = url;
  }
  w.focus();
  return true;
}

async function send(i) {
  const side = sideData(i);
  if (!side || !side.raw) return;
  const url = urlInput.value.trim();
  if (!validURL(url)) {
    toast("Enter the Forge URL at the top of the page first, for example http://127.0.0.1:7860/", true);
    urlInput.focus();
    return;
  }
  rememberURL(url);
  try {
    await copyText(side.raw);
  } catch (err) {
    log("error", "copy to clipboard failed", { side: i, error: err.message });
    toast(`Couldn't copy image ${i + 1}'s parameters: ${err.message}`, true);
    return;
  }
  const opened = openBackend(url);
  log("debug", "sent parameters", { side: i, url, chars: side.raw.length, opened });
  const how = "In Forge's txt2img tab, paste them into the Prompt box and click ↙ " +
    "(Read generation parameters) to fill in the settings.";
  toast(opened ? `Copied image ${i + 1}'s parameters. ${how}`
    : `Copied image ${i + 1}'s parameters. The browser blocked opening Forge, so switch to it yourself. ${how}`);
}

for (const btn of sendButtons) {
  btn.addEventListener("click", () => send(Number(btn.dataset.side)));
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
