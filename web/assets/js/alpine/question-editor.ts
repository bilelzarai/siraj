/* The question editor of a set (O04): which language tab is open, which answer is the
   right one, the live character count and the points the difficulty is worth. The
   rules stay on the server — this only keeps the form's local state (NF-03, MQ-04). */
export function questionEditor(main: string, correct: number, domain: string, level: number) {
  return {
    tab: main,
    correct,
    domain,
    level,
    lengths: {} as Record<string, number>,
    /** ready is false until the question could actually be saved: the main language
        written, its four answers filled, and a category chosen. The Save buttons
        follow it, so neither is pressable while it would only be refused (MQ-03). */
    ready: false,
    init(this: any) {
      const root = this.$el as HTMLElement;
      root.querySelectorAll<HTMLTextAreaElement>("textarea[name^='body_']").forEach((box) => {
        this.lengths[box.name.slice("body_".length)] = box.value.length;
      });
      // Anything typed anywhere in the editor re-checks whether it could be saved,
      // and so does moving to another language tab.
      root.addEventListener("input", () => this.check());
      root.addEventListener("change", () => this.check());
      this.$watch("tab", () => this.check());
      this.check();
      /* A category belongs to one domain; the select only shows that domain's, so a
         hidden option must never stay selected. */
      this.$watch("domain", () => {
        const select = root.querySelector<HTMLSelectElement>("#qe-cat");
        const chosen = select?.selectedOptions[0];
        if (select && chosen && chosen.value && chosen.hidden) select.value = "";
      });
    },
    count(this: any, lang: string) {
      return this.lengths[lang] ?? 0;
    },
    /** check is the same rule the server keeps, asked of the form as it stands. */
    check(this: any) {
      const root = this.$el as HTMLElement;
      const body = root.querySelector<HTMLTextAreaElement>(`textarea[name="body_${this.tab}"]`);
      const written = (body?.value ?? "").trim().length > 0;
      let answers = 0;
      root
        .querySelectorAll<HTMLInputElement>(`input[name^="answer_${this.tab}_"]`)
        .forEach((box) => {
          if (box.value.trim()) answers += 1;
        });
      const category = root.querySelector<HTMLSelectElement>("#qe-cat");
      this.ready = written && answers === 4 && !!category?.value;
    },
  };
}
