/* The room code as the design types it: six boxes over one real field. Letters and
   digits only, upper case, spaces allowed while typing and dropped on the way out,
   and the button opens at six (RM-02). */
export function roomCode() {
  return {
    ready: false,
    /** chars is one entry per box, so the template can print them without a loop. */
    chars: ["", "", "", "", "", ""],
    /** at is the box the caret sits in, which the design outlines. */
    at: 0,
    init(this: any) {
      this.clean();
    },
    clean(this: any) {
      const box = this.$refs.code as HTMLInputElement | undefined;
      if (!box) return;
      const cleaned = box.value.replace(/[^a-zA-Z0-9]/g, "").toUpperCase().slice(0, 6);
      if (box.value !== cleaned) box.value = cleaned;
      this.chars = Array.from({ length: 6 }, (_, i) => cleaned[i] || "");
      this.at = Math.min(cleaned.length, 5);
      this.ready = cleaned.length === 6;
    },
  };
}
