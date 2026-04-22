import { useState, useCallback, useRef, useEffect } from "react";

interface UseResizeHandleOptions {
  defaultSize: number;
  minSize: number;
  maxSize: number;
  /** "x" for column resize, "y" for row resize. Default "x". */
  axis?: "x" | "y";
  /** true for right/bottom panels where dragging toward origin makes the panel larger */
  reverse?: boolean;
  /** localStorage key — if set, size is persisted across sessions */
  storageKey?: string;
  /** Called on drag end with the final size — useful for persisting to a store */
  onResizeEnd?: (size: number) => void;
}

function clamp(val: number, min: number, max: number) {
  return Math.max(min, Math.min(max, val));
}

export function useResizeHandle({
  defaultSize,
  minSize,
  maxSize,
  axis = "x",
  reverse = false,
  storageKey,
  onResizeEnd,
}: UseResizeHandleOptions) {
  const [size, setSize] = useState(() => {
    if (storageKey) {
      const saved = localStorage.getItem(storageKey);
      if (saved) return clamp(Number(saved), minSize, maxSize);
    }
    return clamp(defaultSize, minSize, maxSize);
  });
  const [isResizing, setIsResizing] = useState(false);
  const sizeRef = useRef(size);
  useEffect(() => {
    sizeRef.current = size;
  }, [size]);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setIsResizing(true);
      const start = axis === "x" ? e.clientX : e.clientY;
      const startSize = sizeRef.current;

      const onMouseMove = (ev: MouseEvent) => {
        const current = axis === "x" ? ev.clientX : ev.clientY;
        const delta = reverse ? start - current : current - start;
        setSize(clamp(startSize + delta, minSize, maxSize));
      };

      const onMouseUp = () => {
        setIsResizing(false);
        document.removeEventListener("mousemove", onMouseMove);
        document.removeEventListener("mouseup", onMouseUp);
        if (storageKey) {
          localStorage.setItem(storageKey, String(sizeRef.current));
        }
        onResizeEnd?.(sizeRef.current);
      };

      document.addEventListener("mousemove", onMouseMove);
      document.addEventListener("mouseup", onMouseUp);
    },
    [minSize, maxSize, axis, reverse, storageKey, onResizeEnd]
  );

  return { size, isResizing, handleMouseDown };
}
