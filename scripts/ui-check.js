/* Layout check: open every screen at a range of widths in headless Chrome and fail
   on the two faults a screenshot shows but a Go test cannot — a page that scrolls
   sideways, and a box that cuts its own content off with no scrollbar to reach it.
 *
 * Run it with `make ui-check` (the server and a Chrome binary must be there).
 * Bun supplies fetch and WebSocket, so this needs no package.
 */
const BASE = process.env.BASE_URL || "http://localhost:8080";
const CDP = Number(process.env.CDP_PORT || 9222);
const WIDTHS = (process.env.WIDTHS || "1440,1280,1100,1024,900,768,430,390,360")
  .split(",").map(Number);
const LOGIN = process.env.UI_LOGIN || "";
const PASSWORD = process.env.UI_PASSWORD || "";

const PUBLIC_PAGES = ["/welcome", "/signin", "/signup", "/guest", "/explore"];
const PLAYER_PAGES = [
  "/home", "/play", "/setup", "/domains", "/leaderboard", "/profile", "/history",
  "/settings", "/friends", "/notifications", "/my-questions", "/support",
  "/challenges", "/challenges/new", "/messages", "/rooms/new", "/rooms/join", "/duel",
];
const ADMIN_PAGES = [
  "/admin", "/admin/users", "/admin/domains", "/admin/categories", "/admin/questions",
  "/admin/review", "/admin/comments", "/admin/rated", "/admin/health", "/admin/audit",
  "/admin/support",
];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The page reports both faults about itself. Elements hidden for screen readers are
// one pixel wide on purpose; they are not a cut.
const PROBE = `(() => {
  const d = document.documentElement, vw = d.clientWidth;
  const wide = [], cut = [];
  document.querySelectorAll("body *").forEach((el) => {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden" || +cs.opacity === 0) return;
    const r = el.getBoundingClientRect();
    const name = el.tagName.toLowerCase() + "." +
      ((el.className.baseVal ?? el.className ?? "").toString().trim().split(/\\s+/)[0] || "");
    if (r.width > 0 && r.height > 0 && (r.right > vw + 1 || r.left < -1)) {
      wide.push({ name, left: Math.round(r.left), right: Math.round(r.right) });
    }
    if (el.clientWidth > 2 && el.scrollWidth > el.clientWidth + 1 &&
        (cs.scrollbarWidth === "none" || cs.overflowX === "hidden" || cs.overflowX === "clip")) {
      cut.push({ name, shown: el.clientWidth, needs: el.scrollWidth });
    }
  });
  // keep the outermost box of a chain, not every parent of one wide child
  const outer = wide.filter((o, i) => !wide.some((p, j) =>
    j !== i && p.left <= o.left && p.right >= o.right && (p.right - p.left) > (o.right - o.left)));
  return JSON.stringify({ vw, scrollW: d.scrollWidth, wide: outer.slice(0, 4), cut: cut.slice(0, 4) });
})()`;

async function targets() {
  const list = await (await fetch(`http://127.0.0.1:${CDP}/json/list`)).json();
  const page = list.find((t) => t.type === "page" && !t.url.startsWith("chrome-extension"));
  if (!page) throw new Error("no page target: start Chrome with --remote-debugging-port=" + CDP);
  return page;
}

async function main() {
  const target = await targets();
  const ws = new WebSocket(target.webSocketDebuggerUrl);
  let id = 0;
  const pending = new Map();
  const send = (method, params = {}) =>
    new Promise((resolve) => { const i = ++id; pending.set(i, resolve); ws.send(JSON.stringify({ id: i, method, params })); });
  await new Promise((r) => (ws.onopen = r));
  ws.onmessage = (e) => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m.result); pending.delete(m.id); } };
  await send("Page.enable");
  await send("Runtime.enable");

  const pages = [...PUBLIC_PAGES];
  if (LOGIN && PASSWORD) {
    await send("Emulation.setDeviceMetricsOverride", { width: 1400, height: 900, deviceScaleFactor: 1, mobile: false });
    await send("Page.navigate", { url: BASE + "/signin" });
    await sleep(1200);
    await send("Runtime.evaluate", { expression:
      `(() => { const f = document.querySelector("form");
         f.querySelector("[name=login]").value = ${JSON.stringify(LOGIN)};
         f.querySelector("[name=password]").value = ${JSON.stringify(PASSWORD)};
         f.submit(); })()` });
    await sleep(1800);
    pages.push(...PLAYER_PAGES, ...ADMIN_PAGES);
  } else {
    console.log("· no UI_LOGIN/UI_PASSWORD: checking the signed-out screens only");
  }

  let faults = 0;
  for (const width of WIDTHS) {
    await send("Emulation.setDeviceMetricsOverride", { width, height: 900, deviceScaleFactor: 1, mobile: width <= 430 });
    const lines = [];
    for (const path of pages) {
      await send("Page.navigate", { url: BASE + path });
      await sleep(500);
      const res = await send("Runtime.evaluate", { expression: PROBE, returnByValue: true });
      let out;
      try { out = JSON.parse(res.result.value); } catch { continue; }
      if (out.scrollW > out.vw + 1) {
        lines.push(`  ${path}: the page scrolls sideways (${out.vw} → ${out.scrollW}) · ` +
          out.wide.map((w) => w.name).join(", "));
      }
      for (const c of out.cut) {
        lines.push(`  ${path}: ${c.name} cuts its content (${c.shown} shown, ${c.needs} needed)`);
      }
    }
    faults += lines.length;
    console.log(`${String(width).padStart(5)}px  ${lines.length ? "\n" + lines.join("\n") : "ok"}`);
  }
  ws.close();
  if (faults) {
    console.error(`\n${faults} layout fault(s). A screen must never scroll sideways, and nothing may be cut off without a way to reach it.`);
    process.exit(1);
  }
  console.log("\nEvery screen fits at every width checked.");
  process.exit(0);
}

main().catch((e) => { console.error("ui-check:", e.message); process.exit(1); });
