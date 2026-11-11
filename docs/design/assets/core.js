/* Sirāj v2 prototype core: state, helpers, router, shell, overlays, prototype controls. */
(function () {
  "use strict";
  const SJ = window.SJ;

  /* ---------------- icons ---------------- */
  const P = {
    home: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V21h14V9.5"/><path d="M10 21v-6h4v6"/>',
    play: '<rect x="2" y="7" width="20" height="12" rx="6"/><path d="M7 11v4M5 13h4"/><circle cx="16" cy="12" r="1" fill="currentColor"/><circle cx="18" cy="14.5" r="1" fill="currentColor"/>',
    swords: '<path d="M14.5 17.5 3 6V3h3l11.5 11.5"/><path d="m13 19 6-6M16 16l4 4M19 21l2-2"/><path d="M9.5 6.5 13 3h3v3l-3.5 3.5"/><path d="m5 14 4 4M7 17l-3 3M3 19l2 2"/>',
    chat: '<path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.4A8 8 0 1 1 21 12z"/>',
    users: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
    trophy: '<path d="M8 21h8M12 17v4"/><path d="M7 4h10v5a5 5 0 0 1-10 0z"/><path d="M17 5h3v2a3 3 0 0 1-3 3M7 5H4v2a3 3 0 0 0 3 3"/>',
    globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a14 14 0 0 1 0 18a14 14 0 0 1 0-18z"/>',
    monitor: '<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
    moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
    user: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
    star: '<path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3 6.4 20.2l1.1-6.2L3 9.6l6.2-.9z"/>',
    sliders: '<path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/>',
    shield: '<path d="M12 3 4 6v6c0 5 3.4 8.5 8 9.5 4.6-1 8-4.5 8-9.5V6z"/>',
    logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>',
    book: '<path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20V3H6.5A2.5 2.5 0 0 0 4 5.5z"/><path d="M4 19.5A2.5 2.5 0 0 0 6.5 22H20v-5"/>',
    mosque: '<path d="M4 21h16"/><path d="M6 21v-8a6 6 0 0 1 12 0v8"/><path d="M12 4v3"/><path d="M10 21v-4a2 2 0 0 1 4 0v4"/>',
    scale: '<path d="M12 3v18"/><path d="M7 21h10"/><path d="M5 7h14"/><path d="m5 7-3 6a3 3 0 0 0 6 0z"/><path d="m19 7-3 6a3 3 0 0 0 6 0z"/>',
    scroll: '<path d="M8 21h10a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2H8"/><path d="M8 3a2 2 0 0 0-2 2v12"/><path d="M4 17h4v2a2 2 0 1 1-4 0z"/><path d="M11 8h6M11 12h6"/>',
    columns: '<path d="M3 21h18"/><path d="M4 10h16"/><path d="m12 3 9 5H3z"/><path d="M6 10v8M10 10v8M14 10v8M18 10v8"/>',
    type: '<path d="M4 7V5h16v2"/><path d="M9 19h6"/><path d="M12 5v14"/>',
    languages: '<path d="m5 8 6 6"/><path d="m4 14 6-6 2-3"/><path d="M2 5h12"/><path d="M7 2h1"/><path d="m22 22-5-10-5 10"/><path d="M14 18h6"/>',
    puzzle: '<path d="M14 4a2 2 0 1 0-4 0v2H6v4h2a2 2 0 1 1 0 4H6v4h4v-2a2 2 0 1 1 4 0v2h4v-4h-2a2 2 0 1 1 0-4h2V6h-4z"/>',
    feather: '<path d="M20.2 3.8a6 6 0 0 0-8.5 0L5 10.5V19h8.5l6.7-6.7a6 6 0 0 0 0-8.5z"/><path d="M16 8 2 22"/><path d="M17.5 15H9"/>',
    pen: '<path d="M17 3a2.8 2.8 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5z"/>',
    bolt: '<path d="M13 2 3 14h9l-1 8 10-12h-9z"/>',
    layers: '<path d="m12 3 9 5-9 5-9-5 9-5z"/><path d="m3 13 9 5 9-5"/>',
    shuffle: '<path d="M2 18h1.4a4 4 0 0 0 3.3-1.7l5.6-8.6A4 4 0 0 1 15.6 6H22"/><path d="m18 2 4 4-4 4"/><path d="M2 6h1.9a4 4 0 0 1 3.2 1.6"/><path d="M22 18h-6.1a4 4 0 0 1-3.3-1.8l-.4-.6"/><path d="m18 14 4 4-4 4"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    check: '<path d="m5 12 5 5 9-10"/>',
    x: '<path d="M18 6 6 18M6 6l12 12"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    flame: '<path d="M12 22c4 0 7-2.7 7-7 0-3.2-2-5.6-3.5-7.2-.4 1.9-1.5 3-2.5 3.2.4-3.6-1.3-6.6-4-9 0 3.4-4 6.2-4 13 0 4.3 3 7 7 7z"/>',
    eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
    lock: '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
    copy: '<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/>',
    send: '<path d="m22 2-7 20-4-9-9-4z"/><path d="M22 2 11 13"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
    chevR: '<path d="m9 6 6 6-6 6"/>',
    chevL: '<path d="m15 6-6 6 6 6"/>',
    arrowL: '<path d="M19 12H5"/><path d="m12 19-7-7 7-7"/>',
    flag: '<path d="M4 22V4"/><path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V4s-1 1-4 1-5-2-8-2-4 1-4 1"/>',
    up: '<path d="M7 10v12"/><path d="M15 5.88 14 10h5.83a2 2 0 0 1 1.92 2.56l-2.33 8A2 2 0 0 1 17.5 22H4a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2h2.76a2 2 0 0 0 1.79-1.11L12 2a3.13 3.13 0 0 1 3 3.88z"/>',
    down: '<path d="M17 14V2"/><path d="M9 18.12 10 14H4.17a2 2 0 0 1-1.92-2.56l2.33-8A2 2 0 0 1 6.5 2H20a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-2.76a2 2 0 0 0-1.79 1.11L12 22a3.13 3.13 0 0 1-3-3.88z"/>',
    mail: '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 7 9 6 9-6"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 16v-4"/><path d="M12 8h.01"/>',
    alert: '<path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
    heart: '<path d="M19 14c1.5-1.5 3-3.2 3-5.5A5.5 5.5 0 0 0 12 5a5.5 5.5 0 0 0-10 3.5c0 2.3 1.5 4 3 5.5l7 7z"/>',
    mic: '<rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 10a7 7 0 0 0 14 0"/><path d="M12 17v5"/>',
    clip: '<path d="m21 12-8.6 8.6a5.5 5.5 0 0 1-7.8-7.8l8.6-8.6a3.7 3.7 0 0 1 5.2 5.2l-8.6 8.6a1.8 1.8 0 0 1-2.6-2.6l7.9-7.9"/>',
    smile: '<circle cx="12" cy="12" r="9"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><path d="M9 9h.01M15 9h.01"/>',
    more: '<circle cx="12" cy="5" r="1.3" fill="currentColor"/><circle cx="12" cy="12" r="1.3" fill="currentColor"/><circle cx="12" cy="19" r="1.3" fill="currentColor"/>',
    reply: '<path d="m9 17-5-5 5-5"/><path d="M4 12h11a5 5 0 0 1 5 5v2"/>',
    pause: '<rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/>',
    image: '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-5-5L5 21"/>',
    upload: '<path d="M12 21V9"/><path d="m7 14 5-5 5 5"/><path d="M5 3h14"/>',
    history: '<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
    pin: '<path d="M12 17v5"/><path d="M9 10.8V4h6v6.8l3 3.2H6z"/>',
    mute: '<path d="M11 5 6 9H2v6h4l5 4z"/><path d="m22 9-6 6M16 9l6 6"/>',
    phone: '<rect x="6" y="2" width="12" height="20" rx="2"/><path d="M11 18h2"/>',
    shieldT: '<path d="M12 3 4 6v6c0 5 3.4 8.5 8 9.5 4.6-1 8-4.5 8-9.5V6z"/><path d="M12 8v8M8 12h8"/>',
    tri: '<path d="M7 4l13 8-13 8z"/>',
    skip: '<path d="m5 4 10 8-10 8z"/><path d="M19 5v14"/>',
    half: '<circle cx="12" cy="12" r="9"/><path d="M12 3v18"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor"/>',
    crown: '<path d="m2 8 4 10h12l4-10-6 4-4-7-4 7z"/><path d="M6 21h12"/>',
    link: '<path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7"/><path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7"/>',
    bell: '<path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.9 1.9 0 0 0 3.4 0"/>',
    trash: '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>',
    target: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1"/>',
    radio: '<circle cx="12" cy="12" r="2"/><path d="M16.2 7.8a6 6 0 0 1 0 8.4M7.8 16.2a6 6 0 0 1 0-8.4M19.1 4.9a10 10 0 0 1 0 14.2M4.9 19.1a10 10 0 0 1 0-14.2"/>',
    door: '<path d="M13 4h3a2 2 0 0 1 2 2v14"/><path d="M2 20h3"/><path d="M13 20h9"/><path d="M10 12v.01"/><path d="M13 4.56v16.88a.5.5 0 0 1-.63.48L5 20V5.4a1 1 0 0 1 .76-.97l6.62-1.66a.5.5 0 0 1 .62.49z"/>',
    gift: '<rect x="3" y="8" width="18" height="4" rx="1"/><path d="M12 8v13M19 12v7a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-7"/><path d="M7.5 8a2.5 2.5 0 0 1 0-5C11 3 12 8 12 8s1-5 4.5-5a2.5 2.5 0 0 1 0 5"/>',
    ball: '<circle cx="12" cy="12" r="9"/><path d="m12 7 4 3-1.5 4.5h-5L8 10z"/><path d="M12 3v4M16 10l4.5-1.5M14.5 14.5 17 19M9.5 14.5 7 19M8 10 3.5 8.5"/>',
    briefcase: '<rect x="3" y="7" width="18" height="13" rx="2"/><path d="M8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M3 13h18"/>',
    map: '<path d="M9 4 3 6v14l6-2 6 2 6-2V4l-6 2z"/><path d="M9 4v14M15 6v14"/>',
    atom: '<circle cx="12" cy="12" r="1.6" fill="currentColor"/><ellipse cx="12" cy="12" rx="10" ry="4"/><ellipse cx="12" cy="12" rx="10" ry="4" transform="rotate(60 12 12)"/><ellipse cx="12" cy="12" rx="10" ry="4" transform="rotate(120 12 12)"/>',
    flask: '<path d="M9 3h6"/><path d="M10 3v6L4.5 19a1.5 1.5 0 0 0 1.3 2h12.4a1.5 1.5 0 0 0 1.3-2L14 9V3"/><path d="M7 15h10"/>',
    chart: '<path d="M3 3v18h18"/><path d="m7 15 4-4 3 3 6-7"/><path d="M16 7h4v4"/>',
    coin: '<circle cx="12" cy="12" r="9"/><path d="M14.8 9a2.6 2.6 0 0 0-2.8-1.5c-1.6 0-2.8.9-2.8 2.1 0 2.9 5.8 1.6 5.8 4.6 0 1.2-1.2 2.1-2.9 2.1A3 3 0 0 1 9 14.8"/><path d="M12 6v1.5M12 16.5V18"/>',
    google: '<path d="M21.8 12.2c0-.7-.1-1.4-.2-2H12v3.8h5.5a4.7 4.7 0 0 1-2 3.1v2.6h3.3c1.9-1.8 3-4.4 3-7.5z" fill="#4285F4" stroke="none"/><path d="M12 22c2.7 0 5-.9 6.7-2.4l-3.3-2.6c-.9.6-2 1-3.4 1-2.6 0-4.8-1.8-5.6-4.1H3v2.6A10 10 0 0 0 12 22z" fill="#34A853" stroke="none"/><path d="M6.4 13.9a6 6 0 0 1 0-3.8V7.5H3a10 10 0 0 0 0 9z" fill="#FBBC05" stroke="none"/><path d="M12 5.9c1.5 0 2.8.5 3.8 1.5l2.9-2.9A10 10 0 0 0 3 7.5l3.4 2.6C7.2 7.7 9.4 5.9 12 5.9z" fill="#EA4335" stroke="none"/>'
  };
  SJ.icon = (n, c = "") => `<svg class="i ${c}" viewBox="0 0 24 24" aria-hidden="true">${P[n] || ""}</svg>`;
  SJ.LAMP = '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="19.6" cy="6.4" r="4.2" fill="#ffd885" opacity=".22"/><path d="M19.7 2.4c1.5 1.8 1.9 3.2 1.3 4.5-.4.8-1.2 1.1-1.9.8-.9-.4-1-1.5-.3-2.6.5-.7.8-1.5.9-2.7z" fill="#ffd885"/><path d="M2 12.6h13.1c1.5 0 2.5-.5 3.4-1.4l1.9-1.8c.3-.3.8 0 .6.4-.9 2.4-2.9 6.1-8.3 6.1H7.6c-2.9 0-4.9-1.3-5.6-3.3z" fill="#e8b65a"/><path d="M8.6 15.9 7.5 19.6h7.2l-1.1-3.7z" fill="#c48f2f"/><path d="M6.5 20.4h9.2" stroke="#e8b65a" stroke-width="1.5" stroke-linecap="round"/></svg>';
  SJ.PATTERN = (id) => `<svg class="pattern" aria-hidden="true"><defs><pattern id="${id}" width="64" height="64" patternUnits="userSpaceOnUse"><rect x="14" y="14" width="36" height="36" fill="none" stroke="#e8d3a2" stroke-width="1.2"/><rect x="14" y="14" width="36" height="36" fill="none" stroke="#e8d3a2" stroke-width="1.2" transform="rotate(45 32 32)"/><circle cx="32" cy="32" r="5" fill="none" stroke="#e8d3a2" stroke-width="1.2"/></pattern></defs><rect width="100%" height="100%" fill="url(#${id})"/></svg>`;

  /* ---------------- state ---------------- */
  const store = {
    get(k, d) { try { const v = localStorage.getItem("sj2-" + k); return v === null ? d : JSON.parse(v); } catch (e) { return d; } },
    set(k, v) { try { localStorage.setItem("sj2-" + k, JSON.stringify(v)); } catch (e) { /* storage blocked: keep in memory */ } }
  };
  const S = SJ.S = {
    role: store.get("role", "player"),      // visitor | guest | new | player | staff
    lang: store.get("lang", "en"),
    theme: store.get("theme", "device"),
    guestName: store.get("guestName", "Guest 4821"),
    route: "home", params: {}, menu: null, proto: false,
    domain: "islamic",
    qotd: { picked: null },
    setup: { domain: "islamic", cats: [], level: "mixed", count: 10, timer: 20, mode: "solo" },
    friends: SJ.FRIENDS.slice(), requests: SJ.REQUESTS.slice(), sent: [],
    challenges: [
      { id: 1, kind: "in", who: "omar", cat: "hadith", n: 10, note: "expires in 5 h" },
      { id: 2, kind: "in", who: "khadija", cat: "ramadan", n: 10, note: "her score 8 / 10", score: 8 },
      { id: 3, kind: "out", who: "sarah", cat: "prophets", n: 5, note: "sent yesterday · waiting" },
      { id: 4, kind: "done", who: "youssef", cat: "fiqh", n: 10, mine: 9, theirs: 7, note: "yesterday" },
      { id: 5, kind: "done", who: "omar", cat: "quran", n: 10, mine: 6, theirs: 8, note: "Monday" },
      { id: 6, kind: "done", who: "khadija", cat: "seerah", n: 5, mine: 5, theirs: 5, note: "last week" }
    ],
    threads: null, ratings: {}, unread: { messages: 3 }
  };
  SJ.store = store;

  SJ.t = (k) => { const e = SJ.T[k]; return e ? (e[S.lang] || e.en) : k; };
  SJ.tx = (en, ar, fr) => (S.lang === "ar" ? ar : S.lang === "fr" ? (fr || en) : en);
  SJ.L = (obj) => (obj ? obj[S.lang] || obj.en : "");
  SJ.esc = (s) => String(s == null ? "" : s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  SJ.cat = (id) => SJ.CATS.find((c) => c.id === id);
  SJ.dom = (id) => SJ.DOMAINS.find((d) => d.id === id);
  SJ.person = (id) => SJ.PEOPLE.find((p) => p.id === id);
  SJ.pname = (p) => (S.lang === "ar" ? p.ar : p.name);
  SJ.isGuest = () => S.role === "guest";
  SJ.signedIn = () => S.role === "player" || S.role === "staff";
  SJ.av = (p, size = "", dot = false) => `<span class="av ${size}" style="--c:${p.c}">${SJ.esc(S.lang === "ar" ? p.ar.charAt(0) : p.ini)}${dot && p.online ? '<span class="dot"></span>' : ""}</span>`;
  SJ.meAv = (size = "") => SJ.isGuest() ? `<span class="av guest ${size}">G</span>` : `<span class="av ${size}" style="--c:linear-gradient(160deg,#c48f2f,#8a5a0c);background:linear-gradient(160deg,#c48f2f,#8a5a0c)">${S.lang === "ar" ? SJ.ME.initialsAr : SJ.ME.initials}</span>`;
  SJ.myName = () => SJ.isGuest() ? S.guestName : (S.lang === "ar" ? "عائشة ت." : SJ.ME.name);
  SJ.catIc = (c, size = "") => `<span class="cat-ic ${size}" style="--c:${c.color}">${SJ.icon(c.icon)}</span>`;
  SJ.lvlName = (l) => SJ.t(["", "easy", "medium", "hard"][l]);

  /* ---------------- theme + language ---------------- */
  function applyTheme() {
    const r = document.documentElement;
    if (S.theme === "device") r.removeAttribute("data-theme"); else r.setAttribute("data-theme", S.theme);
  }
  function applyLang() {
    document.documentElement.lang = S.lang;
    document.documentElement.dir = S.lang === "ar" ? "rtl" : "ltr";
  }
  SJ.setTheme = (t) => { S.theme = t; store.set("theme", t); applyTheme(); render(); };
  SJ.setLang = (l) => { S.lang = l; store.set("lang", l); applyLang(); render(); };
  SJ.setRole = (r) => {
    S.role = r; store.set("role", r);
    if (r === "visitor") go("welcome"); else if (r === "new") go("onboarding"); else go("home");
  };

  /* ---------------- router ---------------- */
  const LOCKED_FOR_GUEST = ["messages", "friends", "profile", "achievements", "notifications", "history", "my-questions"];
  const PUBLIC = ["welcome", "signin", "signup", "guest-name", "support"];
  const FOCUS = ["game", "live", "duel-game", "welcome", "signin", "signup", "guest-name", "onboarding", "countdown", "handoff"];
  SJ.screens = {};
  function go(route, params) {
    SJ.stopTimers && SJ.stopTimers();
    SJ.closeOverlay && SJ.closeOverlay();
    document.querySelectorAll(".countdown").forEach((n) => n.remove());
    S.route = route; S.params = params || {}; S.menu = null;
    try { if (location.hash.slice(1) !== route) history.replaceState(null, "", "#" + route); } catch (e) { /* sandboxed: ignore */ }
    render();
    window.scrollTo(0, 0);
  }
  SJ.go = go;
  window.addEventListener("hashchange", () => { const r = location.hash.slice(1); if (r && r !== S.route && SJ.screens[r]) go(r); });

  function resolve() {
    let r = S.route;
    if (S.role === "visitor" && !PUBLIC.includes(r)) r = "welcome";
    if (S.role === "new" && r !== "onboarding") r = "onboarding";
    if (!SJ.screens[r]) r = "home";
    return r;
  }

  /* ---------------- shell ---------------- */
  function navCounts(id) {
    if (!SJ.signedIn()) return "";
    if (id === "challenges") { const n = S.challenges.filter((c) => c.kind === "in").length; return n ? `<span class="count gold">${n}</span>` : ""; }
    if (id === "messages" && S.unread.messages) return `<span class="count green">${S.unread.messages}</span>`;
    return "";
  }
  const NAV = [["home", "home"], ["play", "play"], ["challenges", "swords"], ["messages", "chat"], ["friends", "users"], ["leaderboard", "trophy"]];
  function activeNav(r) {
    if (["setup", "results", "room-create", "room-join", "lobby", "podium", "duel", "domain"].includes(r)) return "play";
    if (r === "challenge-new") return "challenges";
    return r;
  }
  function topnav(r) {
    const act = activeNav(r);
    const links = NAV.map(([id, ic]) => {
      const locked = SJ.isGuest() && LOCKED_FOR_GUEST.includes(id);
      return `<button class="navlink" data-go="${id}" ${act === id ? 'aria-current="page"' : ""}>${SJ.icon(ic, id === "messages" ? "flip" : "")}<span>${SJ.t(id)}</span>${locked ? SJ.icon("lock", "i-sm lock") : navCounts(id)}</button>`;
    }).join("");
    const langCode = { en: "EN", ar: "ع", fr: "FR" }[S.lang];
    const right = SJ.isGuest()
      ? `<div class="signin-cta hide-sm"><button class="btn btn-sm btn-ghost" data-go="signin">${SJ.t("signin")}</button><button class="btn btn-sm btn-primary" data-go="signup">${SJ.t("signup")}</button></div>`
      : "";
    const pct = Math.round(SJ.ME.xp / SJ.ME.xpNext * 100);
    return `<header class="topnav"><div class="topnav-in">
      <a class="brand" href="#home" data-go="home" aria-label="Sirāj"><span class="brand-mark">${SJ.LAMP}</span><span class="brand-name">${S.lang === "ar" ? "سراج" : "Sirāj"}</span></a>
      <nav class="navlinks" aria-label="Main">${links}</nav>
      <div class="nav-tools">
        ${right}
        <button class="tool" data-act="menu" data-menu="lang" aria-label="Language" aria-expanded="${S.menu === "lang"}">${SJ.icon("globe")}<span>${langCode}</span></button>
        <button class="tool theme-tool" data-act="menu" data-menu="theme" aria-label="Theme" aria-expanded="${S.menu === "theme"}">${SJ.icon("monitor")}</button>
        <button class="avatar-btn" style="--p:${SJ.isGuest() ? 0 : pct}" data-act="menu" data-menu="me" aria-label="Your account" aria-expanded="${S.menu === "me"}">${SJ.meAv()}${SJ.isGuest() ? "" : `<span class="lvl-badge">${SJ.ME.level}</span>`}</button>
        ${S.menu ? menus() : ""}
      </div></div></header>`;
  }
  SJ.langMenu = () => menus("lang");
  function menus(force) {
    if ((force || S.menu) === "lang") return `<div class="menu" role="menu" style="inset-inline-end:${force ? 0 : 96}px">
      ${[["ar", "العربية", "AR"], ["en", "English", "EN"], ["fr", "Français", "FR"]].map(([k, n, c]) => `<button class="mi ${S.lang === k ? "on" : ""}" role="menuitemradio" aria-checked="${S.lang === k}" data-act="lang" data-v="${k}" lang="${k}" style="${k === "ar" ? "font-family:var(--font-ar-text);font-size:17px" : ""}"><span style="flex:1">${n}</span>${S.lang === k ? SJ.icon("check", "i-sm") : `<span class="muted" style="font-size:12px;font-family:var(--font)">${c}</span>`}</button>`).join("")}</div>`;
    if (S.menu === "theme") return `<div class="menu" role="menu" style="inset-inline-end:50px">
      ${[["light", "sun"], ["dark", "moon"], ["device", "monitor"]].map(([k, ic]) => `<button class="mi ${S.theme === k ? "on" : ""}" role="menuitemradio" aria-checked="${S.theme === k}" data-act="theme" data-v="${k}">${SJ.icon(ic)}${SJ.t(k)}</button>`).join("")}</div>`;
    if (S.menu === "me") {
      if (SJ.isGuest()) return `<div class="menu" role="menu" style="min-width:260px"><div class="mh"><b>${SJ.esc(S.guestName)}</b><div class="muted" style="font-size:13px">Guest · progress stays on this device</div></div>
        <button class="mi" data-go="signup">${SJ.icon("star")}${SJ.t("signup")}</button><button class="mi" data-go="signin">${SJ.icon("user")}${SJ.t("signin")}</button><button class="mi" data-go="settings">${SJ.icon("sliders")}${SJ.t("settings")}</button><button class="mi" data-go="support">${SJ.icon("mail")}${SJ.t("support")}</button><div class="divider"></div><button class="mi danger" data-act="role" data-v="visitor">${SJ.icon("logout", "flip")}Leave guest mode</button></div>`;
      const n = (SJ.NOTIFS || []).filter((x) => x.unread).length;
      return `<div class="menu" role="menu" style="min-width:270px"><div class="mh"><b>${SJ.myName()}</b><div class="muted" style="font-size:13px">@${SJ.ME.handle} · ${SJ.tx("Level","المستوى","Niveau")} ${SJ.ME.level}</div></div>
        <button class="mi" data-go="profile">${SJ.icon("user")}${SJ.tx("Profile","الملف الشخصي","Profil")}</button>
        <button class="mi" data-go="notifications">${SJ.icon("bell")}<span style="flex:1">${SJ.tx("Notifications","الإشعارات","Notifications")}</span>${n ? `<span class="count gold">${n}</span>` : ""}</button>
        <button class="mi" data-go="history">${SJ.icon("history")}${SJ.tx("History","السجل","Historique")}</button>
        <button class="mi" data-go="friends">${SJ.icon("users")}${SJ.t("friends")}</button>
        <button class="mi" data-go="my-questions">${SJ.icon("pen")}${SJ.tx("My questions","أسئلتي","Mes questions")}</button>
        <button class="mi" data-go="support">${SJ.icon("mail")}${SJ.tx("Support","الدعم","Assistance")}</button>
        <button class="mi" data-go="settings">${SJ.icon("sliders")}${SJ.t("settings")}</button>
        ${S.role === "staff" ? `<div class="divider"></div><button class="mi" data-act="toast" data-msg="Opens /admin, the admin console you already have">${SJ.icon("shield")}${SJ.tx("Admin","الإدارة","Admin")}</button>` : ""}
        <div class="divider"></div><button class="mi danger" data-act="role" data-v="visitor">${SJ.icon("logout", "flip")}${SJ.t("signout")}</button></div>`;
    }
    return "";
  }
  function tabbar(r) {
    const act = activeNav(r);
    const tabs = [["home", "home", "home"], ["play", "play", "play"], ["challenges", "swords", "challenges"], ["messages", "chat", "messages"], ["leaderboard", "trophy", "ranks"]];
    return `<nav class="tabbar" aria-label="Main">${tabs.map(([id, ic, lab]) => {
      const locked = SJ.isGuest() && LOCKED_FOR_GUEST.includes(id);
      let c = "";
      if (!locked && SJ.signedIn()) { if (id === "challenges") { const n = S.challenges.filter((x) => x.kind === "in").length; if (n) c = `<span class="count gold">${n}</span>`; } if (id === "messages" && S.unread.messages) c = `<span class="count gold">${S.unread.messages}</span>`; }
      return `<button class="tab" data-go="${id}" ${act === id ? 'aria-current="page"' : ""}><span class="pill">${SJ.icon(locked ? "lock" : ic, id === "messages" ? "flip" : "")}${c}</span>${SJ.t(lab)}</button>`;
    }).join("")}</nav>`;
  }

  /* ---------------- render ---------------- */
  const app = () => document.getElementById("app");
  function render() {
    const r = resolve();
    if (r !== S.route) S.route = r;
    if (SJ.beforeRender) SJ.beforeRender(r);
    const screen = SJ.screens[r];
    let body;
    if (SJ.isGuest() && LOCKED_FOR_GUEST.includes(r)) body = lockedPage(r);
    else body = screen();
    const focus = FOCUS.includes(r);
    app().innerHTML = focus ? body : topnav(r) + `<main id="main">${body}</main>` + tabbar(r);
    app().insertAdjacentHTML("beforeend", protoUI());
    SJ.after && SJ.after[r] && SJ.after[r]();
  }
  SJ.render = render;

  function lockedPage(r) {
    const what = { notifications: "get notifications", history: "keep your game history", "my-questions": "write your own questions", "challenge-new": "challenge friends", challenges: "challenge friends", messages: "send messages", friends: "add friends", profile: "keep a profile with your stats", duel: "play ranked duels", "room-create": "host live rooms", achievements: "earn achievements" }[r] || "use this";
    return `<div class="page"><div class="lock-page"><span class="e-ic">${SJ.icon("lock", "i-lg")}</span><h1 style="font-size:26px">Create a free account to ${what}</h1>
      <p class="muted">You're playing as ${SJ.esc(S.guestName)}. Your points from today move to your new account.</p>
      <div class="row wrap" style="justify-content:center"><button class="btn btn-primary btn-lg" data-go="signup">${SJ.t("signup")}</button><button class="btn btn-lg" data-go="signin">${SJ.t("signin")}</button></div>
      <button class="link-btn" data-go="play">Keep playing as a guest</button></div></div>`;
  }

  /* ---------------- overlays ---------------- */
  SJ.toast = (msg) => {
    let box = document.querySelector(".toasts");
    if (!box) { box = document.createElement("div"); box.className = "toasts"; box.setAttribute("aria-live", "polite"); document.body.appendChild(box); }
    const t = document.createElement("div");
    t.className = "toast"; t.innerHTML = SJ.icon("check") + "<span></span>"; t.querySelector("span").textContent = msg;
    box.appendChild(t); setTimeout(() => t.remove(), 3800);
  };
  SJ.closeOverlay = () => document.querySelectorAll(".ov").forEach((n) => n.remove());
  SJ.modal = (html) => {
    SJ.closeOverlay();
    document.body.insertAdjacentHTML("beforeend", `<div class="ov scrim" data-act="close"></div><div class="ov modal" role="dialog" aria-modal="true">${html}</div>`);
    const f = document.querySelector(".modal input, .modal textarea, .modal .btn-primary"); if (f) f.focus();
  };
  SJ.drawer = (html) => {
    SJ.closeOverlay();
    document.body.insertAdjacentHTML("beforeend", `<div class="ov scrim" data-act="close"></div><aside class="ov drawer" role="dialog" aria-modal="true">${html}</aside>`);
    const f = document.querySelector(".drawer input, .drawer textarea"); if (f) f.focus();
  };
  SJ.confetti = () => {
    const box = document.createElement("div"); box.className = "confetti";
    const cols = ["#e8b65a", "#35b88b", "#7c3aed", "#db2777", "#2563eb", "#f3d394"];
    for (let i = 0; i < 70; i++) { const p = document.createElement("i"); p.style.left = Math.random() * 100 + "%"; p.style.background = cols[i % cols.length]; p.style.animationDelay = Math.random() * .8 + "s"; p.style.transform = `rotate(${Math.random() * 180}deg)`; box.appendChild(p); }
    document.body.appendChild(box); setTimeout(() => box.remove(), 3600);
  };

  /* ---------------- prototype controls ---------------- */
  const SCREENS_LIST = [
    ["welcome", "Welcome (signed out)"], ["signin", "Sign in"], ["signup", "Create account"], ["guest-name", "Guest name"], ["onboarding", "Onboarding"],
    ["home", "Home"], ["domain", "Domain page"], ["play", "Play hub"], ["setup", "Game setup"], ["game", "Solo game"], ["results", "Results"],
    ["room-create", "Create a live room"], ["room-join", "Join with a code"], ["lobby", "Room lobby"], ["live", "Live game"], ["podium", "Podium"],
    ["duel", "1v1 duel matchmaking"], ["challenges", "Challenges"], ["challenge-new", "New challenge / round"], ["notifications", "Notifications"], ["history", "History"], ["my-questions", "My questions"], ["messages", "Messages"], ["friends", "Friends"], ["leaderboard", "Leaderboard"],
    ["profile", "Profile"], ["settings", "Settings"], ["support", "Contact us"]
  ];
  function protoUI() {
    const seg = (key, opts) => `<div class="pseg" style="--n:${opts.length}">${opts.map(([v, l]) => `<button data-act="proto-${key}" data-v="${v}" aria-pressed="${S[key] === v}">${l}</button>`).join("")}</div>`;
    return `<button class="proto-fab" data-act="proto" aria-expanded="${S.proto}">${SJ.icon(S.proto ? "x" : "sliders")}Prototype</button>
      ${S.proto ? `<div class="proto-panel" role="dialog" aria-label="Prototype controls">
        <h3>View the app as</h3>${seg("role", [["visitor", "Signed out"], ["guest", "Guest"], ["player", "Player"], ["staff", "Staff"]])}
        <h3>Language</h3>${seg("lang", [["ar", "العربية"], ["en", "English"], ["fr", "Français"]])}
        <h3>Theme</h3>${seg("theme", [["light", "Light"], ["dark", "Dark"], ["device", "Device"]])}
        <h3>Content</h3><div class="pseg" style="--n:2"><button data-act="proto-isl" data-v="0" aria-pressed="${!S.islamicOnly}">All domains</button><button data-act="proto-isl" data-v="1" aria-pressed="${!!S.islamicOnly}">Islamic only</button></div>
        <h3>Domains</h3><div class="pseg" style="--n:2"><button data-act="dscale" data-v="2" aria-pressed="${!SJ.DOMAINS.some((d) => d.sim)}">${SJ.BASE_DOMS ? SJ.BASE_DOMS.length : 6} real</button><button data-act="dscale" data-v="100" aria-pressed="${SJ.DOMAINS.some((d) => d.sim)}">100 sample</button></div>
        <h3>Jump to a screen</h3><select data-act="proto-jump" aria-label="Jump to a screen">${SCREENS_LIST.map(([k, l]) => `<option value="${k}" ${S.route === k ? "selected" : ""}>${l}</option>`).join("")}</select>
        <h3>Try a flow</h3>
        <button class="preset" data-act="flow" data-v="guest">Guest: play a quick game, then sign up</button>
        <button class="preset" data-act="flow" data-v="host">Host a live room with friends</button>
        <button class="preset" data-act="flow" data-v="joinroom">Guest joins a room with a code</button>
        <button class="preset" data-act="flow" data-v="duel">Ranked 1v1 duel</button>
        <button class="preset" data-act="flow" data-v="sport">Sport domain: page, quick game, results</button>
        <button class="preset" data-act="flow" data-v="mixed">Mixed game across all domains</button>
        <button class="preset" data-act="flow" data-v="islamiconly">Islamic-only mode (the v1 feel)</button>
        <button class="preset" data-act="flow" data-v="arabic">Arabic Language domain, in Arabic</button>
        <button class="preset" data-act="flow" data-v="device">Pass the device: 3 players on one phone</button>
        <button class="preset" data-act="flow" data-v="new">New account onboarding</button>
        <p class="note">Sample data. Other players in rooms and duels are simulated. Nothing is saved except these settings, on this device.</p>
      </div>` : ""}`;
  }

  /* ---------------- global events ---------------- */
  document.addEventListener("click", (e) => {
    const g = e.target.closest("[data-go]");
    if (g) {
      e.preventDefault();
      const p = Object.assign({}, g.dataset); delete p.go;
      go(g.dataset.go, p); return;
    }
    const a = e.target.closest("[data-act]");
    if (!a) { if (S.menu && !e.target.closest(".menu")) { S.menu = null; render(); } return; }
    const act = a.dataset.act, v = a.dataset.v;
    switch (act) {
      case "menu": S.menu = S.menu === a.dataset.menu ? null : a.dataset.menu; render(); return;
      case "lang": S.menu = null; SJ.setLang(v); return;
      case "theme": SJ.setTheme(v); return;
      case "role": SJ.setRole(v); return;
      case "close": SJ.closeOverlay(); return;
      case "toast": S.menu = null; render(); SJ.toast(a.dataset.msg); return;
      case "proto": S.proto = !S.proto; render(); return;
      case "proto-role": SJ.setRole(v); return;
      case "proto-lang": SJ.setLang(v); return;
      case "proto-theme": SJ.setTheme(v); return;
      case "flow": SJ.flow(v); return;
    }
    if (SJ.actions[act]) SJ.actions[act](a, e);
  });
  document.addEventListener("change", (e) => {
    const a = e.target.closest("[data-act]");
    if (!a) return;
    if (a.dataset.act === "proto-jump") {
      const r = a.value;
      if (S.role === "visitor" && !PUBLIC.includes(r)) { S.role = "player"; store.set("role", "player"); }
      if (r === "onboarding") { S.role = "new"; }
      else if (S.role === "new") { S.role = "player"; }
      if (["game", "live", "results", "podium"].includes(r)) { SJ.demoStart(r); return; }
      go(r); return;
    }
    if (SJ.changes[a.dataset.act]) SJ.changes[a.dataset.act](a, e);
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { SJ.closeOverlay(); if (S.menu) { S.menu = null; render(); } }
    if (SJ.onKey) SJ.onKey(e);
  });
  SJ.actions = {}; SJ.changes = {}; SJ.after = {};

  SJ.boot = () => {
    applyTheme(); applyLang();
    const h = location.hash.slice(1);
    if (h && SJ.screens[h]) S.route = h;
    render();
  };
})();
