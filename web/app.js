// point.vote client. No build step, no framework, no client-side state
// merging: every SSE event carries the full redacted room state and the
// page re-renders from it.
"use strict";

(() => {
  const $ = (sel, el = document) => el.querySelector(sel);

  const KIND_GLYPH = { human: "\u{1F464}", agent: "\u{1F916}", observer: "\u{1F441}" };
  // Mirrors the server's allowlist; the server is the authority.
  const REACTIONS = ["👏", "🍿", "🤔", "😮", "🎉", "☕"];

  let toastTimer;
  function toast(msg) {
    const el = $("#toast");
    el.textContent = msg;
    el.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.remove("show"), 2600);
  }

  async function api(method, path, body, token) {
    const opts = { method, headers: {} };
    if (body !== undefined) opts.body = JSON.stringify(body);
    if (token) opts.headers["Authorization"] = "Bearer " + token;
    const res = await fetch(path, opts);
    const data = res.status === 204 ? null : await res.json().catch(() => null);
    if (!res.ok) {
      const err = new Error(data?.error?.message || "HTTP " + res.status);
      err.status = res.status;
      err.code = data?.error?.code;
      throw err;
    }
    return data;
  }

  function copyText(text, btn) {
    navigator.clipboard?.writeText(text).then(
      () => {
        const prev = btn.textContent;
        btn.textContent = "copied";
        setTimeout(() => (btn.textContent = prev), 1400);
      },
      () => toast("Couldn't copy. Old-fashioned selection it is."),
    );
  }

  /* ---------- landing ---------- */

  function initLanding() {
    for (const el of document.querySelectorAll(".origin")) {
      el.textContent = location.origin;
    }

    let deck = "fibonacci";
    const chips = [...document.querySelectorAll(".deck-chip")];
    for (const chip of chips) {
      chip.addEventListener("click", () => {
        deck = chip.dataset.deck;
        for (const c of chips) {
          c.classList.toggle("selected", c === chip);
          c.setAttribute("aria-checked", String(c === chip));
        }
      });
    }

    for (const btn of document.querySelectorAll(".copybtn")) {
      btn.addEventListener("click", () => copyText($(btn.dataset.copy).textContent, btn));
    }

    $("#create").addEventListener("click", async () => {
      const body = { deck };
      const subject = $("#subject").value.trim();
      if (subject) body.subject = subject;
      try {
        const resp = await api("POST", "/api/v1/rooms", body);
        location.href = "/r/" + resp.room_id;
      } catch (err) {
        toast(err.code === "rate_limited"
          ? "Steady on. Room creation is limited; try again in a bit."
          : "Couldn't start a room: " + err.message);
      }
    });
  }

  /* ---------- room ---------- */

  function initRoom() {
    const roomId = decodeURIComponent(location.pathname.split("/").pop());
    const base = "/api/v1/rooms/" + encodeURIComponent(roomId);
    const key = (k) => "pv:" + roomId + ":" + k;

    let token = sessionStorage.getItem(key("token"));
    let pid = sessionStorage.getItem(key("pid"));
    let state = null;
    let deckDrawn = false;
    let es = null;
    let backoff = 1000;
    let reconnectTimer = null;
    let sseGen = 0; // bumps on every SSE render; guards stale fetch races
    let wasJoined = false; // seen ourselves in state at least once

    $("#room-id").textContent = roomId;
    $("#copy-link").addEventListener("click", (e) => copyText(location.href, e.target));

    const me = () => state?.round.participants.find((p) => p.id === pid);
    const myVoteKey = () => key("vote:" + state.round.seq);

    /* --- rendering: full re-render from full state, always --- */

    function render() {
      if (!state) return;
      const r = state.round;
      const voting = r.state === "voting";
      const iAmObserver = me()?.kind === "observer";
      const joined = Boolean(token && me());

      $("#round-no").textContent = "round " + r.seq + " · " + r.state;
      const h1 = $("#subject");
      h1.textContent = r.subject || "No subject. Vibes only.";
      h1.classList.toggle("untitled", !r.subject);

      $("#context-box").hidden = !r.context;
      if (r.context) $("#context").textContent = r.context;

      renderParticipants(r);
      renderStatus(r, voting);
      renderCards(voting, iAmObserver, joined);

      $("#reveal").hidden = !(voting && joined);
      $("#next-round-row").hidden = !(joined && !voting);
      $("#rationale-row").hidden = !joined || iAmObserver || !voting;
      $("#observer-note").hidden = !iAmObserver;
      renderReactBar(joined);
      renderSettleControls(joined, voting);

      renderResults();
      renderSettlement();
      renderHistory();
    }

    function renderParticipants(r) {
      const ul = $("#participants");
      ul.textContent = "";
      if (r.participants.length === 0) {
        const li = document.createElement("li");
        li.className = "empty";
        li.textContent = "Nobody here yet. Democracy awaits.";
        ul.append(li);
        return;
      }
      for (const p of r.participants) {
        const li = document.createElement("li");
        if (p.id === pid) li.classList.add("me");
        const glyph = document.createElement("span");
        glyph.className = "glyph";
        glyph.textContent = KIND_GLYPH[p.kind] || "?";
        glyph.title = p.kind;
        const who = document.createElement("span");
        who.className = "who";
        who.textContent = p.name + (p.id === pid ? " (you)" : "");
        const tick = document.createElement("span");
        tick.className = "tick" + (p.has_voted ? " done" : "");
        tick.textContent = p.kind === "observer" ? "\u{1F441}" : p.has_voted ? "✓" : "·";
        li.append(glyph, who, tick);
        ul.append(li);
      }
    }

    function renderStatus(r, voting) {
      const voters = r.participants.filter((p) => p.kind !== "observer").length;
      let text;
      if (!voting) {
        text = "Round " + r.seq + " revealed.";
      } else if (voters === 0) {
        text = "No voters yet. Someone has to go first.";
      } else {
        const waiting = voters - r.votes_cast;
        text = waiting === 0
          ? "All votes in."
          : "Waiting on " + waiting + " of " + voters + ".";
      }
      $("#status").textContent = text;
    }

    function renderCards(voting, iAmObserver, joined) {
      const wrap = $("#cards");
      if (!deckDrawn) {
        for (const value of state.deck) {
          const btn = document.createElement("button");
          btn.className = "card";
          btn.dataset.v = value;
          btn.textContent = value;
          btn.setAttribute("role", "radio");
          btn.addEventListener("click", () => castVote(value));
          wrap.append(btn);
        }
        deckDrawn = true;
      }
      const myVote = sessionStorage.getItem(myVoteKey());
      for (const btn of wrap.children) {
        btn.disabled = !voting || !joined || iAmObserver;
        const selected = voting && btn.dataset.v === myVote;
        btn.classList.toggle("selected", selected);
        btn.setAttribute("aria-checked", String(selected));
      }
    }

    let settleBuilt = false;
    function renderSettleControls(joined, voting) {
      const row = $("#settle-row");
      row.hidden = !(joined && !voting);
      if (row.hidden) return;
      const select = $("#settle-value");
      if (!settleBuilt) {
        settleBuilt = true;
        for (const value of state.deck) {
          const opt = document.createElement("option");
          opt.value = value;
          opt.textContent = value;
          select.append(opt);
        }
        select.dataset.touched = "";
        select.addEventListener("change", () => (select.dataset.touched = "1"));
      }
      // Suggest the top card until the user has an opinion of their own.
      const top = state.results?.stats?.top;
      if (!select.dataset.touched && top && !top.tied) {
        select.value = top.values[0];
      }
    }

    function renderSettlement() {
      const box = $("#settlement");
      const s = state.settled;
      box.hidden = !s;
      if (!s) return;
      $("#settled-line").textContent =
        "Settled on " + s.value + " — called by " + s.by + ".";
      const awards = $("#awards");
      awards.textContent = "";
      for (const a of s.awards) {
        const div = document.createElement("div");
        div.className = "award";
        const title = document.createElement("span");
        title.className = "award-title";
        title.textContent = a.title;
        const who = document.createElement("span");
        who.className = "award-who";
        who.textContent = a.names.join(", ");
        const detail = document.createElement("span");
        detail.className = "award-detail";
        detail.textContent = a.detail;
        div.append(title, who, detail);
        awards.append(div);
      }
    }

    let reactBarBuilt = false;
    function renderReactBar(joined) {
      const bar = $("#react-bar");
      bar.hidden = !joined;
      if (reactBarBuilt || !joined) return;
      reactBarBuilt = true;
      for (const emoji of REACTIONS) {
        const btn = document.createElement("button");
        btn.className = "react-btn";
        btn.type = "button";
        btn.textContent = emoji;
        btn.title = "react " + emoji;
        btn.addEventListener("click", async () => {
          try {
            await api("POST", base + "/react", { emoji }, token);
          } catch (err) {
            if (err.status === 429) toast("Steady on.");
            else if (err.status === 401) forgetIdentity();
          }
        });
        bar.append(btn);
      }
    }

    // A reaction floats up from the gallery and is gone. Nothing to
    // re-render; it was never state. Background tabs throttle timers and
    // pause animations, so removal is belt (animationend), braces
    // (timeout) and a hard cap pruning the oldest floats.
    function floatReaction(re) {
      const overlay = $("#react-overlay");
      while (overlay.children.length >= 8) overlay.firstChild.remove();
      const el = document.createElement("span");
      el.className = "react-float";
      el.textContent = re.emoji;
      el.title = re.name;
      el.style.left = 35 + Math.random() * 30 + "%";
      el.addEventListener("animationend", () => el.remove());
      overlay.append(el);
      setTimeout(() => el.remove(), 2500);
    }

    function statChip(label, value, cls) {
      const div = document.createElement("div");
      div.className = "stat" + (cls ? " " + cls : "");
      div.append(label);
      const b = document.createElement("b");
      b.textContent = value;
      div.append(b);
      return div;
    }

    function renderResults() {
      const box = $("#results");
      const results = state.results;
      box.hidden = !results;
      if (!results) return;

      $("#results-title").textContent = "Round " + state.round.seq + " · the damage";

      const stats = $("#stats");
      stats.textContent = "";
      const s = results.stats;
      if (s.consensus) {
        stats.append(statChip("consensus", "Suspiciously agreeable.", "consensus"));
      }
      if (s.spread !== null && s.spread !== undefined) {
        stats.append(statChip("spread", String(s.spread)));
        stats.append(statChip("median", String(s.median)));
        stats.append(statChip("mean", String(Math.round(s.mean * 100) / 100)));
        if (!s.consensus && s.spread > 0) {
          stats.append(statChip("verdict", "Someone knows something."));
        }
      }

      const votesBox = $("#votes");
      votesBox.textContent = "";
      const byValue = new Map();
      for (const v of results.votes) {
        if (!byValue.has(v.value)) byValue.set(v.value, []);
        byValue.get(v.value).push(v);
      }
      // deck order, biggest groups' order comes from the deck itself
      let i = 0;
      for (const value of state.deck) {
        const group = byValue.get(value);
        if (!group) continue;
        const div = document.createElement("div");
        div.className = "vote-group";
        div.style.setProperty("--i", i++);
        const val = document.createElement("div");
        val.className = "val";
        val.textContent = value;
        const ul = document.createElement("ul");
        for (const v of group) {
          const li = document.createElement("li");
          const who = document.createElement("div");
          who.className = "voter";
          who.textContent = (KIND_GLYPH[v.kind] || "?") + " " + v.name;
          li.append(who);
          if (v.rationale) {
            const why = document.createElement("div");
            why.className = "why";
            why.textContent = "“" + v.rationale + "”";
            li.append(why);
          }
          ul.append(li);
        }
        div.append(val, ul);
        votesBox.append(div);
      }
      if (results.votes.length === 0) {
        const p = document.createElement("p");
        p.className = "fine";
        p.textContent = "Nobody voted. A bold collective statement.";
        votesBox.append(p);
      }
    }

    function renderHistory() {
      const box = $("#history-box");
      const hist = state.history;
      box.hidden = hist.length === 0;
      if (hist.length === 0) return;
      $("#history-summary").textContent = "Previous rounds (" + hist.length + ")";
      const ul = $("#history");
      ul.textContent = "";
      for (const h of [...hist].reverse()) {
        const li = document.createElement("li");
        const spread = h.stats.spread === null || h.stats.spread === undefined
          ? "—" : String(h.stats.spread);
        const b = document.createElement("b");
        b.textContent = "#" + h.seq;
        const called = h.called ? " · called " + h.called : "";
        li.append(b, " " + (h.subject || "(untitled)") + " · spread " + spread
          + " · " + h.votes.length + " vote" + (h.votes.length === 1 ? "" : "s") + called);
        ul.append(li);
      }
    }

    /* --- actions --- */

    async function castVote(value) {
      const body = { value };
      const rationale = $("#rationale").value.trim();
      if (rationale) body.rationale = rationale;
      try {
        await api("POST", base + "/vote", body, token);
        sessionStorage.setItem(myVoteKey(), value);
        render(); // show selection now; SSE snapshot follows
      } catch (err) {
        if (err.status === 409) toast("Round's already revealed. Start a new one.");
        else if (err.status === 401) forgetIdentity();
        else toast("Vote refused: " + err.message);
      }
    }

    $("#reveal").addEventListener("click", async () => {
      try {
        await api("POST", base + "/reveal", undefined, token);
      } catch (err) {
        if (err.status === 409) toast("Already revealed.");
        else if (err.status === 401) forgetIdentity();
        else toast("Couldn't reveal: " + err.message);
      }
    });

    $("#settle").addEventListener("click", async () => {
      try {
        await api("POST", base + "/settle", { value: $("#settle-value").value }, token);
      } catch (err) {
        if (err.status === 409) toast("Reveal the round first.");
        else if (err.status === 401) forgetIdentity();
        else toast("Couldn't settle: " + err.message);
      }
    });

    $("#next-round").addEventListener("click", async () => {
      const body = {};
      const subject = $("#next-subject").value.trim();
      if (subject) body.subject = subject;
      try {
        await api("POST", base + "/rounds", body, token);
        $("#next-subject").value = "";
        $("#rationale").value = "";
      } catch (err) {
        if (err.status === 401) forgetIdentity();
        else toast("Couldn't start a round: " + err.message);
      }
    });

    /* --- join --- */

    function forgetIdentity() {
      token = null;
      pid = null;
      wasJoined = false;
      sessionStorage.removeItem(key("token"));
      sessionStorage.removeItem(key("pid"));
      showJoin();
    }

    function showJoin() {
      const dlg = $("#join-dialog");
      if (dlg.open) return;
      $("#name").value = localStorage.getItem("pv:name") || "";
      dlg.showModal();
    }

    $("#join-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const name = $("#name").value.trim();
      const kind = $("#kind").value;
      if (!name) {
        toast("A name would help.");
        return;
      }
      try {
        const resp = await api("POST", base + "/participants", { name, kind });
        token = resp.token;
        pid = resp.participant_id;
        sessionStorage.setItem(key("token"), token);
        sessionStorage.setItem(key("pid"), pid);
        localStorage.setItem("pv:name", name);
        $("#join-dialog").close();
        // The joined event will arrive via SSE, but fetch immediately for
        // snappiness on slow connections — unless an SSE render beat us to
        // it, in which case the fetch is the staler of the two.
        const gen = sseGen;
        const fetched = await api("GET", base);
        if (gen === sseGen) {
          state = fetched;
          render();
        }
      } catch (err) {
        toast(err.status === 404
          ? "This room has expired. Rooms evaporate after two hours of quiet."
          : "Couldn't join: " + err.message);
      }
    });

    /* --- live updates: reconnect with jittered backoff --- */

    function setLive(on) {
      const el = $("#live");
      el.classList.toggle("on", on);
      el.classList.toggle("off", !on);
      el.title = on ? "live" : "reconnecting…";
    }

    function connect() {
      es = new EventSource(base + "/events");
      const onEvent = (e) => {
        sseGen++;
        state = JSON.parse(e.data);
        if (me()) {
          wasJoined = true;
        } else if (token && wasJoined) {
          // We were in this room and now we're not (room recycled after
          // expiry): the token is dead, rejoin honestly. The wasJoined
          // guard stops a stale pre-join snapshot from wiping a fresh
          // identity.
          forgetIdentity();
        }
        render();
      };
      for (const name of ["state", "joined", "left", "voted", "revealed", "round_started", "settled"]) {
        es.addEventListener(name, onEvent);
      }
      es.addEventListener("reaction", (e) => floatReaction(JSON.parse(e.data)));
      es.onopen = () => {
        backoff = 1000;
        setLive(true);
      };
      es.onerror = () => {
        es.close();
        setLive(false);
        if (reconnectTimer) return; // one pending reconnect, ever
        const delay = backoff * (0.5 + Math.random());
        backoff = Math.min(backoff * 2, 30000);
        reconnectTimer = setTimeout(async () => {
          reconnectTimer = null;
          try {
            await api("GET", base);
          } catch (err) {
            if (err.status === 404) {
              // The room evaporated. Stop knocking.
              $("#room-main").hidden = true;
              $("#room-missing").hidden = false;
              return;
            }
            // Transient failure: reconnect anyway and let backoff grow.
          }
          connect();
        }, delay);
      };
    }

    /* --- boot --- */

    (async () => {
      try {
        state = await api("GET", base);
      } catch (err) {
        if (err.status === 404) {
          $("#room-missing").hidden = false;
          return;
        }
        toast("Couldn't load the room: " + err.message);
        return;
      }
      $("#room-main").hidden = false;
      render();
      connect();
      if (!token) showJoin();
    })();
  }

  if (document.body.dataset.page === "landing") initLanding();
  if (document.body.dataset.page === "room") initRoom();
})();
