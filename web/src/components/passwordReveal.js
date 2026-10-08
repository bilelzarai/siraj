// Showing a password while it is being typed, and hiding it again.
//
// The button is rendered by the server with its label already on it, so with
// no script the field stays a password field and nothing is missing — a
// control that needs script does not appear without it, so the button carries
// its own fallback state rather than being invented here.
export default () => ({
  revealed: false,

  init() {
    // Never leave a password on screen after the form has been sent.
    const form = this.$refs.input && this.$refs.input.form;
    if (form) form.addEventListener("submit", () => this.hide());
  },

  toggle() {
    this.revealed ? this.hide() : this.show();
  },

  show() {
    this.revealed = true;
    this.apply();
  },

  hide() {
    this.revealed = false;
    this.apply();
  },

  // The caret stays where it was. Changing an input's type moves it to the end
  // in every browser, and a password half-typed is exactly when that is most
  // annoying.
  apply() {
    const input = this.$refs.input;
    if (!input) return;
    const at = input.selectionStart;
    input.type = this.revealed ? "text" : "password";
    input.focus();
    if (at !== null) {
      try {
        input.setSelectionRange(at, at);
      } catch (e) {
        /* a type that does not support selection */
      }
    }
  },

  get label() {
    const el = this.$refs.button || this.$el;
    return this.revealed ? el.dataset.labelHide : el.dataset.labelShow;
  },

  get hidingSlot() {
    return this.revealed;
  },

  get showingSlot() {
    return !this.revealed;
  },

  get pressed() {
    return String(this.revealed);
  },
});
