// Which pane of the round-setup form is showing: the bank, a set the player
// wrote, or questions written here and now.
//
// Server-rendered and complete without this: every pane is on the page and the
// server reads only the fields belonging to the chosen source. This narrows it
// to the one that applies.
export default () => ({
  source: "bank",

  init() {
    const chosen = this.$el.querySelector('input[name="source"]:checked');
    this.source = chosen ? chosen.value : "bank";
  },

  choose(event) {
    if (event.target.checked) this.source = event.target.value;
  },
});
