/* One event stream for the whole app (NF-02). The server sends a word — "messages",
   "notifications", "friends" — and the screens ask it for the part of themselves that
   changed. Nothing here knows a rule; it only says "something happened".

   Any element can listen:
     hx-trigger="sj:live from:body"              every signal
     hx-trigger="sj:live:messages from:body"     one topic
*/
const RETRY_MIN = 2000;
const RETRY_MAX = 30000;

export function openLiveStream() {
  if (!("EventSource" in window)) return; // very old browser: the polls still work
  let retry = RETRY_MIN;
  let source: EventSource | null = null;

  const topics = ["notifications", "messages", "friends", "challenges", "rooms", "duels", "catalog"];

  const announce = (topic: string) => {
    document.body.dispatchEvent(new CustomEvent("sj:live", { detail: topic, bubbles: true }));
    document.body.dispatchEvent(new CustomEvent(`sj:live:${topic}`, { detail: topic, bubbles: true }));
  };

  const connect = () => {
    source = new EventSource("/events");
    source.onopen = () => { retry = RETRY_MIN; };
    topics.forEach((topic) => source!.addEventListener(topic, () => announce(topic)));
    source.onerror = () => {
      source?.close();
      /* The stream drops on sleep, a lost network or a deploy: come back, slower each
         time, so a server that is down is not hammered. */
      setTimeout(connect, retry);
      retry = Math.min(retry * 2, RETRY_MAX);
    };
  };

  connect();
  /* A screen that was asleep may have missed signals: ask once on the way back. */
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") announce("notifications");
  });
}
