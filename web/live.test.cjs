const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const { test } = require("node:test");

const source = fs.readFileSync(__dirname + "/live.js", "utf8");
const turn = () => new Promise(setImmediate);

function harness(fetcher) {
  let now = 0, nextID = 1;
  const timers = new Map(), streams = [], requests = [], states = [], statuses = [];
  let missing = 0;
  function target() {
    const listeners = new Map();
    return {
      hidden: false,
      addEventListener(name, fn) {
        if (!listeners.has(name)) listeners.set(name, new Set());
        listeners.get(name).add(fn);
      },
      removeEventListener(name, fn) { listeners.get(name)?.delete(fn); },
      emit(name) { for (const fn of [...(listeners.get(name) || [])]) fn(); },
    };
  }
  const document = target(), window = target();
  const response = (revision = 0) => ({
    ok: true, status: 200, json: async () => ({ revision, round: { seq: 1 } }),
  });
  const context = {
    module: { exports: {} }, AbortController, Date: { now: () => now },
    document, window,
    Math: Object.assign(Object.create(Math), { random: () => 0.5 }),
    setTimeout(fn, delay) {
      const id = nextID++; timers.set(id, { fn, at: now + delay }); return id;
    },
    clearTimeout(id) { timers.delete(id); },
    setInterval(fn, delay) {
      const id = nextID++; timers.set(id, { fn, at: now + delay, interval: delay }); return id;
    },
    clearInterval(id) { timers.delete(id); },
    fetch(path, opts) {
      requests.push({ at: now, path, opts });
      return fetcher ? fetcher(path, opts, requests.length) : Promise.resolve(response());
    },
    EventSource: class {
      constructor() { this.listeners = new Map(); streams.push(this); }
      addEventListener(name, fn) { this.listeners.set(name, fn); }
      emit(name, data) { this.listeners.get(name)?.({ data: JSON.stringify(data) }); }
      close() { this.closed = true; }
    },
  };
  vm.createContext(context);
  vm.runInContext(source, context);
  const { RoomConnection, request } = context.module.exports;
  const live = new RoomConnection("/api/v1/rooms/test", {
    onState: state => states.push({ at: now, ...state }),
    onLive: on => statuses.push(on),
    onMissing: () => missing++,
    onReaction: () => {},
  });
  async function advance(ms) {
    await turn();
    const end = now + ms;
    for (;;) {
      const next = [...timers.entries()].sort((a, b) => a[1].at - b[1].at)[0];
      if (!next || next[1].at > end) break;
      const [id, timer] = next;
      now = timer.at;
      if (timer.interval) timer.at += timer.interval;
      else timers.delete(id);
      timer.fn();
      await turn();
    }
    now = end;
    await turn();
  }
  return { live, request, advance, streams, requests, states, statuses, document, window,
    response, timers, missing: () => missing };
}

function hangUntilAbort(_path, opts) {
  return new Promise((_resolve, reject) => {
    opts.signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
  });
}

test("silent streams keep polling every two seconds and fail the initial delivery deadline", async () => {
  const h = harness();
  h.live.start();
  await h.advance(10000);
  assert(h.states.length >= 5);
  for (let i = 1; i < h.states.length; i++) {
    assert(h.states[i].at - h.states[i - 1].at <= 2000, "polling fell behind");
  }
  assert.equal(h.streams[0].closed, true);
  assert.equal(h.streams.length, 1);
  assert(!h.statuses.includes(true));
  h.live.stop();
});

test("pings alone cannot establish an initial room snapshot", async () => {
  const h = harness();
  h.live.start();
  h.streams[0].emit("ping", {});
  await h.advance(5000);
  assert.equal(h.streams[0].closed, true);
  assert(!h.statuses.includes(true));
  h.live.stop();
});

test("healthy streams still reconcile state when pings mask a missed update", async () => {
  const h = harness(async () => h.response(2));
  h.live.start();
  h.streams[0].emit("state", { revision: 1, round: { seq: 1 } });
  await h.advance(15000);
  h.streams[0].emit("ping", {});
  await h.advance(15000);
  assert.equal(h.streams.length, 1);
  assert.equal(h.states.at(-1).revision, 2);
  assert(h.requests.length >= 3);
  h.live.stop();
});

test("a hung refresh is single-flight, times out and allows the next poll", async () => {
  const h = harness(hangUntilAbort);
  h.live.start();
  const first = h.live.pending.promise;
  assert.equal(h.live.refresh(), first);
  await h.advance(7999);
  assert.equal(h.requests.length, 1);
  await h.advance(1);
  assert.equal(h.live.pending, null);
  assert.equal(h.requests[0].opts.signal.aborted, true);
  await h.advance(2000);
  assert.equal(h.requests.length, 2);
  h.live.stop();
  await turn();
});

test("replacement refreshes and old stream callbacks cannot publish stale state", async () => {
  const pending = [];
  const h = harness(() => new Promise(resolve => pending.push(resolve)));
  h.live.start();
  const oldStream = h.streams[0];
  const newer = h.live.refresh(true);
  assert.equal(h.requests[0].opts.signal.aborted, true);
  pending[1](h.response(2));
  await newer;
  pending[0](h.response(1));
  await turn();
  assert.deepEqual(h.states.map(s => s.revision), [2]);
  h.window.emit("online");
  oldStream.emit("state", { revision: 99, round: {} });
  oldStream.onerror();
  assert.equal(h.streams.length, 2);
  assert(!h.streams[1].closed);
  assert.deepEqual(h.states.map(s => s.revision), [2]);
  h.live.stop();
  pending[2](h.response(3));
  await turn();
});

test("hidden tabs close streams and stop polling; returning resumes once", async () => {
  const h = harness();
  h.live.start();
  await h.advance(0);
  h.document.hidden = true;
  h.document.emit("visibilitychange");
  const count = h.requests.length;
  await h.advance(60000);
  assert.equal(h.requests.length, count);
  assert(h.streams.every(s => s.closed));
  h.document.hidden = false;
  h.document.emit("visibilitychange");
  await h.advance(0);
  assert.equal(h.streams.filter(s => !s.closed).length, 1);
  assert.equal(h.requests.length, count + 1);
  h.live.stop();
});

test("an expired room stops all retry timers even when the proxy returns HTML", async () => {
  const h = harness(async () => ({
    ok: false, status: 404, json: async () => { throw new SyntaxError("HTML"); },
  }));
  h.live.start();
  await h.advance(180000);
  assert.equal(h.missing(), 1);
  assert.equal(h.requests.length, 1);
  assert.equal(h.timers.size, 0);
  assert(h.streams.every(s => s.closed));
  h.window.emit("online");
  assert.equal(h.streams.length, 1);
});

test("mutation timeouts never retry an uncertain POST", async () => {
  const h = harness(hangUntilAbort);
  const result = h.request("POST", "/vote", { value: "5" }, "token").catch(err => err);
  await h.advance(8000);
  assert.equal((await result).name, "TimeoutError");
  assert.equal(h.requests.length, 1);
  assert.equal(h.requests[0].opts.cache, "no-store");
  assert.equal(h.timers.size, 0);
});

test("the request deadline includes a stalled response body", async () => {
  const h = harness(async (path, opts) => ({
    ok: true, status: 200, json: () => hangUntilAbort(path, opts),
  }));
  const result = h.request("GET", "/room").catch(err => err);
  await h.advance(8000);
  assert.equal((await result).name, "TimeoutError");
  assert.equal(h.timers.size, 0);
});
