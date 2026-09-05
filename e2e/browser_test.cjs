// Run against a local binary. PLAYWRIGHT_MODULE may point to a temporary
// Playwright install; no browser dependency is shipped with the application.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const { before, after, test } = require("node:test");
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");

const base = process.env.POINTVOTE_URL || "http://127.0.0.1:8092";
let browser;
const errors = [];
before(async () => {
  browser = await chromium.launch({
    ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}),
    headless: true,
    args: ["--no-sandbox"],
  });
});
after(async () => {
  await browser?.close();
  assert.deepEqual(errors, [], "browser JavaScript errors");
});

async function api(path, body, token) {
  const response = await fetch(base + "/api/v1/rooms" + path, {
    method: body === undefined ? "GET" : "POST",
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: "Bearer " + token } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  assert(response.ok, "API HTTP " + response.status);
  return response.json();
}

async function participant(t, room, name, { buffered = false, mobile = false } = {}) {
  const context = await browser.newContext({
    viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 },
  });
  t.after(() => context.close());
  const page = await context.newPage();
  page.on("pageerror", error => errors.push(error.message));
  if (buffered) {
    await page.addInitScript(() => {
      window.bufferedStreams = [];
      window.EventSource = class {
        constructor() { this.listeners = new Map(); window.bufferedStreams.push(this); }
        addEventListener(name, fn) { this.listeners.set(name, fn); }
        close() { this.closed = true; }
      };
    });
  }
  await page.goto(base + "/r/" + room.room_id);
  await page.locator("#name").fill(name);
  await page.locator("#join-btn").click();
  await page.waitForFunction(() => !document.querySelector("#join-dialog").open &&
    !!document.querySelector("#participants .me"));
  return page;
}

test("six browsers vote, reveal and start a new round", { timeout: 30000 }, async t => {
  const room = await api("", { subject: "Six-user browser check" });
  const pages = [];
  for (let i = 0; i < 6; i++) {
    pages.push(await participant(t, room, "Browser " + i, { mobile: i === 1 }));
  }
  await Promise.all(pages.map(p => p.waitForFunction(() =>
    document.querySelectorAll("#participants li").length === 6)));
  await Promise.all(pages.map(p => p.locator('[data-v="5"]').click()));
  await Promise.all(pages.map(p => p.waitForFunction(() =>
    document.querySelector("#round-no").textContent.includes("revealed"))));
  for (const page of pages) {
    assert.equal(await page.locator("#participants .done").count(), 6);
    assert(await page.locator("#results").isVisible());
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  }
  const directory = process.env.SCREENSHOT_DIR || "/tmp/pointvote-screenshots";
  await fs.mkdir(directory, { recursive: true });
  await pages[0].screenshot({ path: directory + "/desktop.png", fullPage: true });
  await pages[1].screenshot({ path: directory + "/mobile.png", fullPage: true });
  await pages[0].locator("#next-subject").fill("Next round");
  await pages[0].locator("#next-round").click();
  await Promise.all(pages.map(p => p.waitForFunction(() =>
    document.querySelector("#round-no").textContent.includes("round 2") &&
    document.querySelector("#round-no").textContent.includes("voting"))));
});

test("an idle SSE connection survives the write deadline and delivers its heartbeat", { timeout: 35000 }, async t => {
  const room = await api("", {});
  const context = await browser.newContext();
  t.after(() => context.close());
  const page = await context.newPage();
  page.on("pageerror", error => errors.push(error.message));
  await page.addInitScript(() => {
    const NativeEventSource = window.EventSource;
    window.streamCount = 0;
    window.pingCount = 0;
    window.EventSource = class extends NativeEventSource {
      constructor(url) {
        super(url);
        window.streamCount++;
        this.addEventListener("ping", () => window.pingCount++);
      }
    };
  });
  await page.goto(base + "/r/" + room.room_id);
  await page.waitForFunction(() => window.pingCount > 0, null, { timeout: 30000 });
  assert.equal(await page.evaluate(() => window.streamCount), 1);
  assert.equal(await page.locator("#live").getAttribute("title"), "live");
});

test("buffered SSE falls back to polling; action responses work even if GET stalls", { timeout: 20000 }, async t => {
  const room = await api("", { subject: "Buffered stream", auto_reveal: false });
  const page = await participant(t, room, "Alice", { buffered: true });
  const bob = await api("/" + room.room_id + "/participants", { name: "Bob", kind: "human" });
  await page.waitForFunction(() => document.querySelectorAll("#participants li").length === 2,
    null, { timeout: 4000 });
  await page.locator('[data-v="5"]').click();
  await page.waitForFunction(() => document.querySelector("#participants .me .done"),
    null, { timeout: 4000 });
  await api("/" + room.room_id + "/vote", { value: "8" }, bob.token);
  await page.waitForFunction(() => document.querySelectorAll("#participants .done").length === 2,
    null, { timeout: 4000 });
  // Only mutation responses can now update the page.
  await page.route(base + "/api/v1/rooms/" + room.room_id, route => route.abort());
  await page.locator("#reveal").click();
  await page.waitForFunction(() => !document.querySelector("#results").hidden, null, { timeout: 1500 });
  await page.locator("#settle-value").selectOption("5");
  await page.locator("#settle").click();
  await page.waitForFunction(() => !document.querySelector("#settlement").hidden, null, { timeout: 1500 });
  await page.locator("#next-round").click();
  await page.waitForFunction(() => document.querySelector("#round-no").textContent.includes("round 2"),
    null, { timeout: 1500 });
});

test("a delayed mutation response cannot roll back a newer streamed round", { timeout: 15000 }, async t => {
  const room = await api("", { auto_reveal: false });
  const page = await participant(t, room, "Alice");
  const bob = await api("/" + room.room_id + "/participants", { name: "Bob", kind: "human" });
  let release, received;
  const gate = new Promise(resolve => { release = resolve; });
  const ready = new Promise(resolve => { received = resolve; });
  await page.route("**/reveal", async route => {
    const response = await route.fetch();
    received();
    await gate;
    await route.fulfill({ response });
  });
  await page.locator("#reveal").click();
  await ready;
  await api("/" + room.room_id + "/rounds", { subject: "Newer round" }, bob.token);
  await page.waitForFunction(() => document.querySelector("#subject").textContent === "Newer round");
  release();
  await page.waitForFunction(() => !document.querySelector("#reveal").disabled);
  assert((await page.locator("#round-no").textContent()).includes("round 2"));
  assert(await page.locator("#results").isHidden());
});

test("a vote response arriving in the next round stores the selection under the original round", { timeout: 15000 }, async t => {
  const room = await api("", {});
  const page = await participant(t, room, "Alice");
  const token = await page.evaluate(id => sessionStorage.getItem("pv:" + id + ":token"), room.room_id);
  let release, received;
  const gate = new Promise(resolve => { release = resolve; });
  const ready = new Promise(resolve => { received = resolve; });
  await page.route("**/vote", async route => {
    const response = await route.fetch();
    received();
    await gate;
    await route.fulfill({ response });
  });
  await page.locator('[data-v="5"]').click();
  await ready;
  await api("/" + room.room_id + "/rounds", {}, token);
  await page.waitForFunction(() => document.querySelector("#round-no").textContent.includes("round 2"));
  release();
  await page.waitForFunction(() => !document.querySelector('[data-v="5"]').disabled);
  const selections = await page.evaluate(id => [
    sessionStorage.getItem("pv:" + id + ":vote:1"),
    sessionStorage.getItem("pv:" + id + ":vote:2"),
  ], room.room_id);
  assert.deepEqual(selections, ["5", null]);
  assert.equal(await page.locator("#cards .selected").count(), 0);
});
