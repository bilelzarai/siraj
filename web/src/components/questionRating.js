import { postJSON, tmplText } from "../js/helpers.js";

// Rating the question just answered, one press, once.
//
// Hovering a star lights every star up to it — the gesture people already
// expect from a star row — while the one that is *chosen* is the one marked
// for assistive technology, which is a different thing and must not drift into
// the hover state.
export default () => ({
  chosen: 0,
  hovered: 0,
  sending: false,
  status: "",

  // Each star asks whether it is lit. A named method rather than an
  // expression, because the policy-safe build evaluates nothing.
  lit(star) {
    return Number(star) <= (this.hovered || this.chosen);
  },

  preview(event) {
    if (this.sending) return;
    this.hovered = Number(event.currentTarget.dataset.star) || 0;
  },

  clear() {
    this.hovered = 0;
  },

  async choose(event) {
    if (this.sending) return;
    const stars = Number(event.currentTarget.dataset.star) || 0;
    const round = document.querySelector("[data-round]");
    if (!stars || !round) return;

    this.sending = true;
    this.chosen = stars;
    this.hovered = 0;
    try {
      const res = await postJSON("/play/rate", {
        position: Number(round.dataset.position),
        stars,
      });
      this.status =
        res.votes > 1
          ? tmplText("rate-thanks") + " " + res.average.toFixed(1) + "/5"
          : tmplText("rate-thanks");
    } catch (err) {
      // Put it back: a rating that did not reach the server is not a rating,
      // and leaving the stars lit would say it was.
      this.chosen = 0;
      this.status = tmplText("error");
    } finally {
      this.sending = false;
    }
  },
});
