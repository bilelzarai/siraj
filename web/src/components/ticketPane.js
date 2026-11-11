// One pane of a support form, shown when it belongs to the kind chosen above.
//
// Its own component rather than a binding on the parent: the policy-safe build
// has no expressions, so "is this pane one of the kinds in my data attribute"
// has to be a method somewhere, and the pane is where that question belongs.
export default () => ({
  kinds: [],

  init() {
    this.kinds = (this.$el.dataset.ticketPane || "").split(" ").filter(Boolean);
  },

  get shown() {
    // Before a kind is chosen every pane stands, which is also what the page
    // looks like with no script at all.
    return !this.kind || this.kinds.includes(this.kind);
  },
});
