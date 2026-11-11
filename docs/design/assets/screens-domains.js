/* Sirāj v2: domain navigation that scales from 2 to 100+ domains.
   - Domain switch: recent domains as cards + "All domains" opens a searchable sheet.
   - Topic picker: search across domains and categories, recent-domain bar, one domain's categories at a time, picked chips.
   - Mastery: sort (most played / strongest / needs work), search, first 6 then "show more", domain detail in a drawer.
   - Prototype: "Domains: 2 / 100" loads 98 sample domains to test the layouts. */
(function () {
  "use strict";
  const SJ = window.SJ, S = SJ.S, I = SJ.icon, E = SJ.esc, t = SJ.t, L = SJ.L, X = SJ.tx;
  const qn = (id) => SJ.COUNT[id] || 0;
  const visCats = (d) => SJ.CATS.filter((c) => !c.hidden && (!d || c.domain === d));
  const domCount = (d) => visCats(d).reduce((a, c) => a + qn(c.id), 0);
  const domAvg = (d) => { const cs = visCats(d); return cs.length ? Math.round(cs.reduce((a, c) => a + (c.mastery || 0), 0) / cs.length) : 0; };
  const many = () => SJ.DOMAINS.length > 4;
  const norm = (s) => String(s || "").toLowerCase();
  const dname = (d) => L(d.name);
  const cname = (c) => L(c.name);
  const match = (obj, q) => ["en", "ar", "fr"].some((l) => norm(obj.name[l]).includes(q));

  /* ---------- recent domains ---------- */
  S.recentDoms = S.recentDoms || SJ.DOMAINS.slice(0, 3).map((d) => d.id);
  SJ.touchDom = (id) => { if (!SJ.dom(id)) return; S.recentDoms = [id].concat(S.recentDoms.filter((x) => x !== id && SJ.dom(x))).slice(0, 6); };
  const recents = (cur, n) => { const ids = [cur].concat(S.recentDoms).filter((x, i, a) => x && SJ.dom(x) && a.indexOf(x) === i); SJ.DOMAINS.forEach((d) => { if (ids.length < n && !ids.includes(d.id)) ids.push(d.id); }); return ids.slice(0, n).map(SJ.dom); };

  /* ---------- slider + pager ---------- */
  const byPlays = () => SJ.DOMAINS.slice().sort((a, b) => (b.plays || 0) - (a.plays || 0));
  SJ.slider = (key, items) => `<div class="slider" data-sl="${key}"><button class="sl-arrow prev" data-act="sl-move" data-sl="${key}" data-dir="-1" aria-label="${X("Previous", "السابق", "Précédent")}">${I("chevL", "flip")}</button><div class="sl-track" id="sl-${key}">${items}</div><button class="sl-arrow next" data-act="sl-move" data-sl="${key}" data-dir="1" aria-label="${X("Next", "التالي", "Suivant")}">${I("chevR", "flip")}</button></div>`;
  const rtl = () => document.documentElement.dir === "rtl";
  function slUpd(sl, tr) { const max = tr.scrollWidth - tr.clientWidth, pos = Math.abs(tr.scrollLeft); sl.classList.toggle("static", max < 4); const pv = sl.querySelector(".prev"), nx = sl.querySelector(".next"); if (pv) pv.disabled = pos < 4; if (nx) nx.disabled = pos > max - 4; }
  function syncSliders() { document.querySelectorAll(".slider").forEach((sl) => { const tr = sl.querySelector(".sl-track"); if (!tr) return; const k = sl.dataset.sl; if (!tr.dataset.r) { tr.dataset.r = "1"; if (S.slPos && S.slPos[k] != null) { tr.style.scrollBehavior = "auto"; tr.scrollLeft = S.slPos[k]; tr.style.scrollBehavior = ""; } } slUpd(sl, tr); }); }
  SJ.actions["sl-move"] = (a) => { const tr = document.getElementById("sl-" + a.dataset.sl); if (!tr) return; tr.scrollBy({ left: (+a.dataset.dir) * (rtl() ? -1 : 1) * tr.clientWidth * 0.85, behavior: "smooth" }); };
  document.addEventListener("scroll", (e) => { const tr = e.target; if (!tr.classList || !tr.classList.contains("sl-track")) return; const sl = tr.closest(".slider"); (S.slPos || (S.slPos = {}))[sl.dataset.sl] = tr.scrollLeft; slUpd(sl, tr); }, true);
  window.addEventListener("resize", syncSliders);
  const appEl = document.getElementById("app"); if (appEl && window.MutationObserver) new MutationObserver(() => requestAnimationFrame(syncSliders)).observe(appEl, { childList: true });
  SJ.pager = (page, pages, act) => {
    if (pages <= 1) return "";
    const nums = []; for (let i = 1; i <= pages; i++) { if (i === 1 || i === pages || Math.abs(i - page) <= 1) nums.push(i); else if (nums[nums.length - 1] !== "…") nums.push("…"); }
    return `<nav class="pager" aria-label="${X("Pages", "الصفحات", "Pages")}"><button class="pg" data-act="${act}" data-v="${page - 1}" ${page <= 1 ? "disabled" : ""} aria-label="${X("Previous page", "الصفحة السابقة", "Page précédente")}">${I("chevL", "i-sm flip")}</button>${nums.map((n) => n === "…" ? `<span class="pg-gap">…</span>` : `<button class="pg ${n === page ? "on" : ""}" data-act="${act}" data-v="${n}" ${n === page ? 'aria-current="page"' : ""}>${n}</button>`).join("")}<button class="pg" data-act="${act}" data-v="${page + 1}" ${page >= pages ? "disabled" : ""} aria-label="${X("Next page", "الصفحة التالية", "Page suivante")}">${I("chevR", "i-sm flip")}</button></nav>`;
  };
  const PER = 6;

  /* ---------- sample domains for the prototype ---------- */
  const NAMES = ["Tafsir", "Aqeedah", "Usul al-Fiqh", "Fiqh of Prayer", "Fiqh of Zakat", "Fiqh of Hajj", "Fiqh of Family", "Islamic Finance", "Hadith Sciences", "Sahabah", "Women in Islamic History", "Mothers of the Believers", "The Four Imams", "Islamic Golden Age", "Andalusia", "The Ottomans", "Mamluks", "Abbasids", "Umayyads", "Rightly Guided Caliphs", "Battles of the Seerah", "Makkah and Madinah", "Al-Aqsa and Jerusalem", "Islamic Art", "Islamic Architecture", "Calligraphy", "Muslim Scientists", "Astronomy in Islam", "Medicine in Islamic History", "Islamic Geography", "Muslim World Today", "Islamic Calendar", "Eid and Celebrations", "Duas and Adhkar", "Names of Allah", "Prophets in the Qur'an", "Parables of the Qur'an", "Juz Amma", "Surah al-Baqarah", "Surah al-Kahf", "Tajweed", "Qira'at", "Memorisation Helpers", "Arabic Verbs", "Arabic Nouns", "Arabic Particles", "Arabic Poetry", "Pre-Islamic Arabia", "Arabic Proverbs", "Classical Texts", "Islamic Ethics at Work", "Character of the Prophet", "Manners of Learning", "Parenting in Islam", "Islam for Kids", "Islam for Teens", "New Muslims", "Comparative Religion", "Purification", "Fasting Beyond Ramadan", "Friday and Jumu'ah", "Death and the Hereafter", "Signs of the Hour", "Angels and the Unseen", "Jinn in Islam", "Dreams in Islam", "Islamic Law Basics", "Inheritance", "Halal and Haram", "Food and Drink", "Trade and Markets", "Islamic Governance", "Scholars of Hadith", "Scholars of Tafsir", "Sufism History", "West Africa and Islam", "South Asia and Islam", "Southeast Asia and Islam", "China and Islam", "East Africa and Islam", "Islam in Europe", "Islam in the Americas", "Mosques of the World", "Islamic Manuscripts", "Ramadan Nights", "Hajj Journey", "Umrah Guide", "Charity and Waqf", "Environment in Islam", "Animals in the Qur'an", "Plants in the Qur'an", "Numbers in the Qur'an", "Stories for Bedtime", "Quiz Night Classics", "Sirah for Kids", "Prophetic Medicine", "Islamic Psychology", "Seasons of Worship"];
  const ICONS = ["book", "mosque", "scale", "scroll", "columns", "type", "languages", "puzzle", "feather", "star", "heart", "moon", "sun", "globe", "layers", "shield", "flame", "users", "pen", "bolt"];
  const COLS = ["#0d9488", "#7c3aed", "#c2410c", "#2563eb", "#db2777", "#0f766e", "#4f46e5", "#a16207", "#059669", "#be123c", "#0369a1", "#9333ea", "#b45309", "#15803d", "#475569"];
  const CATW = ["Basics", "Key figures", "Places", "Events", "Terms", "Rulings", "Stories", "Quotes", "Dates", "Advanced"];
  let seed = 7; const rnd = () => { seed = (seed * 16807) % 2147483647; return seed / 2147483647; };
  let simCache = null;
  function buildSim() {
    seed = 7; const doms = [], cats = [], count = {};
    NAMES.slice(0, 98).forEach((nm, i) => {
      const id = "sd" + i, n = 3 + Math.floor(rnd() * 6), icon = ICONS[i % ICONS.length], color = COLS[i % COLS.length];
      doms.push({ id, sim: true, name: { en: nm, ar: nm, fr: nm }, desc: { en: n + " categories", ar: n + " فئات", fr: n + " catégories" }, icon, color, plays: Math.floor(rnd() * 60) });
      for (let k = 0; k < n; k++) { const cid = id + "c" + k; cats.push({ id: cid, sim: true, domain: id, icon: ICONS[(i + k + 3) % ICONS.length], color: COLS[(i + k * 2) % COLS.length], name: { en: nm + " · " + CATW[k], ar: nm + " · " + CATW[k], fr: nm + " · " + CATW[k] }, mastery: Math.floor(rnd() * 95) }); count[cid] = 4 + Math.floor(rnd() * 30); }
    });
    return { doms, cats, count };
  }
  SJ.DOMAINS.forEach((d) => { if (d.plays == null) d.plays = d.id === "islamic" ? 120 : 14; });
  SJ.setDomainScale = (n) => {
    SJ.DOMAINS.splice(0, SJ.DOMAINS.length, ...SJ.DOMAINS.filter((d) => !d.sim));
    SJ.CATS.splice(0, SJ.CATS.length, ...SJ.CATS.filter((c) => !c.sim));
    Object.keys(SJ.COUNT).forEach((k) => { if (k.startsWith("sd")) delete SJ.COUNT[k]; });
    if (n > 2) { simCache = simCache || buildSim(); SJ.DOMAINS.push(...simCache.doms); SJ.CATS.push(...simCache.cats); Object.assign(SJ.COUNT, simCache.count); }
    if (!SJ.dom(S.domain)) S.domain = SJ.DOMAINS[0].id;
    S.recentDoms = S.recentDoms.filter((x) => SJ.dom(x)); if (!S.recentDoms.length) S.recentDoms = SJ.DOMAINS.slice(0, 3).map((d) => d.id);
    if (n > 2) ["sd4", "sd13"].forEach((x) => { if (!S.recentDoms.includes(x)) S.recentDoms.push(x); });
    [S.setup, S.nr].forEach((s) => { if (s && s.domain !== "all" && !SJ.dom(s.domain)) { s.domain = "all"; s.cats = []; } });
    S.tpView = {}; S.mastP = 1; S.slPos = {}; SJ.render();
  };
  SJ.actions["dscale"] = (a) => { SJ.setDomainScale(+a.dataset.v); SJ.toast(+a.dataset.v > 2 ? X("100 sample domains loaded. Their questions reuse the sample bank.", "حُمّل 100 مجال تجريبي. أسئلتها من البنك التجريبي.", "100 domaines d'exemple chargés.") : X("Back to the 2 real domains.", "عودة إلى المجالين الحقيقيين.", "Retour aux 2 domaines réels.")); };

  /* ---------- domain sheet: 6 cards per page + search ---------- */
  function sheetRows(ns, q) {
    const cur = ns === "home" ? S.domain : ns === "open" ? (S.route === "domain" ? (S.params && S.params.id) || S.domain : "") : (S.tpView && S.tpView[ns]) || "";
    const isOn = (d) => ns === "add" ? S.interests.includes(d.id) : d.id === cur;
    const card = (d) => `<button class="dcard ${isOn(d) ? "on" : ""}" data-act="dom-pick" data-ns="${ns}" data-v="${d.id}" ${ns === "add" ? `aria-pressed="${isOn(d)}"` : ""}><span class="row" style="gap:10px"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span>${isOn(d) ? `<span class="dcard-ck">${I("check", "i-sm")}</span>` : ns === "add" ? `<span class="dcard-add">${I("plus", "i-sm")}</span>` : ns === "open" ? `<span class="dcard-go">${I("chevR", "i-sm flip")}</span>` : ""}</span><b>${E(dname(d))}</b><span class="help">${visCats(d.id).length} ${X("categories", "فئات", "catégories")} · ${domCount(d.id)} ${X("questions", "سؤالًا", "questions")}</span>${SJ.signedIn() ? `<span class="bar" style="height:5px"><i style="--v:${domAvg(d.id)}%;--c:${d.color}"></i></span>` : ""}</button>`;
    let ds = q ? SJ.DOMAINS.filter((d) => match(d, q)) : SJ.DOMAINS.slice().sort((a, b) => dname(a).localeCompare(dname(b), S.lang));
    const pages = Math.max(1, Math.ceil(ds.length / PER)), page = Math.min(Math.max(1, S.dsPage || 1), pages);
    const cs = q && ns !== "add" ? visCats().filter((c) => match(c, q)).slice(0, 8) : [];
    const rec = !q && ns !== "add" ? S.recentDoms.map(SJ.dom).filter(Boolean) : [];
    if (q && !ds.length && !cs.length) return `<div class="empty" style="padding:30px 10px"><p>${X("Nothing matches. Try another word.", "لا نتائج. جرّب كلمة أخرى.", "Aucun résultat. Essayez un autre mot.")}</p></div>`;
    return `${rec.length ? `<div class="ds-h">${X("Recent", "الأخيرة", "Récents")}</div><div class="ds-recent">${rec.map((d) => `<button class="tp-dp ${isOn(d) ? "on" : ""}" data-act="dom-pick" data-ns="${ns}" data-v="${d.id}"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span>${E(dname(d))}</span></button>`).join("")}</div>` : ""}
      ${ds.length ? `<div class="ds-h row"><span>${q ? X("Domains", "المجالات", "Domaines") : X("All domains · A–Z", "كل المجالات · أ–ي", "Tous les domaines · A–Z")}</span><span class="spacer"></span><span class="num" style="text-transform:none">${(page - 1) * PER + 1}–${Math.min(page * PER, ds.length)} ${X("of", "من", "sur")} ${ds.length}</span></div>
        <div class="dcard-grid">${ds.slice((page - 1) * PER, page * PER).map(card).join("")}</div>${SJ.pager(page, pages, "ds-page")}` : ""}
      ${cs.length ? `<div class="ds-h">${X("Categories", "الفئات", "Catégories")} · ${cs.length}</div>${cs.map((c) => `<button class="ds-row" data-act="dom-pick" data-ns="${ns}" data-v="${c.domain}" data-cat="${c.id}">${SJ.catIc(c, "sm")}<span class="ds-t"><b>${E(cname(c))}</b><span class="help">${X("in", "في", "dans")} ${E(dname(SJ.dom(c.domain)))} · ${qn(c.id)} ${X("questions", "سؤالًا", "questions")}</span></span></button>`).join("")}` : ""}`;
  }
  const sheetRedraw = () => { const box = document.getElementById("ds-list"), inp = document.getElementById("ds-q"); if (box && inp) { box.innerHTML = sheetRows(inp.dataset.ns, norm(inp.value.trim())); box.scrollTop = 0; } };
  SJ.actions["ds-page"] = (a) => { S.dsPage = +a.dataset.v; sheetRedraw(); };
  SJ.actions["dom-sheet"] = (a) => {
    const ns = a.dataset.ns; S.dsPage = 1;
    const head = ns === "add" ? [X("Add to your domains", "أضف إلى مجالاتك", "Ajouter à vos domaines"), X("Tap to add or remove. Your home updates right away.", "انقر للإضافة أو الإزالة. تتحدّث الرئيسية فورًا.", "Touchez pour ajouter ou retirer. L'accueil se met à jour.")]
      : ns === "open" ? [X("Explore all domains", "استكشف كل المجالات", "Explorer tous les domaines"), `${SJ.DOMAINS.length} ${X("domains", "مجالًا", "domaines")} · ${visCats().length} ${X("categories", "فئة", "catégories")} · ${X("tap one to open it", "انقر لفتحه", "touchez pour l'ouvrir")}`]
      : [X("Choose a domain", "اختر مجالًا", "Choisir un domaine"), `${SJ.DOMAINS.length} ${X("domains", "مجالًا", "domaines")} · ${visCats().length} ${X("categories", "فئة", "catégories")}`];
    SJ.drawer(`<div class="drawer-head"><div><h2>${head[0]}</h2><p>${head[1]}</p></div><span class="spacer"></span><button class="icon-btn sm" data-act="close" aria-label="Close">${I("x")}</button></div>
      <div class="ds-search"><label class="input-icon" style="display:block">${I("search")}<span class="sr-only">Search</span><input class="input" id="ds-q" data-ns="${ns}" placeholder="${ns === "add" ? X("Search domains", "ابحث في المجالات", "Chercher des domaines") : X("Search domains and categories", "ابحث في المجالات والفئات", "Chercher domaines et catégories")}" autocomplete="off"></label></div>
      <div class="drawer-body ds-list" id="ds-list">${sheetRows(ns, "")}</div>${ns === "add" ? `<div class="drawer-foot"><span class="help" id="ds-cnt">${S.interests.length} ${X("on your home", "في الرئيسية", "sur l'accueil")}</span><span class="spacer"></span><button class="btn btn-primary" data-act="close">${X("Done", "تم", "Terminé")}</button></div>` : ""}`);
  };
  SJ.actions["dom-pick"] = (a) => {
    const ns = a.dataset.ns, v = a.dataset.v, cat = a.dataset.cat;
    if (ns === "add") {
      const on = S.interests.includes(v);
      if (on && S.interests.length < 2) { SJ.toast(X("Keep at least one domain.", "أبقِ مجالًا واحدًا على الأقل.", "Gardez au moins un domaine.")); return; }
      S.interests = on ? S.interests.filter((x) => x !== v) : S.interests.concat(v); SJ.store.set("interests", S.interests);
      if (!on) { S.homeDom = v; SJ.store.set("homeDom", v); }
      sheetRedraw(); const c = document.getElementById("ds-cnt"); if (c) c.textContent = `${S.interests.length} ${X("on your home", "في الرئيسية", "sur l'accueil")}`;
      SJ.render(); return;
    }
    SJ.touchDom(v); SJ.closeOverlay();
    if (ns === "open") { S.domain = v; if (cat) { SJ.actions["pick-cat"]({ dataset: { v: cat } }); return; } SJ.go("domain", { id: v }); return; }
    if (ns === "home") S.domain = v;
    else if (ns === "onb") { const o = S.onb; if (o && !o.domains.includes(v)) o.domains.push(v); S.onbQ = ""; }
    else { S.tpView = S.tpView || {}; S.tpView[ns] = v; if (S.tpQ) S.tpQ[ns] = ""; if (cat) selectCat(ns, cat); }
    SJ.render();
  };

  /* ---------- domain switch (Home, Play, Duel): slider ---------- */
  SJ.domainSwitch = (act) => {
    const card = (d) => `<button class="dom" data-act="${act}" data-v="${d.id}" aria-pressed="${S.domain === d.id}"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span style="min-width:0"><span class="dn">${E(dname(d))}${d.isNew ? ` <span class="badge gold" style="height:20px;font-size:11.5px">${X("New", "جديد", "Nouveau")}</span>` : ""}</span><span class="dd">${visCats(d.id).length} ${X("categories", "فئات", "catégories")}</span></span></button>`;
    // one line only, then pages: 5 per line on desktop, 3 on tablet, 2 on phone
    const doms = SJ.myDoms(), per = dswPer(), pages = Math.max(1, Math.ceil(doms.length / per));
    if (S.dswRoute !== S.route) { S.dswRoute = S.route; S.dswPage = null; }
    if (S.dswPage == null) { const i = doms.findIndex((d) => d.id === S.domain); S.dswPage = i < 0 ? 1 : Math.floor(i / per) + 1; }
    const pg = Math.min(Math.max(1, S.dswPage), pages); S.dswPage = pg;
    const shown = doms.slice((pg - 1) * per, pg * per);
    return `<div class="dsw1"><div class="domain-switch mine one" role="group" aria-label="Domain" style="--per:${per}">${shown.map(card).join("")}</div>
      <div class="dsw-foot"><button class="link-btn" data-act="dom-sheet" data-ns="add">${I("plus", "i-sm")}${X("Add a domain", "أضف مجالًا", "Ajouter un domaine")}</button>${pages > 1 ? `<span class="help num">${(pg - 1) * per + 1}–${Math.min(pg * per, doms.length)} ${X("of", "من", "sur")} ${doms.length}</span>${SJ.pager(pg, pages, "dsw-page")}` : ""}</div></div>`;
  };
  const dswPer = () => { const w = window.innerWidth; return w >= 1100 ? 5 : w >= 720 ? 3 : 2; };
  let dswLast = dswPer();
  window.addEventListener("resize", () => { const n = dswPer(); if (n !== dswLast) { dswLast = n; S.dswPage = null; if (document.querySelector(".dsw1")) SJ.render(); } });
  SJ.actions["dsw-page"] = (a) => { S.dswPage = +a.dataset.v; SJ.render(); };
  SJ.actions["domain"] = (a) => { S.domain = a.dataset.v; SJ.touchDom(a.dataset.v); SJ.render(); };

  /* ---------- topic picker ---------- */
  const tpSt = (ns) => (ns === "nr" ? SJ.nrState() : S.setup);
  function viewDom(ns, sel) { S.tpView = S.tpView || {}; let v = S.tpView[ns]; if (!v || !SJ.isMine(v)) v = sel.domain !== "all" && SJ.isMine(sel.domain) ? sel.domain : SJ.myDoms()[0].id; return v; }
  function selectCat(ns, v) {
    const s = tpSt(ns), d = SJ.cat(v).domain;
    if (s.domain !== d) { s.domain = d; s.cats = [v]; }
    else if (!s.cats.length) s.cats = [v];
    else s.cats = s.cats.includes(v) ? s.cats.filter((x) => x !== v) : s.cats.concat(v);
    if (s.cats.length && s.cats.length === visCats(d).filter((c) => qn(c.id)).length) s.cats = [];
    S.tpView = S.tpView || {}; S.tpView[ns] = d; SJ.touchDom(d);
  }
  function tpResults(ns, q) {
    const ds = SJ.myDoms().filter((d) => match(d, q)).slice(0, 6), cs = SJ.myCats().filter((c) => match(c, q)).slice(0, 12), sel = tpSt(ns);
    if (!ds.length && !cs.length) return `<p class="muted" style="padding:14px 4px">${X("Nothing matches. Try another word.", "لا نتائج. جرّب كلمة أخرى.", "Aucun résultat.")}</p>`;
    return `${ds.length ? `<div class="ds-h">${X("Domains", "المجالات", "Domaines")}</div>${ds.map((d) => `<button class="ds-row" data-act="dom-pick" data-ns="${ns}" data-v="${d.id}"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span class="ds-t"><b>${E(dname(d))}</b><span class="help">${visCats(d.id).length} ${X("categories", "فئات", "catégories")} · ${domCount(d.id)} ${X("questions", "سؤالًا", "questions")}</span></span>${I("chevR", "i-sm flip")}</button>`).join("")}` : ""}
      ${cs.length ? `<div class="ds-h">${X("Categories", "الفئات", "Catégories")}</div>${cs.map((c) => { const on = sel.domain === c.domain && (!sel.cats.length || sel.cats.includes(c.id)); return `<button class="ds-row ${on ? "on" : ""}" data-act="dom-pick" data-ns="${ns}" data-v="${c.domain}" data-cat="${c.id}">${SJ.catIc(c, "sm")}<span class="ds-t"><b>${E(cname(c))}</b><span class="help">${X("in", "في", "dans")} ${E(dname(SJ.dom(c.domain)))}</span></span><span class="tp-n num">${qn(c.id)}</span></button>`; }).join("")}` : ""}`;
  }
  SJ.topicPicker = (sel, ns) => {
    const vd = viewDom(ns, sel), d = SJ.dom(vd), allOn = sel.domain === "all";
    const q = (S.tpQ && S.tpQ[ns]) || "";
    const cats = visCats(vd), dc = domCount(vd), mine = sel.domain === vd, domOn = mine && !sel.cats.length;
    const ck = `<span class="tp-ck">${I("check", "i-sm")}</span>`;
    const picked = sel.domain !== "all" && sel.cats.length ? sel.cats : [];
    return `<div class="tp2 ${q ? "searching" : ""}" data-ns="${ns}">
      ${SJ.myCats().length > 8 ? `<label class="input-icon tp-search" style="display:block">${I("search")}<span class="sr-only">Search</span><input class="input" id="tp-q-${ns}" data-tpq="${ns}" value="${E(q)}" placeholder="${X(`Search your ${SJ.myDoms().length} domains and ${SJ.myCats().length} categories`, `ابحث في مجالاتك (${SJ.myDoms().length}) و${SJ.myCats().length} فئة`, `Chercher dans vos ${SJ.myDoms().length} domaines et ${SJ.myCats().length} catégories`)}" autocomplete="off"></label><div class="tp-res" id="tp-res-${ns}">${q ? tpResults(ns, norm(q)) : ""}</div>` : ""}
      <button class="tp-all ${allOn ? "on" : ""}" data-act="tp-all" data-ns="${ns}" aria-pressed="${allOn}"><span class="cat-ic" style="--c:#0b7a5c">${I("shuffle")}</span><span class="tp-t"><b>${X("All my domains", "كل مجالاتي", "Tous mes domaines")}</b><span class="help">${SJ.myDoms().map(dname).map(E).join(" · ")} · ${X("mixed together", "ممزوجة", "mélangés")}</span></span><span class="tp-n num">${SJ.scopeCount({ domain: "all", cats: [] })}</span>${allOn ? ck : ""}</button>
      <div class="tp-bar">${(() => { const pill = (p) => `<button role="tab" class="tp-dp ${p.id === vd ? "on" : ""} ${sel.domain === p.id ? "has" : ""}" data-act="tp-view" data-ns="${ns}" data-v="${p.id}" aria-selected="${p.id === vd}"><span class="cat-ic sm" style="--c:${p.color}">${I(p.icon)}</span><span>${E(dname(p))}</span>${sel.domain === p.id ? `<span class="tp-dot"></span>` : ""}</button>`; return `<div class="tp-pills" role="tablist">${SJ.myDoms().map(pill).join("")}</div>`; })()}
        <button class="tp-more" data-act="dom-sheet" data-ns="add">${I("plus", "i-sm")}${X("Add a domain", "أضف مجالًا", "Ajouter un domaine")}</button></div>
      <div class="tp-dom act-view ${mine ? "act" : ""}"><div class="tp-head"><div class="tp-tog static"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span class="tp-t"><b>${E(dname(d))}</b><span class="help">${cats.length} ${X("categories", "فئات", "catégories")} · ${dc} ${X("questions", "سؤالًا", "questions")}</span></span></div>
          <button class="tp-pill ${domOn ? "on" : ""}" data-act="tp-dom" data-ns="${ns}" data-v="${vd}" aria-pressed="${domOn}" ${dc ? "" : "disabled"}>${domOn ? I("check", "i-sm") : ""}${X("All of it", "الكل", "Tout")}</button></div>
        <div class="tp-grid">${cats.map((c) => { const on = mine && (domOn || sel.cats.includes(c.id)), n = qn(c.id); return `<button class="tp-cat ${on ? "on" : ""} ${domOn ? "soft" : ""}" data-act="tp-cat" data-ns="${ns}" data-v="${c.id}" aria-pressed="${on}" ${n ? "" : "disabled"}>${SJ.catIc(c, "sm")}<span class="tp-cn">${E(cname(c))}</span><span class="tp-n num">${n || X("Soon", "قريبًا", "Bientôt")}</span>${on && !domOn ? ck : ""}</button>`; }).join("")}</div></div>
      ${picked.length ? `<div class="tp-picked"><span class="help">${X("Picked", "المختار", "Choisis")}:</span>${picked.map((id) => `<button class="tp-chip" data-act="tp-cat" data-ns="${ns}" data-v="${id}" aria-label="${X("Remove", "إزالة", "Retirer")} ${E(cname(SJ.cat(id)))}">${E(cname(SJ.cat(id)))}${I("x", "i-sm")}</button>`).join("")}</div>` : ""}
    </div>`;
  };
  SJ.actions["tp-view"] = (a) => { S.tpView = S.tpView || {}; S.tpView[a.dataset.ns] = a.dataset.v; SJ.render(); };
  SJ.actions["tp-all"] = (a) => { const s = tpSt(a.dataset.ns); s.domain = "all"; s.cats = []; SJ.render(); };
  SJ.actions["tp-dom"] = (a) => { const s = tpSt(a.dataset.ns); s.domain = a.dataset.v; s.cats = []; SJ.touchDom(a.dataset.v); SJ.render(); };
  SJ.actions["tp-cat"] = (a) => { selectCat(a.dataset.ns, a.dataset.v); SJ.render(); };

  /* ---------- mastery ---------- */
  SJ.masteryList = () => {
    const sort = S.mastSort || "played", q = norm(S.mastQ || "");
    let ds = SJ.myDoms().filter((d) => visCats(d.id).length).map((d) => ({ d, avg: domAvg(d.id) }));
    const nDoms = ds.length, overall = ds.length ? Math.round(ds.reduce((a, x) => a + x.avg, 0) / ds.length) : 0;
    if (q) ds = ds.filter((x) => match(x.d, q) || visCats(x.d.id).some((c) => match(c, q)));
    ds.sort(sort === "strong" ? (a, b) => b.avg - a.avg : sort === "weak" ? (a, b) => a.avg - b.avg : (a, b) => (b.d.plays || 0) - (a.d.plays || 0));
    const pages = Math.max(1, Math.ceil(ds.length / PER)), page = Math.min(Math.max(1, S.mastP || 1), pages);
    const shown = ds.slice((page - 1) * PER, page * PER);
    return `<div class="mast">
      <div class="mast-top"><div class="mast-ov"><span class="num">${overall}%</span><span class="help">${X("average across your", "المتوسط عبر مجالاتك", "moyenne sur vos")} ${nDoms} ${X("domains", "", "domaines")}</span></div>
        <div class="seg">${[["played", X("Most played", "الأكثر لعبًا", "Plus jouées")], ["strong", X("Strongest", "الأقوى", "Points forts")], ["weak", X("Needs work", "تحتاج تدريبًا", "À travailler")]].map(([k, l]) => `<button data-act="mast-sort" data-v="${k}" aria-pressed="${sort === k}">${l}</button>`).join("")}</div></div>
      ${nDoms > 6 ? `<label class="input-icon" style="display:block;margin-bottom:10px">${I("search")}<span class="sr-only">Search</span><input class="input" id="mast-q" value="${E(S.mastQ || "")}" placeholder="${X("Search a domain or category", "ابحث عن مجال أو فئة", "Chercher un domaine ou une catégorie")}" autocomplete="off"></label>` : ""}
      <div class="mast-list">${shown.map(({ d, avg }) => `<button class="mast-row" data-act="mast-open" data-v="${d.id}"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span class="mast-t"><b>${E(dname(d))}</b><span class="help">${visCats(d.id).length} ${X("categories", "فئات", "catégories")}${d.plays ? ` · ${X("played", "لُعب", "joué")} ${d.plays}×` : ""}</span></span><span class="mast-m"><span class="bar" style="height:6px"><i style="--v:${avg}%;--c:${d.color}"></i></span><b class="num">${avg}%</b></span>${I("chevR", "i-sm flip mast-chev")}</button>`).join("") || `<p class="muted" style="padding:14px">${X("No domain matches.", "لا مجال مطابق.", "Aucun domaine.")}</p>`}</div>
      ${ds.length > PER ? `<div class="mast-foot"><span class="help num">${(page - 1) * PER + 1}–${Math.min(page * PER, ds.length)} ${X("of", "من", "sur")} ${ds.length}</span>${SJ.pager(page, pages, "mast-page")}</div>` : ""}
    </div>`;
  };
  SJ.actions["mast-sort"] = (a) => { S.mastSort = a.dataset.v; S.mastP = 1; SJ.render(); };
  SJ.actions["mast-page"] = (a) => { S.mastP = +a.dataset.v; SJ.render(); };
  SJ.actions["mast-open"] = (a) => {
    const d = SJ.dom(a.dataset.v), cats = visCats(d.id).slice().sort((x, y) => x.mastery - y.mastery), weak = cats[0];
    SJ.drawer(`<div class="drawer-head"><span class="cat-ic" style="--c:${d.color}">${I(d.icon)}</span><div><h2>${E(dname(d))}</h2><p>${X("Mastery", "الإتقان", "Maîtrise")} ${domAvg(d.id)}% · ${cats.length} ${X("categories", "فئات", "catégories")}</p></div><span class="spacer"></span><button class="icon-btn sm" data-act="close" aria-label="Close">${I("x")}</button></div>
      <div class="drawer-body stack" style="gap:16px">${cats.map((c) => `<div><div class="row" style="margin-bottom:6px">${SJ.catIc(c, "sm")}<span style="flex:1;font-weight:650">${E(cname(c))}</span><span class="num muted">${c.mastery}%</span></div><div class="bar"><i style="--v:${c.mastery}%;--c:${c.color}"></i></div></div>`).join("")}<p class="help">${X("Sorted from weakest to strongest.", "مرتبة من الأضعف إلى الأقوى.", "Du plus faible au plus fort.")}</p></div>
      <div class="drawer-foot"><button class="btn" data-act="close">${X("Close", "إغلاق", "Fermer")}</button>${weak ? `<button class="btn btn-primary" data-act="mast-practice" data-v="${weak.id}">${I("tri", "i-sm flip")}${X("Practise", "تدرّب على", "S'entraîner :")} ${E(cname(weak))}</button>` : ""}</div>`);
  };
  SJ.actions["mast-practice"] = (a) => { SJ.closeOverlay(); const c = SJ.cat(a.dataset.v); S.setup = Object.assign(S.setup, { domain: c.domain, cats: [c.id], mode: "solo" }); S.tpView = S.tpView || {}; S.tpView.setup = c.domain; SJ.go("setup"); };

  /* ---------- onboarding with many domains ---------- */
  SJ.onbDomains = () => {
    const o = S.onb || { domains: [] }, q = norm(S.onbQ || "");
    if (!many()) return SJ.DOMAINS;
    if (q) return SJ.DOMAINS.filter((d) => match(d, q)).slice(0, 12);
    const sel = o.domains.map(SJ.dom).filter(Boolean);
    const pop = SJ.DOMAINS.slice().sort((a, b) => (b.plays || 0) - (a.plays || 0)).filter((d) => !o.domains.includes(d.id));
    return sel.concat(pop).slice(0, Math.max(6, sel.length));
  };
  SJ.onbSearch = () => many() ? `<label class="input-icon" style="display:block;margin-top:16px">${I("search")}<span class="sr-only">Search</span><input class="input" id="onb-q" value="${E(S.onbQ || "")}" placeholder="${X(`Search ${SJ.DOMAINS.length} domains`, `ابحث في ${SJ.DOMAINS.length} مجالًا`, `Chercher parmi ${SJ.DOMAINS.length} domaines`)}" autocomplete="off"></label><p class="help" style="margin-top:8px">${S.onbQ ? "" : X("Most popular first. Search to find the rest.", "الأكثر شعبية أولًا. ابحث لتجد الباقي.", "Les plus populaires d'abord. Cherchez pour le reste.")}</p>` : "";

  /* ---------- search inputs ---------- */
  const keepFocus = (id, pos) => { const n = document.getElementById(id); if (n) { n.focus(); try { n.setSelectionRange(pos, pos); } catch (e) {} } };
  document.addEventListener("input", (e) => {
    const el = e.target;
    if (el.id === "ds-q") { S.dsPage = 1; sheetRedraw(); return; }
    if (el.dataset && el.dataset.tpq) {
      const ns = el.dataset.tpq; S.tpQ = S.tpQ || {}; S.tpQ[ns] = el.value;
      const box = document.getElementById("tp-res-" + ns), wrap = el.closest(".tp2");
      if (box) box.innerHTML = el.value.trim() ? tpResults(ns, norm(el.value.trim())) : "";
      if (wrap) wrap.classList.toggle("searching", !!el.value.trim());
      return;
    }
    if (el.id === "mast-q") { S.mastQ = el.value; S.mastP = 1; const pos = el.selectionStart; SJ.render(); keepFocus("mast-q", pos); return; }
    if (el.id === "onb-q") { S.onbQ = el.value; const pos = el.selectionStart; SJ.render(); keepFocus("onb-q", pos); }
  });
})();
