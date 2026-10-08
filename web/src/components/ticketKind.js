// Which extra field a support form shows, from the kind of ticket chosen.
//
// The panes are rendered by the server and start visible: with no script every
// one of them is on the page and the server reads only the field belonging to
// the chosen kind. This narrows that down to the one that applies, which is a
// convenience, not a capability — the contract in STACK §3.1, rule 4.
export default () => ({
  kind: "",

  init() {
    // Whatever the server rendered as checked is where this starts, so a form
    // coming back from a refusal opens on the kind that was chosen.
    const chosen = this.$el.querySelector("input[name=kind]:checked");
    this.kind = chosen ? chosen.value : "";
  },

  // Named rather than an expression: the policy-safe build evaluates nothing,
  // so a directive carries the name of something on this object and never a
  // snippet of JavaScript.
  choose(event) {
    this.kind = event.target.value;
  },
});
