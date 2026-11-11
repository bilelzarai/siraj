/* "Import a CSV": the rules are shown first, and picking a file sends it straight
   away — there is no screen in between (MQ-05). */
export function upload() {
  return {
    open: false,
    send(this: any) {
      const box = this.$refs.file as HTMLInputElement | undefined;
      if (box?.files?.length) (this.$refs.form as HTMLFormElement).requestSubmit();
    },
  };
}
