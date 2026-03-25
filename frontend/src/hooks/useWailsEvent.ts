import { useEffect } from "react";
import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime";

/**
 * Subscribe to a Wails event with automatic cleanup.
 */
export function useWailsEvent<T = unknown>(eventName: string, handler: (data: T) => void) {
  useEffect(() => {
    EventsOn(eventName, handler);
    return () => {
      EventsOff(eventName);
    };
  }, [eventName, handler]);
}
