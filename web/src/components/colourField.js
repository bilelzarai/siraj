// The two halves of a colour field: a swatch for choosing and a text box for
// pasting six characters somebody was given.
//
// Both are submitted-capable on their own; the text box is the one the form
// actually sends, so with no script the field still works and the swatch is
// simply decoration.
export default () => ({
  // A half-typed value is not a colour yet. Pushing it to the swatch would
  // have the browser snap it to black and then push black back over what was
  // being typed.
  typed() {
    const text = this.$refs.text;
    const well = this.$refs.well;
    if (!text || !well) return;
    const value = text.value.trim();
    if (/^#[0-9a-fA-F]{6}$/.test(value)) well.value = value.toLowerCase();
  },

  picked() {
    const text = this.$refs.text;
    const well = this.$refs.well;
    if (!text || !well) return;
    text.value = well.value;
    text.setAttribute("aria-invalid", "false");
  },
});
