// dView web UI: two images side by side with their metadata compared below.

const TOP = "/"; // the top of the file tree (the virtual root)

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
    try {
      listing = await getJSON(`/api/list?path=${q(dir)}`);
    } catch (err) {
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

function diffText(segments) {
  const div = el("div", { className: "text" });
  for (const s of segments) {
    if (s.op === "equal") div.append(s.text);
    else div.append(el("span", { className: s.op === "insert" ? "ins" : "del", textContent: s.text }));
  }
  return div;
}

function plainSettings(settings) {
  const pre = el("pre", { className: "settings" });
  for (const s of settings) pre.append(el("div", { className: "line", textContent: `${s.key}: ${s.value}` }));
  return pre;
}

function diffSettings(lines) {
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
function renderMeta(panel, side, diff) {
  if (!side) { panel.replaceChildren(note("No image selected.")); return; }
  if (side.status === "missing") { panel.replaceChildren(note("This image has no generation metadata.")); return; }
  if (side.status !== "ok") {
    panel.replaceChildren(note(`This image's metadata couldn't be read: ${side.error || "unknown error"}`));
    return;
  }
  const p = side.params;
  const parts = [];
  if (diff) {
    parts.push(field("Prompt", diffText(diff.prompt)));
    if (diff.negative.length) parts.push(field("Negative prompt", diffText(diff.negative)));
    if (diff.settings.length) parts.push(field("Settings", diffSettings(diff.settings)));
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

async function refresh() {
  renderImage(0);
  renderImage(1);
  const panels = [$('.meta[data-side="0"]'), $('.meta[data-side="1"]')];
  const [a, b] = state.images;
  if (!a && !b) {
    panels.forEach((p) => renderMeta(p, null));
    return;
  }
  const seq = ++refreshSeq;
  const params = new URLSearchParams();
  if (a) params.set("a", a.path);
  if (b) params.set("b", b.path);
  try {
    const resp = await getJSON(`/api/compare?${params}`);
    if (seq !== refreshSeq) return; // a newer request superseded this one
    renderMeta(panels[0], resp.a, null);
    renderMeta(panels[1], resp.b, resp.diff);
  } catch (err) {
    if (seq !== refreshSeq) return;
    panels.forEach((p) => p.replaceChildren(note(`Couldn't load metadata: ${err.message}`)));
  }
}

// ---- Open buttons -------------------------------------------------------------

for (const btn of $$("button.open")) {
  const i = Number(btn.dataset.side);
  btn.addEventListener("click", async () => {
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
