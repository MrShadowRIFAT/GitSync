import { useState, useEffect, useCallback } from 'react';
import type { RepoStatus, LogEntry } from '../types';
import * as WailsRuntime from '../wailsjs/runtime/runtime';

/**
 * Subscribe to a Wails backend event.
 * Returns a cleanup function.
 */
export function useWailsEvent<T>(eventName: string, handler: (data: T) => void) {
  useEffect(() => {
    let cleanup: (() => void) | undefined;
    try {
      if (typeof window !== 'undefined' && (window as any)['runtime']?.['EventsOn']) {
        cleanup = WailsRuntime.EventsOn(eventName, handler);
      }
    } catch {
      // running outside Wails (browser dev mode)
    }
    return () => {
      if (cleanup) cleanup();
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventName]);
}

/**
 * Hook that tracks live repo statuses via Wails events.
 */
export function useRepoStatuses(initial: RepoStatus[] = []) {
  const [statuses, setStatuses] = useState<RepoStatus[]>(initial);

  useEffect(() => {
    // Initial fetch on mount
    if (typeof window !== 'undefined' && (window as any)['go']?.['main']?.['App']?.['GetRepoStatuses']) {
      (window as any)['go']['main']['App']['GetRepoStatuses']().then((res: RepoStatus[]) => {
        if (Array.isArray(res)) setStatuses(res);
      }).catch(() => {});
    }
  }, []);

  useWailsEvent<RepoStatus[]>('repo:status', useCallback((data) => {
    setStatuses(data ?? []);
  }, []));

  return statuses;
}

/**
 * Hook that accumulates log entries from the Wails log event stream.
 */
export function useLiveLog(initial: LogEntry[] = [], maxEntries = 500) {
  const [logs, setLogs] = useState<LogEntry[]>(initial);

  useWailsEvent<LogEntry>('log:entry', useCallback((entry) => {
    setLogs(prev => {
      const next = [...prev, entry];
      return next.length > maxEntries ? next.slice(next.length - maxEntries) : next;
    });
  }, [maxEntries]));

  return [logs, setLogs] as const;
}
