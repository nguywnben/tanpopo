import '@testing-library/jest-dom/vitest';

if (!HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal() {
    this.setAttribute('open', '');
    this.setAttribute('aria-modal', 'true');
  };
}

if (!HTMLDialogElement.prototype.close) {
  HTMLDialogElement.prototype.close = function close() {
    this.removeAttribute('open');
    this.removeAttribute('aria-modal');
  };
}
