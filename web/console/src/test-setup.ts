import '@testing-library/jest-dom/vitest';
import '@astryxdesign/core/reset.css';
import '@astryxdesign/core/astryx.css';

// Ensure jsdom runs axe-core checks like label-content-name-mismatch without _isIconLigature crashing or treating text as ligatures.
Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
  configurable: true,
  value: function getContext(this: HTMLCanvasElement) {
    let lastText = '';
    const canvasRef = this;
    return {
      canvas: canvasRef,
      font: '',
      textAlign: 'left',
      textBaseline: 'alphabetic',
      fillText: (text: string) => {
        lastText = String(text);
      },
      clearRect: () => {
        lastText = '';
      },
      measureText: (text: string) => ({width: String(text).length * 10}),
      getImageData: () => {
        const data = new Uint8ClampedArray(16);
        for (let i = 0; i < Math.min(4, lastText.length); i += 1) {
          data[i * 4 + 3] = (lastText.charCodeAt(i) % 250) + 1;
        }
        return {data};
      },
    };
  },
});

// jsdom's CSSOM parser ignores @layer rules, leaving @astryxdesign/core's <VisuallyHidden> visible in tests.
// Mirror VisuallyHidden's clip styles in a test stylesheet so axe-core SC 2.5.3 evaluates real visibility.
if (typeof document !== 'undefined' && !document.getElementById('tp-test-astryx-a11y')) {
  const style = document.createElement('style');
  style.id = 'tp-test-astryx-a11y';
  style.textContent =
    '.x10l6tqk.x87ps6o{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0;}';
  document.head.appendChild(style);
}

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
