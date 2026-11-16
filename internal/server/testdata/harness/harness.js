// Drives the dView UI in the #app frame and reports each step to the Go
// test through POST /harness/report. Steps run in order; after the first
// failure the rest are skipped, since each builds on the one before.
"use strict";

const expectedURL = new URLSearchParams(location.search).get("url") || "";
const frame = document.getElementById("app");
let w, d; // the app's window and document
const pageErrors = [];

function report(body) {
  return fetch("/harness/report", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// waitFor polls fn until it returns something truthy, and returns that.
async function waitFor(what, fn, ms = 10000) {
  const end = Date.now() + ms;
  let last;
  for (;;) {
    try {
      const v = fn();
      if (v) return v;
    } catch (err) { last = err; }
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}` + (last ? ` (${last.message})` : ""));
    await sleep(50);
  }
}

function expectEqual(what, got, want) {
  const g = JSON.stringify(got), x = JSON.stringify(want);
  if (g !== x) throw new Error(`${what}: got ${g}, want ${x}`);
}

function expect(what, ok) {
  if (!ok) throw new Error(what);
}

// ---- Helpers for the app's elements ----------------------------------------

const $ = (sel) => d.querySelector(sel);
const $$ = (sel) => [...d.querySelectorAll(sel)];
const dialog = () => $("#picker");
const dialogPath = () => $(".picker-path").textContent;
const dialogNames = () => $$(".picker-list li[data-path] .name").map((e) => e.textContent);
const filenames = () => $$(".filename").map((e) => e.textContent);
const entry = (name) => $$(".picker-list li[data-path]").find((li) => li.querySelector(".name").textContent === name);

// openDialog clicks an Open button and waits until the dialog shows the
// listing for the folder it should start in.
async function openDialog(side, startPath) {
  $(`button.open[data-side="${side}"]`).click();
  await waitFor("the dialog to open", () => dialog().open);
  await waitFor(`the dialog to show ${startPath}`,
    () => dialogPath() === startPath && $$(".picker-list li").length > 0);
}

// enter clicks a folder (or "..") and waits until its listing is shown.
async function enter(name) {
  const before = dialogPath();
  const target = name === ".." ? before.replace(/\/[^/]*$/, "") || "/"
    : (before === "/" ? "" : before) + "/" + name;
  const li = await waitFor(`entry ${name}`, () => entry(name));
  li.click();
  await waitFor(`the dialog to show ${target}`, () => dialogPath() === target);
}

// ---- Steps -----------------------------------------------------------------

const steps = [
  ["page loads with Open and Send buttons", async () => {
    await waitFor("the app", () => $("button.open") && w.dviewLog);
    expectEqual("Open buttons", $$("button.open").length, 2);
    expectEqual("Send buttons disabled", $$("button.send").map((b) => b.disabled), [true, true]);
    expectEqual("Copy Image buttons disabled", $$("button.copy-image").map((b) => b.disabled), [true, true]);
    expectEqual("logo", $(".logo").textContent, "dView");
    expect("only-changes checkbox is disabled with no diff", $("#only-changes").disabled);
  }],

  ["Forge URL box shows the --url value", async () => {
    await waitFor("the URL box to be filled", () => $("#backend-url").value === expectedURL);
  }],

  ["left Open starts the dialog at the virtual root", async () => {
    await openDialog(0, "/");
    expectEqual("path", dialogPath(), "/");
    expectEqual("entries", dialogNames(), ["outputs", "work"]);
  }],

  ["dialog lists folders, then images, skipping hidden and other files", async () => {
    await enter("outputs");
    expectEqual("path", dialogPath(), "/outputs");
    expectEqual("entries", dialogNames(), ["..", "day1"]);
    await enter("day1");
    expectEqual("entries", dialogNames(), ["..", "fox1.png", "fox2.png", "plain.png"]);
  }],

  [".. goes up a level", async () => {
    await enter("..");
    expectEqual("path", dialogPath(), "/outputs");
    await enter("day1");
  }],

  ["selecting two files puts the second on the other side", async () => {
    entry("fox1.png").click();
    entry("fox2.png").click();
    expectEqual("count", $(".picker-count").textContent, "2 files selected");
    $('#picker [data-action="open"]').click();
    await waitFor("the dialog to close", () => !dialog().open);
    await waitFor("both file names", () => filenames().join() === "/outputs/day1/fox1.png,/outputs/day1/fox2.png");
  }],

  ["both images load", async () => {
    await waitFor("both images", () => {
      const imgs = $$(".frame img");
      return imgs.length === 2 && imgs.every((i) => i.complete && i.naturalWidth > 0);
    });
  }],

  ["image 2 shows a word diff of the prompt", async () => {
    const panel = await waitFor("the diff", () => $('.meta[data-side="1"] .ins') && $('.meta[data-side="1"]'));
    const del = [...panel.querySelectorAll(".text .del")].map((e) => e.textContent.trim());
    const ins = [...panel.querySelectorAll(".text .ins")].map((e) => e.textContent.trim());
    expectEqual("removed", del, ["red"]);
    expectEqual("added", ins, ["grey"]);
  }],

  ["changed lines only is on by default", async () => {
    const box = $("#only-changes");
    expect("checkbox enabled", !box.disabled);
    expect("checkbox checked", box.checked);
    const lines = $$('.meta[data-side="1"] .settings .line').map((e) => e.textContent);
    expectEqual("settings lines", lines, ["- CFG scale: 7", "+ CFG scale: 5"]);
    expect("unchanged negative prompt says so",
      $('.meta[data-side="1"]').textContent.includes("No changes."));
  }],

  ["unticking shows all settings", async () => {
    $("#only-changes").click();
    await waitFor("all settings lines", () => $$('.meta[data-side="1"] .settings .line').length === 6);
    expectEqual("first line", $('.meta[data-side="1"] .settings .line').textContent, "  Steps: 30");
    $("#only-changes").click(); // back on for the rest
  }],

  ["image 1 shows its metadata as-is", async () => {
    const panel = $('.meta[data-side="0"]');
    expect("no diff markup on image 1", !panel.querySelector(".ins, .del"));
    expect("prompt shown", panel.textContent.includes("a photo of a red fox in the snow"));
  }],

  ["Send is enabled for images with metadata", async () => {
    expectEqual("Send disabled", $$("button.send").map((b) => b.disabled), [false, false]);
  }],

  ["Copy Image is enabled once an image is shown", async () => {
    expectEqual("Copy Image disabled", $$("button.copy-image").map((b) => b.disabled), [false, false]);
  }],

  ["right Open has its own folder memory and starts at the top", async () => {
    await openDialog(1, "/");
    expectEqual("path", dialogPath(), "/");
  }],

  ["double-clicking one file replaces only that side", async () => {
    await enter("outputs");
    await enter("day1");
    const li = await waitFor("entry plain.png", () => entry("plain.png"));
    li.dispatchEvent(new w.MouseEvent("dblclick", { bubbles: true }));
    await waitFor("the dialog to close", () => !dialog().open);
    await waitFor("file names", () => filenames().join() === "/outputs/day1/fox1.png,/outputs/day1/plain.png");
  }],

  ["an image without metadata says so and can't be sent", async () => {
    await waitFor("the note", () => $('.meta[data-side="1"]').textContent.includes("no generation metadata"));
    expectEqual("Send disabled", $$("button.send").map((b) => b.disabled), [false, true]);
    expectEqual("Copy Image still enabled without metadata",
      $$("button.copy-image").map((b) => b.disabled), [false, false]);
    expect("checkbox disabled without a diff", $("#only-changes").disabled);
  }],

  ["left Open remembers its folder; Cancel changes nothing", async () => {
    await openDialog(0, "/outputs/day1");
    expectEqual("path", dialogPath(), "/outputs/day1");
    $('#picker [data-action="cancel"]').click();
    await waitFor("the dialog to close", () => !dialog().open);
    expectEqual("file names", filenames(), ["/outputs/day1/fox1.png", "/outputs/day1/plain.png"]);
  }],

  ["URL menu lists recent URLs", async () => {
    $(".combo-toggle").click();
    await waitFor("the menu", () => !$("#backend-history").hidden);
    const items = $$("#backend-history li").map((e) => e.textContent);
    expect(`menu contains ${expectedURL}: ${JSON.stringify(items)}`, items.includes(expectedURL));
    $(".combo-toggle").click();
    await waitFor("the menu to close", () => $("#backend-history").hidden);
  }],

  ["an invalid URL is marked and not remembered", async () => {
    const input = $("#backend-url");
    input.value = "not a url";
    input.dispatchEvent(new w.Event("input", { bubbles: true }));
    input.dispatchEvent(new w.Event("change", { bubbles: true }));
    expect("marked invalid", input.classList.contains("invalid"));
    const saved = JSON.parse(w.localStorage.getItem("dview.urls") || "[]");
    expect(`not saved: ${JSON.stringify(saved)}`, !saved.includes("not a url"));
    input.value = expectedURL;
    input.dispatchEvent(new w.Event("input", { bubbles: true }));
    expect("valid again", !input.classList.contains("invalid"));
  }],

  ["Copy Image converts a JPEG to PNG", async () => {
    // Run the page's own toPNG on a JPEG. Copying itself needs a real user's
    // click, which a script can't give.
    const src = await (await fetch("/app.js")).text();
    const start = src.indexOf("async function toPNG(");
    expect("toPNG found in app.js", start >= 0);
    let depth = 0, end = -1;
    for (let k = src.indexOf("{", start); k < src.length; k++) {
      if (src[k] === "{") depth++;
      if (src[k] === "}" && --depth === 0) { end = k + 1; break; }
    }
    const toPNG = new Function(src.slice(start, end) + "; return toPNG;")();
    const jpg = await (await fetch("/api/image?path=" + encodeURIComponent("/work/photo.jpg"))).blob();
    expectEqual("source type", jpg.type, "image/jpeg");
    const png = await toPNG(jpg);
    expectEqual("converted type", png.type, "image/png");
    const bmp = await createImageBitmap(png);
    expectEqual("size", [bmp.width, bmp.height], [40, 30]);
    const passThrough = await (await fetch("/api/image?path=" + encodeURIComponent("/outputs/day1/fox1.png"))).blob();
    expect("a PNG is passed through unchanged", (await toPNG(passThrough)) === passThrough);
  }],

  ["no JavaScript errors", async () => {
    expectEqual("errors", pageErrors, []);
  }],
];

async function run() {
  let failed = false;
  for (const [name, fn] of steps) {
    if (failed) {
      await report({ step: name, ok: false, detail: "skipped after an earlier failure" });
      continue;
    }
    try {
      await fn();
      await report({ step: name, ok: true });
    } catch (err) {
      failed = true;
      await report({ step: name, ok: false, detail: err.message || String(err) });
    }
  }
  await report({ done: true });
}

frame.addEventListener("load", () => {
  w = frame.contentWindow;
  d = frame.contentDocument;
  w.addEventListener("error", (ev) => pageErrors.push(`${ev.message} (${ev.filename}:${ev.lineno})`));
  w.addEventListener("unhandledrejection", (ev) => pageErrors.push(`unhandled rejection: ${ev.reason}`));
  run().catch((err) => report({ step: "harness", ok: false, detail: String(err) }).then(() => report({ done: true })));
}, { once: true });
