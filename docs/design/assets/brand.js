/* Sirāj v2.1 — multi-domain brand layer.
   - Neutral shell with lamp-gold brand; the domain in focus re-colours the UI (game, results, setup, domain page, lobby, leaderboard).
   - Islamic geometric motif only inside the Islamic family; other domains get their own icon motif.
   - Home: "Your domains" world cards + featured flagship. New domain landing page.
   - Interests + "Islamic content only" mode (onboarding, settings, prototype panel). */
(function () {
  "use strict";
  const SJ = window.SJ, S = SJ.S, I = SJ.icon, E = SJ.esc, t = SJ.t, L = SJ.L, X = SJ.tx;

  /* ---------------- content mode + interests ---------------- */
  SJ.BASE_DOMS = SJ.DOMAINS.filter((d) => !d.sim).slice();
  S.islamicOnly = SJ.store.get("islamicOnly", false);
  S.interests = SJ.store.get("interests", ["islamic", "arabic", "sport", "geography"]);
  const isIslamic = (d) => !!d && (d.family === "islamic" || d.sim);
  const baseDom = (id) => SJ.BASE_DOMS.find((d) => d.id === id);
  const visCats = (id) => SJ.CATS.filter((c) => !c.hidden && c.domain === id);
  const qCount = (id) => visCats(id).reduce((a, c) => a + (SJ.COUNT[c.id] || 0), 0);
  const mastery = (id) => { const cs = visCats(id); return cs.length ? Math.round(cs.reduce((a, c) => a + (c.mastery || 0), 0) / cs.length) : 0; };
  const PLAYERS = { islamic: 1284, arabic: 312, sport: 946, geography: 618, business: 207, science: 391 };
  const FACTOR = { islamic: 0.82, arabic: 0.18, sport: 0.34, geography: 0.41, business: 0.12, science: 0.22 };
  const dname = (d) => L(d.name);
  /* The whole app works inside "Your domains" (S.interests). Explore / Add sheets are the only place showing every domain. */
  SJ.myDoms = () => { const m = SJ.DOMAINS.filter((d) => (S.interests || []).includes(d.id)); return m.length ? m : SJ.DOMAINS.slice(0, 1); };
  SJ.isMine = (id) => SJ.myDoms().some((d) => d.id === id);
  SJ.myCats = () => { const ids = SJ.myDoms().map((d) => d.id); return SJ.CATS.filter((c) => !c.hidden && ids.includes(c.domain)); };

  function restore() {
    const sims = SJ.DOMAINS.filter((d) => d.sim);
    SJ.DOMAINS.splice(0, SJ.DOMAINS.length, ...SJ.BASE_DOMS, ...sims);
    SJ.CATS.forEach((c) => { if (c._off) { c.hidden = false; delete c._off; } });
  }
  function filter() {
    if (!S.islamicOnly) return;
    SJ.DOMAINS.splice(0, SJ.DOMAINS.length, ...SJ.DOMAINS.filter(isIslamic));
    SJ.CATS.forEach((c) => { const d = baseDom(c.domain); if (d && !isIslamic(d) && !c.hidden) { c.hidden = true; c._off = true; } });
    if (!SJ.dom(S.domain)) S.domain = "islamic";
    [S.setup, S.nr].forEach((s) => { if (s && s.domain !== "all" && !SJ.dom(s.domain)) { s.domain = "islamic"; s.cats = []; } });
    if (S.lb && S.lb.domain !== "all" && !SJ.dom(S.lb.domain)) S.lb.domain = "all";
    S.recentDoms = (S.recentDoms || []).filter((x) => SJ.dom(x)); if (!S.recentDoms.length) S.recentDoms = SJ.DOMAINS.slice(0, 3).map((d) => d.id);
  }
  SJ.applyContent = () => {
    restore(); filter();
    SJ.QOTD = S.islamicOnly ? SJ.QOTD_ISLAMIC : SJ.QOTD_ALL; S.qotd.picked = null;
    S.tpView = {};
  };
  SJ.setIslamicOnly = (on) => { S.islamicOnly = !!on; SJ.store.set("islamicOnly", S.islamicOnly); SJ.applyContent(); };
  const origScale = SJ.setDomainScale;
  SJ.setDomainScale = (n) => { restore(); origScale(n); filter(); SJ.render(); };
  SJ.applyContent();

  SJ.actions["proto-isl"] = (a) => { SJ.setIslamicOnly(a.dataset.v === "1"); SJ.render(); SJ.toast(S.islamicOnly ? X("Islamic content only. Other domains are hidden.", "محتوى إسلامي فقط. أُخفيت المجالات الأخرى.", "Contenu islamique uniquement.") : X("All domains are back.", "عادت كل المجالات.", "Tous les domaines sont de retour.")); };
  SJ.changes["isl-toggle"] = (a) => { SJ.setIslamicOnly(a.checked); SJ.render(); };
  SJ.actions["interest"] = (a) => {
    const v = a.dataset.v, cur = S.interests;
    S.interests = cur.includes(v) ? cur.filter((x) => x !== v) : cur.concat(v);
    if (!S.interests.length) S.interests = [v];
    SJ.store.set("interests", S.interests);
    if (document.querySelector(".modal")) { const box = document.getElementById("int-box"); if (box) box.innerHTML = interestChips(); SJ.render(); return; }
    SJ.render();
  };
  const interestChips = () => SJ.BASE_DOMS.map((d) => {
    const off = S.islamicOnly && !isIslamic(d);
    return `<button class="ichip" style="--d:${d.color}" data-act="interest" data-v="${d.id}" aria-pressed="${S.interests.includes(d.id) && !off}" ${off ? "disabled" : ""}><span class="ci">${I(d.icon)}</span>${E(dname(d))}</button>`;
  }).join("");
  /* Manage = remove domains from home (adding happens in the "Add a domain" sheet) */
  const manageRows = () => {
    const mine = SJ.DOMAINS.filter((d) => S.interests.includes(d.id));
    return mine.map((d) => `<div class="mg-row"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span class="grow"><b>${E(dname(d))}</b><span class="help" style="display:block">${visCats(d.id).length} ${X("categories", "فئات", "catégories")} · ${qCount(d.id)} ${X("questions", "سؤالًا", "questions")}</span></span><button class="icon-btn sm" data-act="interest-rm" data-v="${d.id}" ${mine.length < 2 ? "disabled" : ""} aria-label="${X("Remove", "إزالة", "Retirer")} ${E(dname(d))}">${I("x")}</button></div>`).join("");
  };
  SJ.actions["interests-edit"] = () => SJ.modal(`<div class="m-ic">${I("sliders", "i-lg")}</div><h2>${X("Manage your domains", "إدارة مجالاتك", "Gérer vos domaines")}</h2><p>${X("These appear on Home and in “Jump into a category”. Keep at least one.", "تظهر في الرئيسية وفي «ادخل إلى فئة». أبقِ مجالًا واحدًا على الأقل.", "Ils apparaissent sur l'accueil et dans « Aller à une catégorie ». Gardez-en au moins un.")}</p>
    <div class="mg-list" id="mg-box">${manageRows()}</div>
    <div class="actions"><button class="btn" data-act="dom-sheet" data-ns="add">${I("plus")}${X("Add a domain", "أضف مجالًا", "Ajouter un domaine")}</button><button class="btn btn-primary" data-act="close">${X("Done", "تم", "Terminé")}</button></div>`);
  SJ.actions["interest-rm"] = (a) => {
    const v = a.dataset.v; if (S.interests.length < 2) return;
    S.interests = S.interests.filter((x) => x !== v); SJ.store.set("interests", S.interests);
    const box = document.getElementById("mg-box"); if (box) box.innerHTML = manageRows();
    SJ.render();
  };
  S.homeDom = SJ.store.get("homeDom", "mine");
  SJ.actions["home-dom"] = (a) => { S.homeDom = a.dataset.v; SJ.store.set("homeDom", S.homeDom); if (S.homeDom !== "mine") SJ.touchDom && SJ.touchDom(S.homeDom); SJ.render(); };

  /* ---------------- theme the screen by the domain in focus ---------------- */
  function focusDomain(r) {
    const g = S.game;
    if (["game", "live", "duel-game"].includes(r) && g && g.qs && g.qs[g.i]) return SJ.cat(g.qs[g.i].cat).domain;
    if (r === "results" && g && g.qs && g.qs.length) { const ds = [...new Set(g.qs.map((q) => SJ.cat(q.cat).domain))]; return ds.length === 1 ? ds[0] : null; }
    if (r === "setup" || r === "room-create") return S.setup.domain;
    if ((r === "lobby" || r === "podium") && S.room && S.room.st) return S.room.st.domain;
    if (r === "domain") return (S.params && S.params.id) || S.domain;
    if (r === "play" || r === "duel") return S.domain;
    if (r === "leaderboard" && S.lb && S.lb.domain !== "all") return S.lb.domain;
    return null;
  }
  SJ.beforeRender = (r) => {
    SJ.keepMine && SJ.keepMine(); SJ.keepQotd && SJ.keepQotd();
    const root = document.documentElement, id = focusDomain(r), d = id && id !== "all" ? SJ.dom(id) : null;
    if (d) { root.dataset.dom = d.id; root.style.setProperty("--d", d.color); root.dataset.family = isIslamic(d) ? "islamic" : "general"; }
    else { delete root.dataset.dom; root.style.removeProperty("--d"); delete root.dataset.family; }
    SJ.T.greeting = S.islamicOnly ? SJ.T.greetingIslamic : { en: "Welcome back", ar: "أهلًا بعودتك", fr: "Bon retour" };
  };

  /* ---------------- shared pieces ---------------- */
  SJ.domChip = (d) => `<span class="dom-chip">${I(d.icon)}${E(dname(d))}</span>`;
  SJ.domHero = (d) => {
    const fam = isIslamic(d), cats = visCats(d.id).length, n = qCount(d.id), g = SJ.isGuest();
    return `<section class="dhero ${fam ? "fam-islamic" : ""}" style="--d:${d.color}" aria-labelledby="dh-${d.id}">${SJ.PATTERN("dh-pt-" + d.id)}${I(d.motif || d.icon, "motif")}
      <div class="in"><div class="dh-copy"><span class="dh-ic">${I(d.icon)}</span>
        <div class="row wrap" style="gap:8px">${d.featured ? `<span class="badge gold">${I("star", "i-sm")}${X("Flagship", "المجال الرئيسي", "Domaine phare")}</span>` : ""}${d.isNew ? `<span class="badge gold">${X("New", "جديد", "Nouveau")}</span>` : ""}</div>
        <h1 id="dh-${d.id}">${E(dname(d))}</h1><p>${E(L(d.desc))}</p>
        <div class="dh-stats"><span><b>${cats}</b>${X("categories", "فئات", "catégories")}</span><span><b>${n}</b>${X("questions", "سؤالًا", "questions")}</span>${g ? "" : `<span><b>${mastery(d.id)}%</b>${X("your mastery", "إتقانك", "votre maîtrise")}</span>`}<span><b>${(PLAYERS[d.id] || 48).toLocaleString()}</b>${X("players this week", "لاعبًا هذا الأسبوع", "joueurs cette semaine")}</span></div></div>
        <div class="dh-act"><button class="btn btn-lg btn-onhero" data-act="quick">${I("tri", "flip")}${t("quick")} · 5</button><button class="btn btn-lg btn-hero-ghost" data-act="${g ? "duel" : "duel"}">${I("swords")}${t("duel")}</button><button class="btn btn-lg btn-hero-ghost" data-act="custom">${I("sliders")}${X("Custom", "مخصّصة", "Personnalisée")}</button></div></div></section>`;
  };
  function worldCard(d, featured) {
    const n = qCount(d.id), cats = visCats(d.id).length, m = mastery(d.id), g = SJ.isGuest();
    return `<button class="world ${featured ? "featured" : ""} ${isIslamic(d) ? "fam-islamic" : ""}" style="--d:${d.color}" data-go="domain" data-id="${d.id}">${featured && isIslamic(d) ? SJ.PATTERN("w-pt-" + d.id) : I(d.motif || d.icon, "w-motif")}
      ${d.isNew ? `<span class="badge gold w-badge">${X("New", "جديد", "Nouveau")}</span>` : featured ? `<span class="badge gold w-badge">${I("star", "i-sm")}${X("Flagship", "الرئيسي", "Phare")}</span>` : ""}
      <span class="w-ic">${I(d.icon)}</span>
      <span><span class="w-name" style="display:block">${E(dname(d))}</span><span class="w-sub">${featured ? E(L(d.desc)) + " · " : ""}${cats} ${X("categories", "فئات", "catégories")} · ${n} ${X("questions", "سؤالًا", "questions")}</span></span>
      <span class="w-foot">${g ? `<span class="w-sub" style="flex:1">${(PLAYERS[d.id] || 48).toLocaleString()} ${X("playing this week", "يلعبون هذا الأسبوع", "joueurs cette semaine")}</span>${I("chevR", "i-sm flip")}` : `<span class="bar"><i style="--v:${m}%;--c:${featured ? "#fff" : d.color}"></i></span><b>${m}%</b>`}</span></button>`;
  }
  /* Home card: same look as worldCard + its categories as chips (tap = start that category) */
  function homeCard(d, featured) {
    const cs = visCats(d.id), n = qCount(d.id), m = mastery(d.id), g = SJ.isGuest(), max = featured ? 6 : 3;
    const chips = cs.slice(0, max).map((c) => `<button class="w-chip" data-act="pick-cat" data-v="${c.id}" title="${X("Play", "العب", "Jouer")} ${E(L(c.name))}">${I(c.icon || d.icon, "i-sm")}<span>${E(L(c.name))}</span></button>`).join("")
      + (cs.length > max ? `<button class="w-chip more" data-go="domain" data-id="${d.id}">+${cs.length - max}</button>` : "");
    return `<div class="world home ${featured ? "featured" : ""} ${isIslamic(d) ? "fam-islamic" : ""}" style="--d:${d.color}">${featured && isIslamic(d) ? SJ.PATTERN("w-pt-" + d.id) : I(d.motif || d.icon, "w-motif")}
      ${d.isNew ? `<span class="badge gold w-badge">${X("New", "جديد", "Nouveau")}</span>` : featured ? `<span class="badge gold w-badge">${I("star", "i-sm")}${X("Flagship", "الرئيسي", "Phare")}</span>` : ""}
      <span class="w-ic">${I(d.icon)}</span>
      <button class="w-link" data-go="domain" data-id="${d.id}"><span class="w-name" style="display:block">${E(dname(d))}</span><span class="w-sub">${featured ? E(L(d.desc)) + " · " : ""}${cs.length} ${X("categories", "فئات", "catégories")} · ${n} ${X("questions", "سؤالًا", "questions")}</span></button>
      ${cs.length ? `<div class="w-cats">${chips}</div>` : ""}
      <span class="w-foot">${g ? `<span class="w-sub" style="flex:1">${(PLAYERS[d.id] || 48).toLocaleString()} ${X("playing this week", "يلعبون هذا الأسبوع", "joueurs cette semaine")}</span>${I("chevR", "i-sm flip")}` : `<span class="bar"><i style="--v:${m}%;--c:${featured ? "#fff" : d.color}"></i></span><b>${m}%</b>`}</span></div>`;
  }
  /* keep every "current domain" inside Your domains */
  SJ.keepMine = () => {
    const first = SJ.myDoms()[0].id;
    if (!SJ.isMine(S.domain)) S.domain = first;
    [S.setup, S.nr].forEach((st) => { if (st && st.domain !== "all" && !SJ.isMine(st.domain)) { st.domain = "all"; st.cats = []; } });
    if (S.lb && S.lb.domain !== "all" && !SJ.isMine(S.lb.domain)) S.lb.domain = "all";
    if (S.tpView) Object.keys(S.tpView).forEach((k) => { if (!SJ.isMine(S.tpView[k])) delete S.tpView[k]; });
  };
  SJ.scopeCount = (sel) => sel.domain === "all" ? SJ.myCats().reduce((a, c) => a + (SJ.COUNT[c.id] || 0), 0) : !sel.cats.length ? visCats(sel.domain).reduce((a, c) => a + (SJ.COUNT[c.id] || 0), 0) : sel.cats.reduce((a, id) => a + (SJ.COUNT[id] || 0), 0);
  const origLabel = SJ.scopeLabel;
  SJ.scopeLabel = (sel) => sel.domain === "all" ? X("All my domains", "كل مجالاتي", "Tous mes domaines") : origLabel(sel);
  const origPool = SJ.pool;
  SJ.pool = (st) => { const qs = origPool(st); if (st.domain !== "all") return qs; const ids = SJ.myCats().map((c) => c.id); const f = qs.filter((q) => ids.includes(q.cat)); return f.length ? f : qs; };
  SJ.addMine = (id, quiet) => {
    if (!id || !SJ.dom(id) || SJ.isMine(id)) return false;
    S.interests = S.interests.concat(id); SJ.store.set("interests", S.interests);
    if (!quiet) SJ.toast(X(`${dname(SJ.dom(id))} added to your domains.`, `أُضيف ${dname(SJ.dom(id))} إلى مجالاتك.`, `${dname(SJ.dom(id))} ajouté à vos domaines.`));
    return true;
  };
  SJ.actions["dom-add-this"] = (a) => { SJ.addMine(a.dataset.v); SJ.render(); };
  /* playing from a domain that isn't one of yours adds it first, so Play / Setup / boards don't snap back */
  const playDomOf = (k, a) => k === "pick-cat" ? (SJ.cat(a.dataset.v) || {}).domain : k === "lb-open" ? a.dataset.v : S.route === "domain" ? (S.params && S.params.id) || S.domain : null;
  ["quick", "duel", "custom", "daily-mix", "pick-cat", "lb-open"].forEach((k) => {
    const orig = SJ.actions[k]; if (!orig) return;
    SJ.actions[k] = (a) => { const id = playDomOf(k, a); if (id && !SJ.isMine(id)) { SJ.addMine(id); if (k !== "lb-open" && k !== "pick-cat") S.domain = id; } return orig(a); };
  });
  /* question of the day always comes from your domains */
  const dayN = () => { const d = new Date(); return Math.floor((d - new Date(d.getFullYear(), 0, 0)) / 864e5); };
  SJ.keepQotd = () => {
    const q = SJ.QUESTIONS.find((x) => x.id === SJ.QOTD), c = q && SJ.cat(q.cat);
    if (c && SJ.isMine(c.domain)) return;
    const ids = SJ.myCats().map((x) => x.id), pool = SJ.QUESTIONS.filter((x) => ids.includes(x.cat) && x.q && x.a);
    if (!pool.length) return;
    const pick = pool[dayN() % pool.length]; if (pick.id !== SJ.QOTD) { SJ.QOTD = pick.id; if (S.qotd) S.qotd.picked = null; }
  };
  SJ.actions["yd-page"] = (a) => { S.ydPage = +a.dataset.v; SJ.render(); };
  SJ.catGrid = (domain, withMix) => {
    const cats = SJ.CATS.filter((c) => c.domain === domain && !c.hidden), d = SJ.dom(domain);
    return `<div class="grid-auto">${cats.map((c) => `<button class="tile col" data-act="pick-cat" data-v="${c.id}"><span class="row">${SJ.catIc(c)}<span class="spacer"></span>${d && d.isNew ? `<span class="badge gold">${X("New", "جديد", "Nouveau")}</span>` : ""}</span>
      <span><span class="tt">${E(L(c.name))}</span><span class="ts">${SJ.isGuest() ? (SJ.COUNT[c.id] || 0) + " " + X("questions", "سؤالًا", "questions") : X("Mastery", "الإتقان", "Maîtrise") + " " + c.mastery + "%"}</span></span>
      ${SJ.isGuest() ? "" : `<span class="bar" style="height:6px"><i style="--v:${c.mastery}%;--c:${c.color}"></i></span>`}</button>`).join("")}
      ${withMix ? `<button class="tile col dashed" data-act="daily-mix">${I("shuffle", "i-lg")}<span class="tt">${X("Daily mix", "مزيج اليوم", "Mix du jour")}</span><span class="ts">${X("One question from every category", "سؤال من كل فئة", "Une question par catégorie")}</span></button>` : ""}</div>`;
  };

  /* ---------------- Domain landing page ---------------- */
  SJ.screens["domain"] = () => {
    let id = (S.params && S.params.id) || S.domain; if (!SJ.dom(id)) id = SJ.DOMAINS[0].id;
    S.domain = id; SJ.touchDom && SJ.touchDom(id);
    const d = SJ.dom(id), f = FACTOR[id] || 0.1;
    const top = SJ.PEOPLE.slice().sort((a, b) => b.week - a.week).slice(0, 5);
    const others = SJ.DOMAINS.filter((x) => x.id !== id && !x.sim).slice(0, 6);
    return `<div class="page" style="display:flex;flex-direction:column;gap:26px">
      <div class="row"><button class="btn btn-ghost btn-sm" data-go="home">${I("arrowL", "flip")}${t("home")}</button><span class="spacer"></span>${SJ.isMine(id) ? "" : `<button class="btn btn-sm btn-primary" data-act="dom-add-this" data-v="${id}">${I("plus", "i-sm")}${X("Add to your domains", "أضف إلى مجالاتك", "Ajouter à vos domaines")}</button>`}<button class="btn btn-sm" data-act="dom-sheet" data-ns="open">${I("search", "i-sm")}${X("Explore", "استكشف", "Explorer")}</button></div>
      ${SJ.isMine(id) ? "" : `<div class="mode-banner">${I("info")}<span class="grow">${X("<b>Not in your domains yet.</b> Playing here adds it, so it shows on Home, in Play and on your boards.", "<b>ليس ضمن مجالاتك بعد.</b> اللعب هنا يضيفه ليظهر في الرئيسية واللعب والترتيب.", "<b>Pas encore dans vos domaines.</b> Jouer ici l'ajoute à l'accueil, à Jouer et à vos classements.")}</span></div>`}
      ${SJ.domHero(d)}
      <div class="cols">
        <div class="col-main">
          <section class="section"><div class="section-head"><h2>${X("Categories", "الفئات", "Catégories")}</h2><span class="help">${X("Pick one to set up a game", "اختر فئة لتجهيز لعبة", "Choisissez-en une pour préparer une partie")}</span></div>${SJ.catGrid(id, true)}</section>
          <section class="section"><div class="section-head"><h2>${X("With others", "مع الآخرين", "Avec d'autres")}</h2></div>${SJ.modeTiles()}</section>
        </div>
        <div class="col-side">
          <section class="card pad"><div class="card-head"><h2>${X("Top this week", "الأفضل هذا الأسبوع", "Top de la semaine")}</h2><button class="link-btn" data-act="lb-open" data-v="${id}">${t("leaderboard")}</button></div>
            <ol class="list" style="margin:0 -16px">${top.map((p, i) => `<li class="li"><span class="rank ${i === 0 ? "r1" : ""}">${i + 1}</span>${SJ.av(p, "sm")}<span class="grow t">${E(SJ.pname(p))}</span><span class="num" style="font-weight:650;color:var(--ink-2)">${Math.round(p.week * f).toLocaleString()}</span></li>`).join("")}</ol>
            <p class="help" style="margin-top:10px">${X("Each domain has its own board, plus a global one.", "لكل مجال ترتيبه الخاص، مع ترتيب عام.", "Chaque domaine a son classement, plus un classement global.")}</p></section>
          <section class="card pad stack" style="gap:10px"><h2 style="font-size:16px">${X("How questions are made", "كيف تُعدّ الأسئلة", "Comment sont faites les questions")}</h2>
            <div class="row" style="gap:10px;align-items:flex-start">${I("book", "i-sm flip")}<span style="font-size:14px">${X("Every answer cites a source you can check.", "كل إجابة موثّقة بمصدر يمكنك مراجعته.", "Chaque réponse cite une source vérifiable.")}</span></div>
            <div class="row" style="gap:10px;align-items:flex-start">${I("languages", "i-sm")}<span style="font-size:14px">${X("Written in Arabic, English and French.", "مكتوبة بالعربية والإنجليزية والفرنسية.", "Rédigées en arabe, anglais et français.")}</span></div>
            <div class="row" style="gap:10px;align-items:flex-start">${I("shield", "i-sm")}<span style="font-size:14px">${X("Family-friendly rules apply in every domain.", "قواعد مناسبة للعائلة في كل المجالات.", "Règles adaptées à la famille dans tous les domaines.")}</span></div></section>
        </div></div>
      ${others.length ? `<section class="section"><div class="section-head"><h2>${X("More domains", "مجالات أخرى", "Autres domaines")}</h2></div><div class="worlds">${others.map((x) => worldCard(x, false)).join("")}</div></section>` : ""}
    </div>`;
  };
  SJ.actions["lb-open"] = (a) => { S.lb = S.lb || { period: "week", scope: "all", domain: "all" }; S.lb.domain = a.dataset.v; SJ.go("leaderboard"); };

  /* ---------------- Home: "Your domains" ---------------- */
  const origHome = SJ.screens["home"];
  SJ.screens["home"] = () => {
    let out = origHome();
    // dates: Gregorian only unless Islamic-only mode
    if (!S.islamicOnly) out = out.replace(`${E(SJ.greg())} · ${E(SJ.hijri())}`, E(SJ.greg()));
    // categories section → worlds + one domain's categories
    const h2 = `<h2>${X("Categories", "الفئات", "Catégories")}</h2>`;
    const at = out.indexOf(h2);
    if (at > -1) {
      const start = out.lastIndexOf("<section", at), end = out.indexOf("</section>", at) + 10;
      const mine = SJ.DOMAINS.filter((d) => S.interests.includes(d.id));
      const rest = SJ.DOMAINS.filter((d) => !S.interests.includes(d.id));
      const feat = mine.find((d) => d.featured);
      // pagination: 2 rows of 3 slots; the flagship takes 2 slots; the "Add" card is always the last slot
      const PER = 6, items = (feat ? [feat] : []).concat(mine.filter((d) => d !== feat));
      const slots = items.map((d) => (d === feat ? 2 : 1)).concat(rest.length ? [1] : []);
      const pages = []; let cur = [], used = 0;
      slots.forEach((w, i) => { if (used + w > PER) { pages.push(cur); cur = []; used = 0; } cur.push(i); used += w; });
      if (cur.length) pages.push(cur);
      const np = Math.max(1, pages.length), pg = Math.min(Math.max(1, S.ydPage || 1), np); S.ydPage = pg;
      const addCard = `<button class="world add" data-act="dom-sheet" data-ns="add"><span class="w-ic" style="background:var(--sunk);color:var(--ink-2);box-shadow:none">${I("plus")}</span><span class="w-name">${X("Add a domain", "أضف مجالًا", "Ajouter un domaine")}</span><span class="w-sub">${X(`${rest.length} more to choose from`, `${rest.length} مجالًا آخر للاختيار`, `${rest.length} autres au choix`)}</span></button>`;
      const cards = (pages[pg - 1] || []).map((i) => (i < items.length ? homeCard(items[i], items[i] === feat) : addCard));
      const pager = np > 1 ? `<div class="yd-foot"><span class="help">${X(`${items.length} domains · page ${pg} of ${np}`, `${items.length} مجالات · صفحة ${pg} من ${np}`, `${items.length} domaines · page ${pg} sur ${np}`)}</span>${SJ.pager(pg, np, "yd-page")}</div>` : "";
      const worlds = `<section class="section"><div class="section-head"><h2>${X("Your domains", "مجالاتك", "Vos domaines")}</h2><div class="row" style="gap:14px"><button class="link-btn" data-act="interests-edit">${I("sliders", "i-sm")}${X("Manage", "إدارة", "Gérer")}</button><button class="link-btn" data-act="dom-sheet" data-ns="open">${I("search", "i-sm")}${X(`Explore all ${SJ.DOMAINS.length}`, `استكشف الكل (${SJ.DOMAINS.length})`, `Tout explorer (${SJ.DOMAINS.length})`)}</button></div></div>
        ${S.islamicOnly ? `<div class="mode-banner" style="margin-bottom:14px">${I("shield")}<span class="grow">${X("<b>Islamic content only.</b> Sport, business, geography and other general domains are hidden.", "<b>محتوى إسلامي فقط.</b> أُخفيت الرياضة والأعمال والجغرافيا وغيرها.", "<b>Contenu islamique uniquement.</b> Sport, business, géographie et autres sont masqués.")}</span><button class="btn btn-sm" data-go="settings">${t("settings")}</button></div>` : ""}
        <div class="worlds">${cards.join("")}</div>${pager}</section>`;
      out = out.slice(0, start) + worlds + out.slice(end);
    }
    // Ramadan card only for people who follow the Islamic domain
    if (!S.interests.includes("islamic") && !S.islamicOnly) { const r = out.indexOf('<section class="ramadan">'); if (r > -1) out = out.slice(0, r) + out.slice(out.indexOf("</section>", r) + 10); }
    return themeQotd(out);
  };
  function themeQotd(out) {
    const q = SJ.QUESTIONS.find((x) => x.id === SJ.QOTD); if (!q) return out;
    const d = SJ.dom(SJ.cat(q.cat).domain) || baseDom(SJ.cat(q.cat).domain); if (!d) return out;
    out = out.replace('<section class="lamp-card"', `<section class="lamp-card dscope ${isIslamic(d) ? "fam-islamic" : ""}" style="--d:${d.color}"`);
    const ey = out.indexOf('<span class="eyebrow">'); if (ey > -1) { const m = out.indexOf('<span class="meta">', ey); if (m > -1) out = out.slice(0, m) + SJ.domChip(d) + out.slice(m); }
    return out;
  }

  /* ---------------- Welcome ---------------- */
  const origWelcome = SJ.screens["welcome"];
  const OLDW = { en: ["Test what you know about Islam and Arabic.", "Questions in Arabic, English and French, each checked against its source. Play alone, with friends in a live room, or one on one."], ar: ["اختبر معرفتك بالإسلام واللغة العربية.", "أسئلة بالعربية والإنجليزية والفرنسية، موثّقة بمصادرها. العب وحدك، أو مع أصدقائك في غرفة مباشرة، أو في مبارزة فردية."], fr: ["Testez vos connaissances sur l'islam et la langue arabe.", "Des questions en arabe, en anglais et en français, chacune vérifiée par sa source. Jouez seul, entre amis dans un salon en direct, ou en duel."] };
  const NEWW = { en: ["Light up what you know.", "From the Qur'an and Arabic to football, capitals and science. Questions in Arabic, English and French, each checked against its source. Play alone, in a live room with friends, or one on one."], ar: ["أضئ ما تعرفه.", "من القرآن والعربية إلى كرة القدم والعواصم والعلوم. أسئلة بالعربية والإنجليزية والفرنسية، موثّقة بمصادرها. العب وحدك، أو مع أصدقائك في غرفة مباشرة، أو في مبارزة فردية."], fr: ["Allumez ce que vous savez.", "Du Coran et de l'arabe au football, aux capitales et aux sciences. Des questions en arabe, anglais et français, chacune vérifiée par sa source. Jouez seul, entre amis dans un salon en direct, ou en duel."] };
  SJ.screens["welcome"] = () => {
    let out = origWelcome();
    out = out.replace('<div class="welcome">', '<div class="welcome"><div class="glow"></div>');
    if (!S.islamicOnly) {
      const o = OLDW[S.lang], n = NEWW[S.lang];
      out = out.replace(E(o[0]), E(n[0])).replace(o[0], n[0]).replace(E(o[1]), E(n[1])).replace(o[1], n[1]);
      const doms = `<div class="w-doms">${SJ.BASE_DOMS.map((d) => `<span style="--d:${d.color}"><i></i>${E(dname(d))}</span>`).join("")}<span style="--d:#8a8478"><i></i>${X("more every month", "والمزيد كل شهر", "et d'autres chaque mois")}</span></div>`;
      out = out.replace('<div class="facts">', doms + '<div class="facts">');
    }
    return themeQotd(out);
  };

  /* ---------------- Onboarding: interests + Islamic-only switch ---------------- */
  const origOnb = SJ.screens["onboarding"];
  SJ.screens["onboarding"] = () => {
    let out = origOnb();
    if (S.onb && S.onb.step === 2) {
      out = out.replace("What would you like to learn?", X("Pick your interests", "اختر اهتماماتك", "Choisissez vos centres d'intérêt"))
        .replace("Pick at least one. Your Home shows these first.", X("Pick at least one. Your Home shows these first, and everything else stays one tap away.", "اختر واحدًا على الأقل. تظهر في الرئيسية أولًا، والباقي على بعد نقرة.", "Au moins un. L'accueil les montre en premier, le reste reste à portée."));
      const sw = `<label class="switch card pad" style="margin-top:16px;align-items:flex-start"><input type="checkbox" data-act="isl-toggle" ${S.islamicOnly ? "checked" : ""}><span><b>${X("Islamic content only", "محتوى إسلامي فقط", "Contenu islamique uniquement")}</b><br><span class="help">${X("Hide sport, business, geography and other general domains. You can change this any time in Settings.", "إخفاء الرياضة والأعمال والجغرافيا وغيرها. يمكنك تغيير ذلك في الإعدادات متى شئت.", "Masquer le sport, le business, la géographie et les autres domaines. Modifiable à tout moment dans Paramètres.")}</span></span></label>`;
      const k = out.lastIndexOf('<div class="row" style="margin-top:24px">');
      if (k > -1) out = out.slice(0, k) + sw + out.slice(k);
    }
    return out;
  };
  const origOnbNext = SJ.actions["onb-next"];
  SJ.actions["onb-next"] = (a) => { if (S.onb && S.onb.step === 2 && S.onb.domains.length) { S.interests = S.onb.domains.slice(); SJ.store.set("interests", S.interests); } origOnbNext(a); };

  /* ---------------- Settings: content section ---------------- */
  const origSettings = SJ.screens["settings"];
  SJ.screens["settings"] = () => {
    let out = origSettings();
    const sec = `<section class="card pad stack" style="gap:16px"><div><h2 style="font-size:17px">${X("Content", "المحتوى", "Contenu")}</h2><p class="help" style="margin-top:4px">${X("Choose what Sirāj shows you.", "اختر ما يعرضه لك سراج.", "Choisissez ce que Sirāj vous montre.")}</p></div>
      <label class="switch" style="align-items:flex-start"><input type="checkbox" data-act="isl-toggle" ${S.islamicOnly ? "checked" : ""}><span><b>${X("Islamic content only", "محتوى إسلامي فقط", "Contenu islamique uniquement")}</b><br><span class="help">${X("Shows only Islamic Knowledge and Arabic & Qur'anic Sciences. Live rooms and duels stay inside these domains.", "يعرض العلوم الإسلامية وعلوم العربية والقرآن فقط، وتبقى الغرف والمبارزات داخلها.", "Affiche uniquement le savoir islamique et l'arabe. Salons et duels restent dans ces domaines.")}</span></span></label>
      <div><div class="label" style="margin-bottom:8px">${X("Your domains", "مجالاتك", "Vos domaines")} <span class="muted" style="font-weight:500">· ${X("the app shows only these", "يعرض التطبيق هذه فقط", "l'appli n'affiche que ceux-ci")}</span></div><div class="ichips">${SJ.myDoms().map((d) => `<span class="ichip" style="--d:${d.color}" aria-pressed="true"><span class="ci">${I(d.icon)}</span>${E(dname(d))}</span>`).join("")}</div>
        <div class="row wrap" style="gap:8px;margin-top:12px"><button class="btn btn-sm" data-act="interests-edit">${I("sliders", "i-sm")}${X("Manage", "إدارة", "Gérer")}</button><button class="btn btn-sm" data-act="dom-sheet" data-ns="add">${I("plus", "i-sm")}${X("Add a domain", "أضف مجالًا", "Ajouter un domaine")}</button></div></div></section>`;
    const marker = '<section class="card pad stack" style="gap:16px"><h2 style="font-size:17px">Language and display</h2>';
    return out.includes(marker) ? out.replace(marker, sec + marker) : out;
  };

  /* ---------------- Game: domain chip on every question ---------------- */
  ["game", "live", "duel-game"].forEach((r) => {
    const orig = SJ.screens[r];
    SJ.screens[r] = () => {
      let out = orig(); const g = S.game;
      if (g && g.qs && g.qs[g.i]) { const d = SJ.dom(SJ.cat(g.qs[g.i].cat).domain) || baseDom(SJ.cat(g.qs[g.i].cat).domain); if (d) out = out.replace('<div class="q-meta">', `<div class="q-meta">${SJ.domChip(d)}`); }
      return out;
    };
  });

  /* ---------------- Results: by-domain breakdown for mixed games ---------------- */
  const origResults = SJ.screens["results"];
  SJ.screens["results"] = () => {
    let out = origResults(); const g = S.game;
    if (!g || !g.log || !g.log.length) return out;
    const by = {}; g.log.forEach((l) => { const id = SJ.cat(l.q.cat).domain; (by[id] = by[id] || { n: 0, ok: 0 }).n++; if (l.ok) by[id].ok++; });
    const ids = Object.keys(by);
    if (ids.length > 1) {
      const rows = ids.map((id) => { const d = baseDom(id) || SJ.dom(id); const p = Math.round(by[id].ok / by[id].n * 100); return `<div class="row" style="gap:12px"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span style="flex:1;min-width:0;font-weight:650">${E(dname(d))}</span><span class="bar" style="flex:1.4;height:8px"><i style="--v:${p}%;--c:${d.color}"></i></span><b class="num" style="min-width:52px;text-align:end">${by[id].ok} / ${by[id].n}</b></div>`; }).join("");
      const sec = `<section class="card pad stack" style="gap:12px"><div class="card-head" style="margin:0"><h2>${X("By domain", "حسب المجال", "Par domaine")}</h2><span class="help">${X("Your strongest and weakest areas in this game", "أقوى مجالاتك وأضعفها في هذه اللعبة", "Vos points forts et faibles dans cette partie")}</span></div>${rows}</section>`;
      const k = out.indexOf('<div class="row wrap" style="gap:10px"><button class="btn btn-primary btn-lg" data-act="again">');
      if (k > -1) out = out.slice(0, k) + sec + out.slice(k);
    }
    return out;
  };

  /* ---------------- Leaderboard: global + one board per domain ---------------- */
  SJ.screens["leaderboard"] = () => {
    const lb = S.lb || (S.lb = { period: "week", scope: "all", domain: "all" });
    if (lb.domain !== "all" && !SJ.dom(lb.domain)) lb.domain = "all";
    const mult = { week: 1, month: 3.6, all: 0 }[lb.period];
    const f = lb.domain === "all" ? 1 : (FACTOR[lb.domain] || 0.1);
    const seed = lb.domain.length;
    let ps = (lb.scope === "friends" ? S.friends.map(SJ.person) : SJ.PEOPLE.slice()).map((p, i) => ({ p, pts: lb.period === "all" ? Math.round(p.all * (lb.domain === "all" ? 1 : f)) : Math.round(p.week * mult * f * (lb.domain === "all" ? 1 : (0.7 + ((i * 7 + seed) % 6) / 10))) }));
    const mine = lb.period === "all" ? Math.round(8120 * f) : Math.round(SJ.ME.weekPts * mult * f);
    if (!SJ.isGuest()) ps.push({ me: true, pts: mine });
    ps.sort((a, b) => b.pts - a.pts);
    const myIdx = ps.findIndex((x) => x.me);
    const myRank = lb.scope === "friends" ? myIdx + 1 : (lb.domain === "all" ? (lb.period === "week" ? 18 : lb.period === "month" ? 23 : 61) : 6 + (seed % 9));
    const seg = (k, opts) => `<div class="seg">${opts.map(([v, l]) => `<button data-act="lb" data-k="${k}" data-v="${v}" aria-pressed="${lb[k] === v}">${l}</button>`).join("")}</div>`;
    const avOf = (x, s) => x.me ? SJ.meAv(s) : SJ.av(x.p, s);
    const nm = (x) => x.me ? X("You", "أنت", "Vous") : SJ.pname(x.p);
    const top = ps.slice(0, 3);
    const rest = lb.scope === "friends" ? ps.slice(3) : ps.slice(3, 9).filter((x) => !x.me);
    const d = lb.domain === "all" ? null : SJ.dom(lb.domain);
    const pills = `<div class="lb-doms" role="group" aria-label="${X("Board", "الترتيب", "Classement")}"><button style="--d:#8a5a0c" data-act="lb" data-k="domain" data-v="all" aria-pressed="${lb.domain === "all"}"><span class="ci">${I("globe")}</span>${X("Global", "عام", "Global")}</button>${SJ.myDoms().map((x) => `<button style="--d:${x.color}" data-act="lb" data-k="domain" data-v="${x.id}" aria-pressed="${lb.domain === x.id}"><span class="ci">${I(x.icon)}</span>${E(dname(x))}</button>`).join("")}</div>`;
    const sub = d ? X(`Points from ${dname(d)} games only.`, `النقاط من ألعاب ${dname(d)} فقط.`, `Points des parties ${dname(d)} uniquement.`) : X("Points from every game, in every domain.", "النقاط من كل الألعاب في كل المجالات.", "Points de toutes les parties, dans tous les domaines.");
    const reset = S.islamicOnly ? X("Weekly boards reset on Friday after Maghrib.", "يُعاد ضبط الترتيب الأسبوعي يوم الجمعة بعد المغرب.", "Les classements hebdomadaires repartent le vendredi après Maghrib.") : X("Weekly boards reset every Friday.", "يُعاد ضبط الترتيب الأسبوعي كل جمعة.", "Les classements hebdomadaires repartent chaque vendredi.");
    return `<div class="page mid"><div class="page-head"><div><h1>${t("leaderboard")}${d ? ` <span class="dom-chip" style="vertical-align:middle;font-size:14px;height:30px">${I(d.icon)}${E(dname(d))}</span>` : ""}</h1><p class="sub">${sub} ${reset}</p></div></div>
      ${pills}
      <div class="row wrap" style="gap:10px;margin-bottom:20px">${seg("period", [["week", X("This week", "هذا الأسبوع", "Cette semaine")], ["month", X("This month", "هذا الشهر", "Ce mois")], ["all", X("All time", "كل الأوقات", "Depuis toujours")]])}${seg("scope", [["all", X("Everyone", "الجميع", "Tout le monde")], ["friends", t("friends")]])}</div>
      ${SJ.isGuest() ? `<div class="banner info" style="margin-bottom:20px"><div class="grow"><b>Guests don't appear on leaderboards.</b> Create an account and today's points count.</div><button class="btn btn-primary btn-sm" data-go="signup">${t("signup")}</button></div>` : ""}
      <div class="podium" style="margin-bottom:24px">${[top[1], top[0], top[2]].map((x, k) => x ? `<div class="pod p${[2, 1, 3][k]}">${avOf(x, "lg")}<b>${E(nm(x))}</b><span class="pts num">${x.pts.toLocaleString()}</span><div class="block">${[2, 1, 3][k]}</div></div>` : "<div></div>").join("")}</div>
      <section class="card"><ol class="list">${rest.map((x) => `<li class="li ${x.me ? "me" : ""}"><span class="rank">${ps.indexOf(x) + 1}</span>${avOf(x, "sm")}<span class="grow t">${E(nm(x))}</span><b class="num">${x.pts.toLocaleString()}</b></li>`).join("")}
        ${!SJ.isGuest() && lb.scope !== "friends" ? `<li class="li" style="justify-content:center;color:var(--ink-3)">···</li><li class="li me"><span class="rank">${myRank}</span>${SJ.meAv("sm")}<span class="grow t">${X("You", "أنت", "Vous")}</span><b class="num">${mine.toLocaleString()}</b></li>` : ""}</ol></section>
      ${!SJ.isGuest() ? `<p class="help" style="margin-top:12px">${d ? X(`A couple of ${dname(d)} games would move you up.`, `لعبتان في ${dname(d)} ترفعانك في الترتيب.`, `Deux parties ${dname(d)} vous feraient monter.`) : lb.period === "week" ? X("Two good games would take you into the top 15.", "لعبتان جيدتان تدخلانك المراكز الخمسة عشر الأولى.", "Deux bonnes parties vous placeraient dans le top 15.") : X("Keep your streak to climb.", "حافظ على سلسلتك لتتقدّم.", "Gardez votre série pour grimper.")}</p>` : ""}</div>`;
  };

  /* ---------------- Play hub: open the domain page from the hub ---------------- */
  const origPlay = SJ.screens["play"];
  SJ.screens["play"] = () => {
    let out = origPlay(); const d = SJ.dom(S.domain);
    if (d) out = out.replace(`<section class="section"><div class="section-head"><h2>${X("On your own", "وحدك", "En solo")}</h2></div>`, `<section class="section"><div class="section-head"><h2>${X("On your own", "وحدك", "En solo")} · <span style="color:var(--brand-ink)">${E(dname(d))}</span></h2><button class="link-btn" data-go="domain" data-id="${d.id}">${X("Domain page", "صفحة المجال", "Page du domaine")}</button></div>`);
    return out;
  };

  /* ---------------- prototype flows ---------------- */
  const origFlow = SJ.flow;
  SJ.flow = (v) => {
    if (v === "sport" || v === "mixed" || v === "islamiconly") {
      S.proto = false;
      if (S.role === "visitor" || S.role === "new") { S.role = "player"; SJ.store.set("role", "player"); }
      if (v === "sport") { if (S.islamicOnly) SJ.setIslamicOnly(false); SJ.go("domain", { id: "sport" }); return; }
      if (v === "mixed") { if (S.islamicOnly) SJ.setIslamicOnly(false); const st = Object.assign({}, S.setup, { domain: "all", cats: [], level: "mixed", count: 6, timer: 20, title: () => X("Mixed · your domains", "مزيج · مجالاتك", "Mix · vos domaines") }); const pool = SJ.pool(st); const pick = []; SJ.myDoms().forEach((d) => { const q = pool.filter((x) => SJ.cat(x.cat).domain === d.id); if (q.length) pick.push(q[Math.floor(Math.random() * q.length)]); }); SJ.startGame("solo", pick, st); return; }
      if (v === "islamiconly") { SJ.setIslamicOnly(true); SJ.go("home"); SJ.toast(X("Islamic content only: the app looks and feels like v1.", "محتوى إسلامي فقط: يبدو التطبيق كالإصدار الأول.", "Contenu islamique uniquement : comme la v1.")); return; }
    }
    if (v === "arabic" && S.islamicOnly === false) { /* keep */ }
    origFlow(v);
  };
})();
