/* Dates are rendered in the browser with Intl, so the three languages get their own
   month names and the Hijri date uses the Umm al-Qura calendar (DM-09, docs 03 §5). */

function lang() {
  return document.documentElement.lang || "en";
}

export function today() {
  return {
    text: "",
    init(this: any) {
      try {
        this.text = new Intl.DateTimeFormat(lang() + "-u-nu-latn", {
          weekday: "long",
          day: "numeric",
          month: "long",
        }).format(new Date());
      } catch {
        this.text = new Date().toDateString();
      }
    },
  };
}

export function hijri() {
  return {
    text: "",
    init(this: any) {
      try {
        this.text = new Intl.DateTimeFormat(lang() + "-u-ca-islamic-umalqura-nu-latn", {
          day: "numeric",
          month: "long",
          year: "numeric",
        }).format(new Date());
      } catch {
        this.text = "";
      }
    },
  };
}

/* Days until 1 Ramadan, walking forward through the Hijri calendar (HM-08). */
export function ramadan() {
  return {
    days: -1,
    date: "",
    year: "",
    init(this: any) {
      try {
        const parts = new Intl.DateTimeFormat("en-u-ca-islamic-umalqura", { day: "numeric", month: "numeric", year: "numeric" });
        const now = new Date();
        for (let i = 0; i < 400; i++) {
          const day = new Date(now.getTime() + i * 86400000);
          const fields = parts.formatToParts(day);
          const month = Number(fields.find((p) => p.type === "month")?.value);
          const date = Number(fields.find((p) => p.type === "day")?.value);
          if (month === 9 && (date === 1 || i === 0)) {
            this.days = i;
            this.year = fields.find((p) => p.type === "year")?.value ?? "";
            this.date = new Intl.DateTimeFormat(lang() + "-u-nu-latn", { day: "numeric", month: "long", year: "numeric" }).format(day);
            return;
          }
        }
      } catch {
        this.days = -1;
      }
    },
  };
}

/* "New question in 7 h 12 min": the question of the day turns over at the next UTC
   midnight, so the countdown is computed in the browser (HM-02). */
export function nextQuestion() {
  return {
    left: "",
    init(this: any) {
      const tick = () => {
        const now = new Date();
        const next = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1);
        const mins = Math.max(0, Math.round((next - now.getTime()) / 60000));
        const h = Math.floor(mins / 60);
        const m = mins % 60;
        // Intl.RelativeTimeFormat would say "in 7 hours"; the design wants the
        // compact "7 h 12 min", so the units come from the page (LG-01).
        const units = document.documentElement.dataset.timeUnits || "h|min";
        const [hu, mu] = units.split("|");
        const nf = new Intl.NumberFormat(lang() + "-u-nu-latn");
        this.left = (h ? nf.format(h) + " " + hu + " " : "") + nf.format(m) + " " + mu;
      };
      tick();
      setInterval(tick, 30000);
    },
  };
}
