/* Sirāj v2: live rooms, duels, challenges, messages, friends, leaderboard, profile, settings, support. */
(function () {
  "use strict";
  const SJ = window.SJ, S = SJ.S, I = SJ.icon, E = SJ.esc, t = SJ.t, L = SJ.L, X = SJ.tx;

  let simTimers = [];
  const later = (ms, fn) => { const id = setTimeout(fn, ms); simTimers.push(id); return id; };
  const baseStop = SJ.stopTimers;
  SJ.stopTimers = () => { baseStop(); simTimers.forEach(clearTimeout); simTimers = []; if (S.room) S.room.sim = false; if (S.duelState) S.duelState.sim = false; };

  const asPlayer = (p, extra) => Object.assign({ id: p.id, label: SJ.pname(p), c: p.c, ini: p.ini, ar: p.ar, skill: 0.55 + Math.random() * 0.3, score: 0, ready: false }, extra || {});
  const meP = () => ({ id: "me", me: true, label: SJ.myName(), score: 0, ready: true, guest: SJ.isGuest() });
  const code = () => { const ch = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"; let s = ""; for (let i = 0; i < 6; i++) s += ch[Math.floor(Math.random() * ch.length)]; return s; };
  const fmtCode = (c) => c.slice(0, 3) + " " + c.slice(3);

  /* ---------------- Live room ---------------- */
  SJ.createRoom = (pool) => {
    S.room = { code: code(), host: "me", pool, st: Object.assign({}, S.setup), players: [meP()], chat: [{ sys: true, text: SJ.isGuest() ? X("Guest room created. Only guests can join, and it closes when you leave.", "أُنشئت غرفة ضيوف. ينضم إليها الضيوف فقط وتُغلق عند مغادرتك.", "Salon invité créé. Seuls les invités peuvent entrer ; il se ferme à votre départ.") : "Room created. Share the code to invite players." }], sim: false, script: "host", guest: SJ.isGuest() };
    SJ.go("lobby");
  };
  function botJoin(p, guestName) {
    const r = S.room; if (!r || r.players.length >= 8) return;
    const pl = guestName ? { id: "g" + Date.now(), label: guestName, guest: true, c: "#6b7280", ini: "G", ar: "ضيف", skill: 0.5 + Math.random() * 0.25, score: 0, ready: false } : asPlayer(p);
    r.players.push(pl); r.chat.push({ sys: true, text: `${pl.label} joined` });
    if (S.route === "lobby") SJ.render();
    later(1200 + Math.random() * 2200, () => { pl.ready = true; if (S.route === "lobby") SJ.render(); });
  }
  function botSay(name, text) { const r = S.room; if (!r) return; r.chat.push({ who: name, text }); if (S.route === "lobby") { SJ.render(); const log = document.querySelector(".chat-log"); if (log) log.scrollTop = log.scrollHeight; } }
  SJ.after["lobby"] = () => {
    const log = document.querySelector(".chat-log"); if (log) log.scrollTop = log.scrollHeight;
    const r = S.room; if (!r || r.sim) return; r.sim = true;
    if (r.script === "host" && r.guest && r.players.length < 4) {
      later(1200, () => botJoin(null, "Guest 4410"));
      later(2400, () => botSay("Guest 4410", "Salam! First time here"));
      later(3400, () => botJoin(null, "Guest 2207"));
      later(5200, () => botJoin(null, "Guest 3391"));
      later(6400, () => botSay("Guest 3391", "Ready"));
    } else if (r.script === "host" && r.players.length < 4) {
      later(1200, () => botJoin(SJ.person("omar")));
      later(2300, () => botSay(SJ.pname(SJ.person("omar")), "Salam everyone"));
      later(3200, () => botJoin(SJ.person("khadija")));
      later(4600, () => botJoin(null, "Guest 2207"));
      later(5600, () => botSay(SJ.pname(SJ.person("khadija")), "Ready when you are. Hadith please, not fiqh 😄".replace(" 😄", "")));
      later(6800, () => botJoin(SJ.person("youssef")));
    }
    if (r.script === "join") {
      const me = r.players.find((p) => p.me);
      if (me.ready) later(2600, () => { botSay(r.players[0].label, "Starting now, bismillah"); later(900, () => startRoom()); });
    }
  };
  SJ.screens["lobby"] = () => {
    const r = S.room;
    if (!r) return `<div class="page"><div class="empty"><div class="e-ic">${I("radio")}</div><h3>You're not in a room</h3><div class="row"><button class="btn btn-primary" data-act="room-create">Host a room</button><button class="btn" data-go="room-join">${t("join")}</button></div></div></div>`;
    const st = r.st, me = r.players.find((p) => p.me), host = r.host === "me";
    const ready = r.players.filter((p) => p.ready).length, canStart = r.players.length >= 2;
    const slots = r.players.map((p) => `<div class="slot">${p.me ? SJ.meAv() : p.guest ? `<span class="av guest">G</span>` : SJ.av(SJ.person(p.id) || p)}<div style="min-width:0;flex:1"><div class="nm">${E(p.me ? p.label + " (you)" : p.label)}${(p.me && host) || (!p.me && r.host === p.id) ? I("crown", "i-sm") : ""}</div><div class="rd ${p.ready ? "yes" : "no"}">${p.ready ? t("ready") : "Not ready"}${p.guest ? " · guest" : ""}</div></div>
      ${host && !p.me ? `<button class="icon-btn sm" data-act="kick" data-v="${p.id}" aria-label="Remove ${E(p.label)}">${I("x", "i-sm")}</button>` : ""}</div>`).join("") + Array.from({ length: Math.max(0, 8 - r.players.length) }, () => `<div class="slot empty">Waiting for a player</div>`).join("");
    return `<div class="page" style="display:flex;flex-direction:column;gap:20px">
      <section class="lamp-card">${SJ.PATTERN("kh-lobby")}<div class="in">
        <div class="row wrap"><span class="eyebrow">${I("radio", "i-sm")}${t("liveRoom")} · ${t("lobby")}</span><span class="meta">${E(SJ.scopeLabel(st))} · ${st.count} questions · ${st.timer || 30} s</span></div>
        <div class="room-code"><span class="meta" style="font-size:14px">Room code</span><span class="code num" aria-label="Room code ${r.code.split("").join(" ")}">${fmtCode(r.code)}</span>
          <button class="btn btn-sm btn-gold" data-act="copy" data-text="${r.code}">${I("copy", "i-sm")}Copy code</button><button class="btn btn-sm btn-outline-light" style="--b:transparent;--f:#eef5f1;--l:rgba(255,255,255,.35)" data-act="copy" data-text="https://siraj.app/r/${r.code}">${I("link", "i-sm")}Copy link</button></div>
        <p class="meta">${r.guest ? X("Guest room: only guests can join with this code. Nothing is saved, and the room closes when the host leaves.", "غرفة ضيوف: ينضم بهذا الرمز الضيوف فقط. لا يُحفظ شيء، وتُغلق الغرفة عند مغادرة المضيف.", "Salon invité : seuls les invités entrent avec ce code. Rien n'est gardé ; le salon se ferme au départ de l'hôte.") : `Friends join from Play › ${t("join")}, or by opening the link. Guests can join too.`}</p></div></section>
      <div class="cols">
        <div class="col-main" style="gap:16px">
          <div class="row"><h2 style="font-size:20px">Players <span class="muted num" style="font-weight:600">${r.players.length} / 8</span></h2><span class="spacer"></span><span class="badge ${ready === r.players.length ? "green" : ""}">${ready} of ${r.players.length} ready</span></div>
          <div class="slots">${slots}</div>
          <div class="row wrap" style="gap:10px">
            ${host ? `<button class="btn btn-primary btn-lg" data-act="start-room" ${canStart ? "" : "disabled"}>${I("tri", "flip")}${ready === r.players.length ? "Start game" : "Start anyway"}</button>${!canStart ? `<span class="help">You need at least one more player.</span>` : ""}<button class="btn btn-lg" data-go="room-create">${I("sliders")}Room settings</button>`
              : `<button class="btn btn-lg ${me.ready ? "" : "btn-primary"}" data-act="toggle-ready">${me.ready ? "I'm not ready" : "I'm ready"}</button><span class="help">${E(r.players[0].label)} starts the game when everyone is ready.</span>`}
            <span class="spacer"></span><button class="btn btn-ghost btn-lg" data-act="leave-room">${I("door")}Leave room</button></div>
        </div>
        <aside class="col-side"><section class="card chat"><div class="card-head" style="padding:14px 14px 0;margin:0"><h2>Room chat</h2></div>
          <div class="chat-log" aria-live="polite">${r.chat.map((m) => m.sys ? `<div class="chat-msg sys">${E(m.text)}</div>` : `<div class="chat-msg"><b>${E(m.who)}</b><span>${E(m.text)}</span></div>`).join("")}</div>
          <form class="chat-form" data-form="room-chat"><label class="sr-only" for="rc-in">Message the room</label><input class="input" id="rc-in" placeholder="Say salam" autocomplete="off"><button class="btn btn-primary" type="submit" aria-label="Send">${I("send", "flip")}</button></form></section></aside>
      </div></div>`;
  };
  SJ.forms["room-chat"] = () => { const i = document.getElementById("rc-in"); const v = i.value.trim(); if (!v) return; S.room.chat.push({ who: "You", text: v }); SJ.render(); const n = document.getElementById("rc-in"); if (n) n.focus(); later(1400, () => botSay(S.room.players.find((p) => !p.me) ? S.room.players.find((p) => !p.me).label : "Omar K.", "Wa alaykum as-salam")); };
  SJ.actions["kick"] = (a) => { const r = S.room; const p = r.players.find((x) => x.id === a.dataset.v); r.players = r.players.filter((x) => x !== p); r.chat.push({ sys: true, text: `${p.label} was removed from the room` }); SJ.render(); };
  SJ.actions["toggle-ready"] = () => { const me = S.room.players.find((p) => p.me); me.ready = !me.ready; S.room.sim = false; simTimers.forEach(clearTimeout); simTimers = []; SJ.render(); };
  SJ.actions["leave-room"] = () => SJ.modal(`<div class="m-ic red">${I("door", "i-lg")}</div><h2>Leave the room?</h2><p>${S.room.host === "me" ? "You're the host. The room closes for everyone." : "You can rejoin with the same code while the room is open."}</p><div class="actions"><button class="btn" data-act="close">Stay</button><button class="btn btn-danger" data-act="leave-yes">Leave room</button></div>`);
  SJ.actions["leave-yes"] = () => { SJ.closeOverlay(); S.room = null; SJ.go("play"); SJ.toast("You left the room."); };
  function startRoom() {
    const r = S.room; if (!r) return;
    const pool = r.pool && r.pool.length ? r.pool : SJ.pool(r.st);
    r.players.forEach((p) => { p.score = 0; });
    SJ.startGame("live", pool, r.st, false, r.players);
  }
  SJ.actions["start-room"] = () => startRoom();

  SJ.screens["room-join"] = () => `<div class="page narrow"><button class="btn btn-ghost btn-sm" data-go="play" style="margin-bottom:12px">${I("arrowL", "flip")}${t("play")}</button>
    <div class="card pad" style="padding:28px;border-radius:var(--r-xl)"><span class="mode-ic gold">${I("door")}</span><h1 style="font-size:26px;margin:14px 0 6px">${t("join")}</h1><p class="muted">Ask the host for the 6-character code. Letters and numbers, no spaces needed.</p>
      <form data-form="join" style="margin-top:20px" novalidate><div class="field"><label class="label" for="jn-code">Room code</label><input class="input num" id="jn-code" maxlength="7" autocomplete="off" autocapitalize="characters" value="${E(S.params.code || "")}" placeholder="e.g. 7K4 Q2M" style="font-size:24px;letter-spacing:.15em;font-weight:800;text-transform:uppercase;min-height:60px"><span class="err" id="jn-err" hidden></span></div>
      ${SJ.isGuest() ? `<p class="help" style="margin-top:12px">You'll join as <b>${E(S.guestName)}</b>.</p>` : ""}
      <button class="btn btn-primary btn-lg btn-block" style="margin-top:18px" type="submit">Join room</button></form></div></div>`;
  SJ.forms["join"] = () => {
    const v = document.getElementById("jn-code").value.replace(/\s/g, "").toUpperCase(), err = document.getElementById("jn-err");
    if (!/^[A-Z0-9]{6}$/.test(v)) { err.hidden = false; err.textContent = "Room codes have 6 letters or numbers."; return; }
    if (v === "000000") { err.hidden = false; err.textContent = "No open room has this code. Check it with the host."; return; }
    if (SJ.isGuest()) {
      const gp = (n, ready) => ({ id: "g:" + n, label: "Guest " + n, guest: true, c: "#6b7280", ini: "G", ar: "ضيف", skill: 0.5 + Math.random() * 0.25, score: 0, ready });
      S.room = { code: v, host: "g:4410", guest: true, pool: null, st: Object.assign({}, S.setup, { domain: "islamic", cats: ["prophets", "seerah", "quran"], count: 5, timer: 20, level: "mixed" }), players: [gp("4410", true), gp("2207", true), Object.assign(meP(), { ready: false })], chat: [{ sys: true, text: X("You joined a guest room. Only guests are here.", "انضممت إلى غرفة ضيوف، كل من فيها ضيوف.", "Vous avez rejoint un salon invité.") }, { who: "Guest 4410", text: "Welcome! Press ready and we start." }], sim: false, script: "join" };
      SJ.go("lobby"); return;
    }
    const host = asPlayer(SJ.person("omar"), { ready: true });
    S.room = { code: v, host: "omar", pool: null, st: Object.assign({}, S.setup, { domain: "islamic", cats: ["hadith", "seerah", "quran"], count: 5, timer: 20, level: "mixed" }), players: [host, asPlayer(SJ.person("khadija"), { ready: true }), asPlayer(SJ.person("mariam"), { ready: false }), Object.assign(meP(), { ready: false })], chat: [{ sys: true, text: "You joined the room" }, { who: SJ.pname(SJ.person("omar")), text: "Welcome! Press ready and we start." }], sim: false, script: "join" };
    SJ.go("lobby");
  };
  SJ.demoRoom = (r) => {
    S.room = { code: "7K4Q2M", host: "me", pool: null, st: Object.assign({}, S.setup, { domain: "islamic", cats: [], count: 5, timer: 20, level: "mixed" }), players: [meP(), asPlayer(SJ.person("omar"), { ready: true }), asPlayer(SJ.person("khadija"), { ready: true }), asPlayer(SJ.person("youssef"), { ready: true })], chat: [], sim: true, script: "none" };
    if (r === "live") { startRoom(); return; }
    const qs = SJ.shuffle(SJ.pool(S.room.st)).slice(0, 5);
    const players = S.room.players;
    players.forEach((p, i) => { p.score = [146, 172, 121, 98][i]; });
    S.game = { mode: "live", qs, i: qs.length - 1, st: S.room.st, timer: 20, left: 0, picked: null, revealed: true, hidden: [], score: 146, fifty: 0, skip: 0, players,
      log: qs.map((q, i) => ({ q, picked: i === 3 ? (q.c + 1) % 4 : q.c, ok: i !== 3, gain: i !== 3 ? 36 : 0, secs: 7 })) };
    SJ.go("podium");
  };

  SJ.screens["podium"] = () => {
    const g = S.game;
    if (!g || !g.players) return `<div class="page"><div class="empty"><h3>No live game finished yet</h3><button class="btn btn-primary" data-act="room-create">Host a room</button></div></div>`;
    const ps = g.players.slice().sort((a, b) => b.score - a.score);
    const myRank = ps.findIndex((p) => p.me) + 1;
    const DEVC = ["#3365a8", "#0f766e", "#8a4fbf", "#c0612b", "#be185d", "#4f46e5", "#a16207", "#475569", "#b4372c"];
    const pav = (p, size) => p.me ? SJ.meAv(size) : g.mode === "device" ? `<span class="av ${size}" style="--c:${DEVC[g.players.indexOf(p) % 9]}">${E(p.label.charAt(0).toUpperCase())}</span>` : p.guest ? `<span class="av ${size} guest">G</span>` : SJ.av(SJ.person(p.id) || p, size);
    const teams = g.teams ? `<div class="row wrap" style="justify-content:center;gap:12px">${g.teams.map((tm, k) => { const sum = tm.reduce((s, id) => s + (g.players.find((p) => p.id === id) || { score: 0 }).score, 0); return `<span class="badge ${k === 0 ? "green" : "blue"}" style="height:34px;font-size:14px;padding:0 14px">${X("Team","فريق","Équipe")} ${"AB"[k]} · <b class="num">${sum}</b></span>`; }).join("")}</div>` : "";
    const pod = [ps[1], ps[0], ps[2]].map((p, k) => p ? `<div class="pod p${[2, 1, 3][k]}">${pav(p, "lg")}<b style="text-align:center">${E(p.me ? "You" : p.label)}</b><span class="pts num">${p.score}</span><div class="block">${[2, 1, 3][k]}</div></div>` : "<div></div>").join("");
    return `<div class="page mid" style="display:flex;flex-direction:column;gap:24px">
      <div style="text-align:center"><span class="badge gold">${g.mode === "device" ? X("On this device","على هذا الجهاز","Sur cet appareil") : t("liveRoom") + " · " + (S.room ? fmtCode(S.room.code) : "")}</span><h1 style="font-size:32px;font-weight:800;margin-top:10px">${g.mode === "device" ? (ps[0].me ? X("You win","أنت الفائز","Vous gagnez") : E(ps[0].label) + " " + X("wins","يفوز","gagne")) : myRank === 1 ? X("You won the room","فزت بالغرفة","Vous gagnez la salle") : X(`You finished #${myRank}`,`حللت في المركز ${myRank}`,`Vous finissez n°${myRank}`)}</h1><p class="muted">${g.qs.length} questions · ${g.players.length} players</p></div>
      ${teams}<div class="podium">${pod}</div>
      <section class="card"><ol class="list">${ps.map((p, i) => `<li class="li ${p.me ? "me" : ""}"><span class="rank ${i === 0 ? "r1" : ""}">${i + 1}</span>${pav(p, "sm")}<span class="grow"><span class="t">${E(p.me ? "You" : p.label)}</span>${p.guest ? ' <span class="badge" style="height:20px">guest</span>' : ""}</span><b class="num">${p.score} pts</b>${!p.me && !p.guest && g.mode !== "device" && SJ.signedIn() && !S.friends.includes(p.id) ? `<button class="btn btn-sm" data-act="add-friend" data-v="${p.id}">${I("plus", "i-sm")}Add friend</button>` : ""}</li>`).join("")}</ol></section>
      <div class="row wrap" style="gap:10px;justify-content:center">${g.mode === "device" ? `<button class="btn btn-primary btn-lg" data-act="dev-again">${I("tri", "flip")}${X("Play again, same players","العبوا مرة أخرى","Rejouer, mêmes joueurs")}</button><button class="btn btn-lg" data-go="challenge-new" data-who="device">${X("Change the round","غيّروا الجولة","Modifier la manche")}</button>` : `<button class="btn btn-primary btn-lg" data-act="room-again">${I("tri", "flip")}Play again in this room</button><button class="btn btn-lg" data-act="room-back">Back to the lobby</button>`}<button class="btn btn-ghost btn-lg" data-act="results-live">Review my answers</button></div>
    </div>`;
  };
  SJ.actions["room-again"] = () => startRoom();
  SJ.actions["room-back"] = () => { S.room.sim = true; SJ.go("lobby"); };
  SJ.actions["results-live"] = () => SJ.go("results");

  /* ---------------- Duel ---------------- */
  SJ.screens["duel"] = () => {
    const d = S.duelState || (S.duelState = { phase: "idle" });
    const me = SJ.ME;
    let body = "";
    const g = SJ.isGuest();
    if (d.phase === "idle") body = `<div class="stack" style="gap:18px;align-items:center;text-align:center"><span class="mode-ic blue" style="width:64px;height:64px">${I("swords", "i-lg")}</span><h1 style="font-size:28px;font-weight:800">${g ? X("Guest duel", "مبارزة الضيوف", "Duel invité") : t("duel")}</h1><p class="muted" style="max-width:46ch">${g ? X("5 questions, 15 seconds each, against another guest. Not rated.", "5 أسئلة، 15 ثانية لكل سؤال، ضد ضيف آخر. غير مصنّفة.", "5 questions, 15 s chacune, contre un autre invité. Non classé.") : X("5 questions, 15 seconds each, against a player near your level. Wins raise your duel rating.", "5 أسئلة، 15 ثانية لكل سؤال، ضد لاعب قريب من مستواك. الفوز يرفع تصنيفك.", "5 questions, 15 s chacune, contre un joueur de votre niveau. Gagner augmente votre classement.")}</p>
      ${SJ.domainSwitch("domain")}${g ? "" : `<div class="row wrap" style="justify-content:center"><span class="badge blue">Your rating 1,436</span><span class="badge">38 wins · 21 losses</span></div>`}
      <button class="btn btn-primary btn-lg" data-act="duel-search">${I("search")}Find an opponent</button></div>`;
    if (d.phase === "search") body = `<div class="stack" style="gap:18px;align-items:center;text-align:center"><div class="radar"><i></i><i></i><i></i>${SJ.meAv("lg")}</div><h1 style="font-size:24px;font-weight:800">Looking for an opponent…</h1><p class="muted">${g ? X("Guests only", "الضيوف فقط", "Invités uniquement") : `Level ${me.level - 2} to ${me.level + 2}`} · ${E(L(SJ.dom(S.domain).name))} · ${X("usually under 20 seconds", "عادة أقل من 20 ثانية", "souvent moins de 20 s")}</p><button class="btn" data-act="duel-cancel">Cancel</button></div>`;
    if (d.phase === "found") { const op = SJ.person("youssef"); body = `<div class="stack" style="gap:22px;align-items:center;text-align:center"><span class="badge green">${X("Opponent found", "وُجد خصم", "Adversaire trouvé")}</span>
      <div class="row" style="gap:28px;justify-content:center"><div class="stack" style="align-items:center;gap:8px">${SJ.meAv("xl")}<b>${g ? E(S.guestName) : X("You", "أنت", "Vous")}</b><span class="muted num">${g ? X("Guest", "ضيف", "Invité") : `Level ${me.level} · 1,436`}</span></div><span class="vs-mid" style="font-size:24px">VS</span><div class="stack" style="align-items:center;gap:8px">${g ? `<span class="av xl guest">G</span><b>Guest 3391</b><span class="muted">${X("Guest", "ضيف", "Invité")}</span>` : `${SJ.av(op, "xl")}<b>${E(SJ.pname(op))}</b><span class="muted num">Level ${op.level} · 1,412</span>`}</div></div>
      <button class="btn btn-primary btn-lg" data-act="duel-go">${I("tri", "flip")}Start the duel</button><span class="help">Starts by itself in a few seconds</span></div>`; }
    return `<div class="page narrow"><button class="btn btn-ghost btn-sm" data-go="play" style="margin-bottom:12px">${I("arrowL", "flip")}${t("play")}</button>${g ? SJ.guestTemp("duel") : ""}<div class="card" style="padding:36px 24px;border-radius:var(--r-xl)">${body}</div></div>`;
  };
  SJ.after["duel"] = () => {
    const d = S.duelState; if (!d || d.sim) return; d.sim = true;
    if (d.phase === "search") later(2600, () => { d.phase = "found"; d.sim = false; SJ.render(); });
    if (d.phase === "found") later(4500, () => { if (S.route === "duel" && d.phase === "found") SJ.actions["duel-go"](); });
  };
  SJ.actions["duel-search"] = () => { S.duelState = { phase: "search" }; SJ.render(); };
  SJ.actions["duel-cancel"] = () => { SJ.stopTimers(); S.duelState = { phase: "idle" }; SJ.render(); };
  SJ.actions["duel-go"] = () => {
    SJ.stopTimers(); S.duelState = { phase: "idle" };
    const st = Object.assign({}, S.setup, { domain: S.domain, cats: [], count: 5, timer: 15, level: "mixed", title: () => t("duel") });
    const op = SJ.isGuest() ? { id: "g:3391", label: "Guest 3391", guest: true, c: "#6b7280", ini: "G", ar: "ضيف", skill: 0.6, score: 0, ready: true } : asPlayer(SJ.person("youssef"), { skill: 0.68 });
    SJ.startGame("duel", SJ.pool(st), st, false, [meP(), op]);
  };

  /* ---------------- Leaderboard ---------------- */
  SJ.screens["leaderboard"] = () => {
    const lb = S.lb || (S.lb = { period: "week", scope: "all", domain: "all" });
    const mult = { week: 1, month: 3.6, all: 0 }[lb.period];
    let ps = (lb.scope === "friends" ? S.friends.map(SJ.person) : SJ.PEOPLE.slice()).map((p) => ({ p, pts: lb.period === "all" ? p.all : Math.round(p.week * mult * (lb.domain === "arabic" ? 0.18 : lb.domain === "islamic" ? 0.82 : 1)) }));
    const mine = lb.period === "all" ? 8120 : Math.round(SJ.ME.weekPts * mult * (lb.domain === "arabic" ? 0.18 : lb.domain === "islamic" ? 0.82 : 1));
    if (!SJ.isGuest()) ps.push({ me: true, pts: mine });
    ps.sort((a, b) => b.pts - a.pts);
    const myIdx = ps.findIndex((x) => x.me);
    const myRank = lb.scope === "friends" ? myIdx + 1 : (lb.period === "week" ? 18 : lb.period === "month" ? 23 : 61);
    const seg = (k, opts) => `<div class="seg">${opts.map(([v, l]) => `<button data-act="lb" data-k="${k}" data-v="${v}" aria-pressed="${lb[k] === v}">${l}</button>`).join("")}</div>`;
    const avOf = (x, s) => x.me ? SJ.meAv(s) : SJ.av(x.p, s);
    const nm = (x) => x.me ? "You" : SJ.pname(x.p);
    const top = ps.slice(0, 3);
    const rest = lb.scope === "friends" ? ps.slice(3) : ps.slice(3, 9).filter((x) => !x.me);
    return `<div class="page mid"><div class="page-head"><div><h1>${t("leaderboard")}</h1><p class="sub">Points from every game you play. Weekly boards reset on Friday after Maghrib.</p></div></div>
      <div class="row wrap" style="gap:10px;margin-bottom:20px">${seg("period", [["week", "This week"], ["month", "This month"], ["all", "All time"]])}${seg("scope", [["all", "Everyone"], ["friends", "Friends"]])}
        <select class="select" style="width:auto;min-height:44px" data-act="lb-domain" aria-label="Domain"><option value="all">All domains</option>${SJ.myDoms().map((d) => `<option value="${d.id}" ${lb.domain === d.id ? "selected" : ""}>${E(L(d.name))}</option>`).join("")}</select></div>
      ${SJ.isGuest() ? `<div class="banner info" style="margin-bottom:20px"><div class="grow"><b>Guests don't appear on leaderboards.</b> Create an account and today's points count.</div><button class="btn btn-primary btn-sm" data-go="signup">${t("signup")}</button></div>` : ""}
      <div class="podium" style="margin-bottom:24px">${[top[1], top[0], top[2]].map((x, k) => x ? `<div class="pod p${[2, 1, 3][k]}">${avOf(x, "lg")}<b>${E(nm(x))}</b><span class="pts num">${x.pts.toLocaleString()}</span><div class="block">${[2, 1, 3][k]}</div></div>` : "<div></div>").join("")}</div>
      <section class="card"><ol class="list">${rest.map((x) => `<li class="li ${x.me ? "me" : ""}"><span class="rank">${ps.indexOf(x) + 1}</span>${avOf(x, "sm")}<span class="grow t">${E(nm(x))}</span><b class="num">${x.pts.toLocaleString()}</b></li>`).join("")}
        ${!SJ.isGuest() && lb.scope !== "friends" ? `<li class="li" style="justify-content:center;color:var(--ink-3)">···</li><li class="li me"><span class="rank">${myRank}</span>${SJ.meAv("sm")}<span class="grow t">You</span><b class="num">${mine.toLocaleString()}</b></li>` : ""}</ol></section>
      ${!SJ.isGuest() ? `<p class="help" style="margin-top:12px">${lb.period === "week" ? "Two good games would take you into the top 15." : "Keep your streak to climb."}</p>` : ""}</div>`;
  };
  SJ.actions["lb"] = (a) => { S.lb[a.dataset.k] = a.dataset.v; SJ.render(); };
  SJ.changes["lb-domain"] = (a) => { S.lb.domain = a.value; SJ.render(); };

  /* ---------------- Profile ---------------- */
  SJ.screens["profile"] = () => {
    const me = SJ.ME;
    const cells = Array.from({ length: 84 }, (_, i) => { const v = (i * 37 % 11) / 10; const lvl = i > 71 ? 3 : v > .75 ? 3 : v > .45 ? 2 : v > .2 ? 1 : 0; return `<i style="aspect-ratio:1;border-radius:3px;background:${["var(--sunk)", "color-mix(in srgb,var(--brand) 30%,var(--surface))", "color-mix(in srgb,var(--brand) 60%,var(--surface))", "var(--brand)"][lvl]}"></i>`; }).join("");
    const recent = [["hadith", "7 / 10", "+142", "Today"], ["quran", "9 / 10", "+188", "Today"], ["fiqh", "Duel won 5–3", "+25 rating", "Yesterday"], ["ramadan", "Live room · 2nd of 5", "+120", "Yesterday"], ["prophets", "10 / 10", "+210", "Monday"]];
    return `<div class="page mid" style="display:flex;flex-direction:column;gap:22px">
      <section class="card pad" style="border-radius:var(--r-xl)"><div class="row wrap" style="gap:18px"><span class="avatar-btn" style="--p:${Math.round(me.xp / me.xpNext * 100)};width:96px;height:96px;cursor:default">${SJ.meAv("xl")}<span class="lvl-badge" style="font-size:12px;height:22px;min-width:26px">${me.level}</span></span>
        <div style="flex:1;min-width:200px"><h1 style="font-size:26px;font-weight:800">${SJ.myName()}</h1><p class="muted">@${me.handle} · joined ${me.joined} · plays in English and Arabic</p><div class="row wrap" style="margin-top:10px;gap:8px"><span class="badge gold">${I("flame", "i-sm")}${me.streak}-day streak</span><span class="badge green">Level ${me.level}</span><span class="badge blue">Duel rating 1,436</span></div></div>
        <div class="row" style="gap:8px"><button class="btn" data-go="settings">${I("pen")}Edit profile</button><button class="btn" data-act="copy" data-text="https://siraj.app/u/${me.handle}">${I("link")}Share</button></div></div></section>
      <div class="stat-row"><div class="stat"><div class="k">Games</div><div class="v">${me.games}</div></div><div class="stat"><div class="k">Accuracy</div><div class="v">${me.accuracy}%</div></div><div class="stat gold"><div class="k">Best streak</div><div class="v">21 d</div></div><div class="stat"><div class="k">Duels won</div><div class="v">38</div></div></div>
      <div class="cols">
        <div class="col-main" style="gap:22px">
          <section class="card pad"><div class="card-head"><h2>${X("Mastery","الإتقان","Maîtrise")}</h2><span class="help">${X("Open a domain to see each category","افتح مجالًا لترى فئاته","Ouvrez un domaine pour voir ses catégories")}</span></div>${SJ.masteryList()}</section>
          <section class="card pad" id="ach"><div class="card-head"><h2>${t("achievements")}</h2><span class="muted" style="font-size:13px">${SJ.ACHIEVEMENTS.filter((a) => a.on).length} of ${SJ.ACHIEVEMENTS.length}</span></div>
            <div class="grid-auto" style="grid-template-columns:repeat(auto-fill,minmax(150px,1fr))">${SJ.ACHIEVEMENTS.map((a) => `<div class="ach ${a.on ? "" : "off"}"><span class="medal">${I(a.on ? "star" : "lock")}</span><span class="an">${E(a.name)}</span><span class="ad">${E(a.d)}${a.prog ? " · " + a.prog : ""}</span></div>`).join("")}</div></section>
        </div>
        <div class="col-side">
          <section class="card pad"><div class="card-head"><h2>Last 12 weeks</h2></div><div style="display:grid;grid-template-columns:repeat(12,minmax(0,1fr));grid-auto-flow:column;grid-template-rows:repeat(7,auto);gap:3px">${cells}</div><p class="help" style="margin-top:10px">Darker days had more games.</p></section>
          <section class="card"><div class="card-head" style="padding:16px 16px 0"><h2>Recent games</h2></div><ul class="list">${recent.map(([c, r, p, d]) => `<li class="li">${SJ.catIc(SJ.cat(c), "sm")}<span class="grow"><span class="t" style="display:block">${E(L(SJ.cat(c).name))}</span><span class="s">${r} · ${d}</span></span><b class="num" style="color:var(--brand-ink)">${p}</b></li>`).join("")}</ul></section>
        </div></div></div>`;
  };
  SJ.masteryList = () => {
    const open = S.openDoms || (S.openDoms = [SJ.DOMAINS[0].id]);
    return `<div class="acc">${SJ.DOMAINS.map((d) => {
      const cats = SJ.CATS.filter((c) => c.domain === d.id);
      const avg = cats.length ? Math.round(cats.reduce((s, c) => s + c.mastery, 0) / cats.length) : 0;
      const on = open.includes(d.id);
      return `<div class="acc-item"><button class="acc-head" data-act="acc" data-v="${d.id}" aria-expanded="${on}"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span style="flex:1;min-width:0"><b style="display:block">${E(L(d.name))}</b><span class="help">${cats.length} ${X("categories","فئات","catégories")}</span></span>
        <span class="acc-meter"><span class="bar" style="height:6px"><i style="--v:${avg}%;--c:${d.color}"></i></span><b class="num">${avg}%</b></span>${I("chevR", "acc-chev flip")}</button>
        ${on ? `<div class="acc-body">${cats.map((c) => `<div><div class="row" style="margin-bottom:6px">${SJ.catIc(c, "sm")}<span style="flex:1;font-weight:650">${E(L(c.name))}</span><span class="num muted">${c.mastery}%</span></div><div class="bar"><i style="--v:${c.mastery}%;--c:${c.color}"></i></div></div>`).join("")}</div>` : ""}</div>`;
    }).join("")}</div>`;
  };
  SJ.actions["acc"] = (a) => { const v = a.dataset.v, o = S.openDoms; S.openDoms = o.includes(v) ? o.filter((x) => x !== v) : o.concat(v); SJ.render(); };
  SJ.after["profile"] = () => { if (S.params.tab === "ach") { const el = document.getElementById("ach"); if (el) el.scrollIntoView({ behavior: "smooth" }); S.params.tab = null; } };

  /* ---------------- Settings ---------------- */
  SJ.screens["settings"] = () => {
    const g = SJ.isGuest();
    const seg = (act, cur, opts) => `<div class="seg">${opts.map(([v, l]) => `<button data-act="${act}" data-v="${v}" aria-pressed="${cur === v}">${l}</button>`).join("")}</div>`;
    const tog = (id, label, help, on) => `<label class="switch" style="padding:12px 0;border-bottom:1px solid var(--line)"><input type="checkbox" id="${id}" ${on ? "checked" : ""}><span style="flex:1"><b>${label}</b>${help ? `<br><span class="help">${help}</span>` : ""}</span></label>`;
    return `<div class="page narrow" style="display:flex;flex-direction:column;gap:18px"><div class="page-head" style="margin:0"><div><h1>${t("settings")}</h1></div></div>
      ${g ? `<div class="banner"><div class="grow"><b>Playing as ${E(S.guestName)}.</b> Account, privacy and notification settings come with a free account.</div><button class="btn btn-primary btn-sm" data-go="signup">${t("signup")}</button></div>` : `
      <section class="card pad"><h2 style="font-size:17px;margin-bottom:14px">Profile</h2><div class="form-grid"><div class="field"><label class="label" for="st-name">Name</label><input class="input" id="st-name" value="Aisha T."></div><div class="field"><label class="label" for="st-user">Username</label><input class="input" id="st-user" value="aisha_t"></div></div>
        <div class="field"><label class="label" for="st-email">Email</label><input class="input" id="st-email" value="aisha.t@example.com" type="email"></div></section>`}
      <section class="card pad stack" style="gap:16px"><h2 style="font-size:17px">Language and display</h2>
        <div><div class="label" style="margin-bottom:8px">App language</div>${seg("lang", S.lang, [["ar", "العربية"], ["en", "English"], ["fr", "Français"]])}</div>
        <div><div class="label" style="margin-bottom:8px">Theme</div>${seg("theme", S.theme, [["light", t("light")], ["dark", t("dark")], ["device", t("device")]])}</div></section>
      ${g ? "" : `<section class="card pad"><h2 style="font-size:17px;margin-bottom:4px">Notifications</h2>
        ${tog("n1", "Question of the day", "Every day at 20:00", true)}${tog("n2", "Challenges", "When a friend challenges you or beats your score", true)}${tog("n3", "Messages", "", true)}${tog("n4", "Live room invites", "", true)}${tog("n5", "Weekly summary", "By email on Friday", false)}</section>
      <section class="card pad"><h2 style="font-size:17px;margin-bottom:4px">Privacy</h2>${tog("p1", "Show me on leaderboards", "", true)}
        <div class="form-grid" style="margin-top:14px"><div class="field"><label class="label" for="p2">Who can challenge me</label><select class="select" id="p2"><option>Friends</option><option>Anyone</option><option>No one</option></select></div><div class="field"><label class="label" for="p3">Who can message me</label><select class="select" id="p3"><option>Friends</option><option>No one</option></select></div></div></section>
      <section class="card pad"><h2 style="font-size:17px;margin-bottom:14px">Account</h2><div class="stack"><div class="row">${I("google")}<span style="flex:1">Connected to Google</span><button class="btn btn-sm btn-ghost" data-act="toast" data-msg="Google disconnected">Disconnect</button></div><div class="row">${I("lock")}<span style="flex:1">Password</span><button class="btn btn-sm" data-act="toast" data-msg="We sent a link to change your password">Change</button></div></div>
        <div class="divider" style="margin:16px 0"></div><div class="row wrap"><button class="btn" data-act="role" data-v="visitor">${I("logout", "flip")}${t("signout")}</button><span class="spacer"></span><button class="btn btn-ghost" style="color:var(--danger)" data-act="delete-acc">${I("trash")}Delete account</button></div></section>
      <div class="row"><span class="spacer"></span><button class="btn btn-primary btn-lg" data-act="toast" data-msg="Settings saved">Save changes</button></div>`}
    </div>`;
  };
  SJ.actions["delete-acc"] = () => SJ.modal(`<div class="m-ic red">${I("trash", "i-lg")}</div><h2>Delete your account?</h2><p>This removes your profile, stats, friends and messages for good. Type DELETE to confirm.</p><input class="input" id="del-in" style="margin-top:14px" aria-label="Type DELETE"><div class="actions"><button class="btn" data-act="close">Cancel</button><button class="btn btn-danger" data-act="delete-yes">Delete account</button></div>`);
  SJ.actions["delete-yes"] = () => { if ((document.getElementById("del-in") || {}).value !== "DELETE") { SJ.toast("Type DELETE in capitals to confirm."); return; } SJ.closeOverlay(); SJ.setRole("visitor"); SJ.toast("Your account was deleted."); };

  /* ---------------- prototype flows ---------------- */
  SJ.flow = (v) => {
    S.proto = false;
    if (v === "guest") { S.role = "guest"; SJ.store.set("role", "guest"); S.guestName = "Guest 4821"; SJ.startQuick(); return; }
    if (v === "host") { S.role = "player"; SJ.store.set("role", "player"); S.setup = Object.assign(S.setup, { mode: "room", domain: "islamic", cats: [], count: 5, timer: 20 }); SJ.go("room-create"); return; }
    if (v === "joinroom") { S.role = "guest"; SJ.store.set("role", "guest"); SJ.go("room-join", { code: "7K4Q2M" }); return; }
    if (v === "duel") { S.role = "player"; SJ.store.set("role", "player"); S.duelState = { phase: "idle" }; SJ.go("duel"); return; }
    if (v === "arabic") { S.role = "player"; SJ.store.set("role", "player"); S.domain = "arabic"; SJ.setLang("ar"); SJ.go("play"); return; }
    if (v === "device") { S.role = "player"; SJ.store.set("role", "player"); S.nr = null; SJ.go("challenge-new", { who: "device" }); return; }
    if (v === "new") { S.onb = null; SJ.setRole("new"); }
  };
})();
