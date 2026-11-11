/* The V5 entry screens. Three small pieces of state a page cannot hold on its own:
   a phone number formatted as it is typed, six code boxes that behave like one
   field, and the resend countdown. Everything else — which step, what it says, what
   it does — comes from the server. */

type Country = { code: string; dial: string; digits: number[] };

type PhoneState = {
  channel: string;
  password: boolean;
  country: string;
  dial: string;
  phone: string;
  email: string;
  valid: boolean;
  placeholder: string;
  countries: Country[];
  creating: boolean;
  /* "Your details": the name, the terms, and whether the password passed all five
     checks (the password field keeps its own state and announces this one). */
  name: string;
  terms: boolean;
  pwOk: boolean;
  labels: Record<string, string>;
  init(this: PhoneState): void;
  pick(this: PhoneState, code: string): void;
  format(this: PhoneState): void;
  submitLabel(this: PhoneState): string;
  hint(this: PhoneState): string;
  ready(this: PhoneState): boolean;
};

/* sjPhone: the country picker and the number beside it. The grouping is the design's
   "20 123 456" — pairs after the first three for an 8-digit number, threes otherwise
   — and the green check appears at the length the country uses. */
export function sjPhone(country: string, channel: string, creating: boolean): PhoneState {
  return {
    channel: channel || "phone",
    password: false,
    country: country || "TN",
    dial: "+216",
    phone: "",
    email: "",
    valid: false,
    placeholder: "20 123 456",
    countries: [],
    creating,
    name: "",
    terms: false,
    pwOk: false,
    labels: {},

    init() {
      const host = (this as unknown as { $el: HTMLElement }).$el;
      try {
        this.countries = JSON.parse(host.dataset.countries || "[]");
      } catch {
        this.countries = [];
      }
      const labels = host.querySelector<HTMLElement>("[data-labels]");
      if (labels) this.labels = { ...labels.dataset } as Record<string, string>;
      this.pick(this.country);
      const field = host.querySelector<HTMLInputElement>('input[name="phone"]');
      if (field && field.value) {
        this.phone = field.value;
        this.format();
      }
      /* A step that came back with an error keeps what was typed, so the button
         has to work out again whether it may be pressed. */
      this.email = host.querySelector<HTMLInputElement>('input[name="email"]')?.value || "";
      this.name = host.querySelector<HTMLInputElement>('input[name="display_name"]')?.value || "";
      this.terms = !!host.querySelector<HTMLInputElement>('input[name="accept_terms"]')?.checked;
    },

    pick(code: string) {
      this.country = code;
      const found = this.countries.find((c) => c.code === code);
      this.dial = found ? found.dial : "+216";
      this.placeholder = found ? sample(found.digits[0] || 8) : "20 123 456";
      this.format();
    },

    format() {
      const found = this.countries.find((c) => c.code === this.country);
      const digits = this.phone.replace(/\D/g, "").slice(0, 15);
      this.phone = group(digits);
      this.valid = !!found && found.digits.includes(digits.length);
    },

    submitLabel() {
      if (this.creating) return this.labels.createAccount || "Create my account";
      if (this.channel === "email" && this.password) return this.labels.signIn || "Sign in";
      return this.channel === "phone"
        ? this.labels.textCode || "Text me a code"
        : this.labels.sendCode || "Send me a code";
    },

    hint() {
      if (this.channel === "email" && this.password) return this.labels.hintPassword || "";
      const key = this.creating
        ? this.channel === "phone" ? "hintCreatePhone" : "hintCreateEmail"
        : this.channel === "phone" ? "hintSigninPhone" : "hintSigninEmail";
      return this.labels[key] || "";
    },

    /* The button is live only when the step it belongs to is answered. The server
       checks the same thing again: this only saves a round trip. */
    ready() {
      const reachable = this.channel === "phone" ? this.valid : looksLikeEmail(this.email);
      if (this.creating) return reachable && this.name.trim().length > 0 && this.pwOk && this.terms;
      if (this.channel === "email" && this.password) return looksLikeEmail(this.email);
      return reachable;
    },
  };
}

function looksLikeEmail(value: string): boolean {
  const at = value.indexOf("@");
  return at > 0 && at < value.length - 1 && !/\s/.test(value);
}

type PasswordState = {
  value: string;
  show: boolean;
  words: Record<string, string>;
  readonly checks: Record<string, boolean>;
  strong(this: PasswordState): boolean;
  strength(this: PasswordState): string;
  word(this: PasswordState): string;
  percent(this: PasswordState): number;
};

/* sjPassword: the five checks under the new-password field — 8 chars, A-Z, a-z,
   123, @#$ — and the Weak / Medium / Strong bar beside them. The same rule lives in
   internal/modules/auth/password.go, which is the one that decides. */
export function sjPassword(weak: string, medium: string, strong: string): PasswordState {
  return {
    value: "",
    show: false,
    words: { weak, medium, strong },

    get checks() {
      const v = this.value;
      return {
        length: [...v].length >= 8,
        upper: /[A-Z]/.test(v),
        lower: /[a-z]/.test(v),
        digit: /[0-9]/.test(v),
        symbol: /[^\p{L}\p{N}\s]/u.test(v),
      };
    },

    strong() {
      return Object.values(this.checks).every(Boolean);
    },

    strength() {
      const met = Object.values(this.checks).filter(Boolean).length;
      if (met >= 5) return "strong";
      return met >= 3 ? "medium" : "weak";
    },

    word() {
      return this.words[this.strength()] || "";
    },

    percent() {
      const met = Object.values(this.checks).filter(Boolean).length;
      return Math.round((met / 5) * 100);
    },
  };
}

/* "20 123 456": three, then pairs of three. Short numbers keep their own shape. */
function group(digits: string): string {
  if (digits.length <= 3) return digits;
  const head = digits.slice(0, 2);
  const rest = digits.slice(2).replace(/(.{3})/g, "$1 ").trim();
  return (head + " " + rest).trim();
}

/* A placeholder that reads like a real number of that length: 20 123 456. */
function sample(length: number): string {
  return group("201234567890123".slice(0, length));
}

type CodeState = {
  code: string;
  left: number;
  length: number;
  init(this: CodeState): void;
  box(this: CodeState, i: number): HTMLInputElement | undefined;
  onInput(this: CodeState, i: number, e: Event): void;
  onKey(this: CodeState, i: number, e: KeyboardEvent): void;
  onPaste(this: CodeState, i: number, e: ClipboardEvent): void;
  fill(this: CodeState): void;
  clock(this: CodeState): string;
  /* Filling every box at once (autofill or a paste), and sending the form the
     moment the last digit lands. */
  spread(this: CodeState, value: string): void;
  submit(this: CodeState): void;
};

/* sjCode: six boxes that behave like one field — typing moves on, backspace moves
   back, a pasted code fills them all, and the last digit submits. The countdown is
   seeded by the server, so reloading the page does not shorten it. */
export function sjCode(length: number, resendIn: number): CodeState {
  return {
    code: "",
    left: resendIn,
    length,

    init() {
      const tick = () => {
        if (this.left > 0) {
          this.left -= 1;
          setTimeout(tick, 1000);
        }
      };
      if (this.left > 0) setTimeout(tick, 1000);
      /* Chrome hands a one-time code to the first box; spread it over the rest. */
      this.box(0)?.addEventListener("input", () => {
        const value = this.box(0)?.value || "";
        if (value.length > 1) this.spread(value);
      });
    },

    box(i: number) {
      const refs = (this as unknown as { $refs: Record<string, HTMLInputElement> }).$refs;
      return refs["d" + i];
    },

    spread(value: string) {
      const digits = value.replace(/\D/g, "").slice(0, this.length).split("");
      digits.forEach((digit, i) => {
        const box = this.box(i);
        if (box) box.value = digit;
      });
      this.fill();
      if (digits.length === this.length) this.submit();
    },

    onInput(i: number, e: Event) {
      const box = e.target as HTMLInputElement;
      box.value = box.value.replace(/\D/g, "").slice(-1);
      if (box.value && i + 1 < this.length) this.box(i + 1)?.focus();
      this.fill();
      if (i + 1 === this.length && box.value) this.submit();
    },

    onKey(i: number, e: KeyboardEvent) {
      if (e.key === "Backspace" && !(e.target as HTMLInputElement).value && i > 0) {
        this.box(i - 1)?.focus();
      }
      if (e.key === "ArrowLeft" && i > 0) this.box(i - 1)?.focus();
      if (e.key === "ArrowRight" && i + 1 < this.length) this.box(i + 1)?.focus();
    },

    onPaste(_i: number, e: ClipboardEvent) {
      e.preventDefault();
      this.spread(e.clipboardData?.getData("text") || "");
    },

    fill() {
      let out = "";
      for (let i = 0; i < this.length; i++) out += this.box(i)?.value || "";
      this.code = out;
      /* The field that is actually posted is written here rather than left to
         x-model: the last digit submits the form in the same tick, before Alpine
         has had a chance to push the new value into it. */
      const refs = (this as unknown as { $refs: Record<string, HTMLInputElement> }).$refs;
      if (refs.out) refs.out.value = out;
    },

    submit() {
      this.fill();
      const form = this.box(0)?.closest("form");
      if (form && this.code.length === this.length) {
        form.requestSubmit ? form.requestSubmit() : form.submit();
      }
    },

    clock() {
      const m = Math.floor(this.left / 60);
      const s = this.left % 60;
      return m + ":" + String(s).padStart(2, "0");
    },
  };
}
