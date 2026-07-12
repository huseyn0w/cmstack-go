/*
 * Repeatable FAQ section for the Agentic CMS-Go service editor.
 *
 * A dependency-free Alpine.js component backing the editor's FAQ list. It seeds
 * its rows from the server-rendered JSON (so a no-JS visitor still gets the
 * already-rendered rows and a working form), and exposes add / remove / moveUp /
 * moveDown so the operator can build an ordered FAQ. The posted inputs are the
 * repeated `faq_question[]` / `faq_answer[]` arrays; the server pairs them by
 * index, drops blank rows, and assigns dense positions, so the array order here
 * IS the stored order. CSP-clean (no eval, no inline handlers beyond Alpine).
 */
document.addEventListener('alpine:init', () => {
  window.Alpine.data('faqEditor', (initial) => ({
    items: Array.isArray(initial) ? initial.slice() : [],
    nextKey: 0,
    init() {
      // Ensure every seeded row has a unique key and seed the counter past them.
      this.items.forEach((it, i) => {
        if (typeof it.key !== 'number') it.key = i;
        if (it.key >= this.nextKey) this.nextKey = it.key + 1;
      });
    },
    add() {
      this.items.push({ key: this.nextKey++, question: '', answer: '' });
    },
    remove(index) {
      this.items.splice(index, 1);
    },
    moveUp(index) {
      if (index <= 0) return;
      const [row] = this.items.splice(index, 1);
      this.items.splice(index - 1, 0, row);
    },
    moveDown(index) {
      if (index >= this.items.length - 1) return;
      const [row] = this.items.splice(index, 1);
      this.items.splice(index + 1, 0, row);
    },
  }));
});
