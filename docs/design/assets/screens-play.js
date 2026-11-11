/* Sirāj v2: Play hub, game setup, the game engine (solo, live room, duel) and results. */
(function () {
  "use strict";
  const SJ = window.SJ, S = SJ.S, I = SJ.icon, E = SJ.esc, t = SJ.t, L = SJ.L, X = SJ.tx;
  const BASE = { 1: 10, 2: 20, 3: 30 };
  const letters = () => (S.lang === "ar" ? ["أ", "ب", "ج", "د"] : ["A", "B", "C", "D"]);
  const shuffle = (a) => { a = a.slice(); for (let i = a.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [a[i], a[j]] = [a[j], a[i]]; } return a; };

  /* ---------------- Play hub ---------------- */
  SJ.screens["play"] = () => {
    const d = SJ.dom(S.domain);
    return `<div class="page" style="display:flex;flex-direction:column;gap:28px">
      <div class="page-head" style="margin:0"><div><h1>${t("play")}</h1><p class="sub">${X("Pick a domain, then how you want to play.","اختر مجالًا، ثم طريقة اللعب.","Choisissez un domaine, puis votre façon de jouer.")}</p></div></div>
      ${SJ.domainSwitch("domain")}
      <section class="section"><div class="section-head"><h2>${X("On your own","وحدك","En solo")}</h2></div>
        <div class="grid-auto wide">
          <button class="tile" data-act="quick"><span class="mode-ic green">${I("bolt")}</span><span><span class="tt">${t("quick")}</span><span class="ts">${X("5 mixed questions from","5 أسئلة متنوعة من","5 questions variées :")} ${E(L(d.name))}</span></span></button>
          <button class="tile" data-act="daily-mix"><span class="mode-ic gold">${I("shuffle")}</span><span><span class="tt">${X("Daily mix","مزيج اليوم","Mix du jour")}</span><span class="ts">${X("One question from every category","سؤال من كل فئة","Une question par catégorie")}</span></span></button>
          <button class="tile" data-act="custom"><span class="mode-ic blue">${I("sliders")}</span><span><span class="tt">${X("Custom game","لعبة مخصّصة","Partie personnalisée")}</span><span class="ts">${X("Choose topics, difficulty and length","اختر المواضيع والصعوبة والطول","Thèmes, difficulté et durée")}</span></span></button>
        </div></section>
      <section class="section"><div class="section-head"><h2>${X("With others","مع الآخرين","Avec d'autres")}</h2></div>${SJ.modeTiles()}
        ${SJ.signedIn() || SJ.isGuest() ? `<button class="tile" data-go="challenges" style="max-width:520px"><span class="mode-ic red">${I("send")}</span><span><span class="tt">${SJ.isGuest() ? X("Challenge a guest","تحدَّ ضيفًا","Défier un invité") : X("Challenge a friend","تحدَّ صديقًا","Défier un ami")}</span><span class="ts">${X("Same questions, they play when they can","الأسئلة نفسها، ويلعب متى شاء","Mêmes questions, il joue quand il veut")}</span></span></button>` : ""}</section>
      <section class="section"><div class="section-head"><h2>${X("Categories in","فئات","Catégories :")} ${E(L(d.name))}</h2></div>${SJ.catGrid(S.domain, false)}</section>
    </div>`;
  };
  SJ.actions["custom"] = () => { S.setup = Object.assign(S.setup, { domain: S.domain, cats: [], mode: "solo" }); SJ.go("setup"); };

  /* ---------------- Topic picker (shared by Play setup + New round) ---------------- */
  const qn = (id) => SJ.COUNT[id] || 0;
  const visCats = (d) => SJ.CATS.filter((c) => !c.hidden && (!d || c.domain === d));
  SJ.scopeCount = (sel) => sel.domain === "all" ? visCats().reduce((a, c) => a + qn(c.id), 0) : !sel.cats.length ? visCats(sel.domain).reduce((a, c) => a + qn(c.id), 0) : sel.cats.reduce((a, id) => a + qn(id), 0);
  SJ.scopeLabel = (sel) => sel.domain === "all" ? X("All categories", "كل الفئات", "Toutes les catégories") : !sel.cats.length ? X("All of ", "كل ", "Tout ") + L(SJ.dom(sel.domain).name) : sel.cats.length === 1 ? L(SJ.cat(sel.cats[0]).name) : sel.cats.map((c) => L(SJ.cat(c).name)).join(", ");
  SJ.topicPicker = (sel, ns) => {
    const opens = S.tpOpen || (S.tpOpen = {});
    const op = opens[ns] || (opens[ns] = [sel.domain === "all" ? SJ.DOMAINS[0].id : sel.domain]);
    const allOn = sel.domain === "all", ck = `<span class="tp-ck">${I("check", "i-sm")}</span>`;
    return `<div class="tp">
      <button class="tp-all ${allOn ? "on" : ""}" data-act="tp-all" data-ns="${ns}" aria-pressed="${allOn}"><span class="cat-ic" style="--c:#0b7a5c">${I("shuffle")}</span><span class="tp-t"><b>${X("All categories", "كل الفئات", "Toutes les catégories")}</b><span class="help">${X("Every domain, mixed together", "كل المجالات ممزوجة", "Tous les domaines mélangés")}</span></span><span class="tp-n num">${SJ.scopeCount({ domain: "all", cats: [] })}</span>${allOn ? ck : ""}</button>
      ${SJ.DOMAINS.map((d) => {
        const cats = visCats(d.id), dc = cats.reduce((a, c) => a + qn(c.id), 0);
        const mine = sel.domain === d.id, domOn = mine && !sel.cats.length, partial = mine && sel.cats.length, isOpen = op.includes(d.id);
        return `<div class="tp-dom ${mine ? "act" : ""}"><div class="tp-head">
          <button class="tp-tog" data-act="tp-open" data-ns="${ns}" data-v="${d.id}" aria-expanded="${isOpen}"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span class="tp-t"><b>${E(L(d.name))}</b><span class="help">${cats.length} ${X("categories", "فئات", "catégories")} · ${dc} ${X("questions", "سؤالًا", "questions")}${partial ? ` · <b style="color:var(--brand-ink)">${sel.cats.length} ${X("picked", "مختارة", "choisies")}</b>` : ""}</span></span>${I("chevR", "tp-chev flip")}</button>
          <button class="tp-pill ${domOn ? "on" : ""}" data-act="tp-dom" data-ns="${ns}" data-v="${d.id}" aria-pressed="${domOn}" ${dc ? "" : "disabled"}>${domOn ? I("check", "i-sm") : ""}${X("All of it", "الكل", "Tout")}</button></div>
          ${isOpen ? `<div class="tp-grid">${cats.map((c) => { const on = mine && (domOn || sel.cats.includes(c.id)), n = qn(c.id); return `<button class="tp-cat ${on ? "on" : ""} ${domOn ? "soft" : ""}" data-act="tp-cat" data-ns="${ns}" data-v="${c.id}" aria-pressed="${on}" ${n ? "" : "disabled"}>${SJ.catIc(c, "sm")}<span class="tp-cn">${E(L(c.name))}</span><span class="tp-n num">${n || X("Soon", "قريبًا", "Bientôt")}</span>${on && !domOn ? ck : ""}</button>`; }).join("")}</div>` : ""}</div>`;
      }).join("")}</div>`;
  };
  const tpSt = (ns) => (ns === "nr" ? SJ.nrState() : S.setup);
  const tpOpenAdd = (ns, d) => { const o = (S.tpOpen || (S.tpOpen = {}))[ns] || (S.tpOpen[ns] = []); if (!o.includes(d)) o.push(d); };
  SJ.actions["tp-all"] = (a) => { const s = tpSt(a.dataset.ns); s.domain = "all"; s.cats = []; SJ.render(); };
  SJ.actions["tp-dom"] = (a) => { const s = tpSt(a.dataset.ns); s.domain = a.dataset.v; s.cats = []; tpOpenAdd(a.dataset.ns, a.dataset.v); SJ.render(); };
  SJ.actions["tp-open"] = (a) => { const ns = a.dataset.ns, v = a.dataset.v; const o = (S.tpOpen || (S.tpOpen = {}))[ns] || (S.tpOpen[ns] = []); S.tpOpen[ns] = o.includes(v) ? o.filter((x) => x !== v) : o.concat(v); SJ.render(); };
  SJ.actions["tp-cat"] = (a) => {
    const s = tpSt(a.dataset.ns), v = a.dataset.v, d = SJ.cat(v).domain;
    if (s.domain !== d) { s.domain = d; s.cats = [v]; }
    else if (!s.cats.length) s.cats = [v];
    else s.cats = s.cats.includes(v) ? s.cats.filter((x) => x !== v) : s.cats.concat(v);
    if (s.cats.length && s.cats.length === visCats(d).filter((c) => qn(c.id)).length) s.cats = [];
    SJ.render();
  };

  /* ---------------- Setup (solo + room) ---------------- */
  function pool(st) {
    let qs = SJ.QUESTIONS.filter((q) => !SJ.cat(q.cat).hidden && (st.domain === "all" || SJ.cat(q.cat).domain === st.domain));
    if (st.cats.length) qs = qs.filter((q) => st.cats.includes(q.cat));
    if (st.level !== "mixed") { const lv = { easy: 1, medium: 2, hard: 3 }[st.level]; const f = qs.filter((q) => q.lvl === lv); if (f.length) qs = f; }
    if (!qs.length && SJ.scopeCount(st)) qs = SJ.QUESTIONS.filter((q) => !SJ.cat(q.cat).hidden); /* prototype only: sample domains have no sample questions */
    return qs;
  }
  function setupView(room) {
    const st = S.setup;
    const seg = (key, opts) => `<div class="seg" role="group">${opts.map(([v, l]) => `<button data-act="set" data-k="${key}" data-v="${v}" aria-pressed="${String(st[key]) === String(v)}">${l}</button>`).join("")}</div>`;
    const avail = SJ.scopeCount(st), n = Math.min(st.count, avail);
    const p = pool(st), sample = p.slice(0, n);
    const maxPts = n * ({ easy: 20, medium: 30, hard: 40, mixed: 30 }[st.level]);
    const mins = st.timer ? Math.max(1, Math.round(n * (st.timer * 0.6 + 6) / 60)) : Math.max(1, Math.round(n * 0.4));
    const g = SJ.isGuest();
    return `<div class="page mid">
      <button class="btn btn-ghost btn-sm" data-go="play" style="margin-bottom:12px">${I("arrowL", "flip")}${t("play")}</button>
      <div class="page-head"><div><h1>${room ? X("Host a live room", "استضف غرفة مباشرة", "Créer un salon en direct") : X("Set up your game", "جهّز لعبتك", "Préparez votre partie")}</h1><p class="sub">${room ? X("Everyone in the room gets the same questions at the same time.", "يحصل كل من في الغرفة على الأسئلة نفسها في الوقت نفسه.", "Tout le salon reçoit les mêmes questions en même temps.") : X("Questions come in the app language. Your choices are remembered for next time.", "تظهر الأسئلة بلغة التطبيق، وتُحفظ اختياراتك للمرة القادمة.", "Les questions suivent la langue de l'app. Vos choix sont gardés pour la prochaine fois.")}</p></div></div>
      ${room && g ? SJ.guestTemp("room") : ""}
      <div class="cols">
        <div class="col-main" style="gap:18px">
          <section class="card pad"><div class="row" style="margin-bottom:12px"><span class="label">${X("What to play on", "على ماذا تلعب", "Sur quoi jouer")}</span><span class="spacer"></span><span class="help hide-sm">${X("Pick a whole domain or several categories", "اختر مجالًا كاملًا أو عدة فئات", "Un domaine entier ou plusieurs catégories")}</span></div>${SJ.topicPicker(st, "setup")}</section>
          <section class="card pad stack" style="gap:18px">
            <div><div class="label" style="margin-bottom:10px">${X("Difficulty", "الصعوبة", "Difficulté")}</div>${seg("level", [["easy", t("easy")], ["medium", t("medium")], ["hard", t("hard")], ["mixed", t("mixed")]])}</div>
            <div><div class="label" style="margin-bottom:10px">${X("Number of questions", "عدد الأسئلة", "Nombre de questions")}</div>${seg("count", [[5, "5"], [10, "10"], [15, "15"], [20, "20"]])}</div>
            <div><div class="label" style="margin-bottom:10px">${X("Time per question", "الوقت لكل سؤال", "Temps par question")}</div>${seg("timer", room ? [[10, "10 s"], [20, "20 s"], [30, "30 s"]] : [[10, "10 s"], [20, "20 s"], [30, "30 s"], [0, X("No timer", "بلا وقت", "Sans chrono")]])}</div>
          </section>
          ${room ? `<section class="card pad stack" style="gap:16px"><div class="form-grid"><div class="field"><label class="label" for="rm-who">${X("Who can join", "من يمكنه الانضمام", "Qui peut rejoindre")}</label>${g ? `<select class="select" id="rm-who" disabled><option>${X("Guests with the code", "الضيوف الذين لديهم الرمز", "Invités avec le code")}</option></select><span class="help">${X("Guest rooms are for guests only.", "غرف الضيوف للضيوف فقط.", "Les salons invités sont réservés aux invités.")}</span>` : `<select class="select" id="rm-who"><option>${X("Anyone with the code", "كل من لديه الرمز", "Toute personne avec le code")}</option><option>${X("Friends only", "الأصدقاء فقط", "Amis uniquement")}</option></select>`}</div>
            <div class="field"><label class="label" for="rm-max">${X("Players", "اللاعبون", "Joueurs")}</label><select class="select" id="rm-max"><option>${X("Up to 4", "حتى 4", "Jusqu'à 4")}</option><option selected>${X("Up to 8", "حتى 8", "Jusqu'à 8")}</option>${g ? "" : `<option>${X("Up to 12", "حتى 12", "Jusqu'à 12")}</option>`}</select></div></div>
            <label class="switch"><input type="checkbox" checked id="rm-chat"><span><b>${X("Room chat", "دردشة الغرفة", "Chat du salon")}</b><br><span class="help">${X("Players can send messages in the lobby", "يمكن للاعبين إرسال رسائل في الردهة", "Les joueurs peuvent écrire dans le salon")}</span></span></label></section>` : ""}
        </div>
        <aside class="col-side"><div class="card pad summary">
          <h2 style="font-size:17px;margin-bottom:8px">${X("Summary", "الملخص", "Résumé")}</h2>
          <div class="sum-line"><span>${X("Topics", "المواضيع", "Thèmes")}</span><b>${E(SJ.scopeLabel(st))}</b></div>
          <div class="sum-line"><span>${X("Difficulty", "الصعوبة", "Difficulté")}</span><b>${t(st.level)}</b></div>
          <div class="sum-line"><span>${X("Questions", "الأسئلة", "Questions")}</span><b class="num">${n}${n < st.count ? ` <span class="muted" style="font-weight:500">/ ${st.count}</span>` : ""}</b></div>
          <div class="sum-line"><span>${X("Length", "المدة", "Durée")}</span><b class="num">${X(`about ${mins} min`, `نحو ${mins} د`, `environ ${mins} min`)}</b></div>
          <div class="sum-line"><span>${X("Up to", "حتى", "Jusqu'à")}</span><b class="num">${maxPts} ${t("points")}</b></div>
          ${n < st.count ? `<p class="help" style="margin-top:8px">${X(`Only ${avail} questions for this choice, so the game has ${n}.`, `يوجد ${avail} سؤالًا فقط لهذا الاختيار، فتضم اللعبة ${n}.`, `Seulement ${avail} questions pour ce choix : la partie en aura ${n}.`)}</p>` : ""}
          <button class="btn btn-primary btn-lg btn-block" style="margin-top:14px" data-act="${room ? "create-room" : "start-solo"}" ${n && sample.length ? "" : "disabled"}>${room ? I("radio") + X("Create room", "أنشئ الغرفة", "Créer le salon") : I("tri", "flip") + t("start")}</button>
        </div></aside>
      </div></div>`;
  }
  SJ.screens["setup"] = () => setupView(false);
  SJ.screens["room-create"] = () => setupView(true);
  SJ.actions["set"] = (a) => { const k = a.dataset.k; let v = a.dataset.v; if (k === "count" || k === "timer") v = +v; S.setup[k] = v; SJ.render(); };
  SJ.actions["start-solo"] = () => SJ.startGame("solo", pool(S.setup), Object.assign({}, S.setup, { title: () => SJ.scopeLabel(S.setup) }));
  SJ.actions["create-room"] = () => SJ.createRoom(pool(S.setup));

  SJ.stTitle = (st) => (typeof st.title === "function" ? st.title() : st.title);
  SJ.startQuick = () => { const st = Object.assign({}, S.setup, { domain: S.domain, cats: [], level: "mixed", count: 5, timer: 20, title: () => t("quick") }); SJ.startGame("solo", pool(st), st); };
  SJ.startDailyMix = () => {
    const cats = SJ.CATS.filter((c) => c.domain === S.domain).map((c) => c.id);
    const qs = cats.map((c) => shuffle(SJ.QUESTIONS.filter((q) => q.cat === c))[0]).filter(Boolean);
    SJ.startGame("solo", qs, Object.assign({}, S.setup, { domain: S.domain, count: qs.length, timer: 20, title: () => X("Daily mix", "مزيج اليوم", "Mix du jour") }), true);
  };

  /* ---------------- Engine ---------------- */
  let timerId = null, botTimers = [], autoNext = null;
  SJ.stopTimers = () => { clearInterval(timerId); timerId = null; botTimers.forEach(clearTimeout); botTimers = []; clearTimeout(autoNext); autoNext = null; };

  SJ.startGame = (mode, qpool, st, keepOrder, players) => {
    const qs = (keepOrder ? qpool : shuffle(qpool)).slice(0, st.count || 10);
    S.game = { mode, qs, i: 0, st: Object.assign({}, st), timer: st.timer, left: st.timer, picked: null, revealed: false, hidden: [], log: [], score: 0, fifty: 1, skip: 1, players: players || null, started: Date.now() };
    if (mode === "solo") { SJ.go("game"); beginQuestion(); }
    else { SJ.countdown(() => { SJ.go(mode === "duel" ? "duel-game" : "live"); beginQuestion(); }); }
  };
  SJ.countdown = (cb) => {
    let n = 3;
    const box = document.createElement("div"); box.className = "countdown"; box.setAttribute("aria-live", "assertive");
    box.innerHTML = `<span>${n}</span>`; document.body.appendChild(box);
    const iv = setInterval(() => { n--; if (n === 0) { clearInterval(iv); box.remove(); cb(); } else box.innerHTML = `<span>${n}</span>`; }, 800);
  };
  function beginQuestion() {
    const g = S.game; SJ.stopTimers();
    g.picked = null; g.revealed = false; g.hidden = []; g.left = g.timer; g.qStart = Date.now();
    if (g.players) g.players.forEach((p) => { p.ans = null; p.ok = null; p.gain = 0; });
    SJ.render();
    if (g.timer) timerId = setInterval(tick, 200);
    if (g.players) scheduleBots();
  }
  function tick() {
    const g = S.game; if (!g || g.revealed) return;
    g.left = Math.max(0, g.timer - (Date.now() - g.qStart) / 1000);
    const el = document.querySelector(".timer");
    if (el) { el.style.setProperty("--p", (g.left / g.timer) * 100); el.dataset.s = Math.ceil(g.left); el.classList.toggle("low", g.left <= 5); }
    if (g.left <= 0) reveal(null);
  }
  function scheduleBots(fast) {
    const g = S.game, q = g.qs[g.i], span = (g.timer || 20) * 1000;
    g.players.filter((p) => !p.me && p.ans === null).forEach((p) => {
      const delay = fast ? 250 + Math.random() * 1100 : 1800 + Math.random() * (span * 0.75);
      botTimers.push(setTimeout(() => {
        if (g.revealed || p.ans !== null) return;
        const right = Math.random() < p.skill - (q.lvl - 1) * 0.08;
        p.ans = right ? q.c : (q.c + 1 + Math.floor(Math.random() * 3)) % 4;
        p.t = (Date.now() - g.qStart) / 1000;
        updateRail();
        if (g.picked !== null && g.players.every((x) => x.ans !== null)) reveal(g.picked, true);
      }, delay));
    });
  }
  function pick(i) {
    const g = S.game; if (!g || g.revealed || g.picked !== null) return;
    g.picked = i;
    const me = g.players && g.players.find((p) => p.me);
    if (me) { me.ans = i; me.t = (Date.now() - g.qStart) / 1000; }
    if (g.players && !g.players.every((p) => p.ans !== null)) {
      SJ.render(); botTimers.forEach(clearTimeout); botTimers = []; scheduleBots(true);
      return;
    }
    reveal(i);
  }
  function points(q, secs, timer) { const base = BASE[q.lvl]; if (!timer) return base; return base + Math.round(Math.max(0, 1 - secs / timer) * 10); }
  function reveal(i) {
    const g = S.game; if (!g || g.revealed) return;
    clearInterval(timerId); timerId = null;
    g.revealed = true;
    const q = g.qs[g.i], secs = (Date.now() - g.qStart) / 1000;
    const ok = g.picked !== null && g.picked === q.c;
    const gain = ok ? points(q, secs, g.timer) : 0;
    g.score += gain;
    g.log.push({ q, picked: g.picked, ok, gain, secs: Math.min(secs, g.timer || secs), skipped: g.picked === "skip" });
    if (g.players) g.players.forEach((p) => {
      if (p.me) { p.ok = ok; p.gain = gain; p.score += gain; return; }
      if (p.ans === null) { p.ok = false; p.gain = 0; return; }
      p.ok = p.ans === q.c; p.gain = p.ok ? points(q, p.t, g.timer) : 0; p.score += p.gain;
    });
    SJ.render();
    if (g.mode !== "solo") {
      let n = 4; const lab = () => { const el = document.getElementById("auto-next"); if (el) el.textContent = g.i + 1 < g.qs.length ? `Next question in ${n}` : `Results in ${n}`; };
      lab();
      const step = () => { n--; if (n <= 0) { nextQ(); return; } lab(); autoNext = setTimeout(step, 1000); };
      autoNext = setTimeout(step, 1000);
    }
  }
  function nextQ() {
    const g = S.game; SJ.stopTimers();
    if (g.i + 1 >= g.qs.length) { finish(); return; }
    g.i++; beginQuestion();
  }
  function finish() {
    const g = S.game; SJ.stopTimers();
    if (g.onFinish) { g.onFinish(g); return; }
    if (SJ.isGuest()) { S.guestPts = (S.guestPts || 0) + g.score; S.guestGames = (S.guestGames || 0) + 1; }
    else { SJ.ME.today = Math.min(SJ.ME.goal, SJ.ME.today + 1); }
    if (g.mode === "live") SJ.go("podium"); else SJ.go("results");
    if (g.mode !== "solo" && g.players) { const sorted = g.players.slice().sort((a, b) => b.score - a.score); if (sorted[0].me) SJ.confetti(); }
    else if (g.log.filter((l) => l.ok).length / g.log.length >= 0.8) SJ.confetti();
  }
  SJ.actions["ans"] = (a) => pick(+a.dataset.v);
  SJ.actions["next"] = () => nextQ();
  SJ.actions["fifty"] = () => {
    const g = S.game; if (!g.fifty || g.revealed) return; g.fifty = 0;
    const q = g.qs[g.i]; g.hidden = shuffle([0, 1, 2, 3].filter((x) => x !== q.c)).slice(0, 2); SJ.render();
  };
  SJ.actions["skip"] = () => { const g = S.game; if (!g.skip || g.revealed) return; g.skip = 0; g.picked = "skip"; reveal("skip"); };
    SJ.actions["quit"] = () => SJ.modal(`<div class="m-ic red">${I("door", "i-lg")}</div><h2>Leave this game?</h2><p>${S.game.mode === "solo" ? "Your answers so far won't count." : "The others keep playing. You'll be marked as left."}</p><div class="actions"><button class="btn" data-act="close">Keep playing</button><button class="btn btn-danger" data-act="quit-yes">Leave game</button></div>`);
  SJ.actions["quit-yes"] = () => { SJ.closeOverlay(); SJ.stopTimers(); SJ.go(S.game.mode === "live" ? "lobby" : "play"); };
  SJ.onKey = (e) => {
    if (!["game", "live", "duel-game"].includes(S.route) || !S.game || document.querySelector(".modal")) return;
    if (/input|textarea|select/i.test(document.activeElement.tagName)) return;
    const map = { "1": 0, "2": 1, "3": 2, "4": 3, a: 0, b: 1, c: 2, d: 3 };
    const k = e.key.toLowerCase();
    if (k in map && !S.game.revealed && !S.game.hidden.includes(map[k])) { e.preventDefault(); pick(map[k]); }
    if (e.key === "Enter" && S.game.revealed && S.game.mode === "solo") { e.preventDefault(); nextQ(); }
  };

  /* ---------------- Game screen ---------------- */
  function updateRail() { const r = document.getElementById("rail"); if (r) r.innerHTML = railInner(); const v = document.getElementById("vs"); if (v) v.outerHTML = vsBar(); }
  function stIcon(p, g) {
    if (g.revealed) return p.ok ? `<span class="st ok">${I("check", "i-sm")}</span>` : `<span class="st no">${I("x", "i-sm")}</span>`;
    return p.ans !== null ? `<span class="st done">${I("check", "i-sm")}</span>` : `<span class="st wait"><span class="thinking"><i></i><i></i><i></i></span></span>`;
  }
  function railInner() {
    const g = S.game; const ps = g.players.slice().sort((a, b) => b.score - a.score);
    return `<div class="row" style="margin-bottom:10px"><b>Players</b><span class="spacer"></span><span class="muted" style="font-size:13px">${g.players.filter((p) => p.ans !== null).length} of ${g.players.length} answered</span></div>
      <div class="player-rail">${ps.map((p, i) => `<div class="prow ${p.me ? "me" : ""}"><span class="rank ${i === 0 ? "r1" : ""}">${i + 1}</span>${p.me ? SJ.meAv("sm") : p.guest ? `<span class="av sm guest">G</span>` : SJ.av(p, "sm")}<span class="pn">${E(p.me ? "You" : p.label)}</span>${g.revealed && p.gain ? `<span class="plus">+${p.gain}</span>` : ""}<span class="ps">${p.score}</span>${stIcon(p, g)}</div>`).join("")}</div>`;
  }
  function vsBar() {
    const g = S.game; if (!g || g.mode !== "duel") return "";
    const me = g.players.find((p) => p.me), op = g.players.find((p) => !p.me);
    return `<div class="vs-bar" id="vs"><div class="vs-side">${SJ.meAv()}<div><b>You</b><div class="row" style="gap:6px">${stIcon(me, g)}</div></div><span class="spacer"></span><span class="vs-score num">${me.score}</span></div><span class="vs-mid">VS</span>
      <div class="vs-side right">${op.guest ? `<span class="av guest">G</span>` : SJ.av(op)}<div><b>${E(op.label)}</b><div class="row" style="gap:6px;justify-content:flex-end">${stIcon(op, g)}</div></div><span class="spacer"></span><span class="vs-score num">${op.score}</span></div></div>`;
  }
  function gameScreen() {
    const g = S.game;
    if (!g) return `<div class="page"><div class="empty"><h3>No game running</h3><button class="btn btn-primary" data-act="quick">${t("quick")}</button></div></div>`;
    const q = g.qs[g.i], c = SJ.cat(q.cat), ql = S.lang, L4 = letters();
    const dots = g.qs.map((_, i) => `<i class="${i < g.log.length ? (g.log[i].ok ? "ok" : "no") : i === g.i ? "now" : ""}"></i>`).join("");
    const answers = q.a[ql].map((a, i) => {
      let cls = "ans light" + (ql === "ar" ? " qtext-ar" : "");
      if (g.hidden.includes(i)) cls += " is-gone";
      if (g.revealed) cls += i === q.c ? " is-right" : i === g.picked ? " is-wrong" : " is-dim";
      else if (g.picked === i) cls += " is-dim";
      return `<button class="${cls}" data-act="ans" data-v="${i}" ${g.revealed || g.picked !== null || g.hidden.includes(i) ? "disabled" : ""} ${g.picked === i && !g.revealed ? 'aria-pressed="true" style="outline:2px solid var(--brand);outline-offset:2px"' : ""}><span class="k">${L4[i]}</span><span style="flex:1">${E(a)}</span>${g.revealed && i === q.c ? I("check") : ""}</button>`;
    }).join("");
    const last = g.log[g.log.length - 1];
    let fb = "";
    if (g.revealed) {
      const cls = last.ok ? "good" : "bad";
      const title = last.ok ? `${t("correct")} · +${last.gain} ${t("points")}` : last.skipped ? `${t("skip")} · ${t("answerIs")} ${E(q.a[ql][q.c])}` : g.picked === null ? `${t("timeup")} · ${t("answerIs")} ${E(q.a[ql][q.c])}` : `${t("wrong")} · ${t("answerIs")} ${E(q.a[ql][q.c])}`;
      fb = `<div class="feedback ${cls}" role="status"><div class="ft">${title}</div><p class="fx" lang="${ql}">${E(q.x[ql])}</p>
        <div class="src"><span>${I("book", "i-sm flip")} ${t("source")}: ${E(q.src)}</span><button class="link-btn" style="font-size:13px" data-act="report" data-v="${q.id}">${I("flag", "i-sm")} ${X("Report a problem","الإبلاغ عن مشكلة","Signaler un problème")}</button>${SJ.starRater(q.id, "inline")}
        <span class="spacer"></span>${g.mode === "solo" ? `<button class="btn btn-primary" data-act="next">${g.i + 1 < g.qs.length ? t("next") : t("finish")}${I("chevR", "flip")}</button>` : `<span class="badge blue" id="auto-next">${t("next")}</span>`}</div></div>`;
    } else if (g.picked !== null && g.players) {
      fb = `<div class="feedback" role="status"><div class="ft" style="color:var(--ink-2)">${X("Answer locked in","تم تثبيت إجابتك","Réponse verrouillée")}</div><p class="fx">Waiting for ${g.players.filter((p) => p.ans === null).length} more player${g.players.filter((p) => p.ans === null).length === 1 ? "" : "s"}…</p></div>`;
    }
    const title = g.mode === "live" ? `${t("liveRoom")} · ${S.room ? S.room.code : ""}` : g.mode === "duel" ? t("duel") : SJ.stTitle(g.st) || (SJ.dom(c.domain) ? L(SJ.dom(c.domain).name) : "");
    return `<div class="game">
      <header class="game-top"><div class="game-top-in">
        <button class="icon-btn sm" data-act="quit" aria-label="${t("quit")}">${I("x")}</button>
        <div style="min-width:0;flex:1"><div class="row" style="gap:8px;font-size:13px;color:var(--ink-3);font-weight:700;margin-bottom:6px;white-space:nowrap"><span class="hide-sm" style="overflow:hidden;text-overflow:ellipsis">${E(title)}</span><span class="spacer"></span><span class="num">${t("question")} ${g.i + 1} ${t("of")} ${g.qs.length}</span></div><div class="progress-dots">${dots}</div></div>
        <span class="score-pill num">${I("star", "i-sm")}${g.score}</span>
        <div class="nav-tools"><button class="tool" data-act="menu" data-menu="lang" aria-label="Language" aria-expanded="${S.menu === "lang"}">${I("globe")}<span>${{ en: "EN", ar: "ع", fr: "FR" }[S.lang]}</span></button>${S.menu === "lang" ? SJ.langMenu() : ""}</div>
        <div class="timer ${g.timer ? "" : "off"}" data-s="${Math.ceil(g.left)}" style="--p:${g.timer ? g.left / g.timer * 100 : 100}" role="timer" aria-label="Seconds left"></div>
      </div></header>
      <div class="game-body">
        <div class="game-main">
          ${g.mode === "duel" ? vsBar() : ""}
          <div class="q-card">
            <div class="q-meta">${SJ.catIc(c, "sm")}<span>${E(L(c.name))}</span><span>·</span><span>${SJ.lvlName(q.lvl)}</span><span>·</span><span class="num">${BASE[q.lvl]}+ ${t("points")}</span></div>
            <h1 class="q-text" lang="${ql}">${E(q.q[ql])}</h1>
            <div class="answers">${answers}</div>
            ${g.mode === "solo" && !g.revealed ? `<div class="lifelines"><button class="lifeline" data-act="fifty" ${g.fifty ? "" : "disabled"}>${I("half")}${t("fifty")}</button><button class="lifeline" data-act="skip" ${g.skip ? "" : "disabled"}>${I("skip", "flip")}${t("skip")}</button><span class="spacer"></span><span class="help hide-sm">${X("Keys","المفاتيح","Touches")} <span class="kbd">1</span>–<span class="kbd">4</span></span></div>` : ""}
          </div>
          ${fb}
        </div>
        ${g.mode === "live" ? `<aside class="game-rail card pad" id="rail">${railInner()}</aside>` : ""}
      </div></div>`;
  }
  SJ.screens["game"] = gameScreen;
  SJ.screens["live"] = gameScreen;
  SJ.screens["duel-game"] = gameScreen;

  /* ---------------- Report / comment / rate ---------------- */
  SJ.actions["report"] = (a) => {
    const q = SJ.QUESTIONS.find((x) => x.id === +a.dataset.v);
    SJ.drawer(`<div class="drawer-head"><div><h2>Report a problem</h2><p>Question #${q.id}. The content team reviews every report.</p></div><span class="spacer"></span><button class="icon-btn sm" data-act="close" aria-label="Close">${I("x")}</button></div>
      <div class="drawer-body"><div class="card pad" style="background:var(--surface-2);margin-bottom:18px"><b lang="${S.lang}">${E(q.q[S.lang])}</b></div>
      <div class="label" style="margin-bottom:10px">What's wrong?</div><div class="reasons" data-group="reason">${["The answer is wrong", "The question is unclear", "Translation problem", "Two answers are correct", "Typo", "Something else"].map((r, i) => `<button class="chip" data-act="pick-chip" aria-pressed="${i === 0}">${r}</button>`).join("")}</div>
      <div class="field" style="margin-top:18px"><label class="label" for="rp-text">Details <span class="muted" style="font-weight:500">· optional</span></label><textarea class="textarea" id="rp-text" placeholder="What should it say instead? A source helps."></textarea></div></div>
      <div class="drawer-foot"><button class="btn btn-ghost" data-act="close">Cancel</button><button class="btn btn-primary" data-act="send-report">Send report</button></div>`);
  };
  SJ.actions["pick-chip"] = (a) => { a.parentElement.querySelectorAll(".chip").forEach((c) => c.setAttribute("aria-pressed", String(c === a))); };
  SJ.actions["send-report"] = () => { SJ.closeOverlay(); SJ.toast("Report sent. Thank you, the team will check it."); };
  const REASONS = () => [X("Wrong answer","الإجابة خاطئة","Réponse fausse"), X("Unclear wording","صياغة غير واضحة","Formulation floue"), X("Translation","مشكلة ترجمة","Traduction"), X("Too hard","صعب جدًا","Trop difficile"), X("Answers too similar","خيارات متقاربة","Choix trop proches")];
  const STAR = '<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3 6.4 20.2l1.1-6.2L3 9.6l6.2-.9z"/></svg>';
  SJ.starRater = (id, compact) => {
    const r = S.ratings[id] || {}, n = r.stars || 0;
    if (compact === "inline") return `<div class="stars inline" role="radiogroup" aria-label="${X("Rate this question","قيّم هذا السؤال","Notez cette question")}" title="${X("Rate this question","قيّم هذا السؤال","Notez cette question")}">${[1, 2, 3, 4, 5].map((k) => `<button class="star ${k <= n ? "on" : ""}" role="radio" aria-checked="${k === n}" aria-label="${k} / 5" data-act="stars" data-q="${id}" data-v="${k}">${STAR}</button>`).join("")}</div>`;
    const words = ["", X("Poor","ضعيف","Mauvaise"), X("Weak","مقبول","Faible"), X("OK","جيد","Correcte"), X("Good","جيد جدًا","Bonne"), X("Excellent","ممتاز","Excellente")];
    return `<div class="rate-q ${compact ? "compact" : ""}"><span class="rq-label">${n ? words[n] : X("Rate this question","قيّم هذا السؤال","Notez cette question")}</span>
      <div class="stars" role="radiogroup" aria-label="${X("Rate this question","قيّم هذا السؤال","Notez cette question")}">${[1, 2, 3, 4, 5].map((k) => `<button class="star ${k <= n ? "on" : ""}" role="radio" aria-checked="${k === n}" aria-label="${k} / 5" data-act="stars" data-q="${id}" data-v="${k}">${STAR}</button>`).join("")}</div>
      ${n && n <= 2 ? `<div class="reasons" style="flex-basis:100%">${REASONS().map((x) => `<button class="chip" data-act="rate-reason" data-q="${id}" data-v="${E(x)}" aria-pressed="${r.reason === x}">${E(x)}</button>`).join("")}</div>` : ""}</div>`;
  };
  SJ.actions["stars"] = (a) => { const id = a.dataset.q; S.ratings[id] = { stars: +a.dataset.v, reason: null }; SJ.render(); };
  SJ.actions["rate-reason"] = (a) => { S.ratings[a.dataset.q].reason = a.dataset.v; SJ.render(); SJ.toast(X("Thanks. The content team will look at it.","شكرًا، سيطّلع عليه فريق المحتوى.","Merci, l'équipe va vérifier.")); };
  SJ.actions["comment"] = (a) => {
    const q = SJ.QUESTIONS.find((x) => x.id === +a.dataset.q);
    SJ.drawer(`<div class="drawer-head"><div><h2>Comment on this question</h2><p>Other players see comments after they answer.</p></div><span class="spacer"></span><button class="icon-btn sm" data-act="close" aria-label="Close">${I("x")}</button></div>
      <div class="drawer-body"><div class="card pad" style="background:var(--surface-2)"><b lang="${S.lang}">${E(q.q[S.lang])}</b><div class="help" style="margin-top:6px">${t("answerIs")} ${E(q.a[S.lang][q.c])}</div></div>
      <div class="stack" style="margin-top:18px">
        <div class="row" style="align-items:flex-start">${SJ.av(SJ.person("khadija"), "sm")}<div><b style="font-size:14px">Khadija M.</b> <span class="muted" style="font-size:12.5px">2 h ago</span><p style="font-size:14px">Good one. I always mixed this up.</p></div></div>
        <div class="row" style="align-items:flex-start">${SJ.av(SJ.person("youssef"), "sm")}<div><b style="font-size:14px">Youssef A.</b> <span class="muted" style="font-size:12.5px">yesterday</span><p style="font-size:14px">The explanation with the source helps a lot.</p></div></div></div>
      <div class="field" style="margin-top:20px"><label class="label" for="cm-text">Your comment</label><textarea class="textarea" id="cm-text" maxlength="400" placeholder="Share what you learned, or add a source"></textarea><span class="help">Be kind. Comments are moderated.</span></div></div>
      <div class="drawer-foot"><button class="btn btn-ghost" data-act="close">Cancel</button><button class="btn btn-primary" data-act="send-comment">Post comment</button></div>`);
  };
  SJ.actions["send-comment"] = () => { const v = (document.getElementById("cm-text") || {}).value; if (!v || !v.trim()) { SJ.toast("Write something first."); return; } SJ.closeOverlay(); SJ.toast("Comment posted."); };

  /* ---------------- Results ---------------- */
  SJ.screens["results"] = () => {
    const g = S.game;
    if (!g || !g.log.length) return `<div class="page"><div class="empty"><h3>No results yet</h3><button class="btn btn-primary" data-act="quick">${t("quick")}</button></div></div>`;
    const right = g.log.filter((l) => l.ok).length, n = g.log.length, acc = Math.round(right / n * 100);
    const avg = (g.log.reduce((s, l) => s + l.secs, 0) / n).toFixed(1);
    const xp = Math.round(g.score * 0.8) + 20;
    const duel = g.mode === "duel"; const me = duel && g.players.find((p) => p.me), op = duel && g.players.find((p) => !p.me);
    const won = duel && me.score > op.score, tie = duel && me.score === op.score;
    const msg = duel ? (won ? X("You won the duel","فزت بالمبارزة","Duel gagné") : tie ? X("A draw","تعادل","Égalité") : X(`${op.label} won this time`,`فاز ${op.label} هذه المرة`,`${op.label} gagne cette fois`)) : acc >= 90 ? X("Excellent","ممتاز","Excellent") : acc >= 70 ? X("Well done","أحسنت","Bravo") : acc >= 40 ? X("Good effort","محاولة جيدة","Bel effort") : X("Keep going","واصل","Continuez");
    const review = g.log.map((l) => {
      const q = l.q, ql = S.lang, r = S.ratings[q.id];
      return `<div class="review-q"><span class="mark ${l.skipped ? "skip" : l.ok ? "ok" : "no"}">${I(l.ok ? "check" : l.skipped ? "skip" : "x", "i-sm")}</span><div class="body">
        <div class="qq" lang="${ql}">${E(q.q[ql])}</div>
        <div style="font-size:14px">${l.ok ? `<span style="color:var(--brand-ink);font-weight:700">${E(q.a[ql][q.c])}</span>` : `${l.picked === null || l.skipped ? `<span class="muted">${l.skipped ? "Skipped" : "No answer"}</span>` : `<s class="muted">${E(q.a[ql][l.picked])}</s>`} · <span style="color:var(--brand-ink);font-weight:700">${E(q.a[ql][q.c])}</span>`}<span class="muted"> · ${SJ.esc(L(SJ.cat(q.cat).name))} · ${l.gain ? "+" + l.gain : "0"} pts</span></div>
        <div class="rate">${SJ.starRater(q.id, true)}<span class="spacer"></span><button class="btn btn-ghost btn-sm" data-act="comment" data-q="${q.id}">${I("chat", "i-sm flip")}${X("Comment","تعليق","Commenter")}</button><button class="btn btn-ghost btn-sm" data-act="report" data-v="${q.id}">${I("flag", "i-sm")}${X("Report","إبلاغ","Signaler")}</button></div>
      </div></div>`;
    }).join("");
    return `<div class="page mid" style="display:flex;flex-direction:column;gap:22px">
      <section class="card result-hero" style="border-radius:var(--r-xl)">
        ${duel ? `<div class="row" style="gap:24px;justify-content:center;flex-wrap:wrap"><div class="stack" style="align-items:center;gap:6px">${SJ.meAv("xl")}<b>You</b><span class="big-score" style="font-size:44px">${me.score}</span></div><span class="vs-mid" style="font-size:20px">VS</span><div class="stack" style="align-items:center;gap:6px">${SJ.av(op, "xl")}<b>${E(op.label)}</b><span class="big-score" style="font-size:44px">${op.score}</span></div></div>`
          : `<span class="badge ${acc >= 70 ? "green" : "gold"}">${E(SJ.stTitle(g.st) || t("results"))}</span><div class="big-score num">${right}<small> / ${n}</small></div>`}
        <h1 style="font-size:28px;font-weight:800">${msg}</h1>
        <p class="muted">${duel && SJ.isGuest() ? X("Guest duel · not rated · deleted when you leave", "مبارزة ضيوف · غير مصنّفة · تُحذف عند المغادرة", "Duel invité · non classé · supprimé à la sortie") : duel ? (won ? "+25 rating · +" + xp + " XP" : tie ? "+5 rating · +" + xp + " XP" : "−12 rating · +" + xp + " XP") : SJ.isGuest() ? "Sign up to keep these points." : `+${xp} XP · daily goal ${SJ.ME.today} of ${SJ.ME.goal}`}</p>
      </section>
      <div class="stat-row"><div class="stat gold"><div class="k">${t("points")}</div><div class="v">${g.score}</div></div><div class="stat"><div class="k">${X("Accuracy","الدقة","Précision")}</div><div class="v">${acc}%</div></div><div class="stat"><div class="k">${X("Avg. time","متوسط الوقت","Temps moyen")}</div><div class="v">${avg}s</div></div><div class="stat"><div class="k">XP</div><div class="v">+${xp}</div></div></div>
      ${SJ.isGuest() ? `<div class="banner"><span class="mode-ic gold" style="width:40px;height:40px">${I("star")}</span><div class="grow"><b>Keep your ${S.guestPts || g.score} points.</b> A free account saves them, starts your streak and puts you on the leaderboard.</div><button class="btn btn-primary" data-go="signup">${t("signup")}</button></div>` : ""}
      <div class="row wrap" style="gap:10px"><button class="btn btn-primary btn-lg" data-act="again">${I("tri", "flip")}${t("playAgain")}</button><button class="btn btn-lg" data-go="setup">${X("Change settings","غيّر الإعدادات","Modifier les réglages")}</button>
        ${SJ.signedIn() && !duel ? `<button class="btn btn-lg" data-act="challenge-these">${I("send")}Challenge a friend with these questions</button>` : ""}${duel ? `<button class="btn btn-lg" data-act="duel">${I("swords")}Find another opponent</button>` : ""}<span class="spacer"></span><button class="btn btn-ghost btn-lg" data-go="home">${t("home")}</button></div>
      <section class="card"><div class="card-head" style="padding:18px 18px 0"><h2>${X("Review your answers","راجع إجاباتك","Revoir vos réponses")}</h2><span class="help">${X("Ratings and comments go to the content team","تصل التقييمات والتعليقات إلى فريق المحتوى","Notes et commentaires vont à l'équipe")}</span></div>${review}</section>
    </div>`;
  };
  SJ.actions["again"] = () => { const g = S.game; if (g.mode === "duel") { SJ.go("duel"); return; } SJ.startGame("solo", g.qs, g.st); };
  SJ.actions["challenge-these"] = () => SJ.newChallenge(S.game.qs[0].cat, S.game.qs.length);

  /* ---------------- demo jump helper (prototype panel) ---------------- */
  SJ.demoStart = (r) => {
    if (r === "game" || r === "results") {
      const st = Object.assign({}, S.setup, { domain: "islamic", cats: [], count: 5, timer: 20, level: "mixed" });
      SJ.startGame("solo", pool(st), st);
      if (r === "results") { const g = S.game; g.qs.forEach((q, i) => { g.log.push({ q, picked: i === 2 ? (q.c + 1) % 4 : q.c, ok: i !== 2, gain: i !== 2 ? BASE[q.lvl] + 6 : 0, secs: 6 + i }); }); g.score = g.log.reduce((s, l) => s + l.gain, 0); SJ.stopTimers(); SJ.go("results"); }
    }
    if (r === "live" || r === "podium") SJ.demoRoom(r);
  };
  SJ.pool = pool;
  SJ.shuffle = shuffle;
})();
