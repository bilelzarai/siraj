/* Alpine and its focus plugin ship no type declarations, so the slice of their API
   this app calls is declared here. Keep it to what `main.ts` actually uses: Alpine
   holds local UI state only, the server owns the rules (NF-03, NF-05). */

declare module "alpinejs" {
  interface Alpine {
    /* A component factory, registered under the name templates use in x-data. */
    data(name: string, callback: (...args: any[]) => object): void;
    /* A store survives an htmx swap, which a component bound to the card does not. */
    store(name: string, value?: unknown): any;
    plugin(plugin: unknown): void;
    magic(name: string, callback: (el: HTMLElement) => unknown): void;
    start(): void;
  }
  const Alpine: Alpine;
  export default Alpine;
}

declare module "@alpinejs/focus" {
  const focus: unknown;
  export default focus;
}
