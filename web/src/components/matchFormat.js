// The team panel of the match form, shown when the format chosen is teams.
//
// With no script the panel is rendered by the server in whatever state the
// form came back in, and the server reads the side fields only for a team
// match — so this hides a panel that would otherwise be ignored rather than
// deciding anything.
export default () => ({
  format: "",

  init() {
    const chosen = this.$el.querySelector("input[name=format]:checked");
    this.format = chosen ? chosen.value : "";
  },

  choose(event) {
    this.format = event.target.value;
  },

  get teams() {
    return this.format === "team";
  },
});
