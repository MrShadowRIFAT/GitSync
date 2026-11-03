import type { ToastMessage } from '../types';
import { CheckIcon, AlertIcon, FolderIcon, InfoIcon, CloseIcon } from './Icons';

interface Props {
  toasts: ToastMessage[];
  onDismiss: (id: string) => void;
}

export function ToastContainer({ toasts, onDismiss }: Props) {
  if (toasts.length === 0) return null;

  return (
    <div
      className="fixed bottom-4 right-4 z-50 flex flex-col gap-2.5 max-w-sm w-full pointer-events-none select-none"
      data-wails-no-drag=""
    >
      {toasts.map((toast) => {
        let icon = <InfoIcon className="w-4 h-4 text-accent shrink-0 mt-0.5" />;
        let borderClass = "border-border";

        if (toast.type === 'success') {
          icon = <CheckIcon className="w-4 h-4 text-[#3fb950] shrink-0 mt-0.5" />;
          borderClass = "border-[#3fb950]/30";
        } else if (toast.type === 'backup') {
          icon = <FolderIcon className="w-4 h-4 text-accent shrink-0 mt-0.5" />;
          borderClass = "border-accent/30";
        } else if (toast.type === 'conflict') {
          icon = <AlertIcon className="w-4 h-4 text-[#f85149] shrink-0 mt-0.5" />;
          borderClass = "border-[#f85149]/40 bg-[#f85149]/5";
        } else if (toast.type === 'warning') {
          icon = <AlertIcon className="w-4 h-4 text-[#d29922] shrink-0 mt-0.5" />;
          borderClass = "border-[#d29922]/40";
        } else if (toast.type === 'error') {
          icon = <AlertIcon className="w-4 h-4 text-[#f85149] shrink-0 mt-0.5" />;
          borderClass = "border-[#f85149]/40";
        }

        return (
          <div
            key={toast.id}
            className={`pointer-events-auto bg-[#161b22] border ${borderClass} rounded-lg shadow-2xl shadow-black/80 p-3.5 flex items-start gap-3 backdrop-blur-md animate-slide-up transition-all`}
          >
            {icon}
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between gap-2">
                <h4 className="text-xs font-semibold text-text-primary truncate">
                  {toast.title}
                </h4>
                <button
                  onClick={() => onDismiss(toast.id)}
                  className="text-text-muted hover:text-text-primary transition-colors p-0.5 rounded"
                  title="Dismiss notification"
                >
                  <CloseIcon className="w-3.5 h-3.5" />
                </button>
              </div>
              <p className="text-[11px] text-text-secondary mt-0.5 leading-relaxed break-words">
                {toast.message}
              </p>
            </div>
          </div>
        );
      })}
    </div>
  );
}

