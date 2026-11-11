// Adding another question to the set being written here and now.
//
// The first row is rendered by the server, so with no script a player can
// still write one question and send it — this adds the second and the tenth.
export default () => ({
  max: 10,

  get full() {
    return this.rows().length >= this.max;
  },

  rows() {
    const host = this.$refs.rows;
    return host ? Array.from(host.querySelectorAll(".authored__row")) : [];
  },

  add() {
    const host = this.$refs.rows;
    const all = this.rows();
    if (!host || !all.length || all.length >= this.max) return;

    const index = all.length;
    const copy = all[all.length - 1].cloneNode(true);
    copy.classList.remove("authored__row--bad");
    copy.dataset.row = String(index);

    // The radio group is per row. Cloning without renaming would put the new
    // row in the previous row's group, so marking an answer here would unmark
    // the one above — which is exactly what one shared name did to the whole
    // form before this.
    copy.querySelectorAll("input").forEach((input) => {
      if (input.type === "radio") {
        input.name = "q_correct_" + index;
        input.checked = input.value === "0";
      } else {
        input.value = "";
      }
    });
    // Any error left from the row it was cloned from belongs to that row.
    copy.querySelectorAll("p").forEach((p) => p.remove());

    const legend = copy.querySelector("legend");
    if (legend) legend.textContent = legend.textContent.replace(/\d+/, String(index + 1));

    host.appendChild(copy);
    const first = copy.querySelector("input[type=text]");
    if (first) first.focus();
  },
});
