import { useState, useCallback } from 'react';
import type { ToastMessage } from '../types';
import { useWailsEvent } from './useWailsEvents';

export function useToast() {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const dismissToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const addToast = useCallback((toast: Omit<ToastMessage, 'id'> & { id?: string }) => {
    const id = toast.id || `toast-${Date.now()}-${Math.random().toString(36).substring(2, 6)}`;
    const duration = toast.duration ?? 4500;
    const newToast: ToastMessage = { ...toast, id, duration };

    setToasts((prev) => [...prev.slice(-4), newToast]); // Keep up to 5 concurrent toasts

    if (duration > 0) {
      setTimeout(() => {
        dismissToast(id);
      }, duration);
    }
  }, [dismissToast]);

  // Automatically listen to backend notifications
  useWailsEvent<ToastMessage>('toast:notify', useCallback((msg) => {
    if (!msg || !msg.title) return;
    addToast(msg);
  }, [addToast]));

  return { toasts, addToast, dismissToast };
}

