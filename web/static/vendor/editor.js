/*
 * Self-hosted rich-text editor island for CMStack-Go.
 *
 * A dependency-free Alpine.js component wrapping a contenteditable surface with
 * the DESIGN_SYSTEM §5 toolbar (Bold, Italic, H2, H3, bullet/ordered lists,
 * blockquote, link, undo, redo, media-insert stub). It is CSP-clean (no eval, no
 * inline handlers beyond Alpine bindings) and self-hosted (no CDN).
 *
 * The editor mirrors its HTML into a hidden <textarea> on every input so a plain
 * form POST carries the body; the server bluemonday-sanitizes it on save, so the
 * editor never needs to be the source of truth for safety.
 *
 * It uses document.execCommand — deprecated but universally supported and the
 * pragmatic zero-build choice for a contenteditable toolbar. Output is always
 * re-sanitized server-side, so execCommand's quirks are cosmetic, never a
 * security boundary.
 */
document.addEventListener('alpine:init', () => {
  window.Alpine.data('richTextEditor', (initialHTML) => ({
    html: initialHTML || '',
    active: { bold: false, italic: false, h2: false, h3: false, ul: false, ol: false, blockquote: false },

    init() {
      // Seed the contenteditable surface with the initial (sanitized) HTML.
      this.$refs.surface.innerHTML = this.html;
      this.syncFromSurface();
      // Keep the toolbar pressed-states in sync with the caret position.
      this.$refs.surface.addEventListener('keyup', () => this.refreshActive());
      this.$refs.surface.addEventListener('mouseup', () => this.refreshActive());
    },

    syncFromSurface() {
      this.html = this.$refs.surface.innerHTML;
      this.$refs.hidden.value = this.html;
    },

    exec(command, value = null) {
      this.$refs.surface.focus();
      document.execCommand(command, false, value);
      this.syncFromSurface();
      this.refreshActive();
    },

    format(block) {
      // Toggle a block format (h2/h3/blockquote/p) via formatBlock.
      const current = (document.queryCommandValue('formatBlock') || '').toLowerCase();
      const target = current === block ? 'p' : block;
      this.exec('formatBlock', target);
    },

    refreshActive() {
      try {
        this.active.bold = document.queryCommandState('bold');
        this.active.italic = document.queryCommandState('italic');
        this.active.ul = document.queryCommandState('insertUnorderedList');
        this.active.ol = document.queryCommandState('insertOrderedList');
        const block = (document.queryCommandValue('formatBlock') || '').toLowerCase();
        this.active.h2 = block === 'h2';
        this.active.h3 = block === 'h3';
        this.active.blockquote = block === 'blockquote';
      } catch (e) {
        /* queryCommandState can throw in some browsers; ignore. */
      }
    },

    link() {
      const url = window.prompt('Link URL (http/https/mailto):', 'https://');
      if (!url) return;
      this.exec('createLink', url);
    },

    // TODO(M4): wire to the media-library picker (Modal/Dialog). For now this is
    // a stub that opens a prompt so the toolbar button is functional.
    media() {
      const url = window.prompt('Image URL (http/https) — media library lands in M4:', 'https://');
      if (!url) return;
      this.exec('insertImage', url);
    },
  }));
});
