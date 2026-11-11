// One star in the rating row.
//
// Its own component because the policy-safe build binds to a *name*, never to
// a call: "am I lit" has to be a property on something, and the only object
// that knows which star this is, is this star. It reads the row's state from
// the scope above it, which is what Alpine's nesting is for.
export default () => ({
  get mine() {
    return Number(this.$el.dataset.star) || 0;
  },

  // Lit up to whatever is being hovered, or to what was chosen when nothing
  // is — the gesture people already expect from a star row.
  get lit() {
    return this.mine <= (this.hovered || this.chosen);
  },

  // The class itself rather than a boolean: x-bind:class takes a value, and
  // "stars__one is-on" is what the stylesheet is written against.
  get litClass() {
    return this.lit ? "stars__one is-on" : "stars__one";
  },

  // What was *chosen* is what assistive technology is told, which is a
  // different thing from what is lit under the pointer.
  get checked() {
    return String(this.mine === this.chosen);
  },
});
