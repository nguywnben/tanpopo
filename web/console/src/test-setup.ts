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

if (typeof HTMLElement !== 'undefined') {
  if (!('showPopover' in HTMLElement.prototype)) {
    Object.defineProperty(HTMLElement.prototype, 'showPopover', {
      configurable: true,
      value() {
        this.setAttribute('data-popover-open', 'true');
      },
    });
  }
  if (!('hidePopover' in HTMLElement.prototype)) {
    Object.defineProperty(HTMLElement.prototype, 'hidePopover', {
      configurable: true,
      value() {
        this.removeAttribute('data-popover-open');
      },
    });
  }
}

if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}
