import { postJSON, tmplText } from "../js/helpers.js";

// A remark on the question just answered, written while it is still on screen.
//
// One POST on a press, not a stream: the server resolves the position to a
// question and refuses unless that position has already been answered, so this
// cannot be used to ask about a question before seeing it.
export default () => ({
  status: "",
  sending: false,

  async save() {
    const body = this.$refs.body;
    const round = document.querySelector("[data-round]");
    if (!body || !round || this.sending) return;

    const text = body.value.trim();
    if (text.length < 2) return;

    this.sending = true;
    window.sirajBusy.mark(this.$refs.save);
    try {
      await postJSON("/play/comment", {
        position: Number(round.dataset.position),
        body: text,
      });
      this.status = tmplText("note-saved");
      this.$el.open = false;
    } catch (err) {
      this.status = tmplText("error");
    } finally {
      this.sending = false;
      window.sirajBusy.clear(this.$refs.save);
    }
  },
});
