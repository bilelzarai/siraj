/* Any screen where ticking a card has to answer straight away: the count it shows,
   the options it unlocks and the button that submits it all follow what is picked,
   without waiting for a round trip. The form still posts the same fields — this only
   keeps the screen honest between clicks.

   The picked ids live here, seeded by the server, rather than being counted back out
   of the DOM: a card deep inside its own scope can still say what it did, and Alpine
   tracks the list the same way it tracks anything else.

   Used by "Set up your round" (CH-01) and by onboarding's domain step (HM-01). */
export function picks(initial: (string | number)[] = [], chosen = "") {
  return {
    ids: [...initial].map(String),
    /** chosen is the single pick some screens also make — the set a round plays. */
    chosen,
    /** has says whether one card is picked, which is what lights it up. */
    has(this: any, id: string | number) {
      return this.ids.includes(String(id));
    },
    /** toggle is what a card reports when the player clicks it. */
    toggle(this: any, id: string | number, on: boolean) {
      const key = String(id);
      if (on) {
        if (!this.ids.includes(key)) this.ids.push(key);
        return;
      }
      this.ids = this.ids.filter((x: string) => x !== key);
    },
    get count() {
      return this.ids.length;
    },
  };
}
