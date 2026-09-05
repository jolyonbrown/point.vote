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

  const api = PointVote.request;

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
    let wasJoined = false; // seen ourselves in state at least once
    let votePending = false;

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

      renderContext(r, voting);

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
      $("#status").textContent = voting && votePending ? "Submitting your vote..." : text;
    }

    // Poker cards suit short values; a deck of long decision strings reads
    // as a list. Count code points, not bytes, so "☕" is one character.
    const CARD_MAX_LEN = 4;
    function deckAsCards() {
      return state.deck.every((v) => [...v].length <= CARD_MAX_LEN);
    }

    function renderCards(voting, iAmObserver, joined) {
      const wrap = $("#cards");
      if (!deckDrawn) {
        const asCards = deckAsCards();
        wrap.classList.toggle("as-list", !asCards);
        state.deck.forEach((value, i) => {
          const btn = document.createElement("button");
          btn.dataset.v = value;
          btn.setAttribute("role", "radio");
          btn.addEventListener("click", () => castVote(value));
          if (asCards) {
            btn.className = "card"; // corner pips come from CSS attr(data-v)
            btn.textContent = value;
          } else {
            btn.className = "deck-row";
            const key = document.createElement("span");
            key.className = "rowkey";
            key.textContent = String(i + 1);
            const label = document.createElement("span");
            label.className = "rowlabel";
            label.textContent = value;
            btn.append(key, label);
          }
          wrap.append(btn);
        });
        deckDrawn = true;
      }
      const myVote = sessionStorage.getItem(myVoteKey());
      for (const btn of wrap.children) {
        btn.disabled = votePending || !voting || !joined || iAmObserver;
        const selected = voting && btn.dataset.v === myVote;
        btn.classList.toggle("selected", selected);
        btn.setAttribute("aria-checked", String(selected));
      }
    }

    // The brief is public (PLAN.md §3), so show it. Open by default while
    // voting or when short; re-evaluated per round so a new question's
    // brief reopens, but an intra-round re-render never fights a manual
    // collapse.
    let contextSeq = null;
    function renderContext(r, voting) {
      const box = $("#context-box");
      box.hidden = !r.context;
      if (!r.context) return;
      $("#context").textContent = r.context;
      if (contextSeq !== r.seq) {
        contextSeq = r.seq;
        box.open = voting || r.context.length <= 320;
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
      if (votePending) return;
      const voteKey = myVoteKey();
      const body = { value };
      const rationale = $("#rationale").value.trim();
      if (rationale) body.rationale = rationale;
      votePending = true;
      render();
      try {
        await api("POST", base + "/vote", body, token);
        sessionStorage.setItem(voteKey, value);
      } catch (err) {
        if (err.status === 409) toast("Round's already revealed. Start a new one.");
        else if (err.status === 401) forgetIdentity();
        else toast(err.name === "TimeoutError"
          ? "Vote response timed out. Checking the room..."
          : "Vote refused: " + err.message);
      } finally {
        votePending = false;
        render();
        void refreshAfterAction();
      }
    }

    $("#reveal").addEventListener("click", async () => {
      try {
        await mutate($("#reveal"), "/reveal");
      } catch (err) {
        if (err.status === 409) toast("Already revealed.");
        else if (err.status === 401) forgetIdentity();
        else toast("Couldn't reveal: " + err.message);
      }
    });

    $("#settle").addEventListener("click", async () => {
      try {
        await mutate($("#settle"), "/settle", { value: $("#settle-value").value });
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
        await mutate($("#next-round"), "/rounds", body);
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
        await refreshAfterAction();
        wasJoined = Boolean(me());
        render(); // Identity may have arrived after the joined snapshot.
      } catch (err) {
        toast(err.status === 404
          ? "This room has expired. Rooms evaporate after two hours of quiet."
          : "Couldn't join: " + err.message);
      }
    });

    /* --- live updates and periodic reconciliation --- */

    function applyState(next) {
      if (state && next.revision <= state.revision) return;
      state = next;
      if (me()) wasJoined = true;
      else if (token && wasJoined) forgetIdentity();
      render();
    }

    const live = new PointVote.RoomConnection(base, {
      onState: applyState,
      onLive: (on) => {
        const el = $("#live");
        el.classList.toggle("on", on);
        el.classList.toggle("off", !on);
        el.title = on ? "live" : "reconnecting";
      },
      onMissing: () => {
        $("#join-dialog").close();
        $("#room-main").hidden = true;
        $("#room-missing").hidden = false;
      },
      onReaction: floatReaction,
    });

    // A POST may finish while an older periodic GET is still in flight.
    const refreshAfterAction = () => live.refresh(true).catch(() => {});

    async function mutate(button, path, body) {
      if (button.disabled) return;
      button.disabled = true;
      button.setAttribute("aria-busy", "true");
      try {
        applyState(await api("POST", base + path, body, token));
      } catch (err) {
        void refreshAfterAction();
        throw err;
      } finally {
        button.disabled = false;
        button.removeAttribute("aria-busy");
      }
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
      live.start();
      if (!token) showJoin();
    })();
  }

  if (document.body.dataset.page === "landing") initLanding();
  if (document.body.dataset.page === "room") initRoom();
})();
