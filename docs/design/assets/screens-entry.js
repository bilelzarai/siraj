/* Sirāj v2: entry screens (welcome, sign in, sign up, guest, onboarding) and Home. */
(function () {
  "use strict";
  const SJ = window.SJ, S = SJ.S, I = SJ.icon, E = SJ.esc, t = SJ.t, L = SJ.L, X = SJ.tx;

  /* ---------- dates ---------- */
  SJ.hijri = () => {
    try { return new Intl.DateTimeFormat((S.lang === "ar" ? "ar" : S.lang) + "-u-ca-islamic-umalqura-nu-latn", { day: "numeric", month: "long", year: "numeric" }).format(new Date()); }
    catch (e) { return "26 Rabīʿ al-Thānī 1448"; }
  };
  SJ.greg = () => {
    try { return new Intl.DateTimeFormat((S.lang === "ar" ? "ar" : S.lang) + "-u-nu-latn", { weekday: "long", day: "numeric", month: "long" }).format(new Date()); }
    catch (e) { return "Wednesday 7 October"; }
  };
  SJ.daysToRamadan = () => {
    try {
      const f = new Intl.DateTimeFormat("en-u-ca-islamic-umalqura", { day: "numeric", month: "numeric" });
      const now = new Date();
      for (let i = 0; i < 400; i++) {
        const d = new Date(now.getTime() + i * 864e5);
        const parts = f.formatToParts(d);
        const m = +parts.find((p) => p.type === "month").value, dd = +parts.find((p) => p.type === "day").value;
        if (m === 9 && dd === 1) return { days: i, date: new Intl.DateTimeFormat(S.lang + "-u-nu-latn", { day: "numeric", month: "long", year: "numeric" }).format(d) };
        if (m === 9 && i === 0) return { days: 0, date: "" };
      }
    } catch (e) { /* fall through */ }
    return { days: 124, date: "8 February 2027" };
  };

  /* ---------- Question of the day (shared by welcome + home) ---------- */
  function qotdCard(id, compact) {
    const q = SJ.QUESTIONS.find((x) => x.id === SJ.QOTD);
    const c = SJ.cat(q.cat), lang = S.lang, picked = S.qotd.picked, done = picked !== null, got = picked === q.c;
    const letters = lang === "ar" ? ["أ", "ب", "ج", "د"] : ["A", "B", "C", "D"];
    const ans = q.a[lang].map((a, i) => {
      let cls = "ans dark";
      if (done) cls += i === q.c ? " is-right" : i === picked ? " is-wrong" : " is-dim";
      return `<button class="${cls}${lang === "ar" ? " qtext-ar" : ""}" data-act="qotd" data-v="${i}" ${done ? "disabled" : ""}><span class="k">${letters[i]}</span>${E(a)}</button>`;
    }).join("");
    return `<section class="lamp-card" aria-labelledby="${id}">${SJ.PATTERN("kh-" + id)}<div class="in">
      <div class="row wrap"><span class="eyebrow">${I("bolt", "i-sm")}${t("qotd")}</span><span class="meta">${E(L(c.name))} · ${SJ.lvlName(q.lvl)} · +20 ${t("points")}</span>${compact ? "" : `<span class="meta" style="margin-inline-start:auto">${X("New question in 7 h 12 min","سؤال جديد بعد 7 س 12 د","Nouvelle question dans 7 h 12 min")}</span>`}</div>
      <h2 id="${id}" lang="${lang}">${E(q.q[lang])}</h2>
      <div class="answers">${ans}</div>
      ${done ? `<div class="feedback" style="background:rgba(255,255,255,.07);border-color:rgba(255,255,255,.14);color:#eef5f1">
        <div class="ft" style="color:${got ? "#7fe0b8" : "#f3b0a7"}">${got ? t("correct") + " · +20 " + t("points") : t("answerIs") + " " + E(q.a[lang][q.c])}</div>
        <p class="fx" lang="${lang}" style="color:#e3dccd">${E(q.x[lang])}</p>
        <div class="src" style="color:#c7bda9"><span>${t("source")}: ${E(q.src)}</span><span>${X("71% of 1,284 players got it right","أجاب 71% من 1,284 لاعبًا إجابة صحيحة","71 % des 1 284 joueurs ont trouvé")}</span>
        ${S.route === "welcome" ? `<button class="btn btn-gold btn-sm" data-act="guest-start" style="margin-inline-start:auto">${X("Play a quick game as a guest","العب لعبة سريعة كضيف","Partie rapide en invité")}</button>` : `<button class="link-btn" data-act="qotd-reset" style="color:#f3d394;margin-inline-start:auto">${X("Reset demo","إعادة التجربة","Réinitialiser")}</button>`}</div></div>`
        : `<p class="meta">${SJ.signedIn() ? X("One try. Answer to keep your streak going.","محاولة واحدة. أجب لتستمر سلسلتك.","Un seul essai. Répondez pour garder votre série.") : X("One try. No account needed.","محاولة واحدة، دون حساب.","Un seul essai, sans compte.")}</p>`}
    </div></section>`;
  }
  SJ.actions["qotd"] = (a) => { S.qotd.picked = +a.dataset.v; SJ.render(); };
  SJ.actions["qotd-reset"] = () => { S.qotd.picked = null; SJ.render(); };

  /* ---------- Welcome ---------- */
  const W = {
    en: { h: "Test what you know about Islam and Arabic.", lead: "Questions in Arabic, English and French, each checked against its source. Play alone, with friends in a live room, or one on one.", have: "I already have an account", f1: "Arabic · English · French", f2: "Every answer cites its source", f3: "Free to play" },
    ar: { h: "اختبر معرفتك بالإسلام واللغة العربية.", lead: "أسئلة بالعربية والإنجليزية والفرنسية، موثّقة بمصادرها. العب وحدك، أو مع أصدقائك في غرفة مباشرة، أو في مبارزة فردية.", have: "لدي حساب بالفعل", f1: "العربية · الإنجليزية · الفرنسية", f2: "كل إجابة موثّقة بمصدرها", f3: "مجاني" },
    fr: { h: "Testez vos connaissances sur l'islam et la langue arabe.", lead: "Des questions en arabe, en anglais et en français, chacune vérifiée par sa source. Jouez seul, entre amis dans un salon en direct, ou en duel.", have: "J'ai déjà un compte", f1: "Arabe · anglais · français", f2: "Chaque réponse cite sa source", f3: "Gratuit" }
  };
  SJ.screens["welcome"] = () => {
    const w = W[S.lang];
    return `<div class="welcome">${SJ.PATTERN("kh-w")}
      <div class="welcome-top"><span class="brand-mark">${SJ.LAMP}</span><span class="brand-name" style="color:#fff">${S.lang === "ar" ? "سراج" : "Sirāj"}</span><span class="spacer"></span>
        <div class="row" style="gap:4px">${[["ar", "ع"], ["en", "EN"], ["fr", "FR"]].map(([k, l]) => `<button class="btn btn-sm btn-ghost" data-act="lang" data-v="${k}" aria-pressed="${S.lang === k}" style="${S.lang === k ? "background:rgba(255,255,255,.12)" : ""}">${l}</button>`).join("")}</div>
        <button class="btn btn-sm btn-outline-light hide-sm" data-go="signin">${t("signin")}</button></div>
      <div class="welcome-body">
        <div class="welcome-copy">
          <h1>${w.h}</h1><p class="lead">${w.lead}</p>
          <div class="row wrap" style="gap:12px"><button class="btn btn-gold btn-lg" data-go="guest-name">${I("tri", "flip")}${t("guest")}</button><button class="btn btn-lg btn-outline-light" data-go="signup">${t("signup")}</button></div>
          <button class="link-btn" data-go="signin" style="color:#f3d394;width:fit-content">${w.have} · ${t("signin")}</button>
          <div class="facts"><span>${I("languages")}${w.f1}</span><span>${I("book", "flip")}${w.f2}</span><span>${I("gift")}${w.f3}</span></div>
        </div>
        <div class="welcome-try">${qotdCard("w-qotd", true)}</div>
      </div></div>`;
  };
  SJ.actions["guest-start"] = () => { S.role = "guest"; SJ.store.set("role", "guest"); SJ.startQuick(); };

  /* ---------- Auth ---------- */
  const authTop = () => `<div class="row"><a class="brand" href="#welcome" data-go="welcome"><span class="brand-mark">${SJ.LAMP}</span><span class="brand-name">Sirāj</span></a><span class="spacer"></span><button class="icon-btn sm" data-go="${SJ.isGuest() ? "home" : "welcome"}" aria-label="Close">${I("x")}</button></div>`;
  SJ.screens["signin"] = () => `<div class="auth-wrap"><div class="auth-card">${authTop()}
    <h1>${t("signin")}</h1><p class="muted">Welcome back. Your streak is waiting.</p>
    <form data-form="signin" style="margin-top:22px" novalidate>
      <div class="field"><label class="label" for="si-email">Email or username</label><input class="input" id="si-email" autocomplete="username" value="aisha_t"></div>
      <div class="field"><div class="row"><label class="label" for="si-pass">Password</label><span class="spacer"></span><button type="button" class="link-btn" style="font-size:13px" data-act="toast" data-msg="Password reset link sent to a***@gmail.com">Forgot password?</button></div><input class="input" id="si-pass" type="password" autocomplete="current-password" value="••••••••••"></div>
      <button class="btn btn-primary btn-block btn-lg" style="margin-top:20px" type="submit">${t("signin")}</button>
    </form>
    <div class="or">or</div>
    <button class="btn btn-block" data-act="google">${I("google")}Continue with Google</button>
    <button class="btn btn-block btn-ghost" style="margin-top:8px" data-go="guest-name">${t("guest")}</button>
    <p class="muted" style="text-align:center;margin-top:18px;font-size:14px">New to Sirāj? <button class="link-btn" data-go="signup">${t("signup")}</button></p>
  </div></div>`;
  SJ.screens["signup"] = () => `<div class="auth-wrap"><div class="auth-card">${authTop()}
    <h1>${t("signup")}</h1><p class="muted">${SJ.isGuest() ? "Your guest points and history move to your new account." : "Free. Takes a minute."}</p>
    <form data-form="signup" style="margin-top:22px" novalidate>
      <div class="field"><label class="label" for="su-name">Your name</label><input class="input" id="su-name" autocomplete="name" placeholder="e.g. Aisha T."></div>
      <div class="field"><label class="label" for="su-user">Username</label><div class="input-icon"><span class="i" style="width:auto;font-weight:700">@</span><input class="input" id="su-user" data-act="username" autocomplete="username" placeholder="aisha_t"></div><span class="help" id="su-user-help">Letters, numbers and underscores. Friends find you with it.</span></div>
      <div class="field"><label class="label" for="su-email">Email</label><input class="input" id="su-email" type="email" autocomplete="email" placeholder="you@example.com"></div>
      <div class="field"><label class="label" for="su-pass">Password</label><input class="input" id="su-pass" type="password" autocomplete="new-password" data-act="pass"><span class="help" id="su-pass-help">At least 8 characters.</span></div>
      <label class="switch" style="margin-top:16px"><input type="checkbox" id="su-terms" checked><span style="font-size:14px">I accept the terms and the privacy policy</span></label>
      <p class="err" id="su-err" hidden></p>
      <button class="btn btn-primary btn-block btn-lg" style="margin-top:20px" type="submit">${t("signup")}</button>
    </form>
    <div class="or">or</div>
    <button class="btn btn-block" data-act="google">${I("google")}Sign up with Google</button>
    <p class="muted" style="text-align:center;margin-top:18px;font-size:14px">Already have an account? <button class="link-btn" data-go="signin">${t("signin")}</button></p>
  </div></div>`;
  SJ.actions["google"] = () => { SJ.setRole("player"); SJ.toast("Signed in with Google. Welcome back, Aisha."); };
  document.addEventListener("input", (e) => {
    if (e.target.id === "su-user") {
      const v = e.target.value.trim().toLowerCase(), h = document.getElementById("su-user-help");
      if (!v) { h.textContent = "Letters, numbers and underscores. Friends find you with it."; h.className = "help"; }
      else if (!/^[a-z0-9_]{3,20}$/.test(v)) { h.textContent = "Use 3 to 20 letters, numbers or underscores."; h.className = "err"; }
      else if (["aisha", "omar", "admin", "siraj"].includes(v)) { h.innerHTML = `@${E(v)} is taken. Try @${E(v)}_${Math.floor(Math.random() * 90 + 10)}.`; h.className = "err"; }
      else { h.textContent = `@${v} is available.`; h.className = "help"; h.style.color = "var(--brand-ink)"; }
    }
    if (e.target.id === "su-pass") {
      const n = e.target.value.length, h = document.getElementById("su-pass-help");
      h.textContent = n === 0 ? "At least 8 characters." : n < 8 ? `${8 - n} more character${8 - n === 1 ? "" : "s"} needed.` : "Good password length.";
      h.style.color = n >= 8 ? "var(--brand-ink)" : "";
    }
  });
  document.addEventListener("submit", (e) => {
    const f = e.target.closest("[data-form]");
    if (!f) return;
    e.preventDefault();
    const kind = f.dataset.form;
    if (kind === "signin") { SJ.setRole("player"); SJ.toast("Welcome back, Aisha. 12-day streak."); }
    if (kind === "signup") {
      const name = document.getElementById("su-name").value.trim(), em = document.getElementById("su-email").value.trim(), pw = document.getElementById("su-pass").value, err = document.getElementById("su-err");
      const miss = []; if (!name) miss.push("your name"); if (!/^\S+@\S+\.\S+$/.test(em)) miss.push("a valid email"); if (pw.length < 8) miss.push("a password of 8+ characters");
      if (miss.length) { err.hidden = false; err.textContent = "Add " + miss.join(", ") + " to continue."; return; }
      S.onb = { step: 1, qlang: S.lang, domains: ["islamic"], cats: [], goal: 5, remind: true, time: "20:00" };
      SJ.setRole("new");
    }
    if (kind === "guest") {
      const v = document.getElementById("g-name").value.trim() || "Guest 4821";
      S.guestName = v; SJ.store.set("guestName", v); SJ.setRole("guest"); SJ.toast("You're in as " + v + ". Have fun.");
    }
    if (SJ.forms[kind]) SJ.forms[kind](f);
  });
  SJ.forms = {};

  SJ.screens["guest-name"] = () => `<div class="auth-wrap"><div class="auth-card">${authTop()}
    <h1>Play as a guest</h1><p class="muted">Pick the name other players see in live rooms.</p>
    <form data-form="guest" style="margin-top:22px">
      <div class="field"><label class="label" for="g-name">Display name</label><input class="input" id="g-name" value="${E(S.guestName)}" maxlength="20"></div>
      <div class="card pad" style="margin-top:18px;background:var(--surface-2)">
        <div class="label" style="margin-bottom:8px">As a guest you can</div>
        <div class="stack" style="gap:6px;font-size:14px"><span class="row">${I("check", "i-sm")}Play solo games in every domain and language</span><span class="row">${I("check", "i-sm")}Join a friend's live room with its code</span><span class="row">${I("check", "i-sm")}See the leaderboards</span></div>
        <div class="label" style="margin:14px 0 8px">With a free account you also get</div>
        <div class="stack muted" style="gap:6px;font-size:14px"><span class="row">${I("lock", "i-sm")}Streaks, levels and a profile</span><span class="row">${I("lock", "i-sm")}Duels, challenges, friends and messages</span><span class="row">${I("lock", "i-sm")}Hosting your own live rooms</span></div>
      </div>
      <button class="btn btn-primary btn-block btn-lg" style="margin-top:20px" type="submit">Start playing</button>
    </form></div></div>`;

  /* ---------- Onboarding ---------- */
  SJ.screens["onboarding"] = () => {
    const o = S.onb || (S.onb = { step: 1, qlang: S.lang, domains: ["islamic"], cats: [], goal: 5, remind: true, time: "20:00" });
    const steps = `<div class="steps">${[1, 2, 3].map((n) => `<i class="${n <= o.step ? "on" : ""}"></i>`).join("")}</div>`;
    let body = "";
    if (o.step === 1) body = `<h1 style="margin-top:0">${SJ.tx("Choose your language","اختر لغتك","Choisissez votre langue")}</h1><p class="muted">${SJ.tx("The app and the questions use it. Change it any time from the globe.","يستخدمها التطبيق والأسئلة. يمكنك تغييرها في أي وقت من رمز الكرة الأرضية.","L'application et les questions l'utilisent. Modifiable à tout moment via le globe.")}</p>
      <div class="stack" style="margin-top:20px">${[["ar", "العربية", "Arabic"], ["en", "English", "English"], ["fr", "Français", "French"]].map(([k, n, s]) => `<button class="dom" data-act="onb-lang" data-v="${k}" aria-pressed="${S.lang === k}" style="width:100%"><span class="cat-ic" style="--c:var(--brand)">${I("languages")}</span><span><span class="dn" lang="${k}" style="${k === "ar" ? "font-family:var(--font-ar-text);font-size:18px" : ""}">${n}</span><span class="dd">${s}</span></span></button>`).join("")}</div>`;
    if (o.step === 2) body = `<h1 style="margin-top:0">What would you like to learn?</h1><p class="muted">Pick at least one. Your Home shows these first.</p>${SJ.onbSearch ? SJ.onbSearch() : ""}
      <div class="stack" style="margin-top:20px">${(SJ.onbDomains ? SJ.onbDomains() : SJ.DOMAINS).map((d) => `<div><button class="dom" data-act="onb-dom" data-v="${d.id}" aria-pressed="${o.domains.includes(d.id)}" style="width:100%"><span class="cat-ic" style="--c:${d.color}">${I(d.icon)}</span><span><span class="dn">${E(L(d.name))}${d.isNew ? ' <span class="badge gold" style="height:20px">New</span>' : ""}</span><span class="dd">${E(L(d.desc))}</span></span></button>
        ${o.domains.includes(d.id) ? `<div class="row wrap" style="margin:10px 0 4px;gap:6px">${SJ.CATS.filter((c) => c.domain === d.id).map((c) => `<button class="chip" style="min-height:34px;font-size:13.5px" data-act="onb-cat" data-v="${c.id}" aria-pressed="${o.cats.includes(c.id)}">${I("check", "i-sm ck")}${E(L(c.name))}</button>`).join("")}</div>` : ""}</div>`).join("")}</div>`;
    if (o.step === 3) body = `<h1 style="margin-top:0">Set a daily goal</h1><p class="muted">Small and steady beats big and rare.</p>
      <div class="stack" style="margin-top:20px">${[[3, "Light", "About 10 minutes a day"], [5, "Steady", "About 15 minutes a day"], [10, "Serious", "About 30 minutes a day"]].map(([n, l, s]) => `<button class="dom" data-act="onb-goal" data-v="${n}" aria-pressed="${o.goal === n}" style="width:100%"><span class="cat-ic" style="--c:var(--lamp)"><b style="font-family:var(--font)">${n}</b></span><span><span class="dn">${l} · ${n} games</span><span class="dd">${s}</span></span></button>`).join("")}</div>
      <div class="card pad" style="margin-top:16px"><label class="switch"><input type="checkbox" data-act="onb-remind" ${o.remind ? "checked" : ""}><span><b>Remind me</b><br><span class="help">One notification for the question of the day</span></span></label>
      ${o.remind ? `<div class="field" style="margin-top:12px"><label class="label" for="onb-time">At</label><select class="select" id="onb-time"><option>07:00 · after Fajr</option><option>13:00 · lunch</option><option selected>20:00 · evening</option></select></div>` : ""}</div>`;
    const canNext = o.step !== 2 || o.domains.length > 0;
    return `<div class="auth-wrap"><div class="auth-card" style="width:min(560px,100%)">${steps}${body}
      <div class="row" style="margin-top:24px">${o.step > 1 ? `<button class="btn btn-ghost" data-act="onb-back">${I("chevL", "flip")}Back</button>` : `<button class="btn btn-ghost" data-act="onb-skip">Skip for now</button>`}<span class="spacer"></span>
      <button class="btn btn-primary" data-act="onb-next" ${canNext ? "" : "disabled"}>${o.step === 3 ? "Finish" : "Continue"}${o.step === 3 ? "" : I("chevR", "flip")}</button></div></div></div>`;
  };
  const onb = () => S.onb;
  SJ.actions["onb-lang"] = (a) => { SJ.setLang(a.dataset.v); };
  SJ.actions["onb-dom"] = (a) => { const d = onb().domains, v = a.dataset.v; onb().domains = d.includes(v) ? d.filter((x) => x !== v) : d.concat(v); SJ.render(); };
  SJ.actions["onb-cat"] = (a) => { const d = onb().cats, v = a.dataset.v; onb().cats = d.includes(v) ? d.filter((x) => x !== v) : d.concat(v); SJ.render(); };
  SJ.actions["onb-goal"] = (a) => { onb().goal = +a.dataset.v; SJ.render(); };
  SJ.changes["onb-remind"] = (a) => { onb().remind = a.checked; SJ.render(); };
  SJ.actions["onb-back"] = () => { onb().step--; SJ.render(); };
  SJ.actions["onb-skip"] = () => { SJ.setRole("player"); };
  SJ.actions["onb-next"] = () => {
    if (onb().step < 3) { onb().step++; SJ.render(); return; }
    SJ.ME.goal = onb().goal;
    SJ.setRole("player"); SJ.toast("All set. Your first question of the day is waiting.");
  };

  /* ---------- Home ---------- */
  function playerCard() {
    const me = SJ.ME;
    if (SJ.isGuest()) return `<aside class="card pad stack" style="gap:16px;border-radius:var(--r-xl)" aria-label="Your progress">
      <div class="row" style="gap:14px">${SJ.meAv("lg")}<div><div style="font-size:18px;font-weight:750">${E(S.guestName)}</div><div class="muted" style="font-size:13.5px">${X("Guest · progress stays on this device","ضيف · يبقى تقدّمك على هذا الجهاز","Invité · progression gardée sur cet appareil")}</div></div></div>
      <div class="stat-row" style="grid-template-columns:repeat(2,minmax(0,1fr))"><div class="stat gold"><div class="k">${X("Points today","نقاط اليوم","Points du jour")}</div><div class="v">${S.guestPts || 0}</div></div><div class="stat"><div class="k">${X("Games","الألعاب","Parties")}</div><div class="v">${S.guestGames || 0}</div></div></div>
      <p class="muted" style="font-size:14px">${X("Create a free account to keep these points, start a streak and appear on the leaderboard.","أنشئ حسابًا مجانيًا لتحتفظ بنقاطك وتبدأ سلسلتك وتظهر في الترتيب.","Créez un compte gratuit pour garder ces points, lancer une série et apparaître au classement.")}</p>
      <button class="btn btn-primary btn-lg" data-go="signup" style="margin-top:auto">${t("signup")}</button></aside>`;
    const pct = Math.round(me.xp / me.xpNext * 100);
    const streak = S.qotd.picked === 205 ? me.streak + 1 : me.streak;
    return `<aside class="card pad stack" style="gap:16px;border-radius:var(--r-xl);box-shadow:var(--shadow-2)" aria-label="Your progress">
      <div class="row" style="gap:14px"><span class="avatar-btn" style="--p:${pct};width:64px;height:64px;cursor:default">${SJ.meAv("lg")}</span><div><div style="font-size:18px;font-weight:750">${SJ.myName()}</div><div class="muted num" style="font-size:13.5px">${X("Level","المستوى","Niveau")} ${me.level} · ${me.xp.toLocaleString()} / ${me.xpNext.toLocaleString()} XP</div></div></div>
      <div><div class="bar" style="height:10px"><i style="--v:${pct}%;--c:linear-gradient(90deg,var(--lamp),#e8b65a)"></i></div><div class="help" style="margin-top:6px">${X(`${me.xpNext - me.xp} XP to level ${me.level + 1}`,`باقي ${me.xpNext - me.xp} نقطة خبرة للمستوى ${me.level + 1}`,`${me.xpNext - me.xp} XP avant le niveau ${me.level + 1}`)}</div></div>
      <div class="stat-row" style="grid-template-columns:repeat(3,minmax(0,1fr));gap:8px"><div class="stat gold" style="padding:12px"><div class="k">${I("flame", "i-sm")} ${X("Streak","السلسلة","Série")}</div><div class="v">${streak}<span class="muted" style="font-size:13px"> ${X("d","يوم","j")}</span></div></div><div class="stat" style="padding:12px"><div class="k">${X("Accuracy","الدقة","Précision")}</div><div class="v">${me.accuracy}%</div></div><div class="stat" style="padding:12px"><div class="k">${X("This week","هذا الأسبوع","Cette semaine")}</div><div class="v">#${me.rank}</div></div></div>
      <div class="card" style="padding:14px"><div class="row"><b>${X("Daily goal","الهدف اليومي","Objectif du jour")}</b><span class="spacer"></span><span class="muted" style="font-size:13.5px">${X(`${me.today} of ${me.goal} games`,`${me.today} من ${me.goal} ألعاب`,`${me.today} sur ${me.goal} parties`)}</span></div><div style="display:grid;grid-template-columns:repeat(${me.goal},minmax(0,1fr));gap:6px;margin-top:10px">${Array.from({ length: me.goal }, (_, i) => `<span style="height:8px;border-radius:999px;background:${i < me.today ? "var(--brand)" : "var(--sunk)"}"></span>`).join("")}</div></div>
      <button class="btn btn-primary btn-lg" data-act="quick" style="margin-top:auto">${I("tri", "flip")}${t("quick")}</button></aside>`;
  }
  SJ.domainSwitch = (act) => `<div class="domain-switch" role="group" aria-label="Domain">${SJ.DOMAINS.map((d) => `<button class="dom" data-act="${act}" data-v="${d.id}" aria-pressed="${S.domain === d.id}"><span class="cat-ic sm" style="--c:${d.color}">${I(d.icon)}</span><span><span class="dn">${E(L(d.name))}${d.isNew ? ` <span class="badge gold" style="height:20px;font-size:11.5px">${X("New","جديد","Nouveau")}</span>` : ""}</span><span class="dd">${SJ.CATS.filter((c) => c.domain === d.id).length} ${X("categories","فئات","catégories")}</span></span></button>`).join("")}</div>`;
  SJ.actions["domain"] = (a) => { S.domain = a.dataset.v; SJ.render(); };
  SJ.catGrid = (domain, withMix) => {
    const cats = SJ.CATS.filter((c) => c.domain === domain);
    return `<div class="grid-auto">${cats.map((c) => `<button class="tile col" data-act="pick-cat" data-v="${c.id}"><span class="row">${SJ.catIc(c)}<span class="spacer"></span>${c.domain === "arabic" ? `<span class="badge gold">${X("New","جديد","Nouveau")}</span>` : ""}</span>
      <span><span class="tt">${E(L(c.name))}</span><span class="ts">${SJ.isGuest() ? SJ.COUNT[c.id] + " " + X("questions","سؤالًا","questions") : X("Mastery","الإتقان","Maîtrise") + " " + c.mastery + "%"}</span></span>
      ${SJ.isGuest() ? "" : `<span class="bar" style="height:6px"><i style="--v:${c.mastery}%;--c:${c.color}"></i></span>`}</button>`).join("")}
      ${withMix ? `<button class="tile col dashed" data-act="daily-mix">${I("shuffle", "i-lg")}<span class="tt">${X("Daily mix","مزيج اليوم","Mix du jour")}</span><span class="ts">${X("One question from every category","سؤال من كل فئة","Une question par catégorie")}</span></button>` : ""}</div>`;
  };
  SJ.actions["pick-cat"] = (a) => { const c = SJ.cat(a.dataset.v); S.setup = Object.assign(S.setup, { domain: c.domain, cats: [c.id], mode: "solo" }); SJ.go("setup"); };

  SJ.modeTiles = () => {
    const g = SJ.isGuest();
    const tile = (act, ic, col, tt, ts, locked) => `<button class="tile ${locked ? "locked" : ""}" data-act="${locked ? "need-account" : act}"><span class="mode-ic ${col}">${I(ic)}</span><span style="min-width:0"><span class="tt">${tt}</span><span class="ts">${ts}</span></span>${locked ? `<span class="lockmark">${I("lock")}</span>` : ""}</button>`;
    return `<div class="grid-auto wide">
      ${tile("quick", "bolt", "green", t("quick"), X("5 questions, mixed topics, about 2 minutes","5 أسئلة من مواضيع متنوعة، نحو دقيقتين","5 questions variées, environ 2 minutes"))}
      ${tile("room-create", "radio", "night", t("liveRoom"), g ? X("Host other guests with a room code","استضف ضيوفًا آخرين برمز الغرفة","Invitez d'autres invités avec un code") : X("Host up to 8 players with a room code","استضف حتى 8 لاعبين برمز الغرفة","Jusqu'à 8 joueurs avec un code"))}
      ${tile("room-join", "door", "gold", t("join"), X("Enter the code a friend sent you","أدخل الرمز الذي أرسله صديقك","Saisissez le code reçu d'un ami"))}
      ${tile("duel", "swords", "blue", t("duel"), g ? X("Quick match against another guest, not rated","مباراة سريعة ضد ضيف آخر، غير مصنّفة","Match rapide contre un invité, non classé") : X("Ranked match against a player of your level","مباراة مصنّفة ضد لاعب في مستواك","Match classé contre un joueur de votre niveau"))}
    </div>`;
  };
  SJ.actions["need-account"] = () => SJ.modal(`<div class="m-ic">${I("lock", "i-lg")}</div><h2>This needs a free account</h2><p>Guests can play solo games and join rooms with a code. Hosting rooms, duels, challenges and friends need an account. Your guest points come with you.</p><div class="actions"><button class="btn" data-act="close">Not now</button><button class="btn btn-primary" data-act="close" data-go="signup">${t("signup")}</button></div>`);
  SJ.actions["quick"] = () => SJ.startQuick();
  SJ.actions["daily-mix"] = () => SJ.startDailyMix();
  SJ.actions["room-create"] = () => { S.setup.mode = "room"; SJ.go("room-create"); };
  SJ.actions["room-join"] = () => SJ.go("room-join");
  SJ.actions["duel"] = () => SJ.go("duel");

  SJ.screens["home"] = () => {
    const me = SJ.ME, guest = SJ.isGuest();
    const first = guest ? S.guestName.split(" ")[0] : (S.lang === "ar" ? me.firstAr : me.first);
    const answered = S.qotd.picked !== null;
    const streakLine = guest ? X("Answer today's question, then try a quick game.","أجب عن سؤال اليوم ثم جرّب لعبة سريعة.","Répondez à la question du jour, puis lancez une partie rapide.") : answered ? (S.qotd.picked === 205 ? X(`Streak: ${me.streak + 1} days. See you tomorrow.`,`سلسلتك الآن ${me.streak + 1} يومًا. نلقاك غدًا.`,`Série : ${me.streak + 1} jours. À demain.`) : X("Your streak is safe. One game adds a day.","سلسلتك محفوظة. لعبة واحدة تضيف يومًا.","Votre série est sauve. Une partie ajoute un jour.")) : X(`Answer today's question to keep your ${me.streak}-day streak.`,`أجب عن سؤال اليوم لتحافظ على سلسلة ${me.streak} يومًا.`,`Répondez à la question du jour pour garder votre série de ${me.streak} jours.`);
    const ram = SJ.daysToRamadan();
    const inCh = S.challenges.filter((c) => c.kind === "in").slice(0, 2);
    const friends = S.friends.map(SJ.person);
    const lb = [SJ.person("khadija"), SJ.person("omar"), "me", SJ.person("youssef"), SJ.person("sarah")];
    return `<div class="page" style="display:flex;flex-direction:column;gap:28px">
      <div class="page-head" style="margin:0"><div><p class="muted" style="font-weight:600;font-size:14px">${E(SJ.greg())} · ${E(SJ.hijri())}</p><h1 style="margin-top:6px">${t("greeting")}${S.lang === "ar" ? " يا " : ", "}${E(first)}</h1></div><p style="color:var(--ink-2)">${streakLine}</p></div>
      ${guest ? `<div class="banner"><span class="mode-ic gold" style="width:40px;height:40px">${I("star")}</span><div class="grow">${X("<b>You're playing as a guest.</b> Create a free account to keep your points, start a streak and challenge friends.","<b>أنت تلعب كضيف.</b> أنشئ حسابًا مجانيًا لتحتفظ بنقاطك وتبدأ سلسلتك وتتحدى أصدقاءك.","<b>Vous jouez en invité.</b> Créez un compte gratuit pour garder vos points, lancer une série et défier vos amis.")}</div><button class="btn btn-primary btn-sm" data-go="signup">${t("signup")}</button></div>` : ""}
      <div class="cols" style="align-items:stretch"><div style="flex:999 1 600px;min-width:0">${qotdCard("h-qotd")}</div><div style="flex:1 1 320px;min-width:0;display:flex">${playerCard()}</div></div>
      <section class="section"><div class="section-head"><h2>${X("Start a game","ابدأ لعبة","Lancer une partie")}</h2><button class="link-btn" data-go="play">${X("All ways to play","كل طرق اللعب","Toutes les façons de jouer")}</button></div>${SJ.modeTiles()}</section>
      <div class="cols">
        <div class="col-main">
          <section class="section"><div class="section-head"><h2>${X("Categories","الفئات","Catégories")}</h2></div>${SJ.domainSwitch("domain")}${SJ.catGrid(S.domain, true)}</section>
          ${guest ? "" : `<section class="section"><div class="section-head"><h2>${t("challenges")}</h2><button class="link-btn" data-go="challenges">${X("All challenges","كل التحديات","Tous les défis")}</button></div>
            <div class="card">${inCh.length ? inCh.map((c) => SJ.challengeRow(c)).join("") : `<div class="empty"><h3>No challenges waiting</h3><p>Challenge a friend to the same 10 questions.</p></div>`}</div></section>`}
        </div>
        <div class="col-side">
          <section class="card pad"><div class="card-head"><h2>${X("This week","هذا الأسبوع","Cette semaine")}</h2><button class="link-btn" data-go="leaderboard">${t("leaderboard")}</button></div>
            <ol class="list" style="margin:0 -16px">${lb.map((p, i) => p === "me" ? (guest ? `<li class="li"><span class="rank">·</span>${SJ.meAv("sm")}<span class="grow t">${X("You","أنت","Vous")}</span><span class="muted" style="font-size:13px">${X("Sign up to rank","سجّل لتظهر","Inscrivez-vous")}</span></li>` : `<li class="li me"><span class="rank">${i + 1}</span>${SJ.meAv("sm")}<span class="grow t">${X("You","أنت","Vous")}</span><b class="num">${me.weekPts.toLocaleString()}</b></li>`) : `<li class="li"><span class="rank ${i === 0 ? "r1" : ""}">${i + 1}</span>${SJ.av(p, "sm")}<span class="grow t">${E(SJ.pname(p))}</span><span class="num" style="font-weight:650;color:var(--ink-2)">${p.week.toLocaleString()}</span></li>`).join("")}</ol></section>
          ${guest ? "" : `<section class="card pad"><div class="card-head"><h2>${t("friends")}</h2><span class="muted" style="font-size:13px">${friends.filter((f) => f.online).length} ${X("online","متصلون","en ligne")}</span></div>
            <div class="stack">${friends.slice(0, 3).map((f) => `<div class="row" style="gap:12px">${SJ.av(f, "", true)}<span style="flex:1;min-width:0"><b style="display:block">${E(SJ.pname(f))}</b><span class="muted" style="font-size:13px">${E(f.status)}</span></span><button class="icon-btn sm" data-go="messages" data-with="${f.id}" aria-label="Message ${E(f.name)}" style="border:1px solid var(--line)">${I("chat", "i-sm flip")}</button></div>`).join("")}</div>
            <button class="btn btn-block" style="margin-top:14px" data-act="invite">${I("plus")}${X("Invite a friend","ادعُ صديقًا","Inviter un ami")}</button></section>`}
          <section class="ramadan"><svg class="moon" viewBox="0 0 120 120" aria-hidden="true"><path d="M84 30a34 34 0 1 0 22 50 27 27 0 1 1-22-50z" fill="#f3d394"/><path d="m98 26 2.4 4.9 5.4.8-3.9 3.8.9 5.3-4.8-2.5-4.8 2.5.9-5.3-3.9-3.8 5.4-.8z" fill="#f3d394"/></svg>
            <div class="rin"><span style="font-weight:700;color:#c4cff3">${X("Ramadan 1448","رمضان 1448","Ramadan 1448")}</span><span class="rd"><span class="num">${ram.days}</span> ${X("days","يومًا","jours")}</span><p>${X(`Expected around ${E(ram.date)}, depending on the moon sighting.`,`يُتوقع نحو ${E(ram.date)}، بحسب رؤية الهلال.`,`Prévu vers le ${E(ram.date)}, selon l'observation du croissant.`)}</p><a href="#setup" data-act="pick-cat" data-v="ramadan">${X("Practise Ramadan &amp; Fasting","تدرّب على رمضان والصيام","S'entraîner : Ramadan et jeûne")}</a></div></section>
        </div>
      </div></div>`;
  };
  SJ.actions["invite"] = () => SJ.modal(`<div class="m-ic green">${I("link", "i-lg")}</div><h2>Invite a friend</h2><p>Share this link. When they join, you both get 100 XP.</p>
    <div class="row" style="margin-top:16px"><input class="input" readonly value="https://siraj.app/i/aisha_t" id="inv-link" aria-label="Invite link"><button class="btn" data-act="copy" data-target="inv-link">${I("copy")}Copy</button></div>
    <div class="actions"><button class="btn btn-primary" data-act="close">Done</button></div>`);
  SJ.actions["copy"] = (a) => {
    const el = document.getElementById(a.dataset.target); const val = el ? (el.value || el.textContent) : a.dataset.text;
    const ok = () => SJ.toast("Copied: " + val);
    try { navigator.clipboard.writeText(val).then(ok, () => { if (el && el.select) el.select(); ok(); }); } catch (e) { if (el && el.select) el.select(); ok(); }
  };
})();
