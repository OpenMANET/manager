// =============================================================================
// LatSelect.jsx — Lattice-themed dropdown
// =============================================================================
//
// Native <select> popups are rendered by the OS and cannot be cross-browser
// styled. This component is a drop-in replacement that renders a fully
// Lattice-themed listbox so the popup matches the rest of the UI in dark
// mode. Options are passed as an array; the trigger fills its parent
// (.lat-field), the popup is sized to its content (min-width: trigger,
// width: max-content) so long option labels don't wrap and short option
// lists don't stretch to the trigger width.
//
// Accessibility: button trigger with aria-haspopup="listbox" + aria-expanded.
// Open popup is role="listbox"; each option is role="option" with
// aria-selected. Keyboard: Enter/Space/Arrow toggles open, Arrow Up/Down
// moves highlight, Home/End jump, Enter chooses, Escape closes.

import { useState, useEffect, useRef, useCallback, useId } from 'react';

export default function LatSelect({
  value,
  onChange,
  options,
  ariaLabel,
  disabled = false,
  placeholder = '—',
  className = '',
}) {
  const [open, setOpen] = useState(false);
  const [highlighted, setHighlighted] = useState(-1);
  const rootRef = useRef(null);
  const popupRef = useRef(null);
  const id = useId();

  const selected = options.find(o => o.value === value);
  const displayLabel = selected?.label ?? placeholder;

  const close = useCallback(() => setOpen(false), []);
  const choose = useCallback(
    (v) => {
      onChange?.(v);
      setOpen(false);
    },
    [onChange],
  );

  // Opens the popup with the current value's index pre-highlighted (or 0).
  // Computing the initial highlight at the event source avoids a render-then-
  // effect-then-render cascade.
  const openPopup = useCallback(() => {
    const idx = options.findIndex(o => o.value === value);
    setHighlighted(idx >= 0 ? idx : 0);
    setOpen(true);
  }, [options, value]);

  // Click outside closes the popup. Using mousedown so a click on a child
  // option (which fires onClick afterward) still gets handled before close.
  useEffect(() => {
    if (!open) return undefined;
    const onDoc = (e) => {
      if (rootRef.current && !rootRef.current.contains(e.target)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  useEffect(() => {
    if (!open || highlighted < 0 || !popupRef.current) return;
    const node = popupRef.current.querySelector(`[data-idx="${highlighted}"]`);
    if (typeof node?.scrollIntoView === 'function') {
      node.scrollIntoView({ block: 'nearest' });
    }
  }, [open, highlighted]);

  const onKeyDown = (e) => {
    if (disabled) return;
    if (!open) {
      if (['Enter', ' ', 'ArrowDown', 'ArrowUp'].includes(e.key)) {
        e.preventDefault();
        openPopup();
      }
      return;
    }
    switch (e.key) {
      case 'Escape':
        e.preventDefault();
        close();
        break;
      case 'ArrowDown':
        e.preventDefault();
        setHighlighted(h => Math.min(h + 1, options.length - 1));
        break;
      case 'ArrowUp':
        e.preventDefault();
        setHighlighted(h => Math.max(h - 1, 0));
        break;
      case 'Home':
        e.preventDefault();
        setHighlighted(0);
        break;
      case 'End':
        e.preventDefault();
        setHighlighted(options.length - 1);
        break;
      case 'Enter':
      case ' ':
        e.preventDefault();
        if (options[highlighted]) choose(options[highlighted].value);
        break;
      default:
        break;
    }
  };

  const wrapClass = [
    'lat-select-wrap',
    open ? 'open' : '',
    disabled ? 'disabled' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div ref={rootRef} className={wrapClass}>
      <button
        type="button"
        className="lat-select"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? `${id}-listbox` : undefined}
        aria-disabled={disabled || undefined}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => !disabled && (open ? setOpen(false) : openPopup())}
        onKeyDown={onKeyDown}
      >
        <span className="lat-select-value">{displayLabel}</span>
        <span className="lat-select-caret" aria-hidden="true">▾</span>
      </button>
      {open && (
        <div
          ref={popupRef}
          id={`${id}-listbox`}
          className="lat-select-popup"
          role="listbox"
          aria-label={ariaLabel}
          tabIndex={-1}
        >
          {options.length === 0 ? (
            <div className="lat-select-empty">No options</div>
          ) : (
            options.map((o, i) => {
              const isSelected = o.value === value;
              const isHighlighted = i === highlighted;
              const cls = [
                'lat-select-option',
                isHighlighted ? 'highlight' : '',
                isSelected ? 'selected' : '',
              ]
                .filter(Boolean)
                .join(' ');
              return (
                <button
                  key={String(o.value)}
                  type="button"
                  role="option"
                  aria-selected={isSelected}
                  data-idx={i}
                  className={cls}
                  onMouseEnter={() => setHighlighted(i)}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => choose(o.value)}
                >
                  {o.label}
                </button>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
