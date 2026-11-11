/* Modal / drawer / menu open state with Escape and click-outside (NF-02). */
export function overlay() {
  return {
    open: false,
    /* A drawer that changed something the screen behind it shows (Your domains) asks
       for a repaint when it closes, so the shell never lies (DM-04). */
    dirty: false,
    touch(this: any) {
      this.dirty = true;
    },
    show(this: any) {
      this.open = true;
      const root = this.$el as HTMLElement;
      queueMicrotask(() => root.querySelector<HTMLElement>("input, textarea, button")?.focus());
    },
    hide(this: any) {
      this.open = false;
      if (this.dirty) {
        this.dirty = false;
        window.location.reload();
      }
    },
    toggle(this: any) {
      this.open ? this.hide() : this.show();
    },
  };
}
