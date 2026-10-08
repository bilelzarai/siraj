// A saved reply, pasted into the box a member of staff is already writing in.
//
// Insert rather than replace: a saved reply is a starting point, and staff
// personalise them. Replacing what somebody had typed would be destroying
// their work to save them a paste.
export default () => ({
  paste(event) {
    const box = this.$refs.box;
    const text = event.currentTarget.dataset.canned || "";
    if (!box || !text) return;

    box.value = box.value.trim() ? box.value.trimEnd() + "\n\n" + text : text;
    box.focus();
    box.setSelectionRange(box.value.length, box.value.length);
  },
});
