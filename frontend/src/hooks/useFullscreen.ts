import { useState, useEffect } from "react";
import { WindowIsFullscreen } from "../../wailsjs/runtime/runtime";

export function useFullscreen() {
  const [isFullscreen, setIsFullscreen] = useState(false);

  useEffect(() => {
    // Check initial state
    WindowIsFullscreen()
      .then(setIsFullscreen)
      .catch(() => {});

    // Re-check on resize since Mac fullscreen triggers a resize
    const handleResize = () => {
      WindowIsFullscreen()
        .then(setIsFullscreen)
        .catch(() => {});
    };

    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, []);

  return isFullscreen;
}
