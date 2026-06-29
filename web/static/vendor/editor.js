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
    // Media-library picker (M4). open toggles the modal; html holds the loaded
    // MediaPickerGrid fragment; savedRange preserves the caret so an inserted
    // image lands where the user was editing (the modal steals focus otherwise).
    picker: { open: false, html: '', savedRange: null },

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

    // media() opens the media-library picker modal (M4) and loads the first page
    // of selectable images. The current caret range is saved so the eventual
    // insertion lands where the user was typing.
    media() {
      const sel = window.getSelection();
      this.picker.savedRange = (sel && sel.rangeCount > 0) ? sel.getRangeAt(0).cloneRange() : null;
      this.picker.open = true;
      this.loadPage('/admin/media/picker');
    },

    loadPage(url) {
      this.picker.html = '<p class="text-small text-muted">Loading…</p>';
      fetch(url, { headers: { 'X-Requested-With': 'XMLHttpRequest' } })
        .then((r) => r.text())
        .then((html) => { this.picker.html = html; })
        .catch(() => { this.picker.html = '<p class="text-small text-error">Could not load the media library.</p>'; });
    },

    closePicker() {
      this.picker.open = false;
      this.picker.html = '';
    },

    // pick() is invoked by a grid tile's @click; it reads the chosen image's
    // src/alt from the button's data-* attributes, inserts the <img>, and closes
    // the modal. The inserted markup is re-sanitized server-side on save.
    pick(e) {
      const btn = e.currentTarget;
      const src = btn.getAttribute('data-src');
      const alt = btn.getAttribute('data-alt') || '';
      if (src) this.insertImageFromPicker(src, alt);
      this.closePicker();
    },

    insertImageFromPicker(src, alt) {
      this.$refs.surface.focus();
      // Restore the caret position captured when the picker opened so the image
      // is inserted in-place rather than at the start of the surface.
      const sel = window.getSelection();
      if (this.picker.savedRange && sel) {
        sel.removeAllRanges();
        sel.addRange(this.picker.savedRange);
      }
      const img = document.createElement('img');
      img.src = src;
      img.alt = alt;
      if (sel && sel.rangeCount > 0) {
        const range = sel.getRangeAt(0);
        range.collapse(false);
        range.insertNode(img);
        range.setStartAfter(img);
        range.collapse(true);
        sel.removeAllRanges();
        sel.addRange(range);
      } else {
        this.$refs.surface.appendChild(img);
      }
      this.syncFromSurface();
    },
  }));
});
