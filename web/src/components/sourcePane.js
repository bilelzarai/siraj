// One pane of the round-setup form, shown when it belongs to the source
// chosen above.
//
// A pane can belong to more than one source: the round's length applies to
// anything drawn, but not to questions written here and now.
export default () => ({
  owners: [],

  init() {
    this.owners = (this.$el.dataset.sourcePane || "").split(" ").filter(Boolean);
  },

  get shown() {
    return this.owners.includes(this.source || "bank");
  },
});
