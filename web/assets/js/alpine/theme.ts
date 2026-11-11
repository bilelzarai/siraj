/* Theme switch (BR-04): light / dark / device. The choice is stored server-side
   (user_settings.theme) or in the sj_theme cookie for guests and visitors; this only
   paints the current page at once so the menu feels instant. */
export function theme(current = "system") {
  return {
    current,
    set(this: any, value: string) {
      this.current = value;
      const root = document.documentElement;
      if (value === "system") root.removeAttribute("data-theme");
      else root.setAttribute("data-theme", value);
      document.cookie = `sj_theme=${value};path=/;max-age=31536000;samesite=lax`;
    },
  };
}
