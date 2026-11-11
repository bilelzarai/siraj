/* Domain switch / list pager (DM-07, DM-08): 5 cards per line on desktop, 3 on tablet,
   2 on a phone — recomputed on resize, no server call. */
export function pager(count = 0) {
  return {
    page: 0,
    per: 5,
    count,
    /* Kept so destroy() can take the very listener init() added. */
    onResize: () => {},
    init() {
      this.measure();
      this.onResize = () => this.measure();
      window.addEventListener("resize", this.onResize);
    },
    destroy() {
      window.removeEventListener("resize", this.onResize);
    },
    measure() {
      const w = window.innerWidth;
      this.per = w >= 1024 ? 5 : w >= 700 ? 3 : 2;
      this.page = Math.min(this.page, Math.max(0, this.pages - 1));
    },
    get pages() {
      return Math.max(1, Math.ceil(this.count / this.per));
    },
    shown(index: number) {
      return index >= this.page * this.per && index < (this.page + 1) * this.per;
    },
    next() {
      this.page = Math.min(this.pages - 1, this.page + 1);
    },
    prev() {
      this.page = Math.max(0, this.page - 1);
    },
  };
}
