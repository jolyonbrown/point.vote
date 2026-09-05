// REST deadlines and room transport. No build step or runtime dependencies.
"use strict";

(() => {
  async function request(method, path, body, token, signal) {
    const controller = new AbortController();
    const abort = () => controller.abort();
    if (signal?.aborted) abort();
    else signal?.addEventListener("abort", abort, { once: true });
    let timedOut = false;
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, 8000);
    try {
      const opts = { method, headers: {}, signal: controller.signal, cache: "no-store" };
      if (body !== undefined) {
        opts.body = JSON.stringify(body);
        opts.headers["Content-Type"] = "application/json";
      }
      if (token) opts.headers.Authorization = "Bearer " + token;
      const res = await fetch(path, opts);
      let data = null;
      if (res.status !== 204) {
        try { data = await res.json(); }
        catch (err) {
          if (controller.signal.aborted || res.ok) throw err;
          // A proxy may return an HTML error page instead of API JSON.
        }
      }
      if (!res.ok) {
        const err = new Error(data?.error?.message || "HTTP " + res.status);
        err.status = res.status;
        err.code = data?.error?.code;
        throw err;
      }
      return data;
    } catch (err) {
      if (timedOut) {
        const timeoutError = new Error("Request timed out; the server may have accepted it.");
        timeoutError.name = "TimeoutError";
        throw timeoutError;
      }
      throw err;
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener("abort", abort);
    }
  }

  class RoomConnection {
    constructor(base, { onState, onLive, onMissing, onReaction }) {
      Object.assign(this, { base, onState, onLive, onMissing, onReaction });
      this.stopped = true;
      this.es = null;
      this.pending = null;
      this.healthy = false;
      this.retryDelay = 1000;
      this.nextConnect = 0;
      this.nextPoll = 0;
      this.visibility = () => {
        if (document.hidden) this.pause();
        else this.resume();
      };
      this.online = () => { if (!document.hidden) this.resume(); };
    }

    start() {
      if (!this.stopped) return;
      this.stopped = false;
      document.addEventListener("visibilitychange", this.visibility);
      window.addEventListener("online", this.online);
      if (!document.hidden) this.resume();
      this.timer = setInterval(() => this.tick(), 1000);
    }

    stop() {
      this.stopped = true;
      clearInterval(this.timer);
      document.removeEventListener("visibilitychange", this.visibility);
      window.removeEventListener("online", this.online);
      this.pause();
    }

    pause() {
      this.disconnect();
      this.pending?.controller.abort();
      this.pending = null;
      this.onLive(false);
    }

    resume() {
      if (this.stopped) return;
      this.pause();
      this.retryDelay = 1000;
      this.nextConnect = 0;
      this.nextPoll = 0;
      this.tick();
    }

    disconnect() {
      this.es?.close();
      this.es = null;
      this.healthy = false;
    }

    degrade(delay) {
      this.disconnect();
      this.onLive(false);
      this.nextConnect = Date.now() + delay;
      this.nextPoll = 0;
    }

    tick() {
      if (this.stopped || document.hidden) return;
      const now = Date.now();
      if (this.es && ((!this.healthy && now - this.openedAt >= 5000) ||
          (this.healthy && now - this.lastHeard > 35000))) {
        this.degrade(30000);
      }
      if (!this.es && now >= this.nextConnect) this.connect();
      if (now >= this.nextPoll && !this.pending) {
        void this.refresh().catch(() => {});
      }
    }

    connect() {
      const es = new EventSource(this.base + "/events");
      this.es = es;
      this.openedAt = Date.now();
      const current = () => !this.stopped && this.es === es;
      const heard = () => {
        this.lastHeard = Date.now();
        this.healthy = true;
        this.retryDelay = 1000;
        this.onLive(true);
      };
      const onState = (event) => {
        if (!current()) return;
        let state;
        try { state = JSON.parse(event.data); }
        catch { this.degrade(30000); return; }
        if (!state?.round) { this.degrade(30000); return; }
        heard();
        this.onState(state);
      };
      for (const name of ["state", "joined", "left", "voted", "revealed", "round_started", "settled"]) {
        es.addEventListener(name, onState);
      }
      es.addEventListener("ping", () => {
        if (current() && this.healthy) heard();
      });
      es.addEventListener("reaction", (event) => {
        if (!current()) return;
        try { this.onReaction(JSON.parse(event.data)); } catch { /* Transient event. */ }
      });
      // Headers alone do not prove that a proxy is delivering events.
      es.onerror = () => {
        if (!current()) return;
        this.degrade(this.retryDelay * (0.5 + Math.random()));
        this.retryDelay = Math.min(this.retryDelay * 2, 30000);
      };
    }

    refresh(replace = false) {
      if (this.stopped || document.hidden) return Promise.resolve();
      if (this.pending && !replace) return this.pending.promise;
      this.pending?.controller.abort();
      const pending = { controller: new AbortController() };
      this.pending = pending;
      pending.promise = request("GET", this.base, undefined, undefined, pending.controller.signal)
        .then(state => {
          if (this.pending === pending && !this.stopped) this.onState(state);
          return state;
        })
        .catch(err => {
          if (this.pending === pending && !this.stopped) {
            if (err.status === 404) {
              this.stop();
              this.onMissing();
            } else {
              this.onLive(false);
            }
          }
          throw err;
        })
        .finally(() => {
          if (this.pending === pending) {
            this.pending = null;
            // Occasional reconciliation also catches lost state while pings arrive.
            this.nextPoll = Date.now() + (this.healthy ? 15000 : 2000);
          }
        });
      return pending.promise;
    }
  }

  const exports = { request, RoomConnection };
  if (typeof module !== "undefined") module.exports = exports;
  else globalThis.PointVote = exports;
})();
