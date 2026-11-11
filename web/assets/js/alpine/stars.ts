/* Inline 5-star rating (GP-05, RS-03): reasons appear at 2 stars or fewer. */
export function stars(value = 0) {
  return {
    value,
    hover: 0,
    get shown() {
      return this.hover || this.value;
    },
    get needsReason() {
      return this.value > 0 && this.value <= 2;
    },
    pick(n: number) {
      this.value = n;
    },
  };
}
