/* "Invite with a link" on Friends (FR-05): the dialog, and a copy that works even
   where the clipboard API does not — an http:// origin on a phone, or an older
   browser. The link itself is read from the field, never written into a script. */
export function invite() {
  return {
    open: false,
    show(this: any) {
      this.open = true;
      // The link is selected on open, so copying by hand is one keystroke.
      this.$nextTick(() => this.$refs.link?.select());
    },
    hide(this: any) {
      this.open = false;
    },
    async copy(this: any) {
      const box = this.$refs.link as HTMLInputElement | undefined;
      if (!box) return;
      box.select();
      box.setSelectionRange(0, box.value.length);
      try {
        await navigator.clipboard.writeText(box.value);
      } catch {
        // Insecure origin or no permission: the selection is already made, so the
        // old command still copies it.
        try {
          document.execCommand("copy");
        } catch {
          return; // nothing was copied, so say nothing
        }
      }
      window.sjToast?.(box.dataset.copied ?? "");
    },
  };
}
