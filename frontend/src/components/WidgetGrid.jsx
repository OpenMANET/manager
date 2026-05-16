// =============================================================================
// WidgetGrid.jsx — Reusable draggable/resizable widget grid layout
// =============================================================================
// Provides lock/unlock, drag-to-reorder (whole card), and smooth percentage-
// based resize for dashboard and GPS pages. Layout persisted to localStorage.

import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import './WidgetGrid.css';

// ---------------------------------------------------------------------------
// useWidgetLayout — localStorage-backed layout state
// ---------------------------------------------------------------------------

const LOCK_KEY = 'widgetLayoutLocked';
const MIN_WIDTH_PCT = 20;
const MAX_WIDTH_PCT = 100;

function layoutKey(pageId) {
  return `widgetLayout:${pageId}`;
}

function useWidgetLayout(pageId, widgetDefs) {
  const defaults = useMemo(() => ({
    order: widgetDefs.map((w) => w.id),
    widths: Object.fromEntries(
      widgetDefs.map((w) => [w.id, w.defaultWidth]),
    ),
  }), [widgetDefs]);

  const [locked, setLocked] = useState(() => {
    try {
      const v = localStorage.getItem(LOCK_KEY);
      return v === null ? true : JSON.parse(v);
    } catch { return true; }
  });

  const [layout, setLayout] = useState(() => {
    try {
      const saved = JSON.parse(localStorage.getItem(layoutKey(pageId)));
      if (saved && Array.isArray(saved.order)) {
        const known = new Set(widgetDefs.map((w) => w.id));
        const order = saved.order.filter((id) => known.has(id));
        widgetDefs.forEach((w) => { if (!order.includes(w.id)) order.push(w.id); });
        const widths = { ...defaults.widths, ...(saved.widths || {}) };
        return { order, widths };
      }
    } catch { /* ignore */ }
    return defaults;
  });

  const persist = useCallback((next) => {
    setLayout(next);
    try { localStorage.setItem(layoutKey(pageId), JSON.stringify(next)); } catch { /* ignore */ }
  }, [pageId]);

  const toggleLock = useCallback(() => {
    setLocked((prev) => {
      const next = !prev;
      try { localStorage.setItem(LOCK_KEY, JSON.stringify(next)); } catch { /* ignore */ }
      return next;
    });
  }, []);

  const resetLayout = useCallback(() => {
    persist(defaults);
  }, [persist, defaults]);

  const setOrder = useCallback((order) => {
    persist({ ...layout, order });
  }, [persist, layout]);

  const setWidth = useCallback((id, pct) => {
    const def = widgetDefs.find((w) => w.id === id);
    const minW = def ? def.minWidth : MIN_WIDTH_PCT;
    const clamped = Math.max(minW, Math.min(MAX_WIDTH_PCT, Math.round(pct)));
    persist({ ...layout, widths: { ...layout.widths, [id]: clamped } });
  }, [persist, layout, widgetDefs]);

  const getWidth = useCallback((id) => {
    return layout.widths[id] ?? 50;
  }, [layout.widths]);

  return { locked, layout, toggleLock, resetLayout, setOrder, setWidth, getWidth };
}

// ---------------------------------------------------------------------------
// Interactive element check — don't start drag on buttons, inputs, etc.
// ---------------------------------------------------------------------------

const INTERACTIVE_TAGS = new Set(['INPUT', 'BUTTON', 'SELECT', 'TEXTAREA', 'A', 'CANVAS']);

function isInteractive(el) {
  let node = el;
  while (node) {
    if (INTERACTIVE_TAGS.has(node.tagName)) return true;
    if (node.dataset && node.dataset.widgetId) break;
    node = node.parentElement;
  }
  return false;
}

// ---------------------------------------------------------------------------
// WidgetGrid component
// ---------------------------------------------------------------------------

const DRAG_THRESHOLD = 5;

export default function WidgetGrid({ pageId, title, widgets, renderWidget }) {
  const {
    locked, layout, toggleLock, resetLayout, setOrder, setWidth, getWidth,
  } = useWidgetLayout(pageId, widgets);

  const gridRef = useRef(null);
  const [isMobile, setIsMobile] = useState(false);
  const [dragId, setDragId] = useState(null);
  const [dragOverId, setDragOverId] = useState(null);
  const dragOverRef = useRef(null);
  const dragState = useRef(null);

  // Track responsive breakpoint.
  useEffect(() => {
    const el = gridRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      setIsMobile(entries[0].contentRect.width < 540);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Keep dragOverRef in sync.
  useEffect(() => {
    dragOverRef.current = dragOverId;
  }, [dragOverId]);

  // --- Drag reorder (whole card) ---

  const onCardPointerDown = useCallback((e, id) => {
    if (locked) return;
    if (isInteractive(e.target)) return;

    // Don't intercept resize handle.
    if (e.target.closest('.widget-resize-handle')) return;

    e.preventDefault();
    dragState.current = { id, startX: e.clientX, startY: e.clientY, active: false };

    const onMove = (me) => {
      const ds = dragState.current;
      if (!ds) return;
      const dx = me.clientX - ds.startX;
      const dy = me.clientY - ds.startY;

      if (!ds.active) {
        if (Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD) return;
        ds.active = true;
        setDragId(ds.id);
      }

      const el = document.elementFromPoint(me.clientX, me.clientY);
      if (el) {
        const wrapper = el.closest('[data-widget-id]');
        if (wrapper && wrapper.dataset.widgetId !== ds.id) {
          setDragOverId(wrapper.dataset.widgetId);
        }
      }
    };

    const onUp = () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);

      const ds = dragState.current;
      const overTarget = dragOverRef.current;
      if (ds && ds.active && overTarget != null) {
        const currentOrder = layout.order;
        const fromIdx = currentOrder.indexOf(ds.id);
        const toIdx = currentOrder.indexOf(overTarget);
        if (fromIdx !== -1 && toIdx !== -1 && fromIdx !== toIdx) {
          const next = [...currentOrder];
          next.splice(fromIdx, 1);
          next.splice(toIdx, 0, ds.id);
          setOrder(next);
        }
      }

      dragState.current = null;
      setDragId(null);
      setDragOverId(null);
    };

    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
  }, [locked, layout.order, setOrder]);

  // --- Smooth percentage resize ---

  const onResizePointerDown = useCallback((e, id) => {
    e.preventDefault();
    e.stopPropagation();
    const startX = e.clientX;
    const startWidth = getWidth(id);
    const containerWidth = gridRef.current ? gridRef.current.offsetWidth : 800;

    const onMove = (me) => {
      const dx = me.clientX - startX;
      const deltaPct = (dx / containerWidth) * 100;
      setWidth(id, startWidth + deltaPct);
    };

    const onUp = () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
    };

    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
  }, [getWidth, setWidth]);

  // --- Render ---

  return (
    <div style={{ width: '100%' }}>
      <div className="widget-grid-header">
        <h2>{title}</h2>
        <button className="widget-lock-btn" onClick={toggleLock} title={locked ? 'Unlock layout' : 'Lock layout'}>
          {locked ? 'Locked' : 'Unlocked'}
        </button>
        {!locked && (
          <button className="widget-reset-btn" onClick={resetLayout} title="Reset to default layout">
            Reset
          </button>
        )}
      </div>
      <div className="widget-grid" ref={gridRef}>
        {layout.order.map((id) => {
          const widthPct = isMobile ? 100 : getWidth(id);
          const isDragging = dragId === id;
          const isDragOver = dragOverId === id;
          let cls = 'widget-wrapper';
          if (!locked) cls += ' unlocked';
          if (isDragging) cls += ' dragging';
          if (isDragOver) cls += ' drag-over';

          return (
            <div
              key={id}
              className={cls}
              data-widget-id={id}
              style={{
                width: `calc(${widthPct}% - 8px)`,
                cursor: !locked ? 'grab' : undefined,
                touchAction: !locked ? 'none' : undefined,
              }}
              onPointerDown={(e) => onCardPointerDown(e, id)}
            >
              {renderWidget(id)}
              {!locked && !isMobile && (
                <div
                  className="widget-resize-handle"
                  onPointerDown={(e) => onResizePointerDown(e, id)}
                />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
