/* The running game: one Alpine store for the question timer, plus the keyboard
   shortcuts of the answers (GP-03).

   The store lives on the page, not inside the question card: htmx swaps the card on
   every answer, so a component bound to the card would lose its state. The card tells
   the store when a question starts and when it is answered. */

export type TimerStore = ReturnType<typeof timerStore>;

export function timerStore() {
  return {
    total: 0,          // seconds allowed, 0 = no timer (GP-02)
    left: 0,
    startedAt: 0,
    handle: 0,
    running: false,

    /* Called by every fresh question card. */
    start(seconds: number) {
      this.stop();
      this.total = seconds;
      this.left = seconds;
      this.startedAt = performance.now();
      this.running = true;
      if (!seconds) return; // "no timer": the elapsed time is still measured
      this.handle = window.setInterval(() => {
        this.left = Math.max(0, this.total - (performance.now() - this.startedAt) / 1000);
        if (this.left <= 0) {
          this.stop();
          window.dispatchEvent(new CustomEvent("sj:timeup"));
        }
      }, 100);
    },

    /* Called when the answer is revealed, so the ring freezes. */
    stop() {
      if (this.handle) window.clearInterval(this.handle);
      this.handle = 0;
      this.running = false;
    },

    get secondsLeft() {
      return Math.ceil(this.left);
    },
    get fraction() {
      return this.total ? this.left / this.total : 1;
    },
    get low() {
      return this.total > 0 && this.left <= 5; // red at 5 s (GP-03)
    },
    get elapsedMs() {
      if (!this.startedAt) return 0;
      return Math.max(0, Math.round(performance.now() - this.startedAt));
    },
  };
}

/* Keys 1–4 and A–D pick an answer; Enter moves on once it is revealed (GP-03). */
export function answerKeys() {
  return {
    press(this: any, event: KeyboardEvent) {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target && /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) return;

      const key = event.key.toLowerCase();
      const index = "1234".indexOf(key) >= 0 ? "1234".indexOf(key) : "abcd".indexOf(key);
      if (index >= 0) {
        const root = this.$el as HTMLElement;
        const buttons = root.querySelectorAll<HTMLButtonElement>(".answers button:not([disabled])");
        const button = buttons[index];
        if (button) {
          event.preventDefault();
          button.click();
        }
        return;
      }
      if (key === "enter") {
        const next = (this.$el as HTMLElement).querySelector<HTMLButtonElement>("[data-next]");
        if (next) {
          event.preventDefault();
          next.click();
        }
      }
    },
  };
}
