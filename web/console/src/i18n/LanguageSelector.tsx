import {useEffect, useMemo, useRef} from 'react';
import {Selector} from '@astryxdesign/core/Selector';
import {SUPPORTED_LOCALES, isSupportedLocale} from './locales';
import {useI18n} from './I18nProvider';

function getLayoutTop(element: HTMLElement): number {
  let top = 0;
  let current: HTMLElement | null = element;
  while (current) {
    top += current.offsetTop;
    current = current.offsetParent as HTMLElement | null;
  }
  return top;
}

function isPopoverOpen(el: HTMLElement): boolean {
  try {
    return el.matches(':popover-open');
  } catch {
    return false;
  }
}

export function LanguageSelector({id = 'language-selector', className = ''}: {id?: string; className?: string}) {
  const {locale, setLocale, t} = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);

  const options = useMemo(
    () => SUPPORTED_LOCALES.map(item => ({value: item.code, label: item.nativeName})),
    [],
  );

  // Align Astryx Selector's macOS-style selected-item overlay on the very first open:
  // Astryx measures --_selector-overlay-margin in a layout effect before its passive scroll effect
  // scrolls the selected option into view, so bottom-of-list locales (e.g. vi) otherwise
  // jump upward by scrollTop on the first open. Scrolling the selected option into view and
  // recomputing the overlay margin inside a MutationObserver microtask runs before first paint.
  useEffect(() => {
    const root = rootRef.current;
    if (!root || typeof MutationObserver === 'undefined') return;

    let syncing = false;
    const syncOverlay = () => {
      if (syncing) return;
      const popover = root.querySelector<HTMLElement>('[popover]');
      const listbox = popover?.querySelector<HTMLElement>('[role="listbox"]');
      const combobox = root.querySelector<HTMLElement>('[role="combobox"]');
      const anchor = combobox?.parentElement ?? combobox;
      if (!popover || !listbox || !anchor || !isPopoverOpen(popover)) return;

      const selected = listbox.querySelector<HTMLElement>('[role="option"][aria-selected="true"]');
      const listboxHeight = listbox.offsetHeight;
      if (!selected || listboxHeight <= 0) return;

      const itemTop = getLayoutTop(selected) - getLayoutTop(listbox);
      const itemBottom = itemTop + selected.offsetHeight;
      const bottomPad = parseFloat(getComputedStyle(listbox).paddingBottom) || 0;
      const topPad = parseFloat(getComputedStyle(listbox).paddingTop) || 0;

      if (itemBottom + bottomPad > listbox.scrollTop + listbox.clientHeight) {
        listbox.scrollTop = itemBottom + bottomPad - listbox.clientHeight;
      } else if (itemTop - topPad < listbox.scrollTop) {
        listbox.scrollTop = Math.max(0, itemTop - topPad);
      }

      const anchorRect = anchor.getBoundingClientRect();
      const itemCenter = itemTop - listbox.scrollTop + selected.offsetHeight / 2;
      const anchorCenter = anchorRect.top + anchorRect.height / 2;
      const desiredTop = anchorCenter - itemCenter - 1;
      const maxTop = Math.max(0, window.innerHeight - listboxHeight);
      const clampedTop = Math.min(Math.max(desiredTop, 0), maxTop);
      const offset = Math.max(0, anchorRect.bottom - clampedTop);
      const room = window.innerHeight - listboxHeight - anchorRect.bottom;
      const nextMargin = `min(${-offset}px, max(${-anchorRect.bottom}px, calc(${room}px - var(--_layer-keyboard-inset-block-end, ${room}px))))`;

      if (popover.style.getPropertyValue('--_selector-overlay-margin') !== nextMargin) {
        syncing = true;
        popover.style.setProperty('--_selector-overlay-margin', nextMargin);
        popover.style.setProperty('--_astryx-layer-enter-x', '0');
        popover.style.setProperty('--_astryx-layer-enter-y', '0');
        popover.style.setProperty('--_astryx-layer-enter-gap', '0px');
        syncing = false;
      }
    };

    const observer = new MutationObserver(syncOverlay);
    observer.observe(root, {subtree: true, attributes: true, attributeFilter: ['style'], childList: true});
    return () => observer.disconnect();
  }, [locale]);

  return (
    <div ref={rootRef} className={className ? `tp-lang-selector ${className}` : 'tp-lang-selector'}>
      <Selector
        id={id}
        label={t('common.language')}
        isLabelHidden
        size="sm"
        variant="input"
        hasSearch={false}
        options={options}
        value={locale}
        onChange={next => {
          if (isSupportedLocale(next)) {
            setLocale(next);
          }
        }}
      />
    </div>
  );
}
