/* Confetti on a good result (RS-01): 80 % correct or more solo, or finishing first in
   a room, a duel, a device round or a won challenge. Reduced motion skips it. */
const COLOURS = ["#e8b65a", "#35b88b", "#7c3aed", "#db2777", "#2563eb", "#f3d394"];

export function confetti() {
  return {
    init() {
      if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
      const box = document.createElement("div");
      box.className = "confetti";
      box.setAttribute("aria-hidden", "true");
      for (let i = 0; i < 70; i++) {
        const piece = document.createElement("i");
        piece.style.left = Math.random() * 100 + "%";
        piece.style.background = COLOURS[i % COLOURS.length];
        piece.style.animationDelay = Math.random() * 0.8 + "s";
        piece.style.transform = `rotate(${Math.random() * 180}deg)`;
        box.appendChild(piece);
      }
      document.body.appendChild(box);
      setTimeout(() => box.remove(), 3600);
    },
  };
}
