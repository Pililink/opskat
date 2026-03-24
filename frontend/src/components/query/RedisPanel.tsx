import { useState, useCallback, useRef } from "react";
import { RedisKeyBrowser } from "./RedisKeyBrowser";
import { RedisKeyDetail } from "./RedisKeyDetail";

interface RedisPanelProps {
  tabId: string;
}

const MIN_WIDTH = 160;
const MAX_WIDTH = 400;
const DEFAULT_WIDTH = 220;

export function RedisPanel({ tabId }: RedisPanelProps) {
  const [sidebarWidth, setSidebarWidth] = useState(DEFAULT_WIDTH);
  const dragging = useRef(false);
  const startX = useRef(0);
  const startWidth = useRef(0);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      dragging.current = true;
      startX.current = e.clientX;
      startWidth.current = sidebarWidth;

      const handleMouseMove = (ev: MouseEvent) => {
        if (!dragging.current) return;
        const delta = ev.clientX - startX.current;
        const newWidth = Math.min(
          MAX_WIDTH,
          Math.max(MIN_WIDTH, startWidth.current + delta)
        );
        setSidebarWidth(newWidth);
      };

      const handleMouseUp = () => {
        dragging.current = false;
        document.removeEventListener("mousemove", handleMouseMove);
        document.removeEventListener("mouseup", handleMouseUp);
      };

      document.addEventListener("mousemove", handleMouseMove);
      document.addEventListener("mouseup", handleMouseUp);
    },
    [sidebarWidth]
  );

  return (
    <div className="flex h-full">
      {/* Left: Key browser */}
      <div
        className="shrink-0 border-r"
        style={{ width: sidebarWidth }}
      >
        <RedisKeyBrowser tabId={tabId} />
      </div>

      {/* Resize handle */}
      <div
        className="w-1 shrink-0 cursor-col-resize hover:bg-accent active:bg-accent"
        onMouseDown={handleMouseDown}
      />

      {/* Right: Key detail */}
      <div className="min-w-0 flex-1">
        <RedisKeyDetail tabId={tabId} />
      </div>
    </div>
  );
}
