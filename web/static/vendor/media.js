/*
 * Media library island for CMStack-Go.
 *
 * A dependency-free Alpine.js component that PROGRESSIVELY ENHANCES the native
 * media upload form (DESIGN_SYSTEM §5 File-upload): without JS the form still
 * uploads one file via a normal POST; with JS it intercepts the picker/drop,
 * uploads each file via XHR with a per-file progress bar + status, and reloads
 * the library when the batch completes. It also drives the metadata detail modal
 * (fetch the panel HTML) and the delete-confirm modal (state only; the form
 * posts normally). CSP-clean: no eval, no inline handlers beyond Alpine bindings.
 */
document.addEventListener('alpine:init', () => {
  window.Alpine.data('mediaLibrary', () => ({
    dragging: false,
    queue: [],
    confirmDelete: null,
    detailOpen: false,
    detailHTML: '',
    _seq: 0,

    // Native (no-JS-needed) submit guard: if the JS path already handled the
    // files via onPick/onDrop, prevent the duplicate native multipart POST.
    onNativeSubmit(e) {
      if (this.queue.length > 0) {
        e.preventDefault();
      }
    },

    onPick(e) {
      const files = Array.from(e.target.files || []);
      if (files.length) this.uploadAll(e.target.form, files);
      e.target.value = '';
    },

    onDrop(e) {
      this.dragging = false;
      const files = Array.from((e.dataTransfer && e.dataTransfer.files) || []);
      const form = e.currentTarget.closest('form');
      if (files.length && form) this.uploadAll(form, files);
    },

    uploadAll(form, files) {
      const action = form.getAttribute('action');
      const csrf = (form.querySelector('input[name="csrf_token"]') || {}).value || '';
      let remaining = files.length;
      files.forEach((file) => {
        const item = {
          id: ++this._seq,
          name: file.name,
          progress: 0,
          status: 'uploading',
          statusLabel: '0%',
        };
        this.queue.push(item);
        this.uploadOne(action, csrf, file, item, () => {
          remaining -= 1;
          if (remaining === 0) {
            // Reload once the whole batch settles so new cards appear. A short
            // delay lets the final status render/announce first.
            setTimeout(() => window.location.reload(), 600);
          }
        });
      });
    },

    uploadOne(action, csrf, file, item, done) {
      const fd = new FormData();
      if (csrf) fd.append('csrf_token', csrf);
      fd.append('file', file);

      const xhr = new XMLHttpRequest();
      xhr.open('POST', action, true);
      xhr.setRequestHeader('X-Requested-With', 'XMLHttpRequest');
      xhr.upload.addEventListener('progress', (ev) => {
        if (ev.lengthComputable) {
          item.progress = Math.round((ev.loaded / ev.total) * 100);
          item.statusLabel = item.progress + '%';
        }
      });
      xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          item.progress = 100;
          item.status = 'done';
          item.statusLabel = 'Done';
        } else {
          item.status = 'error';
          item.statusLabel = 'Failed';
        }
        done();
      });
      xhr.addEventListener('error', () => {
        item.status = 'error';
        item.statusLabel = 'Failed';
        done();
      });
      xhr.send(fd);
    },

    openDetail(url) {
      this.detailOpen = true;
      this.detailHTML = '<p class="text-small text-muted">Loading…</p>';
      fetch(url, { headers: { 'X-Requested-With': 'XMLHttpRequest' } })
        .then((r) => r.text())
        .then((html) => { this.detailHTML = html; })
        .catch(() => { this.detailHTML = '<p class="text-small text-error">Could not load file details.</p>'; });
    },

    closeDetail() {
      this.detailOpen = false;
      this.detailHTML = '';
    },
  }));
});
