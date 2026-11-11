/* Sirāj v2: challenges + new round (friends, room, this device), messages, friends,
   notifications, history, my questions, contact us. */
(function () {
  "use strict";
  const SJ = window.SJ, S = SJ.S, I = SJ.icon, E = SJ.esc, t = SJ.t, L = SJ.L, X = SJ.tx;
  let timers = [];
  const later = (ms, fn) => { const id = setTimeout(fn, ms); timers.push(id); return id; };
  const prevStop = SJ.stopTimers;
  SJ.stopTimers = () => { prevStop(); timers.forEach(clearTimeout); timers = []; if (S.msg) S.msg.rec = null; };
  const lvlLabel = (l) => ({ easy: t("easy"), medium: t("medium"), hard: t("hard"), mixed: t("mixed") }[l] || t("mixed"));
  const GUESTS = ["4410", "2207", "3391", "5127", "8802", "6190"];
  const isG = (id) => typeof id === "string" && id.startsWith("g:");
  const pav = (id, size, dot) => id === "me" ? SJ.meAv(size) : id === "link" ? `<span class="av ${size}" style="--c:#475569">${I("link", "i-sm")}</span>` : isG(id) ? `<span class="av ${size} guest">G</span>` : SJ.av(SJ.person(id), size, dot);
  const pnm = (id) => id === "me" ? X("You", "أنت", "Vous") : id === "link" ? X("Anyone with the link", "أي شخص لديه الرابط", "Toute personne avec le lien") : isG(id) ? X("Guest ", "ضيف ", "Invité ") + id.slice(2) : SJ.pname(SJ.person(id));

  /* =====================================================================
     CHALLENGES
     ===================================================================== */
  S.challenges = [
    { id: 1, kind: "in", who: "omar", cat: "hadith", level: "mixed", n: 10, theirs: 63, mine: null, msg: "I got 7. Your turn.", note: X("Expires in 5 h", "ينتهي بعد 5 ساعات", "Expire dans 5 h") },
    { id: 2, kind: "in", who: "khadija", cat: "ramadan", level: "easy", n: 10, theirs: 88, mine: null, msg: "Before Friday!", note: X("Expires in 2 days", "ينتهي بعد يومين", "Expire dans 2 jours") },
    { id: 3, kind: "out", who: "sarah", cat: "prophets", level: "medium", n: 5, theirs: null, mine: 47, msg: "Bonne chance", note: X("Sent yesterday", "أُرسل أمس", "Envoyé hier") },
    { id: 4, kind: "done", who: "youssef", cat: "fiqh", level: "mixed", n: 10, theirs: 61, mine: 79, msg: "", note: X("Yesterday", "أمس", "Hier") },
    { id: 5, kind: "done", who: "bilal", cat: "seerah", level: "mixed", n: 10, theirs: 42, mine: 29, msg: "Bonne chance", note: X("Friday", "الجمعة", "Vendredi") },
    { id: 6, kind: "done", who: "khadija", cat: "quran", level: "hard", n: 5, theirs: 55, mine: 55, msg: "", note: X("Last week", "الأسبوع الماضي", "La semaine dernière") },
    { id: 7, kind: "done", who: "omar", cat: "prophets", level: "easy", n: 10, theirs: 70, mine: 96, msg: "Rematch?", note: X("Last week", "الأسبوع الماضي", "La semaine dernière") },
    { id: 8, kind: "done", who: "sarah", cat: "akhlaq", level: "medium", n: 5, theirs: 38, mine: 51, msg: "", note: X("2 weeks ago", "قبل أسبوعين", "Il y a 2 semaines") },
    { id: 9, kind: "done", who: "youssef", cat: "hadith", level: "hard", n: 10, theirs: 84, mine: 66, msg: "Hadith is my thing 😄", note: X("2 weeks ago", "قبل أسبوعين", "Il y a 2 semaines") },
    { id: 10, kind: "done", who: "mariam", cat: "nahw", level: "mixed", n: 5, theirs: 30, mine: 44, msg: "", note: X("Last month", "الشهر الماضي", "Le mois dernier") }
  ];
  S.gch = [
    { id: 101, kind: "in", who: "g:4410", cat: "quran", level: "easy", n: 5, theirs: 52, mine: null, msg: "From the link I shared", note: X("Expires in 23 h", "ينتهي بعد 23 ساعة", "Expire dans 23 h") },
    { id: 102, kind: "done", who: "g:3391", cat: "prophets", level: "mixed", n: 5, theirs: 40, mine: 58, msg: "", note: X("1 hour ago", "قبل ساعة", "Il y a 1 h") }
  ];
  const CH = () => (SJ.isGuest() ? S.gch : S.challenges);
  const setCH = (list) => { if (SJ.isGuest()) S.gch = list; else S.challenges = list; };
  const res = (c) => (c.mine > c.theirs ? "won" : c.mine < c.theirs ? "lost" : "draw");
  const chCat = (c) => (c.cat ? SJ.cat(c.cat) : { icon: "shuffle", color: "#0b7a5c" });
  const chName = (c) => (c.cat ? L(SJ.cat(c.cat).name) : c.scope || X("Mixed", "متنوعة", "Mélange"));
  function chStatus(c) {
    if (c.kind === "in") return `<span class="badge gold">${E(c.note)}</span>`;
    if (c.kind === "out") return `<span class="badge">${X("Waiting", "بانتظار الرد", "En attente")}</span>`;
    return { won: `<span class="badge green">${X("You won", "فزت", "Gagné")}</span>`, lost: `<span class="badge red">${X("You lost", "خسرت", "Perdu")}</span>`, draw: `<span class="badge">${X("Draw", "تعادل", "Égalité")}</span>` }[res(c)];
  }
  SJ.challengeRow = (c) => {
    const title = c.kind === "in" ? X(`${pnm(c.who)} challenged you`, `${pnm(c.who)} يتحدّاك`, `${pnm(c.who)} vous défie`) : X(`You challenged ${pnm(c.who)}`, `تحدّيت ${pnm(c.who)}`, `Vous avez défié ${pnm(c.who)}`);
    return `<div class="li" style="flex-wrap:wrap">${pav(c.who, "", true)}<div class="grow" style="flex:1 1 200px"><div class="t">${E(title)}</div><div class="s">${E(chName(c))} · ${c.n} ${X("questions", "أسئلة", "questions")} · ${X("score to beat", "النتيجة المطلوبة", "score à battre")} ${c.theirs}</div></div>
      <div class="row" style="gap:8px"><button class="btn btn-sm" data-act="ch-decline" data-v="${c.id}">${X("Decline", "رفض", "Refuser")}</button><button class="btn btn-sm btn-primary" data-act="ch-accept" data-v="${c.id}">${X("Accept and play", "اقبل والعب", "Accepter et jouer")}</button></div></div>`;
  };
  function chCard(c) {
    const cat = chCat(c);
    const left = c.kind === "out" ? "me" : c.who, right = c.kind === "out" ? c.who : "me";
    const ls = c.kind === "out" ? c.mine : c.theirs, rs = c.kind === "out" ? c.theirs : c.mine;
    const score = (v, other) => v == null ? `<span class="vs-num muted">?</span>` : `<span class="vs-num ${other != null && v > other ? "win" : other != null && v < other ? "lose" : ""}">${v}</span>`;
    let acts = "";
    if (c.kind === "in") acts = `<div class="row" style="gap:8px"><button class="btn" style="flex:1" data-act="ch-decline" data-v="${c.id}">${X("Decline", "رفض", "Refuser")}</button><button class="btn btn-primary" style="flex:2" data-act="ch-accept" data-v="${c.id}">${I("tri", "flip")}${X("Accept and play", "اقبل والعب", "Accepter et jouer")}</button></div>`;
    if (c.kind === "out") acts = `<div class="row" style="gap:8px"><button class="btn" style="flex:1" data-act="ch-cancel" data-v="${c.id}">${X("Cancel", "إلغاء", "Annuler")}</button>${c.who === "link" ? `<button class="btn btn-soft" style="flex:1" data-act="copy" data-text="https://siraj.app/c/${c.link || "K7P2QX"}">${I("link", "i-sm")}${X("Copy link", "انسخ الرابط", "Copier le lien")}</button>` : `<button class="btn btn-soft" style="flex:1" data-act="ch-nudge" data-v="${c.id}">${I("bell", "i-sm")}${X("Remind", "ذكّر", "Relancer")}</button>`}</div>`;
    if (c.kind === "done") acts = `<button class="btn btn-soft btn-block" data-act="ch-rematch" data-v="${c.id}">${I("tri", "flip")}${X("Play again", "العب مرة أخرى", "Rejouer")}</button>`;
    return `<article class="card ch-card ${c.kind === "done" ? "r-" + res(c) : ""}"><div class="row"><span class="cat-ic sm" style="--c:${cat.color}">${I(cat.icon)}</span><span class="help" style="flex:1">${E(chName(c))} · ${lvlLabel(c.level)} · ${c.n} ${X("questions", "أسئلة", "questions")}</span>${chStatus(c)}</div>
      <div class="vs-box"><div class="vs-p">${pav(left, "")}<b>${E(pnm(left))}</b>${score(ls, rs)}</div><span class="vs-mid">vs</span><div class="vs-p">${pav(right, "")}<b>${E(pnm(right))}</b>${score(rs, ls)}</div></div>
      ${c.msg ? `<p class="ch-quote">“${E(c.msg)}”</p>` : ""}${c.kind !== "done" ? `<p class="help">${E(c.note)}</p>` : `<p class="help">${E(c.note)}</p>`}${acts}</article>`;
  }
  SJ.guestTemp = (kind) => `<div class="banner guest-temp" style="margin-bottom:18px"><span class="mode-ic gold" style="width:40px;height:40px">${I("clock")}</span><div class="grow"><b>${X("Guest mode is temporary.", "وضع الضيف مؤقت.", "Le mode invité est temporaire.")}</b> ${{ challenge: X("You can only play with other guests. Challenges and results are deleted after 24 hours or when you leave.", "تلعب مع الضيوف فقط، وتُحذف التحديات والنتائج بعد 24 ساعة أو عند مغادرتك.", "Vous jouez uniquement avec d'autres invités. Défis et résultats sont supprimés après 24 h ou à votre départ."), duel: X("You're matched with other guests only. Guest duels aren't rated and are deleted when you leave.", "تُطابَق مع ضيوف فقط، ومبارزات الضيوف غير مصنّفة وتُحذف عند مغادرتك.", "Vous affrontez uniquement des invités. Duels non classés, supprimés à votre départ."), room: X("Only guests can join your room. It closes when you leave, and nothing is saved.", "ينضم إلى غرفتك الضيوف فقط، وتُغلق عند مغادرتك ولا يُحفظ شيء.", "Seuls des invités peuvent entrer. Le salon se ferme à votre départ et rien n'est gardé.") }[kind]}</div><button class="btn btn-sm" data-go="signup">${X("Create a free account", "أنشئ حسابًا مجانيًا", "Créer un compte gratuit")}</button></div>`;
  SJ.screens["challenges"] = () => {
    const tab = S.params.tab || "in", all = CH(), g = SJ.isGuest();
    const n = (k) => all.filter((c) => c.kind === k).length;
    const done = all.filter((c) => c.kind === "done");
    const cnt = { won: done.filter((c) => res(c) === "won").length, lost: done.filter((c) => res(c) === "lost").length, draw: done.filter((c) => res(c) === "draw").length };
    const f = tab === "done" ? S.chf || null : null;
    let list = all.filter((c) => c.kind === tab); if (f) list = list.filter((c) => res(c) === f);
    const rate = done.length ? Math.round((cnt.won / done.length) * 100) : 0;
    const stat = (k, label, ic, sub) => `<button class="ch-stat ${k} ${f === k ? "on" : ""}" data-act="ch-filter" data-v="${k}" aria-pressed="${f === k}"><span class="cs-ic">${I(ic)}</span><span class="cs-t"><span class="cs-k">${label}</span><span class="cs-v num">${cnt[k]}</span></span><span class="cs-sub">${sub}</span></button>`;
    const empty = f ? X("Nothing here with this filter", "لا شيء بهذا الفلتر", "Rien avec ce filtre") : { in: X("No challenges waiting for you", "لا تحديات بانتظارك", "Aucun défi en attente"), out: X("You haven't sent a challenge yet", "لم ترسل أي تحدٍّ بعد", "Vous n'avez envoyé aucun défi"), done: X("No finished challenges yet", "لا تحديات منتهية بعد", "Aucun défi terminé") }[tab];
    const fl = { won: X("Won", "فوز", "Gagnés"), lost: X("Lost", "خسارة", "Perdus"), draw: X("Draws", "تعادل", "Égalités") };
    return `<div class="page mid"><div class="page-head"><div><h1>${t("challenges")}</h1><p class="sub">${g ? X("Same questions for every guest. Each one plays when they can, then you compare.", "الأسئلة نفسها لكل ضيف. يلعب كل واحد متى شاء، ثم تقارنون.", "Mêmes questions pour chaque invité. Chacun joue quand il peut, puis on compare.") : X("Same questions for everyone. Each player plays when they can, then you compare.", "الأسئلة نفسها للجميع. يلعب كل واحد متى شاء، ثم تقارنون النتائج.", "Mêmes questions pour tous. Chacun joue quand il peut, puis on compare.")}</p></div>
      <div class="row wrap" style="gap:8px"><button class="btn btn-soft" data-act="ch-random">${I("shuffle")}${g ? X("Random guest", "ضيف عشوائي", "Invité au hasard") : X("Random opponent", "خصم عشوائي", "Adversaire au hasard")}</button><button class="btn btn-primary" data-go="challenge-new">${I("plus")}${X("New challenge", "تحدٍّ جديد", "Nouveau défi")}</button></div></div>
      ${g ? SJ.guestTemp("challenge") : ""}
      <div class="seg full" role="tablist" style="margin-bottom:14px">${[["in", X("Received", "الواردة", "Reçus")], ["out", X("Sent", "المرسلة", "Envoyés")], ["done", X("Finished", "المنتهية", "Terminés")]].map(([k, l]) => `<button role="tab" aria-selected="${tab === k}" aria-pressed="${tab === k}" data-go="challenges" data-tab="${k}">${l} <span class="n num" style="opacity:.7">${n(k)}</span></button>`).join("")}</div>
      ${tab === "done" ? `<div class="ch-stats" role="group" aria-label="${X("Filter finished challenges", "تصفية التحديات المنتهية", "Filtrer les défis terminés")}">
        ${stat("won", fl.won, "trophy", done.length ? X(`${rate}% win rate`, `نسبة الفوز ${rate}%`, `${rate} % de victoires`) : "")}
        ${stat("lost", fl.lost, "down", X("Tap to see them", "اضغط لعرضها", "Touchez pour voir"))}
        ${stat("draw", fl.draw, "scale", X("Same score", "النتيجة نفسها", "Même score"))}</div>` : ""}
      ${f ? `<div class="filter-note">${X("Showing", "المعروض", "Affichés")}: <b>${fl[f]}</b> · ${list.length}<span class="spacer"></span><button class="link-btn" data-act="ch-filter" data-v="${f}">${I("x", "i-sm")} ${X("Show all", "عرض الكل", "Tout afficher")}</button></div>` : ""}
      ${list.length ? `<div class="ch-grid">${list.map(chCard).join("")}</div>` : `<div class="card"><div class="empty"><div class="e-ic">${I("swords")}</div><h3>${empty}</h3><p>${g ? X("Pick guests who are online, or share a link. They get the same questions.", "اختر ضيوفًا متصلين أو شارك رابطًا، وسيحصلون على الأسئلة نفسها.", "Choisissez des invités en ligne ou partagez un lien.") : X("Pick friends and a topic. They get the same questions.", "اختر أصدقاءك وموضوعًا، وسيحصلون على الأسئلة نفسها.", "Choisissez des amis et un thème : ils auront les mêmes questions.")}</p><button class="btn btn-primary" data-go="challenge-new">${X("New challenge", "تحدٍّ جديد", "Nouveau défi")}</button></div></div>`}</div>`;
  };
  SJ.actions["ch-filter"] = (a) => { const v = a.dataset.v; if ((S.params.tab || "in") !== "done") { S.chf = v; SJ.go("challenges", { tab: "done" }); return; } S.chf = S.chf === v ? null : v; SJ.render(); };
  SJ.actions["ch-accept"] = (a) => {
    const c = CH().find((x) => x.id === +a.dataset.v);
    const st = Object.assign({}, S.setup, { domain: c.cat ? SJ.cat(c.cat).domain : "all", cats: c.cat ? [c.cat] : [], count: c.n, timer: 20, level: c.level, title: () => X(`Challenge from ${pnm(c.who)}`, `تحدٍّ من ${pnm(c.who)}`, `Défi de ${pnm(c.who)}`) });
    SJ.startGame("solo", SJ.pool(st), st);
    S.game.onFinish = (g) => { c.kind = "done"; c.mine = g.score; c.note = X("Just now", "الآن", "À l'instant"); g.onFinish = null; SJ.go("results"); if (c.mine > c.theirs) SJ.confetti(); };
  };
  SJ.actions["ch-decline"] = (a) => { const c = CH().find((x) => x.id === +a.dataset.v); setCH(CH().filter((x) => x !== c)); SJ.render(); SJ.toast(X(`Declined. ${pnm(c.who)} has been told.`, `تم الرفض وأُبلغ ${pnm(c.who)}.`, `Refusé. ${pnm(c.who)} est prévenu.`)); };
  SJ.actions["ch-cancel"] = (a) => { setCH(CH().filter((x) => x.id !== +a.dataset.v)); SJ.render(); SJ.toast(X("Challenge cancelled.", "أُلغي التحدي.", "Défi annulé.")); };
  SJ.actions["ch-nudge"] = () => SJ.toast(X("Reminder sent.", "أُرسل التذكير.", "Relance envoyée."));
  SJ.actions["ch-rematch"] = (a) => { const c = CH().find((x) => x.id === +a.dataset.v); S.nr = null; SJ.go("challenge-new", { who: "friends", pick: c.who === "link" ? "" : c.who, cat: c.cat || "", n: String(c.n) }); };
  SJ.actions["ch-random"] = () => {
    let id;
    if (SJ.isGuest()) id = "g:" + GUESTS[Math.floor(Math.random() * GUESTS.length)];
    else { const pool = SJ.PEOPLE.filter((p) => p.online && !S.friends.includes(p.id)); id = (pool[Math.floor(Math.random() * pool.length)] || SJ.person("hamza")).id; }
    S.nr = null; SJ.go("challenge-new", { who: "friends", pick: id, random: "1" });
  };
  SJ.actions["ch-with"] = (a) => { S.nr = null; SJ.go("challenge-new", { who: "friends", pick: a.dataset.v }); };
  SJ.newChallenge = (cat, n, who) => { S.nr = null; SJ.go("challenge-new", { who: "friends", pick: who || "", cat: cat || "", n: n ? String(n) : "" }); };

  /* =====================================================================
     NEW CHALLENGE / ROUND  (friends or guests · my room · this device)
     ===================================================================== */
  const MIN_Q = 5;
  const linkCode = () => { const ch = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"; let s = ""; for (let i = 0; i < 6; i++) s += ch[Math.floor(Math.random() * ch.length)]; return s; };
  function nr() {
    if (S.nr) return S.nr;
    const p = S.params || {};
    const pc = p.cat && SJ.cat(p.cat) && !SJ.cat(p.cat).hidden ? p.cat : "";
    const ready = SJ.SETS.find((x) => x.qs.length >= MIN_Q);
    S.nr = { who: p.who || "friends", picked: p.pick ? [p.pick] : [], extra: p.pick ? [p.pick] : [], device: [{ id: "me", name: SJ.myName(), me: true }], mode: "ffa", src: "bank", domain: pc ? SJ.cat(pc).domain : "all", cats: pc ? [pc] : [], set: ready ? ready.id : (SJ.SETS[0] || {}).id, written: [], level: "mixed", count: p.n ? +p.n : 10, timer: 20, msg: "", q: "", link: linkCode() };
    return S.nr;
  }
  SJ.nrState = nr;
  function participants(o) {
    if (o.who === "friends") return o.picked.length + 1;
    if (o.who === "room") return S.room ? S.room.players.length : 1;
    return o.device.length;
  }
  function targetCount(o) {
    if (o.src === "sets") { let st = SJ.SETS.find((x) => x.id === o.set); if (!st || st.qs.length < MIN_Q) { st = SJ.SETS.find((x) => x.qs.length >= MIN_Q); o.set = st ? st.id : null; } return st ? st.qs.length : 0; }
    if (o.src === "write") return o.written.length >= MIN_Q ? o.written.length : 0;
    return SJ.scopeCount(o);
  }
  const LVL = (l) => ({ 1: t("easy"), 2: t("medium"), 3: t("hard") }[l] || t("medium"));
  function qrow(w, i, ns, setId) {
    const m = w.text[w.lang] || { q: "", a: [] }, c = w.cat ? SJ.cat(w.cat) : null;
    const langs = ["ar", "en", "fr"].filter((l) => w.text[l] && w.text[l].q);
    const da = `data-ns="${ns}" ${setId ? `data-set="${setId}"` : ""} data-v="${i}"`;
    return `<div class="q-row"><span class="q-num num">${i + 1}</span><button class="q-main" data-act="qe-open" ${da}><b lang="${w.lang}" dir="${w.lang === "ar" ? "rtl" : "ltr"}">${m.q ? E(m.q) : `<span class="muted">${X("Untitled question", "سؤال بلا عنوان", "Question sans titre")}</span>`}</b>
      <span class="q-meta">${c ? `<span class="q-cat" style="--c:${c.color}">${I(c.icon, "i-sm")}${E(L(c.name))}</span>` : ""}<span class="q-lvl l${w.lvl}">${LVL(w.lvl)}</span><span class="q-ok">${I("check", "i-sm")}${E(m.a[w.c] || "")}</span><span class="q-langs">${langs.map((l) => `<span>${l.toUpperCase()}</span>`).join("")}</span></span></button>
      <div class="q-acts"><button class="icon-btn sm" data-act="qe-open" ${da} aria-label="${X("Edit question", "تعديل السؤال", "Modifier")}">${I("pen", "i-sm")}</button><button class="icon-btn sm" data-act="q-dup" ${da} aria-label="${X("Duplicate", "تكرار", "Dupliquer")}">${I("copy", "i-sm")}</button><button class="icon-btn sm" data-act="q-del" ${da} aria-label="${X("Delete question", "حذف السؤال", "Supprimer")}">${I("trash", "i-sm")}</button></div></div>`;
  }
  const minBar = (n) => { const ok = n >= MIN_Q; return `<div class="set-progress ${ok ? "ok" : ""}"><div class="row"><b>${ok ? X("Ready to play", "جاهزة للعب", "Prête à jouer") : X(`${n} of ${MIN_Q} questions`, `${n} من ${MIN_Q} أسئلة`, `${n} sur ${MIN_Q} questions`)}</b><span class="spacer"></span><span class="help">${ok ? X(`${n} questions`, `${n} سؤالًا`, `${n} questions`) : X(`Add ${MIN_Q - n} more. A round needs at least ${MIN_Q}.`, `أضف ${MIN_Q - n} أخرى. تحتاج الجولة ${MIN_Q} على الأقل.`, `Encore ${MIN_Q - n}. Il en faut au moins ${MIN_Q}.`)}</span></div><div class="bar"><i style="--v:${Math.min(100, (n / MIN_Q) * 100)}%;--c:${ok ? "var(--brand)" : "var(--lamp)"}"></i></div></div>`; };
  const addQBtn = (ns, setId) => `<button class="add-q" data-act="qe-open" data-ns="${ns}" ${setId ? `data-set="${setId}"` : ""} data-v="-1"><span class="mode-ic green" style="width:40px;height:40px">${I("plus")}</span><span><b>${X("Add a question", "أضف سؤالًا", "Ajouter une question")}</b><span class="help">${X("Category, difficulty, 4 answers, optional translations", "الفئة والصعوبة و4 إجابات وترجمات اختيارية", "Catégorie, difficulté, 4 réponses, traductions facultatives")}</span></span></button>`;
  SJ.screens["challenge-new"] = () => {
    const o = nr(), np = participants(o), teamsOk = np >= 4, g = SJ.isGuest();
    if (!teamsOk && o.mode === "teams") o.mode = "ffa";
    if (g && o.src === "sets") o.src = "bank";
    const seg3 = `<div class="seg full" role="tablist">${[["friends", g ? X("Guests", "الضيوف", "Invités") : X("Friends", "الأصدقاء", "Amis"), "users"], ["room", X("My room", "غرفتي", "Mon salon"), "radio"], ["device", X("This device", "هذا الجهاز", "Cet appareil"), "phone"]].map(([k, l, ic]) => `<button role="tab" aria-pressed="${o.who === k}" data-act="nr-who" data-v="${k}">${I(ic, "i-sm")}${l}</button>`).join("")}</div>`;
    let who = "";
    if (o.who === "friends") {
      const q = o.q.toLowerCase();
      const base = g ? GUESTS.map((n) => "g:" + n) : S.friends;
      const ids = Array.from(new Set(base.concat(o.extra.filter((x) => g ? isG(x) : !isG(x)))));
      const list = ids.filter((id) => !q || pnm(id).toLowerCase().includes(q) || id.includes(q));
      const sub = (id) => isG(id) ? X("Online · guest", "متصل · ضيف", "En ligne · invité") : SJ.person(id).online ? X("Online", "متصل", "En ligne") : X("Level", "المستوى", "Niveau") + " " + SJ.person(id).level;
      who = `<p class="help" style="margin:10px 0">${g ? X("Guests online now. Pick up to 9, or share a link below.", "ضيوف متصلون الآن. اختر حتى 9، أو شارك رابطًا بالأسفل.", "Invités en ligne. Choisissez-en jusqu'à 9, ou partagez un lien.") : X("Pick 1 or more, up to 9. You are already in.", "اختر واحدًا أو أكثر حتى 9. أنت مشارك تلقائيًا.", "Choisissez 1 ou plus, jusqu'à 9. Vous êtes déjà inclus.")}</p>
        <label class="input-icon" style="display:block;margin-bottom:10px">${I("search")}<span class="sr-only">Search</span><input class="input" id="nr-q" value="${E(o.q)}" placeholder="${g ? X("Search guests", "ابحث عن ضيف", "Chercher un invité") : X("Search friends", "ابحث في أصدقائك", "Chercher un ami")}" autocomplete="off"></label>
        <div class="pick-grid">${list.map((id) => { const on = o.picked.includes(id); return `<button class="pick ${on ? "on" : ""}" data-act="nr-pick" data-v="${id}" aria-pressed="${on}">${pav(id, "", true)}<span class="pk-n">${E(pnm(id))}</span><span class="pk-s">${sub(id)}</span>${on ? `<span class="pk-ck">${I("check", "i-sm")}</span>` : ""}</button>`; }).join("") || `<p class="muted">${X("No one with that name.", "لا أحد بهذا الاسم.", "Personne de ce nom.")}</p>`}</div>
        ${g ? `<div class="share-box"><span class="mode-ic gold" style="width:40px;height:40px">${I("link")}</span><div style="flex:1;min-width:0"><b>${X("Or share a link", "أو شارك رابطًا", "Ou partagez un lien")}</b><div class="help">${X("Any guest who opens it plays the same questions. It expires in 24 hours.", "أي ضيف يفتحه يلعب الأسئلة نفسها، وينتهي بعد 24 ساعة.", "Tout invité qui l'ouvre joue les mêmes questions. Expire dans 24 h.")}</div></div><code class="num">siraj.app/c/${o.link}</code><button class="btn btn-sm" data-act="copy" data-text="https://siraj.app/c/${o.link}">${I("copy", "i-sm")}${X("Copy", "نسخ", "Copier")}</button></div>` : ""}`;
    } else if (o.who === "room") {
      who = S.room ? `<p class="help" style="margin:10px 0">${X("Everyone in your room plays together.", "يلعب كل من في غرفتك معًا.", "Tout votre salon joue ensemble.")} ${X("Code", "الرمز", "Code")} <b class="num">${S.room.code}</b></p><div class="pick-grid">${S.room.players.map((p) => `<div class="pick on">${p.me ? SJ.meAv() : p.guest ? `<span class="av guest">G</span>` : SJ.av(SJ.person(p.id) || p)}<span class="pk-n">${E(p.me ? X("You", "أنت", "Vous") : p.label)}</span><span class="pk-s">${p.ready ? t("ready") : X("Not ready", "غير جاهز", "Pas prêt")}</span></div>`).join("")}</div>`
        : `<div class="empty" style="padding:22px 10px"><p>${X("You're not in a room right now.", "لست في غرفة الآن.", "Vous n'êtes dans aucun salon.")}</p><div class="row" style="justify-content:center"><button class="btn btn-sm btn-primary" data-act="room-create">${X("Host a room", "استضف غرفة", "Créer un salon")}</button><button class="btn btn-sm" data-go="room-join">${t("join")}</button></div></div>`;
    } else {
      who = `<div class="device-box"><h3>${X("Playing on this device", "اللعب على هذا الجهاز", "Jouer sur cet appareil")}</h3><p class="help">${X("Pass the device. Everyone plays their own round with the same questions.", "مرّروا الجهاز. يلعب كل واحد جولته بالأسئلة نفسها.", "Passez l'appareil : chacun joue sa manche avec les mêmes questions.")}</p>
        <div class="row wrap" style="gap:8px;margin:12px 0">${o.device.map((d, i) => `<span class="dev-chip">${d.me ? SJ.meAv("sm") : `<span class="av sm" style="--c:${["#3365a8", "#0f766e", "#8a4fbf", "#c0612b", "#be185d", "#4f46e5", "#a16207", "#475569", "#b4372c"][i % 9]}">${E(d.name.charAt(0).toUpperCase())}</span>`}<b>${E(d.name)}</b>${d.me ? "" : `<button class="x" data-act="nr-dev-del" data-v="${i}" aria-label="Remove ${E(d.name)}">${I("x", "i-sm")}</button>`}</span>`).join("")}</div>
        <form class="row" data-form="nr-dev" style="gap:8px"><label class="sr-only" for="nr-dev-in">Player name</label><input class="input" id="nr-dev-in" maxlength="20" placeholder="${X("Name", "الاسم", "Nom")}" autocomplete="off" ${o.device.length >= 10 ? "disabled" : ""}><button class="btn btn-soft" type="submit">${X("Add", "إضافة", "Ajouter")}</button></form></div>`;
    }
    let src = "";
    if (o.src === "bank") src = `<div class="row" style="margin:18px 0 12px"><span class="label">${X("What to play on", "على ماذا تلعبون", "Sur quoi jouer")}</span><span class="spacer"></span><span class="help hide-sm">${X("A whole domain or several categories", "مجال كامل أو عدة فئات", "Un domaine entier ou plusieurs catégories")}</span></div>${SJ.topicPicker(o, "nr")}`;
    if (o.src === "sets") { const ok = SJ.SETS.filter((st) => st.qs.length >= MIN_Q), drafts = SJ.SETS.length - ok.length;
      src = `<div class="stack" style="margin-top:14px;gap:8px">${ok.map((st) => `<button class="pick row-pick ${o.set === st.id ? "on" : ""}" data-act="nr-set" data-v="${st.id}" aria-pressed="${o.set === st.id}"><span class="cat-ic sm" style="--c:#a16207">${I("pen")}</span><span style="flex:1;text-align:start"><b lang="${st.lang}">${E(st.name)}</b><span class="help" style="display:block">${st.qs.length} ${X("questions", "أسئلة", "questions")} · ${X("played", "لُعبت", "jouée")} ${st.plays}×</span></span>${o.set === st.id ? I("check") : ""}</button>`).join("") || `<div class="empty" style="padding:22px 10px"><p>${X(`You have no set with ${MIN_Q} questions yet.`, `ليست لديك مجموعة فيها ${MIN_Q} أسئلة بعد.`, `Aucune série de ${MIN_Q} questions pour l'instant.`)}</p></div>`}
        <div class="row" style="gap:10px;flex-wrap:wrap">${drafts ? `<span class="help">${X(`${drafts} draft${drafts === 1 ? "" : "s"} with fewer than ${MIN_Q} questions not shown.`, `${drafts} مسودات بأقل من ${MIN_Q} أسئلة غير معروضة.`, `${drafts} brouillon(s) de moins de ${MIN_Q} questions masqué(s).`)}</span>` : ""}<button class="link-btn" data-go="my-questions">${drafts ? X("Finish them in My questions", "أكملها في «أسئلتي»", "Les terminer dans Mes questions") : X("Manage my sets", "إدارة مجموعاتي", "Gérer mes séries")}</button></div></div>`; }
    if (o.src === "write") src = `<div class="stack" style="margin-top:14px;gap:12px">${minBar(o.written.length)}${o.written.length ? `<div class="card q-list">${o.written.map((w, i) => qrow(w, i, "nr")).join("")}</div>` : ""}${addQBtn("nr")}${o.written.length && !g ? `<button class="link-btn" data-act="nr-save-set" style="width:fit-content">${I("plus", "i-sm")} ${X("Save these as a set", "احفظها كمجموعة", "Enregistrer comme série")}</button>` : ""}</div>`;
    const avail = targetCount(o), qn = o.src === "bank" ? Math.min(o.count, avail) : avail;
    let startLabel, can = qn > 0, why = "";
    if (o.who === "friends") {
      const k = o.picked.length;
      startLabel = g && !k ? X("Play first, then share the link", "العب أولًا ثم شارك الرابط", "Jouer puis partager le lien") : X(`Play first, then send to ${k || "…"}`, `العب أولًا ثم أرسل إلى ${k || "…"}`, `Jouer puis envoyer à ${k || "…"}`);
      if (!k && !g) { can = false; why = X("Pick at least one friend.", "اختر صديقًا واحدًا على الأقل.", "Choisissez au moins un ami."); }
    } else if (o.who === "room") { startLabel = X("Start in my room", "ابدأ في غرفتي", "Lancer dans mon salon"); if (!S.room) { can = false; why = X("Host or join a room first.", "استضف غرفة أو انضم إليها أولًا.", "Créez ou rejoignez un salon."); } }
    else { startLabel = X(`Start round · ${o.device.length} players`, `ابدأ الجولة · ${o.device.length} لاعبين`, `Lancer la manche · ${o.device.length} joueurs`); if (o.device.length < 2) { can = false; why = X("Add at least one more player.", "أضف لاعبًا آخر على الأقل.", "Ajoutez au moins un joueur."); } }
    if (!qn) why = o.src === "write" ? X(`Write at least ${MIN_Q} questions.`, `اكتب ${MIN_Q} أسئلة على الأقل.`, `Écrivez au moins ${MIN_Q} questions.`) : o.src === "sets" ? X(`Pick a set with at least ${MIN_Q} questions.`, `اختر مجموعة فيها ${MIN_Q} أسئلة على الأقل.`, `Choisissez une série d'au moins ${MIN_Q} questions.`) : X("Pick something to play on.", "اختر ما تلعبون عليه.", "Choisissez un thème.");
    const seg = (act, cur, opts) => `<div class="seg">${opts.map(([v, l]) => `<button data-act="${act}" data-v="${v}" aria-pressed="${String(cur) === String(v)}">${l}</button>`).join("")}</div>`;
    const teams = o.mode === "teams" ? splitTeams(o) : null;
    const srcs = [["bank", X("The bank", "بنك الأسئلة", "La banque"), "book"]].concat(g ? [] : [["sets", X("One of my sets", "إحدى مجموعاتي", "Une de mes séries"), "pen"]]).concat([["write", X("Write them now", "اكتبها الآن", "Les écrire maintenant"), "plus"]]);
    const srcLabel = o.src === "bank" ? E(SJ.scopeLabel(o)) : o.src === "sets" ? E((SJ.SETS.find((x) => x.id === o.set) || {}).name || "") : X("Written now", "مكتوبة الآن", "Écrites maintenant");
    return `<div class="page mid"><button class="btn btn-ghost btn-sm" data-go="challenges" style="margin-bottom:12px">${I("arrowL", "flip")}${t("challenges")}</button>
      <div class="page-head"><div><h1>${X("Set up your round", "جهّز جولتك", "Préparez votre manche")}</h1><p class="sub">${g ? X("Challenge other guests, play with your room, or pass one phone around.", "تحدَّ ضيوفًا آخرين، أو العب مع غرفتك، أو مرّروا هاتفًا واحدًا.", "Défiez d'autres invités, jouez avec votre salon ou faites tourner un téléphone.") : X("Challenge friends, play with your room, or pass one phone around.", "تحدَّ أصدقاءك، أو العب مع غرفتك، أو مرّروا هاتفًا واحدًا بينكم.", "Défiez des amis, jouez avec votre salon ou faites tourner un téléphone.")}</p></div></div>
      ${g ? SJ.guestTemp("challenge") : ""}
      <div class="cols"><div class="col-main" style="gap:18px">
        <section class="card pad"><div class="row" style="margin-bottom:10px"><span class="label">${X("Who is playing", "من يلعب", "Qui joue")}</span><span class="spacer"></span><span class="help num">${np} ${X("players", "لاعبين", "joueurs")}</span></div>${seg3}${who}</section>
        <section class="card pad"><div class="label" style="margin-bottom:10px">${X("How to play", "طريقة اللعب", "Mode de jeu")}</div>
          <div class="mode-pick"><button class="mp ${o.mode === "ffa" ? "on" : ""}" data-act="nr-mode" data-v="ffa" aria-pressed="${o.mode === "ffa"}">${I("swords")}<span><b>${X("Everyone for themselves", "كل لاعب لنفسه", "Chacun pour soi")}</b><span class="help">${X("Highest score wins", "أعلى نتيجة تفوز", "Le meilleur score gagne")}</span></span></button>
          <button class="mp ${o.mode === "teams" ? "on" : ""}" data-act="nr-mode" data-v="teams" aria-pressed="${o.mode === "teams"}" ${teamsOk ? "" : "disabled"}>${I("shieldT")}<span><b>${X("Teams", "فرق", "Équipes")}</b><span class="help">${teamsOk ? X("Two teams, points add up", "فريقان وتُجمع النقاط", "Deux équipes, points cumulés") : X("Pick at least 4 players", "اختر 4 لاعبين على الأقل", "Au moins 4 joueurs")}</span></span></button></div>
          ${teams ? `<div class="teams">${teams.map((tm, k) => `<div class="team ${k ? "b" : "a"}"><b>${X("Team", "فريق", "Équipe")} ${"AB"[k]}</b><div class="row wrap" style="gap:6px;margin-top:8px">${tm.map((nm) => `<span class="badge outline">${E(nm)}</span>`).join("")}</div></div>`).join("")}<button class="btn btn-sm btn-ghost" data-act="nr-shuffle">${I("shuffle", "i-sm")}${X("Shuffle teams", "أعد توزيع الفرق", "Mélanger")}</button></div>` : ""}</section>
        <section class="card pad"><div class="label" style="margin-bottom:4px">${X("Questions from", "مصدر الأسئلة", "Questions de")}</div><p class="help" style="margin-bottom:10px">${X(`A round uses one source. Your own questions need at least ${MIN_Q}.`, `تستخدم الجولة مصدرًا واحدًا، وتحتاج أسئلتك ${MIN_Q} على الأقل.`, `Une manche utilise une source. Vos questions : au moins ${MIN_Q}.`)}</p>
          <div class="mode-pick ${srcs.length === 3 ? "three" : "two-c"}">${srcs.map(([k, l, ic]) => `<button class="mp ${o.src === k ? "on" : ""}" data-act="nr-src" data-v="${k}" aria-pressed="${o.src === k}">${I(ic)}<b>${l}</b></button>`).join("")}</div>${src}</section>
        <section class="card pad stack" style="gap:16px">
          ${o.src === "bank" ? `<div><div class="label" style="margin-bottom:8px">${X("Difficulty", "الصعوبة", "Difficulté")}</div>${seg("nr-level", o.level, [["easy", t("easy")], ["medium", t("medium")], ["hard", t("hard")], ["mixed", t("mixed")]])}</div>
          <div><div class="label" style="margin-bottom:8px">${X("Number of questions", "عدد الأسئلة", "Nombre de questions")}</div>${seg("nr-count", o.count, [[5, "5"], [10, "10"], [15, "15"]])}</div>` : ""}
          <div><div class="label" style="margin-bottom:8px">${X("Time per question", "الوقت لكل سؤال", "Temps par question")}</div>${seg("nr-timer", o.timer, [[10, "10 s"], [20, "20 s"], [30, "30 s"]])}</div>
          ${o.who === "friends" ? `<div class="form-grid"><div class="field"><label class="label" for="nr-msg">${X("Message", "رسالة", "Message")} <span class="muted" style="font-weight:500">· ${X("optional", "اختياري", "facultatif")}</span></label><input class="input" id="nr-msg" maxlength="80" value="${E(o.msg)}" placeholder="Bonne chance"></div>
            <div class="field"><label class="label" for="nr-dl">${X("They have", "المهلة", "Délai")}</label>${g ? `<select class="select" id="nr-dl" disabled><option>${X("24 hours", "24 ساعة", "24 heures")}</option></select><span class="help">${X("Guest challenges always end after 24 hours.", "تحديات الضيوف تنتهي دائمًا بعد 24 ساعة.", "Les défis invités durent toujours 24 h.")}</span>` : `<select class="select" id="nr-dl"><option>${X("24 hours", "24 ساعة", "24 heures")}</option><option selected>${X("3 days", "3 أيام", "3 jours")}</option><option>${X("1 week", "أسبوع", "1 semaine")}</option></select>`}</div></div>` : ""}
        </section>
      </div>
      <aside class="col-side"><div class="card pad summary">
        <h2 style="font-size:17px;margin-bottom:8px">${X("Summary", "الملخص", "Résumé")}</h2>
        <div class="sum-line"><span>${X("Players", "اللاعبون", "Joueurs")}</span><b class="num">${np}${g && o.who === "friends" ? "+" : ""}</b></div>
        <div class="sum-line"><span>${X("Mode", "الطريقة", "Mode")}</span><b>${o.mode === "teams" ? X("Teams", "فرق", "Équipes") : X("Everyone for themselves", "كل لاعب لنفسه", "Chacun pour soi")}</b></div>
        <div class="sum-line"><span>${X("Questions", "الأسئلة", "Questions")}</span><b class="num">${qn}</b></div>
        <div class="sum-line"><span>${X("Source", "المصدر", "Source")}</span><b>${srcLabel}</b></div>
        ${why ? `<p class="help" style="margin-top:10px;color:var(--warn-ink)">${why}</p>` : ""}
        <button class="btn btn-primary btn-lg btn-block" style="margin-top:14px" data-act="nr-start" ${can ? "" : "disabled"}>${I("tri", "flip")}${startLabel}</button></div></aside></div></div>`;
  };
  function splitTeams(o) {
    let names;
    if (o.who === "friends") names = [SJ.myName()].concat(o.picked.map((id) => pnm(id)));
    else if (o.who === "room") names = S.room ? S.room.players.map((p) => (p.me ? SJ.myName() : p.label)) : [];
    else names = o.device.map((d) => d.name);
    if (!o.order || o.order.length !== names.length) o.order = names.map((_, i) => i);
    const ord = o.order.map((i) => names[i]).filter(Boolean);
    return [ord.filter((_, i) => i % 2 === 0), ord.filter((_, i) => i % 2 === 1)];
  }
  const NR = (fn) => (a) => { fn(nr(), a); SJ.render(); };
  SJ.actions["nr-who"] = NR((o, a) => { o.who = a.dataset.v; o.order = null; });
  SJ.actions["nr-pick"] = NR((o, a) => { const v = a.dataset.v; if (o.picked.includes(v)) o.picked = o.picked.filter((x) => x !== v); else if (o.picked.length < 9) o.picked.push(v); else SJ.toast(X("Up to 9 players.", "حتى 9 لاعبين.", "Jusqu'à 9 joueurs.")); o.order = null; });
  SJ.actions["nr-mode"] = NR((o, a) => { o.mode = a.dataset.v; });
  SJ.actions["nr-shuffle"] = NR((o) => { o.order = SJ.shuffle(o.order || []); });
  SJ.actions["nr-src"] = NR((o, a) => { o.src = a.dataset.v; });
  SJ.actions["nr-set"] = NR((o, a) => { o.set = a.dataset.v; });
  SJ.actions["nr-level"] = NR((o, a) => { o.level = a.dataset.v; });
  SJ.actions["nr-count"] = NR((o, a) => { o.count = +a.dataset.v; });
  SJ.actions["nr-timer"] = NR((o, a) => { o.timer = +a.dataset.v; });
  SJ.actions["nr-dev-del"] = NR((o, a) => { o.device.splice(+a.dataset.v, 1); o.order = null; });
  SJ.actions["nr-save-set"] = () => { const o = nr(); SJ.SETS.unshift({ id: "s" + Date.now(), name: X("Written round", "جولة مكتوبة", "Manche écrite"), lang: S.lang, plays: 0, updated: X("Just now", "الآن", "À l'instant"), qs: JSON.parse(JSON.stringify(o.written)) }); SJ.toast(X("Saved to My questions.", "حُفظت في «أسئلتي».", "Enregistrée dans Mes questions.")); };
  document.addEventListener("input", (e) => {
    if (e.target.id === "nr-q") { nr().q = e.target.value; const pos = e.target.selectionStart; SJ.render(); const n = document.getElementById("nr-q"); if (n) { n.focus(); n.setSelectionRange(pos, pos); } }
    if (e.target.id === "nr-msg") nr().msg = e.target.value;
  });
  SJ.forms["nr-dev"] = () => { const i = document.getElementById("nr-dev-in"), v = i.value.trim(); if (!v) return; const o = nr(); if (o.device.some((d) => d.name.toLowerCase() === v.toLowerCase())) { SJ.toast(X("That name is already playing.", "هذا الاسم موجود.", "Ce nom joue déjà.")); return; } o.device.push({ id: "d" + Date.now(), name: v }); o.order = null; SJ.render(); const n = document.getElementById("nr-dev-in"); if (n) n.focus(); };
  const DEF_X = { en: "Answer from the question's author.", ar: "إجابة كاتب السؤال.", fr: "Réponse de l'auteur de la question." };
  function customQs(list, setName) {
    return list.map((w, i) => {
      const m = w.text[w.lang];
      const pick = (l) => { const tx = w.text[l]; return tx && tx.q && tx.a.every((x) => x) ? tx : m; };
      const o = { id: 9000 + i, cat: w.cat || "custom", lvl: w.lvl || 2, c: w.c, src: X("Written by ", "كتبها ", "Écrite par ") + SJ.myName() + (setName ? " · " + setName : ""), q: {}, a: {}, x: {} };
      ["en", "ar", "fr"].forEach((l) => { const tx = pick(l); o.q[l] = tx.q; o.a[l] = tx.a; o.x[l] = tx.x || DEF_X[l]; });
      return o;
    });
  }
  function roundQuestions(o) {
    if (o.src === "sets") { const st = SJ.SETS.find((x) => x.id === o.set); st.plays++; return { qs: customQs(st.qs, st.name), domain: S.domain }; }
    if (o.src === "write") return { qs: customQs(o.written), domain: S.domain };
    const pool = SJ.pool({ domain: o.domain, cats: o.cats, level: o.level });
    return { qs: SJ.shuffle(pool).slice(0, o.count), domain: o.domain === "all" ? S.domain : o.domain };
  }
  SJ.actions["nr-start"] = () => {
    const o = nr(); const { qs, domain } = roundQuestions(o);
    const st = Object.assign({}, S.setup, { domain, cats: [], count: qs.length, timer: o.timer, level: o.level });
    if (o.who === "friends") {
      const to = o.picked.length ? o.picked : ["link"];
      const scope = o.src === "bank" ? SJ.scopeLabel(o) : o.src === "sets" ? (SJ.SETS.find((x) => x.id === o.set) || {}).name : X("Written questions", "أسئلة مكتوبة", "Questions écrites");
      const sent = to.map((id) => ({ id: Date.now() + Math.random(), kind: "out", who: id, cat: o.src === "bank" && o.cats.length === 1 ? o.cats[0] : null, scope, level: o.level, n: qs.length, theirs: null, mine: null, msg: o.msg, link: o.link, note: SJ.isGuest() ? X("Expires in 24 h", "ينتهي بعد 24 ساعة", "Expire dans 24 h") : X("Sent just now", "أُرسل الآن", "Envoyé à l'instant") }));
      setCH(sent.concat(CH()));
      st.title = () => X("Your challenge round", "جولة التحدي", "Votre manche de défi");
      SJ.startGame("solo", qs, st, true);
      S.game.onFinish = (g) => { sent.forEach((c) => { c.mine = g.score; }); g.onFinish = null; SJ.go("results"); SJ.toast(o.picked.length ? X(`Sent to ${o.picked.length}. They need to beat ${g.score}.`, `أُرسل إلى ${o.picked.length}. عليهم تجاوز ${g.score}.`, `Envoyé à ${o.picked.length}. Ils doivent battre ${g.score}.`) : X(`Your link is ready in Challenges › Sent. Score to beat: ${g.score}.`, `رابطك جاهز في التحديات › المرسلة. النتيجة المطلوبة: ${g.score}.`, `Votre lien est prêt dans Défis › Envoyés. Score à battre : ${g.score}.`)); };
      S.nr = null; return;
    }
    if (o.who === "room") {
      const players = S.room.players; players.forEach((p) => { p.score = 0; });
      SJ.startGame("live", qs, Object.assign(st, { title: () => t("liveRoom") }), true, players);
      if (o.mode === "teams") { const tm = splitTeams(o); S.game.teams = tm.map((names) => names.map((nm) => (players.find((p) => (p.me ? SJ.myName() : p.label) === nm) || {}).id)); }
      return;
    }
    S.dev = { players: o.device.map((d) => ({ id: d.id, label: d.name, me: !!d.me, score: 0, c: "#3365a8" })), qs, st, idx: 0, teams: o.mode === "teams" ? splitTeams(o) : null, log: null };
    devTurn();
  };

  /* ---------------- question editor (sets + written rounds) ---------------- */
  function qlist(ns, setId) { return ns === "nr" ? nr().written : (SJ.SETS.find((x) => x.id === setId) || { qs: [] }).qs; }
  function blankQ(lang, cat, lvl) { return { lang, cat: cat || "", lvl: lvl || 2, c: 0, text: { [lang]: { q: "", a: ["", "", "", ""], x: "" } } }; }
  const LANGS = [["en", "English"], ["ar", "العربية"], ["fr", "Français"]];
  function qeHtml() {
    const q = S.qd, d = q.d, tab = q.tab;
    const T = d.text[tab] || (d.text[tab] = { q: "", a: ["", "", "", ""], x: "" });
    const list = qlist(q.ns, q.setId), setName = q.ns === "nr" ? X("Written for this round", "مكتوب لهذه الجولة", "Écrite pour cette manche") : (SJ.SETS.find((x) => x.id === q.setId) || {}).name;
    const dom = d.cat ? SJ.cat(d.cat).domain : q.dom || SJ.myDoms()[0].id, dir = tab === "ar" ? "rtl" : "ltr";
    const filled = (l) => d.text[l] && d.text[l].q.trim();
    const opt = `<span class="muted" style="font-weight:500">(${X("optional", "اختياري", "facultatif")})</span>`;
    return `<div class="drawer-head"><div><h2>${q.idx >= 0 ? X("Edit question", "تعديل السؤال", "Modifier la question") : X("Add a question", "إضافة سؤال", "Ajouter une question")}</h2><p>${X("Question", "السؤال", "Question")} ${q.idx >= 0 ? q.idx + 1 : list.length + 1} · ${E(setName)}</p></div><span class="spacer"></span><button class="icon-btn sm" data-act="close" aria-label="Close">${I("x")}</button></div>
      <div class="drawer-body qe">
        <div class="form-grid"><div class="field"><label class="label" for="qe-dom">${X("Domain", "المجال", "Domaine")}</label><select class="select" id="qe-dom" data-qe="dom">${SJ.myDoms().concat(SJ.isMine(dom) || !SJ.dom(dom) ? [] : [SJ.dom(dom)]).map((x) => `<option value="${x.id}" ${x.id === dom ? "selected" : ""}>${E(L(x.name))}</option>`).join("")}</select></div>
          <div class="field"><label class="label" for="qe-cat">${X("Category", "الفئة", "Catégorie")}</label><select class="select" id="qe-cat" data-qe="cat"><option value="">${X("Choose…", "اختر…", "Choisir…")}</option>${SJ.CATS.filter((c) => c.domain === dom && !c.hidden).map((c) => `<option value="${c.id}" ${d.cat === c.id ? "selected" : ""}>${E(L(c.name))}</option>`).join("")}</select></div></div>
        <div class="form-grid" style="margin-top:16px"><div class="field"><span class="label">${X("Difficulty", "الصعوبة", "Difficulté")}</span><div class="seg full">${[1, 2, 3].map((l) => `<button type="button" data-act="qe-lvl" data-v="${l}" aria-pressed="${d.lvl === l}">${LVL(l)}</button>`).join("")}</div></div>
          <div class="field"><span class="label">${X("Points", "النقاط", "Points")}</span><div class="pts-box num">${{ 1: 10, 2: 20, 3: 30 }[d.lvl]}</div><span class="help">${X("Set by difficulty, plus up to 10 for speed", "تحدّدها الصعوبة، مع حتى 10 للسرعة", "Selon la difficulté, plus jusqu'à 10 pour la vitesse")}</span></div></div>
        <div class="qe-tabs" role="tablist">${LANGS.map(([l, nm]) => `<button type="button" role="tab" aria-selected="${tab === l}" data-act="qe-tab" data-v="${l}"><span lang="${l}">${nm}</span>${l === d.lang ? `<span class="qe-tag main">${X("main", "أساسية", "principale")}</span>` : filled(l) ? `<span class="qe-dot" title="${X("Translated", "مترجم", "Traduit")}"></span>` : `<span class="qe-tag">${X("optional", "اختياري", "facultatif")}</span>`}</button>`).join("")}</div>
        ${tab !== d.lang ? `<p class="help qe-note">${I("info", "i-sm")} ${X("Optional translation. Players with the app in this language see it; others see the main language. Keep the answers in the same order.", "ترجمة اختيارية يراها من يستخدم التطبيق بهذه اللغة، ويرى الباقون اللغة الأساسية. حافظ على ترتيب الإجابات.", "Traduction facultative, vue par ceux qui utilisent l'app dans cette langue. Gardez l'ordre des réponses.")}</p>` : `<div class="row qe-main"><span class="help">${X("Main language", "اللغة الأساسية", "Langue principale")}</span><span class="spacer"></span><button type="button" class="link-btn" style="font-size:13px" data-act="qe-main">${X("Make another tab the main one", "اجعل لغة أخرى أساسية", "Changer la langue principale")}</button></div>`}
        <div class="field"><div class="row"><label class="label" for="qe-q">${X("Question", "السؤال", "Question")}</label><span class="spacer"></span><span class="help num" id="qe-qc">${T.q.length} / 160</span></div><textarea class="textarea" id="qe-q" data-qe="q" maxlength="160" rows="3" lang="${tab}" dir="${dir}" placeholder="${X("Which surah does not begin with the Basmala?", "أي سورة لا تبدأ بالبسملة؟", "Quelle sourate ne commence pas par la Basmala ?")}">${E(T.q)}</textarea></div>
        <div class="label" style="margin:16px 0 8px">${X("Answers", "الإجابات", "Réponses")} <span class="muted" style="font-weight:500">· ${X("select the correct one", "حدّد الإجابة الصحيحة", "cochez la bonne")}</span></div>
        <div class="stack" style="gap:8px">${[0, 1, 2, 3].map((k) => `<div class="qe-ans ${d.c === k ? "ok" : ""}"><input type="radio" name="qe-c" ${d.c === k ? "checked" : ""} data-act="qe-c" data-v="${k}" aria-label="${X("Correct answer", "الإجابة الصحيحة", "Bonne réponse")} ${k + 1}"><input class="input" data-qe="a${k}" value="${E(T.a[k] || "")}" maxlength="60" aria-label="${X("Answer", "الإجابة", "Réponse")} ${k + 1}" lang="${tab}" dir="${dir}"></div>`).join("")}</div>
        <div class="field" style="margin-top:16px"><label class="label" for="qe-x">${X("Explanation", "الشرح", "Explication")} ${opt}</label><textarea class="textarea" id="qe-x" data-qe="x" rows="3" maxlength="300" lang="${tab}" dir="${dir}" placeholder="${X("Shown after the answer. Add a source if you can.", "يظهر بعد الإجابة. أضف مصدرًا إن أمكن.", "Affichée après la réponse. Ajoutez une source si possible.")}">${E(T.x || "")}</textarea></div>
        ${q.err ? `<p class="err" role="alert" style="margin-top:12px">${q.err}</p>` : ""}
      </div>
      <div class="drawer-foot"><button class="btn" data-act="close">${X("Cancel", "إلغاء", "Annuler")}</button>${q.idx < 0 ? `<button class="btn" data-act="qe-save" data-more="1">${X("Save and add another", "احفظ وأضف آخر", "Enregistrer et continuer")}</button>` : ""}<button class="btn btn-primary" data-act="qe-save">${X("Save question", "احفظ السؤال", "Enregistrer")}</button></div>`;
  }
  function qeRedraw() { const el = document.querySelector(".drawer"); if (!el) return; const b = el.querySelector(".drawer-body"), top = b ? b.scrollTop : 0; el.innerHTML = qeHtml(); const nb = el.querySelector(".drawer-body"); if (nb) nb.scrollTop = top; }
  SJ.actions["qe-open"] = (a) => {
    const ns = a.dataset.ns, setId = a.dataset.set || null, idx = +a.dataset.v, list = qlist(ns, setId);
    const main = ns === "nr" ? S.lang : (SJ.SETS.find((x) => x.id === setId) || {}).lang || S.lang;
    const last = list[list.length - 1];
    const src = idx >= 0 ? list[idx] : blankQ(main, last ? last.cat : "", last ? last.lvl : 2);
    S.qd = { ns, setId, idx, tab: src.lang, d: JSON.parse(JSON.stringify(src)), err: "" };
    SJ.drawer(qeHtml());
  };
  SJ.actions["qe-tab"] = (a) => { S.qd.tab = a.dataset.v; S.qd.err = ""; qeRedraw(); };
  SJ.actions["qe-lvl"] = (a) => { S.qd.d.lvl = +a.dataset.v; qeRedraw(); };
  SJ.actions["qe-c"] = (a) => { S.qd.d.c = +a.dataset.v; qeRedraw(); };
  SJ.actions["qe-main"] = () => { const d = S.qd.d, other = LANGS.map((x) => x[0]).find((l) => l !== d.lang && d.text[l] && d.text[l].q.trim()); if (!other) { SJ.toast(X("Fill another language tab first.", "املأ لغة أخرى أولًا.", "Remplissez d'abord un autre onglet.")); return; } d.lang = other; S.qd.tab = other; qeRedraw(); };
  const qeErr = (msg) => { S.qd.err = msg; qeRedraw(); const e = document.querySelector(".drawer .err"); if (e) e.scrollIntoView({ block: "nearest", behavior: "smooth" }); };
  SJ.actions["qe-save"] = (a) => {
    const q = S.qd, d = q.d, m = d.text[d.lang];
    if (!d.cat) return qeErr(X("Choose a category.", "اختر فئة.", "Choisissez une catégorie."));
    if (!m || !m.q.trim() || m.a.some((x) => !x.trim())) { q.tab = d.lang; return qeErr(X("Write the question and all four answers in the main language.", "اكتب السؤال والإجابات الأربع باللغة الأساسية.", "Écrivez la question et les quatre réponses dans la langue principale.")); }
    if (new Set(m.a.map((x) => x.trim().toLowerCase())).size < 4) { q.tab = d.lang; return qeErr(X("Each answer must be different.", "يجب أن تختلف الإجابات.", "Chaque réponse doit être différente.")); }
    for (const [l, nm] of LANGS) {
      const tx = d.text[l]; if (l === d.lang || !tx) continue;
      const any = tx.q.trim() || tx.a.some((x) => x.trim()) || (tx.x || "").trim();
      if (!any) { delete d.text[l]; continue; }
      if (!tx.q.trim() || tx.a.some((x) => !x.trim())) { q.tab = l; return qeErr(X(`Finish the ${nm} version, or clear it.`, `أكمل نسخة ${nm} أو امسحها.`, `Terminez la version ${nm} ou videz-la.`)); }
    }
    const list = qlist(q.ns, q.setId);
    if (q.idx >= 0) list[q.idx] = d; else list.push(d);
    const set = SJ.SETS.find((x) => x.id === q.setId); if (set) set.updated = X("Just now", "الآن", "À l'instant");
    SJ.render();
    if (a.dataset.more) { S.qd = { ns: q.ns, setId: q.setId, idx: -1, tab: d.lang, d: blankQ(d.lang, d.cat, d.lvl), err: "" }; SJ.drawer(qeHtml()); SJ.toast(X(`Saved. ${list.length} question${list.length === 1 ? "" : "s"} so far.`, `حُفظ. لديك ${list.length} حتى الآن.`, `Enregistrée. ${list.length} pour l'instant.`)); }
    else { SJ.closeOverlay(); SJ.toast(X("Question saved.", "حُفظ السؤال.", "Question enregistrée.")); }
  };
  document.addEventListener("input", (e) => {
    const k = e.target.dataset && e.target.dataset.qe; if (!k || !S.qd) return;
    const T = S.qd.d.text[S.qd.tab];
    if (k === "q") { T.q = e.target.value; const c = document.getElementById("qe-qc"); if (c) c.textContent = T.q.length + " / 160"; }
    else if (k === "x") T.x = e.target.value;
    else if (/^a\d$/.test(k)) T.a[+k[1]] = e.target.value;
  });
  document.addEventListener("change", (e) => {
    const k = e.target.dataset && e.target.dataset.qe; if (!k || !S.qd) return;
    if (k === "dom") { S.qd.dom = e.target.value; S.qd.d.cat = ""; qeRedraw(); }
    if (k === "cat") S.qd.d.cat = e.target.value;
  });
  SJ.actions["q-del"] = (a) => { qlist(a.dataset.ns, a.dataset.set).splice(+a.dataset.v, 1); SJ.render(); };
  SJ.actions["q-dup"] = (a) => { const l = qlist(a.dataset.ns, a.dataset.set), i = +a.dataset.v; l.splice(i + 1, 0, JSON.parse(JSON.stringify(l[i]))); SJ.render(); SJ.toast(X("Duplicated. Edit the copy.", "تم التكرار. عدّل النسخة.", "Dupliquée. Modifiez la copie.")); };


  /* pass-and-play */
  function devTurn() {
    const d = S.dev, p = d.players[d.idx];
    const st = Object.assign({}, d.st, { title: () => X(`${p.label}'s turn`, `دور ${p.label}`, `Au tour de ${p.label}`) });
    SJ.startGame("solo", d.qs, st, true);
    S.game.onFinish = (g) => {
      p.score = g.score; if (p.me) d.log = g.log;
      d.idx++;
      if (d.idx < d.players.length) { SJ.go("handoff"); return; }
      const players = d.players.map((x) => Object.assign({}, x));
      S.game = { mode: "device", qs: d.qs, players, log: d.log || g.log, score: players.find((x) => x.me) ? players.find((x) => x.me).score : 0, st: d.st, revealed: true, hidden: [], i: d.qs.length - 1,
        teams: d.teams ? d.teams.map((names) => names.map((n) => (players.find((x) => x.label === n) || {}).id)) : null };
      SJ.go("podium"); SJ.confetti();
    };
  }
  SJ.screens["handoff"] = () => {
    const d = S.dev; if (!d) return `<div class="page"><div class="empty"><h3>${X("No round running", "لا جولة جارية", "Aucune manche en cours")}</h3></div></div>`;
    const p = d.players[d.idx], prev = d.players[d.idx - 1];
    return `<div class="welcome" style="justify-content:center">${SJ.PATTERN("kh-hand")}<div class="welcome-body" style="flex-direction:column;text-align:center;justify-content:center;gap:18px">
      <span class="badge gold">${X("Turn", "الدور", "Tour")} ${d.idx + 1} ${X("of", "من", "sur")} ${d.players.length}</span>
      <p style="color:#c9dcd3">${E(prev.label)}: <b class="num" style="color:#f3d394">${prev.score}</b> ${t("points")}</p>
      <span class="av xl" style="--c:#3365a8;margin:0 auto">${E(p.label.charAt(0).toUpperCase())}</span>
      <h1 style="font-size:clamp(30px,5vw,48px);font-weight:800">${X(`Pass the device to ${p.label}`, `مرّروا الجهاز إلى ${p.label}`, `Passez l'appareil à ${p.label}`)}</h1>
      <p style="color:#c9dcd3;max-width:44ch;margin:0 auto">${X("Same questions, so no peeking while others play.", "الأسئلة نفسها، فلا تنظروا أثناء لعب الآخرين.", "Mêmes questions : pas de coup d'œil pendant que les autres jouent.")}</p>
      <div class="row" style="justify-content:center;gap:10px"><button class="btn btn-gold btn-lg" data-act="dev-next">${I("tri", "flip")}${X(`I'm ${p.label}, start`, `أنا ${p.label}، ابدأ`, `Je suis ${p.label}, c'est parti`)}</button><button class="btn btn-lg btn-outline-light" data-act="dev-quit">${X("End round", "إنهاء الجولة", "Arrêter")}</button></div></div></div>`;
  };
  SJ.actions["dev-next"] = () => devTurn();
  SJ.actions["dev-quit"] = () => { S.dev = null; SJ.go("challenges"); };
  SJ.actions["dev-again"] = () => { const d = S.dev; d.idx = 0; d.qs = SJ.shuffle(d.qs); d.players.forEach((p) => { p.score = 0; }); devTurn(); };

  /* =====================================================================
     MESSAGES
     ===================================================================== */
  function msgState() {
    if (S.msg) return S.msg;
    const today = X("Today", "اليوم", "Aujourd'hui"), yest = X("Yesterday", "أمس", "Hier");
    S.msg = { tab: "people", q: "", open: null, menu: null, cmenu: null, reply: null, rec: null, draftVoice: null, draftFile: null, playing: null, emoji: false,
      threads: [
        { id: "omar", kind: "people", unread: 2, pinned: true, msgs: [
          { from: "omar", type: "text", text: "Salam! Did you see the new Hadith questions?", time: "10:02", day: yest },
          { from: "me", type: "text", text: "Wa alaykum as-salam, not yet. Are they hard?", time: "10:05", day: yest, read: true },
          { from: "omar", type: "challenge", cat: "hadith", n: 10, score: 63, time: "10:06", day: today },
          { from: "omar", type: "voice", dur: 6, time: "10:07", day: today },
          { from: "omar", type: "text", text: "I got 7. Your turn 😄", time: "10:07", day: today } ] },
        { id: "bilal", kind: "people", unread: 0, msgs: [
          { from: "me", type: "text", text: "Assalamu alaykum ya Bilal", time: "08:25", day: today, read: false },
          { from: "me", type: "file", name: "notes-seerah.pdf", size: "248 KB", time: "08:31", day: today, read: false },
          { from: "me", type: "image", name: "infographic.png", time: "08:32", day: today, read: false },
          { from: "me", type: "voice", dur: 4, time: "08:32", day: today, read: false } ] },
        { id: "khadija", kind: "people", unread: 1, msgs: [{ from: "khadija", type: "text", text: "Ramadan category tonight after Isha?", time: "21:40", day: yest }] },
        { id: "youssef", kind: "people", unread: 0, msgs: [{ from: "me", type: "text", text: "Good game yesterday!", time: "Mon", day: "Mon", read: true }, { from: "youssef", type: "text", text: "Rematch soon, I'm studying fiqh now", time: "Mon", day: "Mon" }] },
        { id: "team", kind: "people", team: true, unread: 0, msgs: [{ from: "team", type: "text", text: "Welcome to Sirāj v2. Live rooms are here: host one from Play and share the code.", time: "Oct 1", day: "Oct 1" }, { from: "team", type: "text", text: "Reply to T-2086: thank you! 24 new Hadith questions are now live.", time: "Yesterday", day: yest }] },
        { id: "g-family", kind: "groups", name: "Family quiz", members: ["omar", "khadija", "sarah"], unread: 3, msgs: [
          { from: "khadija", type: "text", text: "Who's in for Friday night?", time: "19:02", day: today }, { from: "sarah", type: "text", text: "Me! Seerah please", time: "19:04", day: today }, { from: "omar", type: "challenge", cat: "seerah", n: 10, score: 70, time: "19:10", day: today } ] },
        { id: "g-hadith", kind: "groups", name: "Hadith study circle", members: ["youssef", "mariam", "hamza"], unread: 0, msgs: [{ from: "mariam", type: "text", text: "This week: the 40 Hadith of an-Nawawi, numbers 1 to 10.", time: "Sun", day: "Sun" }] },
        { id: "r-7k4", kind: "rooms", name: "Room 7K4 Q2M", members: ["omar", "khadija", "youssef"], unread: 0, msgs: [{ from: "omar", type: "text", text: "Salam everyone", time: "21:02", day: yest }, { from: "khadija", type: "text", text: "Ready when you are", time: "21:03", day: yest }, { from: "sys", type: "sys", text: "Game finished. Omar K. won with 172 points.", time: "21:12", day: yest }] }
      ] };
    return S.msg;
  }
  const thName = (th) => th.team ? X("Sirāj team", "فريق سراج", "Équipe Sirāj") : th.name || SJ.pname(SJ.person(th.id));
  const thAv = (th, size) => th.team ? `<span class="av ${size}" style="background:linear-gradient(160deg,#1d4a3b,#0c2119);padding:6px">${SJ.LAMP}</span>`
    : th.kind === "people" ? SJ.av(SJ.person(th.id), size, true)
    : `<span class="av ${size} grp" style="--c:${th.kind === "rooms" ? "#0f2a22" : "#2f5f9e"}">${I(th.kind === "rooms" ? "radio" : "users", "i-sm")}</span>`;
  const fromName = (id) => id === "me" ? X("You", "أنت", "Vous") : id === "team" ? X("Sirāj team", "فريق سراج", "Équipe Sirāj") : SJ.pname(SJ.person(id));
  function lastLine(th) { const m = th.msgs[th.msgs.length - 1]; if (!m) return X("No messages yet. Say salam", "لا رسائل بعد. ألقِ السلام", "Aucun message. Dites salam"); const pre = m.from === "me" ? X("You: ", "أنت: ", "Vous : ") : th.kind !== "people" && m.from !== "sys" ? fromName(m.from) + ": " : ""; const body = m.deleted ? X("Message deleted", "رسالة محذوفة", "Message supprimé") : { text: m.text, file: "📎 " + m.name, image: X("Photo", "صورة", "Photo"), voice: X("Voice message", "رسالة صوتية", "Message vocal"), challenge: X("Sent a challenge", "أرسل تحديًا", "A envoyé un défi"), sys: m.text }[m.type]; return pre + body; }
  function wave(id, dur, me) {
    const bars = Array.from({ length: 28 }, (_, i) => 6 + Math.round(Math.abs(Math.sin(i * 1.7 + dur)) * 18));
    const playing = msgState().playing === id;
    return `<div class="voice ${playing ? "playing" : ""}" style="--d:${dur}s"><button class="vplay" data-act="m-play" data-v="${id}" aria-label="${playing ? "Pause" : "Play"} voice message">${I(playing ? "pause" : "tri")}</button><span class="wave">${bars.map((h) => `<i style="height:${h}px"></i>`).join("")}</span><span class="num vdur">0:${String(dur).padStart(2, "0")}</span></div>`;
  }
  function bubble(th, m, i) {
    const me = m.from === "me", id = th.id + ":" + i;
    if (m.type === "sys") return `<div class="msg-sys">${E(m.text)}</div>`;
    let inner;
    if (m.deleted) inner = `<div class="bubble deleted">${I("trash", "i-sm")}${X("This message was deleted", "حُذفت هذه الرسالة", "Ce message a été supprimé")}</div>`;
    else if (m.type === "text") inner = `<div class="bubble">${m.replyTo ? `<div class="quote"><b>${E(fromName(m.replyTo.from))}</b><span>${E(m.replyTo.text)}</span></div>` : ""}${E(m.text)}</div>`;
    else if (m.type === "file") inner = `<div class="bubble file">${I("clip")}<span><b>${E(m.name)}</b><span class="fsz">${E(m.size)}</span></span></div>`;
    else if (m.type === "image") inner = `<div class="bubble img">${m.src ? `<img src="${m.src}" alt="${E(m.name)}">` : `<div class="img-ph" role="img" aria-label="${E(m.name)}"><span style="--h:60%"></span><span style="--h:85%"></span><span style="--h:45%"></span><span style="--h:70%"></span><span style="--h:95%"></span></div>`}<span class="fsz">${E(m.name)}</span></div>`;
    else if (m.type === "voice") inner = `<div class="bubble vbub">${wave(id, m.dur, me)}</div>`;
    else inner = `<div class="msg-card"><div class="row">${SJ.catIc(SJ.cat(m.cat), "sm")}<b>${X("Challenge", "تحدٍّ", "Défi")} · ${E(L(SJ.cat(m.cat).name))}</b></div><span class="help">${m.n} ${X("questions", "أسئلة", "questions")} · ${X("score to beat", "النتيجة المطلوبة", "score à battre")}: ${m.score}</span><button class="btn btn-sm btn-primary" data-act="m-accept" data-v="${m.cat}" data-n="${m.n}">${X("Accept and play", "اقبل والعب", "Accepter et jouer")}</button></div>`;
    const ticks = me ? `<span class="ticks ${m.read ? "read" : ""}" aria-label="${m.read ? "Read" : "Sent"}">${I("check", "i-sm")}${m.read ? I("check", "i-sm") : ""}</span>` : "";
    const menuOpen = msgState().menu === id;
    return `<div class="bubble-row ${me ? "me" : ""}">${me ? "" : (th.kind === "people" ? thAv(th, "sm") : SJ.av(SJ.person(m.from) || { c: "#475569", ini: "?", ar: "?" }, "sm"))}
      <div class="b-wrap">${!me && th.kind !== "people" ? `<span class="b-from">${E(fromName(m.from))}</span>` : ""}${inner}<div class="bubble-time">${m.time} ${ticks}</div></div>
      ${m.deleted ? "" : `<div class="m-more"><button class="icon-btn sm" data-act="m-menu" data-v="${id}" aria-label="Message options" aria-expanded="${menuOpen}">${I("more", "i-sm")}</button>${menuOpen ? `<div class="menu msg-menu" role="menu">${me ? `<div class="mh" style="font-size:12.5px"><div><span class="muted">${X("Sent", "أُرسلت", "Envoyé")}</span> ${E(m.day)} ${m.time}</div><div><span class="muted">${X("Read", "قُرئت", "Lu")}</span> ${m.read ? X("Yes", "نعم", "Oui") : X("Not read yet", "لم تُقرأ بعد", "Pas encore lu")}</div></div>` : ""}
        <button class="mi" data-act="m-reply" data-v="${id}">${I("reply", "flip")}${X("Reply", "رد", "Répondre")}</button>${m.type === "text" ? `<button class="mi" data-act="m-copy" data-v="${id}">${I("copy")}${X("Copy text", "نسخ النص", "Copier le texte")}</button>` : ""}
        <button class="mi" data-act="m-del-me" data-v="${id}">${I("eye")}${X("Delete for me", "حذف عندي", "Supprimer pour moi")}</button>${me ? `<button class="mi danger" data-act="m-del-all" data-v="${id}">${I("trash")}${X("Delete for everyone", "حذف عند الجميع", "Supprimer pour tous")}</button>` : `<button class="mi danger" data-act="m-report" data-v="${id}">${I("flag")}${X("Report", "إبلاغ", "Signaler")}</button>`}</div>` : ""}</div>`}</div>`;
  }
  SJ.screens["messages"] = () => {
    const ms = msgState();
    if (S.params.with) { ms.open = S.params.with; ms.tab = "people"; S.params = {}; if (!ms.threads.find((x) => x.id === ms.open)) ms.threads.unshift({ id: ms.open, kind: "people", unread: 0, msgs: [] }); }
    const q = ms.q.toLowerCase();
    const list = ms.threads.filter((th) => th.kind === ms.tab && !th.hidden).filter((th) => !q || thName(th).toLowerCase().includes(q) || th.msgs.some((m) => (m.text || "").toLowerCase().includes(q))).sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0));
    const th = ms.threads.find((x) => x.id === ms.open);
    if (th && th.unread) { S.unread.messages = Math.max(0, S.unread.messages - th.unread); th.unread = 0; }
    const tabCount = (k) => ms.threads.filter((x) => x.kind === k).reduce((s, x) => s + x.unread, 0);
    const tabs = `<div class="seg full" role="tablist">${[["people", X("People", "الأشخاص", "Personnes")], ["groups", X("Groups", "المجموعات", "Groupes")], ["rooms", X("Rooms", "الغرف", "Salons")]].map(([k, l]) => `<button role="tab" aria-pressed="${ms.tab === k}" data-act="m-tab" data-v="${k}">${l}${tabCount(k) ? ` <span class="count green">${tabCount(k)}</span>` : ""}</button>`).join("")}</div>`;
    const hint = { people: X("A private thread between two people.", "محادثة خاصة بين شخصين.", "Une conversation privée à deux."), groups: X("Groups you made with friends.", "مجموعات أنشأتها مع أصدقائك.", "Des groupes créés avec vos amis."), rooms: X("Chat from live rooms you joined.", "دردشات الغرف المباشرة التي انضممت إليها.", "Les discussions de vos salons en direct.") }[ms.tab];
    const items = list.map((x) => { const last = x.msgs[x.msgs.length - 1]; const cOpen = ms.cmenu === x.id; return `<div class="conv ${th && x.id === th.id ? "on" : ""}"><button class="conv-main" data-act="m-open" data-v="${x.id}">${thAv(x, "")}<span class="grow"><span class="row"><span class="t">${E(thName(x))}</span>${x.pinned ? I("pin", "i-sm muted") : ""}${x.muted ? I("mute", "i-sm muted") : ""}<span class="spacer"></span><span class="s">${last ? last.time : X("just now", "الآن", "à l'instant")}</span></span><span class="s ell">${E(lastLine(x))}</span></span>${x.unread ? `<span class="count green">${x.unread}</span>` : ""}</button>
      <div class="c-more"><button class="icon-btn sm" data-act="m-cmenu" data-v="${x.id}" aria-label="Conversation options" title="Conversation options" aria-expanded="${cOpen}">${I("more", "i-sm")}</button>${cOpen ? `<div class="menu" role="menu"><button class="mi" data-act="m-pin" data-v="${x.id}">${I("pin")}${x.pinned ? X("Unpin", "إلغاء التثبيت", "Désépingler") : X("Pin to top", "تثبيت في الأعلى", "Épingler")}</button><button class="mi" data-act="m-mute" data-v="${x.id}">${I("mute")}${x.muted ? X("Unmute", "إلغاء الكتم", "Réactiver") : X("Mute notifications", "كتم الإشعارات", "Couper les notifications")}</button><button class="mi" data-act="m-unread" data-v="${x.id}">${I("mail")}${X("Mark as unread", "تعليم كغير مقروءة", "Marquer non lu")}</button>${x.kind === "people" && !x.team ? `<button class="mi" data-act="m-block" data-v="${x.id}">${I("x")}${X("Block", "حظر", "Bloquer")}</button>` : ""}<div class="divider"></div><button class="mi danger" data-act="m-delconv" data-v="${x.id}">${I("trash")}${X("Delete conversation", "حذف المحادثة", "Supprimer la conversation")}</button></div>` : ""}</div></div>`; }).join("");
    let body = "", lastDay = null;
    if (th) th.msgs.forEach((m, i) => { if (m.hiddenForMe) return; if (m.day !== lastDay) { body += `<div class="day-sep"><span>${E(m.day)}</span></div>`; lastDay = m.day; } body += bubble(th, m, i); });
    if (th && !th.msgs.length) body = `<div class="empty" style="margin:auto"><p>${X("No messages yet. Say salam.", "لا رسائل بعد. ألقِ السلام.", "Aucun message. Dites salam.")}</p></div>`;
    if (th && S.typing === th.id) body += `<div class="bubble-row">${thAv(th, "sm")}<div class="bubble"><span class="thinking" style="color:var(--ink-3)"><i></i><i></i><i></i></span></div></div>`;
    const sub = th ? (th.team ? X("Announcements and support replies", "إعلانات وردود الدعم", "Annonces et réponses du support") : th.kind === "people" ? E(SJ.person(th.id).status) : th.members.map((id) => SJ.pname(SJ.person(id))).join(", ") + X(" and you", " وأنت", " et vous")) : "";
    const recSecs = ms.rec ? Math.floor((Date.now() - ms.rec) / 1000) : 0;
    const composer = !th ? "" : th.team ? `<div class="composer"><span class="help">${X("You can't reply here. For help, use Contact us.", "لا يمكنك الرد هنا. للمساعدة استخدم «تواصل معنا».", "Pas de réponse ici. Pour de l'aide, utilisez Nous contacter.")}</span><button class="btn btn-sm" data-go="support">${t("support")}</button></div>`
      : `<div class="composer-wrap">${ms.reply ? `<div class="reply-bar">${I("reply", "i-sm flip")}<span style="flex:1;min-width:0" class="ell"><b>${E(fromName(ms.reply.from))}</b> · ${E(ms.reply.text)}</span><button class="icon-btn sm" data-act="m-reply-x" aria-label="Cancel reply">${I("x", "i-sm")}</button></div>` : ""}
        ${ms.draftVoice || ms.draftFile ? `<div class="drafts">${ms.draftVoice ? `<span class="draft">${I("mic", "i-sm")}<b>Voice note.webm</b><span class="num muted">0:${String(ms.draftVoice).padStart(2, "0")}</span><button class="x" data-act="m-dv-x" aria-label="Remove voice note">${I("x", "i-sm")}</button></span>` : ""}${ms.draftFile ? `<span class="draft">${I(ms.draftFile.src ? "image" : "clip", "i-sm")}<b>${E(ms.draftFile.name)}</b><span class="muted">${E(ms.draftFile.size)}</span><button class="x" data-act="m-df-x" aria-label="Remove file">${I("x", "i-sm")}</button></span>` : ""}</div>` : ""}
        ${ms.rec ? `<div class="composer rec"><span class="rec-dot"></span><b>${X("Recording", "جارٍ التسجيل", "Enregistrement")}</b><span class="num" id="rec-t">0:${String(recSecs).padStart(2, "0")}</span><span class="spacer"></span><button class="btn btn-sm btn-ghost" data-act="m-rec-x">${X("Cancel", "إلغاء", "Annuler")}</button><button class="btn btn-sm btn-primary" data-act="m-rec-stop">${I("check", "i-sm")}${X("Done", "تم", "Terminé")}</button></div>`
        : `<form class="composer" data-form="msg2"><div class="c-tool"><button type="button" class="icon-btn sm" data-act="m-emoji" aria-label="Emoji" aria-expanded="${ms.emoji}">${I("smile")}</button>${ms.emoji ? `<div class="emoji-pop" role="menu">${["🤲", "😄", "👍", "❤️", "🔥", "🌙", "📖", "🎉", "😅", "🙏", "✅", "💯"].map((x) => `<button type="button" data-act="m-emo" data-v="${x}" aria-label="${x}">${x}</button>`).join("")}</div>` : ""}</div>
          <label class="sr-only" for="msg-in">Message</label><input class="input" id="msg-in" placeholder="${X("Write a message…", "اكتب رسالة…", "Écrire un message…")}" autocomplete="off" value="${E(ms.text || "")}">
          <label class="icon-btn sm" aria-label="Attach a file" title="Attach">${I("clip")}<input type="file" class="sr-only" data-act="m-file"></label>
          <button type="button" class="icon-btn sm" data-act="m-rec" aria-label="Record a voice note">${I("mic")}</button>
          <button class="btn btn-primary" type="submit" aria-label="Send">${I("send", "flip")}</button></form>`}</div>`;
    return `<div class="page"><section class="card inbox ${th ? "open" : ""}">
      <div class="inbox-list"><div class="inbox-top"><div class="row"><h1 style="font-size:24px;font-weight:800;flex:1">${t("messages")}</h1><button class="icon-btn sm" style="border:1px solid var(--line)" data-act="m-new" aria-label="New message">${I("plus")}</button></div>${tabs}<p class="help">${hint}</p>
        <label class="input-icon" style="display:block">${I("search")}<span class="sr-only">Search</span><input class="input" id="m-q" value="${E(ms.q)}" placeholder="${X("Search people and messages", "ابحث في الأشخاص والرسائل", "Chercher personnes et messages")}" autocomplete="off"></label></div>
        <div class="conv-list">${items || `<div class="empty"><p>${X("Nothing here yet.", "لا شيء هنا بعد.", "Rien pour l'instant.")}</p></div>`}</div></div>
      <div class="thread">${th ? `<div class="thread-head"><button class="icon-btn sm back-btn" data-act="m-close" aria-label="Back">${I("chevL", "flip")}</button>${thAv(th, "")}<div style="flex:1;min-width:0"><b>${E(thName(th))}</b><div class="help ell">${sub}</div></div>${th.kind === "people" && !th.team ? `<button class="btn btn-sm" data-act="ch-with" data-v="${th.id}">${I("swords", "i-sm")}<span class="hide-sm">${X("Challenge", "تحدٍّ", "Défier")}</span></button>` : th.kind === "rooms" ? `<button class="btn btn-sm" data-go="room-join" data-code="7K4Q2M">${I("door", "i-sm")}${X("Rejoin", "عُد للغرفة", "Rejoindre")}</button>` : ""}</div>
        <div class="thread-body" id="tb">${body}</div>${composer}`
        : `<div class="empty" style="margin:auto"><div class="e-ic">${I("chat", "flip")}</div><h3>${X("Pick a conversation", "اختر محادثة", "Choisissez une conversation")}</h3><p>${X("Messages, voice notes, files and challenge invites show here.", "تظهر هنا الرسائل والملاحظات الصوتية والملفات ودعوات التحدي.", "Messages, vocaux, fichiers et invitations s'affichent ici.")}</p></div>`}</div></section></div>`;
  };
  let lastKey = "", lastTop = 0;
  SJ.after["messages"] = () => {
    const b = document.getElementById("tb"); if (!b) { lastKey = ""; return; }
    const s = msgState(), th = s.threads.find((x) => x.id === s.open), key = s.open + ":" + (th ? th.msgs.length : 0) + ":" + (S.typing === s.open);
    if (key !== lastKey) b.scrollTop = b.scrollHeight; else b.scrollTop = lastTop;
    lastKey = key;
    const mm = b.querySelector(".msg-menu"); if (mm) mm.scrollIntoView({ block: "nearest" });
    b.onscroll = () => { lastTop = b.scrollTop; }; lastTop = b.scrollTop;
  };
  const M = (fn) => (a) => { fn(msgState(), a); SJ.render(); };
  const findMsg = (key) => { const [tid, i] = key.split(":"); const th = msgState().threads.find((x) => x.id === tid); return { th, m: th.msgs[+i] }; };
  const keepText = () => { const i = document.getElementById("msg-in"); if (i) msgState().text = i.value; };
  SJ.actions["m-tab"] = M((s, a) => { s.tab = a.dataset.v; s.cmenu = null; });
  SJ.actions["m-open"] = M((s, a) => { s.open = a.dataset.v; s.menu = null; s.cmenu = null; s.reply = null; });
  SJ.actions["m-close"] = M((s) => { s.open = null; });
  SJ.actions["m-cmenu"] = M((s, a) => { s.cmenu = s.cmenu === a.dataset.v ? null : a.dataset.v; s.menu = null; });
  SJ.actions["m-menu"] = M((s, a) => { keepText(); s.menu = s.menu === a.dataset.v ? null : a.dataset.v; s.cmenu = null; });
  SJ.actions["m-pin"] = M((s, a) => { const th = s.threads.find((x) => x.id === a.dataset.v); th.pinned = !th.pinned; s.cmenu = null; });
  SJ.actions["m-mute"] = M((s, a) => { const th = s.threads.find((x) => x.id === a.dataset.v); th.muted = !th.muted; s.cmenu = null; SJ.toast(th.muted ? X("Notifications muted.", "كُتمت الإشعارات.", "Notifications coupées.") : X("Notifications on.", "فُعّلت الإشعارات.", "Notifications activées.")); });
  SJ.actions["m-unread"] = M((s, a) => { const th = s.threads.find((x) => x.id === a.dataset.v); th.unread = 1; S.unread.messages++; s.cmenu = null; if (s.open === th.id) s.open = null; });
  SJ.actions["m-block"] = (a) => { msgState().cmenu = null; SJ.modal(`<div class="m-ic red">${I("x", "i-lg")}</div><h2>${X("Block this person?", "حظر هذا الشخص؟", "Bloquer cette personne ?")}</h2><p>${X("They can't message, challenge or invite you. They aren't told.", "لن يستطيع مراسلتك أو تحدّيك أو دعوتك، ولن يُبلّغ بذلك.", "Cette personne ne pourra plus vous écrire, défier ou inviter. Elle n'est pas prévenue.")}</p><div class="actions"><button class="btn" data-act="close">${X("Cancel", "إلغاء", "Annuler")}</button><button class="btn btn-danger" data-act="m-block-yes" data-v="${a.dataset.v}">${X("Block", "حظر", "Bloquer")}</button></div>`); };
  SJ.actions["m-block-yes"] = (a) => { const s = msgState(); const th = s.threads.find((x) => x.id === a.dataset.v); if (th) th.hidden = true; S.friends = S.friends.filter((x) => x !== a.dataset.v); S.fmenu = null; if (s.open === a.dataset.v) s.open = null; SJ.closeOverlay(); SJ.render(); SJ.toast(X("Blocked.", "تم الحظر.", "Bloqué.")); };
  SJ.actions["m-delconv"] = (a) => { msgState().cmenu = null; SJ.modal(`<h2>${X("Delete this conversation?", "حذف هذه المحادثة؟", "Supprimer la conversation ?")}</h2><p>${X("It's removed for you only.", "تُحذف عندك فقط.", "Supprimée pour vous uniquement.")}</p><div class="actions"><button class="btn" data-act="close">${X("Cancel", "إلغاء", "Annuler")}</button><button class="btn btn-danger" data-act="m-delconv-yes" data-v="${a.dataset.v}">${X("Delete", "حذف", "Supprimer")}</button></div>`); };
  SJ.actions["m-delconv-yes"] = (a) => { const s = msgState(); s.threads = s.threads.filter((x) => x.id !== a.dataset.v); if (s.open === a.dataset.v) s.open = null; SJ.closeOverlay(); SJ.render(); };
  SJ.actions["m-reply"] = M((s, a) => { const { m } = findMsg(a.dataset.v); s.reply = { from: m.from, text: m.text || lastLine({ msgs: [m], kind: "people" }) }; s.menu = null; setTimeout(() => { const i = document.getElementById("msg-in"); if (i) i.focus(); }); });
  SJ.actions["m-reply-x"] = M((s) => { s.reply = null; });
  SJ.actions["m-copy"] = (a) => { const { m } = findMsg(a.dataset.v); msgState().menu = null; SJ.render(); try { navigator.clipboard.writeText(m.text).then(() => SJ.toast(X("Copied.", "نُسخ.", "Copié.")), () => SJ.toast(X("Copied.", "نُسخ.", "Copié."))); } catch (e) { SJ.toast(X("Copied.", "نُسخ.", "Copié.")); } };
  SJ.actions["m-del-me"] = M((s, a) => { findMsg(a.dataset.v).m.hiddenForMe = true; s.menu = null; });
  SJ.actions["m-del-all"] = M((s, a) => { findMsg(a.dataset.v).m.deleted = true; s.menu = null; });
  SJ.actions["m-report"] = (a) => { msgState().menu = null; SJ.render(); SJ.toast(X("Reported to the moderation team.", "أُبلغ فريق الإشراف.", "Signalé à la modération.")); };
  SJ.actions["m-play"] = (a) => { const s = msgState(), id = a.dataset.v; keepText(); if (s.playing === id) { s.playing = null; SJ.render(); return; } s.playing = id; SJ.render(); const { m } = findMsg(id); later(m.dur * 1000, () => { if (msgState().playing === id) { msgState().playing = null; if (S.route === "messages") SJ.render(); } }); };
  SJ.actions["m-accept"] = (a) => { const st = Object.assign({}, S.setup, { domain: SJ.cat(a.dataset.v).domain, cats: [a.dataset.v], count: +a.dataset.n, timer: 20, level: "mixed", title: () => X("Challenge", "تحدٍّ", "Défi") }); SJ.startGame("solo", SJ.pool(st), st); };
  SJ.actions["m-emoji"] = M((s) => { keepText(); s.emoji = !s.emoji; });
  SJ.actions["m-emo"] = M((s, a) => { keepText(); s.text = (s.text || "") + a.dataset.v; s.emoji = false; });
  SJ.actions["m-rec"] = (a) => { keepText(); const s = msgState(); s.rec = Date.now(); SJ.render(); const tick = () => { if (!msgState().rec) return; const el = document.getElementById("rec-t"); if (el) { const n = Math.floor((Date.now() - msgState().rec) / 1000); el.textContent = "0:" + String(n).padStart(2, "0"); } later(250, tick); }; tick(); };
  SJ.actions["m-rec-x"] = M((s) => { s.rec = null; });
  SJ.actions["m-rec-stop"] = M((s) => { s.draftVoice = Math.max(1, Math.min(59, Math.round((Date.now() - s.rec) / 1000))); s.rec = null; });
  SJ.actions["m-dv-x"] = M((s) => { keepText(); s.draftVoice = null; });
  SJ.actions["m-df-x"] = M((s) => { keepText(); s.draftFile = null; });
  SJ.actions["m-new"] = () => SJ.modal(`<h2>${X("New message", "رسالة جديدة", "Nouveau message")}</h2><p>${X("Pick a friend.", "اختر صديقًا.", "Choisissez un ami.")}</p><div class="stack" style="margin-top:14px">${S.friends.map((id) => `<button class="tile" style="padding:10px 12px" data-act="m-new-go" data-v="${id}">${SJ.av(SJ.person(id), "sm", true)}<span class="tt" style="font-size:15px">${E(SJ.pname(SJ.person(id)))}</span></button>`).join("")}</div><div class="actions"><button class="btn" data-act="close">${X("Cancel", "إلغاء", "Annuler")}</button></div>`);
  SJ.actions["m-new-go"] = (a) => { SJ.closeOverlay(); const s = msgState(); s.tab = "people"; if (!s.threads.find((x) => x.id === a.dataset.v)) s.threads.unshift({ id: a.dataset.v, kind: "people", unread: 0, msgs: [] }); s.open = a.dataset.v; SJ.render(); };
  SJ.changes["m-file"] = (a) => {
    const f = a.files && a.files[0]; if (!f) return; keepText();
    const size = f.size > 1e6 ? (f.size / 1e6).toFixed(1) + " MB" : Math.max(1, Math.round(f.size / 1e3)) + " KB";
    const s = msgState();
    if (/^image\//.test(f.type)) { const r = new FileReader(); r.onload = () => { s.draftFile = { name: f.name, size, src: r.result }; SJ.render(); }; r.readAsDataURL(f); }
    else { s.draftFile = { name: f.name, size }; SJ.render(); }
  };
  document.addEventListener("input", (e) => { if (e.target.id === "m-q") { msgState().q = e.target.value; const pos = e.target.selectionStart; SJ.render(); const n = document.getElementById("m-q"); if (n) { n.focus(); n.setSelectionRange(pos, pos); } } });
  document.addEventListener("click", (e) => { const s = S.msg; if (!s || S.route !== "messages") return; if ((s.menu || s.cmenu || s.emoji) && !e.target.closest(".menu, .emoji-pop, [data-act='m-menu'], [data-act='m-cmenu'], [data-act='m-emoji']")) { s.menu = null; s.cmenu = null; s.emoji = false; keepText(); SJ.render(); } }, true);
  SJ.forms["msg2"] = () => {
    const s = msgState(), th = s.threads.find((x) => x.id === s.open); if (!th) return;
    const v = document.getElementById("msg-in").value.trim();
    const now = new Date(), time = String(now.getHours()).padStart(2, "0") + ":" + String(now.getMinutes()).padStart(2, "0"), day = X("Today", "اليوم", "Aujourd'hui");
    let sent = false;
    if (s.draftFile) { th.msgs.push(s.draftFile.src ? { from: "me", type: "image", name: s.draftFile.name, src: s.draftFile.src, time, day } : { from: "me", type: "file", name: s.draftFile.name, size: s.draftFile.size, time, day }); s.draftFile = null; sent = true; }
    if (s.draftVoice) { th.msgs.push({ from: "me", type: "voice", dur: s.draftVoice, time, day }); s.draftVoice = null; sent = true; }
    if (v) { th.msgs.push({ from: "me", type: "text", text: v, time, day, replyTo: s.reply }); sent = true; }
    if (!sent) return;
    s.reply = null; s.text = ""; SJ.render(); const n = document.getElementById("msg-in"); if (n) n.focus();
    const who = th.kind === "people" ? th.id : th.members[0];
    later(1200, () => { th.msgs.forEach((m) => { if (m.from === "me") m.read = true; }); S.typing = th.id; if (S.route === "messages") { keepText(); SJ.render(); } });
    later(2600, () => { S.typing = null; th.msgs.push({ from: who, type: "text", text: ["Inshallah, tonight after Isha", "Haha, deal", "JazakAllahu khayran!", "Send me the questions"][Math.floor(Math.random() * 4)], time, day }); if (S.route === "messages") { keepText(); SJ.render(); const n2 = document.getElementById("msg-in"); if (n2) n2.focus(); } });
  };

  /* =====================================================================
     FRIENDS
     ===================================================================== */
  const RECORD = { omar: [5, 3], khadija: [2, 4], youssef: [6, 1], sarah: [1, 0], bilal: [0, 1] };
  SJ.screens["friends"] = () => {
    const q = (S.fq || "").toLowerCase();
    const match = (p) => !q || (p.name + " " + p.ar + " " + p.id).toLowerCase().includes(q);
    const friends = S.friends.map(SJ.person).filter(match);
    const online = S.friends.map(SJ.person).filter((p) => p.online);
    const others = SJ.PEOPLE.filter((p) => !S.friends.includes(p.id) && !S.requests.includes(p.id));
    const found = q ? others.filter(match) : [];
    const suggest = others.filter((p) => ["hamza", "mariam", "leila"].includes(p.id));
    const fcard = (p) => { const rec = RECORD[p.id]; const open = S.fmenu === p.id; return `<article class="card f-card"><div class="f-top">${SJ.av(p, "lg", true)}<div class="f-more"><button class="icon-btn sm" data-act="f-menu" data-v="${p.id}" aria-label="More for ${E(p.name)}" aria-expanded="${open}">${I("more", "i-sm")}</button>${open ? `<div class="menu" role="menu"><button class="mi" data-act="toast" data-msg="${E(p.name)}'s profile">${I("user")}${X("View profile", "عرض الملف", "Voir le profil")}</button><button class="mi" data-act="unfriend" data-v="${p.id}">${I("trash")}${X("Remove friend", "إزالة الصديق", "Retirer")}</button><button class="mi danger" data-act="m-block" data-v="${p.id}">${I("x")}${X("Block", "حظر", "Bloquer")}</button></div>` : ""}</div></div>
      <b class="f-name">${E(SJ.pname(p))}</b><span class="help">@${p.id} · ${X("Level", "المستوى", "Niveau")} ${p.level}</span><span class="f-status ${p.online ? "on" : ""}">${E(p.status)}</span>
      ${rec ? `<span class="f-rec">${X("Your record", "سجلكما", "Votre bilan")} <b class="num">${rec[0]} – ${rec[1]}</b></span>` : `<span class="f-rec">${X("No games together yet", "لم تلعبا معًا بعد", "Pas encore joué ensemble")}</span>`}
      <div class="row" style="gap:6px;width:100%"><button class="btn btn-sm btn-soft" style="flex:1" data-act="ch-with" data-v="${p.id}">${I("swords", "i-sm")}${X("Challenge", "تحدٍّ", "Défier")}</button><button class="btn btn-sm" style="flex:1" data-go="messages" data-with="${p.id}">${I("chat", "i-sm flip")}${X("Message", "مراسلة", "Message")}</button></div></article>`; };
    const pcard = (p, btn) => `<div class="li" style="padding:12px 0">${SJ.av(p, "", true)}<div class="grow"><div class="t">${E(SJ.pname(p))}</div><div class="s">@${p.id} · ${X("Level", "المستوى", "Niveau")} ${p.level}</div></div>${btn}</div>`;
    const addBtn = (p) => S.sent.includes(p.id) ? `<span class="badge">${X("Requested", "تم الطلب", "Demandé")}</span>` : `<button class="btn btn-sm" data-act="add-friend" data-v="${p.id}">${I("plus", "i-sm")}${X("Add", "إضافة", "Ajouter")}</button>`;
    return `<div class="page"><div class="page-head"><div><h1>${t("friends")}</h1><p class="sub">${X("Play together, compare records and send challenges.", "العبوا معًا وقارنوا نتائجكم وأرسلوا التحديات.", "Jouez ensemble, comparez vos bilans et lancez des défis.")}</p></div><button class="btn" data-act="invite">${I("link")}${X("Invite with a link", "ادعُ برابط", "Inviter par lien")}</button></div>
      <label class="input-icon" style="display:block;margin-bottom:20px">${I("search")}<span class="sr-only">Search</span><input class="input" id="f-search" value="${E(S.fq || "")}" placeholder="${X("Search friends or find people by name or @username", "ابحث في أصدقائك أو عن أشخاص بالاسم أو @المعرّف", "Chercher un ami ou une personne (nom ou @pseudo)")}" autocomplete="off"></label>
      <div class="cols"><div class="col-main" style="gap:22px">
        ${!q && online.length ? `<section><div class="label" style="margin-bottom:10px">${X("Online now", "متصلون الآن", "En ligne")}</div><div class="online-strip">${online.map((p) => `<button class="on-av" data-go="messages" data-with="${p.id}">${SJ.av(p, "lg", true)}<span>${E(SJ.pname(p).split(" ")[0])}</span></button>`).join("")}</div></section>` : ""}
        <section><div class="section-head" style="margin-bottom:12px"><h2>${X("Your friends", "أصدقاؤك", "Vos amis")} <span class="muted num" style="font-weight:600">${friends.length}</span></h2></div>
          ${friends.length ? `<div class="f-grid">${friends.map(fcard).join("")}</div>` : `<div class="card"><div class="empty"><p>${q ? X("None of your friends match.", "لا يطابق أي من أصدقائك.", "Aucun ami ne correspond.") : X("No friends yet.", "لا أصدقاء بعد.", "Pas encore d'amis.")}</p></div></div>`}</section>
        ${q ? `<section><div class="section-head" style="margin-bottom:8px"><h2>${X("People on Sirāj", "أشخاص على سراج", "Sur Sirāj")}</h2></div><div class="card pad" style="padding-block:4px">${found.map((p) => pcard(p, addBtn(p))).join("") || `<div class="empty"><p>${X("No one by that name. Invite them with a link.", "لا أحد بهذا الاسم. ادعه برابط.", "Personne de ce nom. Invitez-le par lien.")}</p></div>`}</div></section>` : ""}
      </div>
      <aside class="col-side">
        <section class="card pad"><div class="card-head"><h2>${X("Friend requests", "طلبات الصداقة", "Demandes d'ami")}</h2>${S.requests.length ? `<span class="count gold">${S.requests.length}</span>` : ""}</div>
          ${S.requests.length ? S.requests.map(SJ.person).map((p) => pcard(p, `<div class="row" style="gap:6px"><button class="icon-btn sm" style="border:1px solid var(--line)" data-act="req-no" data-v="${p.id}" aria-label="Decline">${I("x", "i-sm")}</button><button class="btn btn-sm btn-primary" data-act="req-yes" data-v="${p.id}">${X("Accept", "قبول", "Accepter")}</button></div>`)).join("") : `<p class="help">${X("No requests right now.", "لا طلبات الآن.", "Aucune demande.")}</p>`}</section>
        <section class="card pad"><div class="card-head"><h2>${X("People you played with", "لعبت معهم", "Joueurs rencontrés")}</h2></div><p class="help" style="margin-bottom:4px">${X("From your live rooms and duels", "من غرفك المباشرة ومبارزاتك", "De vos salons et duels")}</p>${suggest.map((p) => pcard(p, addBtn(p))).join("")}</section>
      </aside></div></div>`;
  };
  document.addEventListener("input", (e) => { if (e.target.id === "f-search") { S.fq = e.target.value; const pos = e.target.selectionStart; SJ.render(); const n = document.getElementById("f-search"); if (n) { n.focus(); n.setSelectionRange(pos, pos); } } });
  SJ.actions["f-menu"] = (a) => { S.fmenu = S.fmenu === a.dataset.v ? null : a.dataset.v; SJ.render(); };
  document.addEventListener("click", (e) => { if (S.fmenu && S.route === "friends" && !e.target.closest(".f-more")) { S.fmenu = null; SJ.render(); } }, true);
  SJ.actions["add-friend"] = (a) => { S.sent.push(a.dataset.v); SJ.render(); SJ.toast(X(`Friend request sent to ${SJ.pname(SJ.person(a.dataset.v))}.`, `أُرسل طلب صداقة إلى ${SJ.pname(SJ.person(a.dataset.v))}.`, `Demande envoyée à ${SJ.pname(SJ.person(a.dataset.v))}.`)); };
  SJ.actions["req-yes"] = (a) => { const id = a.dataset.v; S.requests = S.requests.filter((x) => x !== id); S.friends.push(id); SJ.render(); SJ.toast(X(`You and ${SJ.pname(SJ.person(id))} are now friends.`, `أصبحت أنت و${SJ.pname(SJ.person(id))} صديقين.`, `Vous êtes maintenant amis avec ${SJ.pname(SJ.person(id))}.`)); };
  SJ.actions["req-no"] = (a) => { S.requests = S.requests.filter((x) => x !== a.dataset.v); SJ.render(); };
  SJ.actions["unfriend"] = (a) => { S.fmenu = null; const p = SJ.person(a.dataset.v); SJ.modal(`<h2>${X(`Remove ${SJ.pname(p)}?`, `إزالة ${SJ.pname(p)}؟`, `Retirer ${SJ.pname(p)} ?`)}</h2><p>${X("They won't be told. Your past challenges stay in your history.", "لن يُبلّغ، وتبقى تحدياتكما السابقة في سجلك.", "Il ne sera pas prévenu. Vos anciens défis restent dans l'historique.")}</p><div class="actions"><button class="btn" data-act="close">${X("Cancel", "إلغاء", "Annuler")}</button><button class="btn btn-danger" data-act="unfriend-yes" data-v="${p.id}">${X("Remove friend", "إزالة الصديق", "Retirer")}</button></div>`); };
  SJ.actions["unfriend-yes"] = (a) => { S.friends = S.friends.filter((x) => x !== a.dataset.v); SJ.closeOverlay(); SJ.render(); };

  /* =====================================================================
     NOTIFICATIONS
     ===================================================================== */
  SJ.screens["notifications"] = () => {
    const f = S.nfilter || "all";
    const list = SJ.NOTIFS.filter((n) => f === "all" || n.unread);
    const unread = SJ.NOTIFS.filter((n) => n.unread).length;
    return `<div class="page narrow"><div class="page-head"><div><h1>${X("Notifications", "الإشعارات", "Notifications")}</h1><p class="sub">${unread ? X(`${unread} unread`, `${unread} غير مقروءة`, `${unread} non lues`) : X("You're all caught up.", "لا جديد.", "Vous êtes à jour.")}</p></div>
      <div class="row" style="gap:8px"><button class="btn btn-sm" data-act="n-all" ${unread ? "" : "disabled"}>${I("check", "i-sm")}${X("Mark all as read", "تعليم الكل كمقروء", "Tout marquer lu")}</button><button class="icon-btn sm" style="border:1px solid var(--line)" data-go="settings" aria-label="Notification settings">${I("sliders", "i-sm")}</button></div></div>
      <div class="seg" style="margin-bottom:16px">${[["all", X("All", "الكل", "Toutes")], ["unread", X("Unread", "غير المقروءة", "Non lues")]].map(([k, l]) => `<button data-act="n-filter" data-v="${k}" aria-pressed="${f === k}">${l}</button>`).join("")}</div>
      <section class="card">${list.map((n) => `<button class="li click notif ${n.unread ? "unread" : ""}" data-act="n-open" data-v="${n.id}"><span class="mode-ic ${{ challenge: "blue", friend: "green", room: "night", support: "gold", achievement: "gold", rank: "green", comment: "blue" }[n.kind]}" style="width:40px;height:40px">${I(n.icon, n.icon === "chat" ? "flip" : "")}</span><span class="grow" style="text-align:start"><span class="t" style="display:block;font-weight:${n.unread ? 700 : 500}">${E(n.text)}</span><span class="s">${E(n.time)}</span></span>${n.unread ? `<span class="udot" aria-label="Unread"></span>` : ""}</button>`).join("") || `<div class="empty"><div class="e-ic">${I("bell")}</div><h3>${X("No unread notifications", "لا إشعارات غير مقروءة", "Aucune notification non lue")}</h3></div>`}</section></div>`;
  };
  SJ.actions["n-filter"] = (a) => { S.nfilter = a.dataset.v; SJ.render(); };
  SJ.actions["n-all"] = () => { SJ.NOTIFS.forEach((n) => { n.unread = false; }); SJ.render(); };
  SJ.actions["n-open"] = (a) => { const n = SJ.NOTIFS.find((x) => x.id === +a.dataset.v); n.unread = false; SJ.go(n.go, n.code ? { code: n.code } : {}); };

  /* =====================================================================
     HISTORY
     ===================================================================== */
  SJ.screens["history"] = () => {
    const f = S.hfilter || "all";
    const modes = [["all", X("All", "الكل", "Tout")], ["solo", X("Solo", "فردي", "Solo")], ["live", t("liveRoom")], ["duel", X("Duels", "مبارزات", "Duels")], ["challenge", t("challenges")], ["device", X("This device", "هذا الجهاز", "Cet appareil")]];
    const icon = { solo: "bolt", live: "radio", duel: "swords", challenge: "send", device: "phone" };
    const list = SJ.HISTORY.filter((h) => f === "all" || h.mode === f);
    return `<div class="page mid"><div class="page-head"><div><h1>${X("History", "السجل", "Historique")}</h1><p class="sub">${X("Every game you played. Open one to review the answers.", "كل الألعاب التي لعبتها. افتح إحداها لمراجعة الإجابات.", "Toutes vos parties. Ouvrez-en une pour revoir les réponses.")}</p></div></div>
      <div class="stat-row" style="margin-bottom:18px"><div class="stat"><div class="k">${X("Games", "الألعاب", "Parties")}</div><div class="v">${SJ.ME.games}</div></div><div class="stat"><div class="k">${X("This week", "هذا الأسبوع", "Cette semaine")}</div><div class="v">23</div></div><div class="stat gold"><div class="k">${X("Best score", "أفضل نتيجة", "Meilleur score")}</div><div class="v">210</div></div><div class="stat"><div class="k">${X("Accuracy", "الدقة", "Précision")}</div><div class="v">${SJ.ME.accuracy}%</div></div></div>
      <div class="seg" style="margin-bottom:16px">${modes.map(([k, l]) => `<button data-act="h-filter" data-v="${k}" aria-pressed="${f === k}">${l}</button>`).join("")}</div>
      <section class="card">${list.map((h) => { const c = SJ.cat(h.cat); return `<div class="li" style="flex-wrap:wrap"><span class="mode-ic" style="width:42px;height:42px;background:color-mix(in srgb,${c.color} 15%,var(--surface));color:${c.color}">${I(icon[h.mode])}</span><div class="grow" style="flex:1 1 200px"><div class="t">${E(h.title)}</div><div class="s">${E(L(c.name))} · ${E(h.when)}</div></div><b class="num" style="min-width:90px">${E(h.score)}</b><span class="badge green num">+${h.pts}</span><button class="btn btn-sm btn-ghost" data-act="h-review">${X("Review", "مراجعة", "Revoir")}${I("chevR", "i-sm flip")}</button></div>`; }).join("") || `<div class="empty"><p>${X("No games of this kind yet.", "لا ألعاب من هذا النوع بعد.", "Aucune partie de ce type.")}</p></div>`}</section>
      <button class="btn btn-block" style="margin-top:14px">${X("Load older games", "تحميل ألعاب أقدم", "Charger plus")}</button></div>`;
  };
  SJ.actions["h-filter"] = (a) => { S.hfilter = a.dataset.v; SJ.render(); };
  SJ.actions["h-review"] = () => SJ.demoStart("results");

  /* =====================================================================
     MY QUESTIONS
     ===================================================================== */
  SJ.screens["my-questions"] = () => {
    const edit = S.editSet ? SJ.SETS.find((s) => s.id === S.editSet) : null;
    if (edit) {
      const n = edit.qs.length, ok = n >= MIN_Q;
      return `<div class="page mid"><button class="btn btn-ghost btn-sm" data-act="set-back" style="margin-bottom:12px">${I("arrowL", "flip")}${X("My questions", "أسئلتي", "Mes questions")}</button>
        <div class="page-head"><div><h1>${X("Edit set", "تعديل المجموعة", "Modifier la série")}</h1><p class="sub">${X("Private to you and the people you play with.", "خاصة بك وبمن تلعب معهم.", "Visible par vous et vos partenaires de jeu.")}</p></div><div class="row" style="gap:8px"><button class="btn" data-act="set-suggest" ${ok ? "" : "disabled"}>${I("send")}${X("Suggest to the bank", "اقترحها للبنك", "Proposer à la banque")}</button><button class="btn btn-primary" data-act="set-save">${ok ? X("Save set", "حفظ المجموعة", "Enregistrer") : X("Save as draft", "حفظ كمسودة", "Enregistrer le brouillon")}</button></div></div>
        <section class="card pad stack" style="margin-bottom:16px;gap:16px"><div class="form-grid"><div class="field"><label class="label" for="set-name">${X("Set name", "اسم المجموعة", "Nom de la série")}</label><input class="input" id="set-name" data-setf="name" value="${E(edit.name)}" maxlength="40"></div>
          <div class="field"><label class="label" for="set-lang">${X("Main language for new questions", "اللغة الأساسية للأسئلة الجديدة", "Langue principale des nouvelles questions")}</label><select class="select" id="set-lang" data-setf="lang">${LANGS.map(([k, l]) => `<option value="${k}" ${edit.lang === k ? "selected" : ""}>${l}</option>`).join("")}</select></div></div>
          ${minBar(n)}</section>
        ${n ? `<section class="card q-list" style="margin-bottom:12px">${edit.qs.map((w, i) => qrow(w, i, "set", edit.id)).join("")}</section>` : ""}
        ${addQBtn("set", edit.id)}</div>`;
    }
    return `<div class="page mid"><div class="page-head"><div><h1>${X("My questions", "أسئلتي", "Mes questions")}</h1><p class="sub">${X(`Write your own sets for family nights, classes or friends. A set needs at least ${MIN_Q} questions to play.`, `اكتب مجموعاتك لسهرات العائلة أو الدروس أو الأصدقاء. تحتاج المجموعة ${MIN_Q} أسئلة على الأقل للعب.`, `Créez vos séries pour la famille, la classe ou les amis. Il faut au moins ${MIN_Q} questions pour jouer.`)}</p></div><button class="btn btn-primary" data-act="set-new">${I("plus")}${X("New set", "مجموعة جديدة", "Nouvelle série")}</button></div>
      <div class="grid-auto wide">${SJ.SETS.map((s) => { const ok = s.qs.length >= MIN_Q, dis = ok ? "" : `disabled title="${X(`Needs ${MIN_Q - s.qs.length} more questions`, `تحتاج ${MIN_Q - s.qs.length} أسئلة أخرى`, `Encore ${MIN_Q - s.qs.length} questions`)}"`; return `<article class="card pad stack set-card" style="gap:12px"><div class="row"><span class="cat-ic" style="--c:#a16207">${I("pen")}</span><div style="flex:1;min-width:0"><b lang="${s.lang}" style="display:block">${E(s.name)}</b><span class="help">${s.qs.length} ${X("questions", "أسئلة", "questions")} · ${X("played", "لُعبت", "jouée")} ${s.plays}× · ${E(s.updated)}</span></div>${ok ? `<span class="badge green">${X("Ready", "جاهزة", "Prête")}</span>` : `<span class="badge gold">${X("Draft", "مسودة", "Brouillon")} · ${s.qs.length}/${MIN_Q}</span>`}</div>
        <div class="bar" style="height:5px"><i style="--v:${Math.min(100, (s.qs.length / MIN_Q) * 100)}%;--c:${ok ? "var(--brand)" : "var(--lamp)"}"></i></div>
        <div class="row wrap" style="gap:6px"><button class="btn btn-sm btn-primary" data-act="set-play" data-v="${s.id}" ${dis}>${I("tri", "i-sm flip")}${X("Play", "العب", "Jouer")}</button><button class="btn btn-sm" data-act="set-round" data-v="${s.id}" ${dis}>${I("users", "i-sm")}${X("Play with others", "العب مع آخرين", "Jouer à plusieurs")}</button><button class="btn btn-sm btn-ghost" data-act="set-edit" data-v="${s.id}">${I("pen", "i-sm")}${ok ? X("Edit", "تعديل", "Modifier") : X("Add questions", "أضف أسئلة", "Ajouter")}</button><span class="spacer"></span><button class="icon-btn sm" data-act="set-del" data-v="${s.id}" aria-label="${X("Delete set", "حذف المجموعة", "Supprimer la série")}">${I("trash", "i-sm")}</button></div></article>`; }).join("")}
        <button class="tile col dashed" data-act="set-new">${I("plus", "i-lg")}<span class="tt">${X("New set", "مجموعة جديدة", "Nouvelle série")}</span><span class="ts">${X(`${MIN_Q} questions or more`, `${MIN_Q} أسئلة أو أكثر`, `${MIN_Q} questions ou plus`)}</span></button></div>
      <div class="banner info" style="margin-top:20px"><div class="grow">${X("<b>Wrote a great question?</b> Suggest the set to the bank. The content team reviews it and it can reach every player.", "<b>كتبت سؤالًا رائعًا؟</b> اقترح المجموعة لبنك الأسئلة، يراجعها فريق المحتوى وقد تصل إلى كل اللاعبين.", "<b>Une super question ?</b> Proposez la série à la banque : l'équipe la relit et elle peut toucher tous les joueurs.")}</div></div></div>`;
  };
  document.addEventListener("input", (e) => { const k = e.target.dataset && e.target.dataset.setf; if (!k || !S.editSet) return; const s = SJ.SETS.find((x) => x.id === S.editSet); if (s) s[k] = e.target.value; });
  document.addEventListener("change", (e) => { const k = e.target.dataset && e.target.dataset.setf; if (!k || !S.editSet) return; const s = SJ.SETS.find((x) => x.id === S.editSet); if (s) s[k] = e.target.value; });
  SJ.actions["set-new"] = () => { const id = "s" + Date.now(); SJ.SETS.unshift({ id, name: X("New set", "مجموعة جديدة", "Nouvelle série"), lang: S.lang, plays: 0, updated: X("Just now", "الآن", "À l'instant"), qs: [] }); S.editSet = id; SJ.render(); window.scrollTo(0, 0); SJ.actions["qe-open"]({ dataset: { ns: "set", set: id, v: "-1" } }); };
  SJ.actions["set-edit"] = (a) => { S.editSet = a.dataset.v; SJ.render(); window.scrollTo(0, 0); };
  SJ.actions["set-back"] = () => { S.editSet = null; SJ.render(); };
  SJ.actions["set-save"] = () => { const s = SJ.SETS.find((x) => x.id === S.editSet); s.name = (s.name || "").trim() || X("Untitled set", "مجموعة بلا اسم", "Série sans nom"); s.updated = X("Just now", "الآن", "À l'instant"); const n = s.qs.length; S.editSet = null; SJ.render(); SJ.toast(n >= MIN_Q ? X("Set saved.", "حُفظت المجموعة.", "Série enregistrée.") : X(`Saved as a draft. Add ${MIN_Q - n} more to play it.`, `حُفظت كمسودة. أضف ${MIN_Q - n} للعب بها.`, `Brouillon enregistré. Encore ${MIN_Q - n} pour jouer.`)); };
  SJ.actions["set-suggest"] = () => SJ.toast(X("Sent to the content team's review queue.", "أُرسلت إلى قائمة مراجعة فريق المحتوى.", "Envoyée à la file de relecture."));
  SJ.actions["set-del"] = (a) => { const i = SJ.SETS.findIndex((x) => x.id === a.dataset.v); SJ.SETS.splice(i, 1); SJ.render(); SJ.toast(X("Set deleted.", "حُذفت المجموعة.", "Série supprimée.")); };
  SJ.actions["set-play"] = (a) => { const s = SJ.SETS.find((x) => x.id === a.dataset.v); if (s.qs.length < MIN_Q) { SJ.toast(X(`Add ${MIN_Q - s.qs.length} more questions first.`, `أضف ${MIN_Q - s.qs.length} أسئلة أولًا.`, `Ajoutez d'abord ${MIN_Q - s.qs.length} questions.`)); return; } s.plays++; const qs = customQs(s.qs, s.name); SJ.startGame("solo", qs, Object.assign({}, S.setup, { count: qs.length, timer: 20, title: s.name }), true); };
  SJ.actions["set-round"] = (a) => { S.nr = null; S.params = {}; const o = nr(); o.src = "sets"; o.set = a.dataset.v; SJ.go("challenge-new"); };

  /* =====================================================================
     CONTACT US
     ===================================================================== */
  const TOPICS = () => [
    ["broken", X("Something is broken", "هناك عطل", "Quelque chose ne marche pas"), "alert", X("Errors, freezes, things not working", "أخطاء أو تجمّد أو ميزة لا تعمل", "Erreurs, blocages, bugs"), X("technical support", "الدعم التقني", "support technique")],
    ["question", X("Question", "سؤال", "Question"), "info", X("A quiz question, or how something works", "سؤال في المسابقة أو طريقة عمل شيء", "Une question du quiz ou un fonctionnement"), X("content", "المحتوى", "contenu")],
    ["account", X("My account", "حسابي", "Mon compte"), "user", X("Sign-in, email, membership", "الدخول أو البريد أو العضوية", "Connexion, e-mail, abonnement"), X("account", "الحسابات", "comptes")],
    ["report", X("Report a player", "الإبلاغ عن لاعب", "Signaler un joueur"), "flag", X("Cheating, abuse, spam", "غش أو إساءة أو إزعاج", "Triche, abus, spam"), X("moderation", "الإشراف", "modération")],
    ["suggestion", X("Suggestion", "اقتراح", "Suggestion"), "star", X("Ideas to improve Sirāj", "أفكار لتحسين سراج", "Idées pour améliorer Sirāj"), X("product", "المنتج", "produit")],
    ["other", X("Something else", "شيء آخر", "Autre chose"), "chat", X("Anything not listed", "أي شيء غير مذكور", "Tout le reste"), X("support", "الدعم", "support")]
  ];
  function device() {
    const ua = navigator.userAgent || "";
    const br = /Edg\//.test(ua) ? "Edge" : /Chrome\/(\d+)/.test(ua) ? "Chrome " + ua.match(/Chrome\/(\d+)/)[1] : /Firefox\/(\d+)/.test(ua) ? "Firefox " + ua.match(/Firefox\/(\d+)/)[1] : /Safari/.test(ua) ? "Safari" : "Browser";
    const os = /Windows NT 10/.test(ua) ? "Windows 11" : /Mac OS X/.test(ua) ? "macOS" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Linux/.test(ua) ? "Linux" : "";
    return `${br}${os ? " · " + os : ""} · App v2.4.1`;
  }
  SJ.screens["support"] = () => {
    const sp = S.sp || (S.sp = { topic: null, sent: false, files: [], device: true, draft: false, f: {} });
    const tps = TOPICS();
    if (sp.sent) { const tp = tps.find((x) => x[0] === sp.topic); return `<div class="page narrow"><section class="card pad" style="text-align:center;padding:44px 24px;border-radius:var(--r-xl)"><div style="margin:0 auto 16px;width:68px;height:68px;border-radius:50%;display:grid;place-items:center;background:var(--brand-soft);color:var(--brand)">${I("check", "i-lg")}</div>
      <h1 style="font-size:28px;font-weight:800">${X("Message sent", "أُرسلت رسالتك", "Message envoyé")}</h1><p class="muted" style="margin:10px auto 0;max-width:46ch">${X(`Thanks. Your message reached the ${tp[4]} team. We usually reply within 24 hours, by email and in your inbox here.`, `شكرًا. وصلت رسالتك إلى فريق ${tp[4]}. نرد عادة خلال 24 ساعة بالبريد وفي صندوق رسائلك هنا.`, `Merci. Votre message est arrivé à l'équipe ${tp[4]}. Nous répondons en général sous 24 h, par e-mail et dans votre messagerie.`)}</p>
      <div class="ticket"><div><span class="help">${X("Ticket", "التذكرة", "Ticket")}</span><b class="num">#T-2092</b></div><div><span class="help">${X("Topic", "الموضوع", "Sujet")}</span><b>${E(tp[1])}</b></div></div>
      <div class="row" style="justify-content:center;gap:10px;margin-top:22px"><button class="btn" data-act="sp-inbox">${X("View my messages", "عرض رسائلي", "Voir mes messages")}</button><button class="btn btn-primary" data-act="sp-new">${X("Send another", "أرسل رسالة أخرى", "En envoyer un autre")}</button></div></section></div>`; }
    const tp = sp.topic, f = sp.f;
    const req = '<span class="req" aria-hidden="true">*</span>';
    const catSel = `<div class="field"><label class="label" for="sp-cat">${X("Category", "الفئة", "Catégorie")} <span class="muted" style="font-weight:500">(${X("optional", "اختياري", "facultatif")})</span></label><select class="select" id="sp-cat" data-sp="cat"><option value="">${X("Not about a specific category", "ليس عن فئة محددة", "Pas une catégorie précise")}</option>${SJ.myDoms().map((d) => `<optgroup label="${E(L(d.name))}">${SJ.CATS.filter((c) => c.domain === d.id).map((c) => `<option value="${c.id}" ${f.cat === c.id ? "selected" : ""}>${E(L(c.name))}</option>`).join("")}</optgroup>`).join("")}</select></div>`;
    let extra = "";
    if (tp === "broken" || tp === "suggestion") extra = catSel;
    if (tp === "question") extra = `<div class="form-grid">${catSel}<div class="field"><label class="label" for="sp-qn">${X("Question number", "رقم السؤال", "Numéro de question")} <span class="muted" style="font-weight:500">(${X("optional", "اختياري", "facultatif")})</span></label><input class="input" id="sp-qn" data-sp="qn" value="${E(f.qn || "")}" placeholder="#604"><span class="help">${X("Shown under each question after you answer", "يظهر تحت كل سؤال بعد الإجابة", "Affiché sous chaque question après réponse")}</span></div></div>`;
    if (tp === "account") extra = `<div class="field"><label class="label" for="sp-acc">${X("Account email or username", "بريد الحساب أو المعرّف", "E-mail ou pseudo du compte")} ${req}</label><input class="input" id="sp-acc" data-sp="acc" value="${E(f.acc != null ? f.acc : SJ.signedIn() ? "aisha_t" : "")}"><span class="help">${I("lock", "i-sm")} ${X("Never send your password. We will never ask for it.", "لا ترسل كلمة المرور أبدًا، ولن نطلبها منك.", "N'envoyez jamais votre mot de passe. Nous ne le demanderons jamais.")}</span></div>`;
    if (tp === "report") extra = `<div class="form-grid"><div class="field"><label class="label" for="sp-u">${X("Player username", "معرّف اللاعب", "Pseudo du joueur")} ${req}</label><input class="input" id="sp-u" data-sp="u" value="${E(f.u || "")}" placeholder="knight_42"></div>
      <div class="field"><label class="label" for="sp-r">${X("Reason", "السبب", "Motif")} ${req}</label><select class="select" id="sp-r" data-sp="r">${[X("Cheating", "غش", "Triche"), X("Offensive name", "اسم مسيء", "Pseudo offensant"), X("Abuse in chat", "إساءة في الدردشة", "Insultes dans le chat"), X("Spam", "إزعاج", "Spam"), X("Other", "أخرى", "Autre")].map((r) => `<option ${f.r === r ? "selected" : ""}>${r}</option>`).join("")}</select></div></div>
      <div class="field"><label class="label" for="sp-link">${X("Game link or room code", "رابط اللعبة أو رمز الغرفة", "Lien de partie ou code du salon")} <span class="muted" style="font-weight:500">(${X("optional", "اختياري", "facultatif")})</span></label><input class="input" id="sp-link" data-sp="link" value="${E(f.link || "")}" placeholder="${X("Paste it so we can review the game", "ألصقه لنراجع اللعبة", "Collez-le pour que nous revoyions la partie")}"></div>`;
    const sub = f.sub || "", msg = f.msg || "";
    const valid = sub.trim().length >= 3 && msg.trim().length >= 20 && (tp !== "report" || (f.u || "").trim()) && (tp !== "account" || ((f.acc != null ? f.acc : SJ.signedIn() ? "aisha_t" : "") + "").trim()) && (SJ.signedIn() || /^\S+@\S+\.\S+$/.test(f.mail || ""));
    return `<div class="page narrow"><div class="page-head"><div><h1>${X("New message", "رسالة جديدة", "Nouveau message")}</h1><p class="sub row" style="gap:6px;color:var(--brand-ink)">${I("clock", "i-sm")}${X("We usually reply within 24 hours.", "نرد عادة خلال 24 ساعة.", "Nous répondons en général sous 24 h.")}</p></div></div>
      <section class="card pad" style="border-radius:var(--r-xl)">
        <div class="label" style="margin-bottom:12px">${X("What is this about?", "ما موضوع رسالتك؟", "De quoi s'agit-il ?")} ${req}</div>
        <div class="topics" role="radiogroup">${tps.map(([k, l, ic, d]) => `<button class="topic ${tp === k ? "on" : ""}" role="radio" aria-checked="${tp === k}" data-act="sp-topic" data-v="${k}">${I(ic)}${tp === k ? `<span class="tk">${I("check", "i-sm")}</span>` : ""}<b>${l}</b><span>${d}</span></button>`).join("")}</div>
        ${!tp ? `<div class="sp-hint">${X("Choose a topic above and we'll show only the fields you need.", "اختر موضوعًا أعلاه وسنعرض الحقول التي تحتاجها فقط.", "Choisissez un sujet : nous n'afficherons que les champs utiles.")}</div>` : `<form data-form="support2" novalidate style="margin-top:22px;display:flex;flex-direction:column;gap:16px">
          ${extra}
          ${SJ.signedIn() ? "" : `<div class="field"><label class="label" for="sp-mail">${X("Your email, so we can reply", "بريدك لنرد عليك", "Votre e-mail pour la réponse")} ${req}</label><input class="input" id="sp-mail" type="email" data-sp="mail" value="${E(f.mail || "")}"></div>`}
          <div class="field" style="margin:0"><div class="row"><label class="label" for="sp-sub">${X("Subject", "الموضوع", "Objet")} ${req}</label><span class="spacer"></span><span class="help num" id="sp-sub-c">${sub.length} / 80</span></div><input class="input" id="sp-sub" data-sp="sub" maxlength="80" value="${E(sub)}"></div>
          <div class="field" style="margin:0"><div class="row"><label class="label" for="sp-msg">${X("Message", "الرسالة", "Message")} ${req}</label><span class="spacer"></span><span class="help num" id="sp-msg-c">${msg.length} ${X("characters", "حرفًا", "caractères")}</span></div><textarea class="textarea" id="sp-msg" data-sp="msg" maxlength="2000" placeholder="${tp === "broken" ? X("What happened, and what did you expect?", "ماذا حدث، وماذا كنت تتوقع؟", "Que s'est-il passé, et qu'attendiez-vous ?") : ""}">${E(msg)}</textarea><span class="help" id="sp-msg-h">${msg.trim().length < 20 ? X(`At least 20 characters (${20 - msg.trim().length} to go)`, `20 حرفًا على الأقل (باقي ${20 - msg.trim().length})`, `Au moins 20 caractères (encore ${20 - msg.trim().length})`) : ""}</span></div>
          ${tp === "broken" || tp === "report" ? `<div class="dropzone" data-drop="1"><span class="mode-ic green" style="width:40px;height:40px">${I("upload")}</span><div style="flex:1"><b>${X("Add screenshots or a recording", "أضف لقطات شاشة أو تسجيلًا", "Ajoutez des captures ou une vidéo")}</b><div class="help">${X("Drag files here or browse · PNG, JPG, MP4 · up to 10 MB", "اسحب الملفات هنا أو تصفّح · PNG وJPG وMP4 · حتى 10 ميغابايت", "Glissez des fichiers ou parcourez · PNG, JPG, MP4 · 10 Mo max")}</div></div><label class="btn btn-sm">${X("Browse", "تصفّح", "Parcourir")}<input type="file" class="sr-only" multiple accept="image/png,image/jpeg,video/mp4" data-act="sp-files"></label></div>
            ${sp.files.length ? `<div class="row wrap" style="gap:8px">${sp.files.map((fl, i) => `<span class="draft">${I(fl.video ? "play" : "image", "i-sm")}<b>${E(fl.name)}</b><span class="muted">${E(fl.size)}</span><button type="button" class="x" data-act="sp-file-x" data-v="${i}" aria-label="Remove ${E(fl.name)}">${I("x", "i-sm")}</button></span>`).join("")}</div>` : ""}` : ""}
          ${tp === "broken" && sp.device ? `<div class="device-chip">${I("monitor", "i-sm")}<span style="flex:1">${E(device())} · ${X("attached automatically", "مرفقة تلقائيًا", "joint automatiquement")}</span><button type="button" class="link-btn" style="font-size:13px" data-act="sp-dev-x">${X("Remove", "إزالة", "Retirer")}</button></div>` : ""}
          <div class="row" style="border-top:1px solid var(--line);padding-top:16px;margin-top:4px"><span class="help row" style="gap:6px" id="sp-draft">${sp.draft ? I("check", "i-sm") + X("Draft saved", "حُفظت المسودة", "Brouillon enregistré") : ""}</span><span class="spacer"></span><button type="button" class="btn" data-act="sp-cancel">${X("Cancel", "إلغاء", "Annuler")}</button><button class="btn btn-primary" type="submit" id="sp-send" ${valid ? "" : "disabled"}>${X("Send message", "أرسل الرسالة", "Envoyer")}${I("arrowL", "flip-r")}</button></div>
        </form>`}</section></div>`;
  };
  SJ.actions["sp-topic"] = (a) => { S.sp.topic = a.dataset.v; SJ.render(); };
  SJ.actions["sp-new"] = () => { S.sp = null; SJ.render(); };
  SJ.actions["sp-cancel"] = () => { S.sp = null; SJ.go("home"); };
  SJ.actions["sp-inbox"] = () => { S.sp = null; const s = msgState(); s.tab = "people"; s.open = "team"; SJ.go("messages"); };
  SJ.actions["sp-dev-x"] = () => { S.sp.device = false; SJ.render(); };
  SJ.actions["sp-file-x"] = (a) => { S.sp.files.splice(+a.dataset.v, 1); SJ.render(); };
  function addFiles(list) {
    Array.from(list || []).forEach((fl) => {
      if (fl.size > 10e6) { SJ.toast(X(`${fl.name} is over 10 MB.`, `${fl.name} أكبر من 10 ميغابايت.`, `${fl.name} dépasse 10 Mo.`)); return; }
      S.sp.files.push({ name: fl.name, size: fl.size > 1e6 ? (fl.size / 1e6).toFixed(1) + " MB" : Math.max(1, Math.round(fl.size / 1e3)) + " KB", video: /video/.test(fl.type) });
    });
    SJ.render();
  }
  SJ.changes["sp-files"] = (a) => addFiles(a.files);
  document.addEventListener("dragover", (e) => { const z = e.target.closest && e.target.closest("[data-drop]"); if (z) { e.preventDefault(); z.classList.add("over"); } });
  document.addEventListener("dragleave", (e) => { const z = e.target.closest && e.target.closest("[data-drop]"); if (z) z.classList.remove("over"); });
  document.addEventListener("drop", (e) => { const z = e.target.closest && e.target.closest("[data-drop]"); if (z) { e.preventDefault(); addFiles(e.dataTransfer.files); } });
  let draftTimer = null;
  document.addEventListener("input", (e) => {
    const k = e.target.dataset && e.target.dataset.sp; if (!k || !S.sp) return;
    S.sp.f[k] = e.target.value;
    if (k === "sub") document.getElementById("sp-sub-c").textContent = e.target.value.length + " / 80";
    if (k === "msg") { const n = e.target.value.trim().length; document.getElementById("sp-msg-c").textContent = e.target.value.length + " " + X("characters", "حرفًا", "caractères"); document.getElementById("sp-msg-h").textContent = n < 20 ? X(`At least 20 characters (${20 - n} to go)`, `20 حرفًا على الأقل (باقي ${20 - n})`, `Au moins 20 caractères (encore ${20 - n})`) : ""; }
    const f = S.sp.f, acc = (f.acc != null ? f.acc : SJ.signedIn() ? "aisha_t" : "") + "";
    const valid = (f.sub || "").trim().length >= 3 && (f.msg || "").trim().length >= 20 && (S.sp.topic !== "report" || (f.u || "").trim()) && (S.sp.topic !== "account" || acc.trim()) && (SJ.signedIn() || /^\S+@\S+\.\S+$/.test(f.mail || ""));
    const b = document.getElementById("sp-send"); if (b) b.disabled = !valid;
    clearTimeout(draftTimer); draftTimer = setTimeout(() => { if (!S.sp) return; S.sp.draft = true; const d = document.getElementById("sp-draft"); if (d) d.innerHTML = I("check", "i-sm") + X("Draft saved", "حُفظت المسودة", "Brouillon enregistré"); }, 700);
  });
  document.addEventListener("change", (e) => { const k = e.target.dataset && e.target.dataset.sp; if (k && S.sp) S.sp.f[k] = e.target.value; });
  SJ.forms["support2"] = () => { S.sp.sent = true; const s = msgState(); const tp = TOPICS().find((x) => x[0] === S.sp.topic); (s.threads.find((x) => x.id === "team") || { msgs: [] }).msgs.push({ from: "team", type: "text", text: X(`We got your message about “${S.sp.f.sub}”. Ticket #T-2092.`, `وصلتنا رسالتك بخصوص «${S.sp.f.sub}». التذكرة #T-2092.`, `Nous avons reçu votre message « ${S.sp.f.sub} ». Ticket #T-2092.`), time: X("Now", "الآن", "Maintenant"), day: X("Today", "اليوم", "Aujourd'hui") }); SJ.render(); window.scrollTo(0, 0); };
})();
