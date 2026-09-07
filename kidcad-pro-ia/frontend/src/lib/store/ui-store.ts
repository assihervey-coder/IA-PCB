/**
 * Store Zustand : état d'interface global (rail latéral, modale active,
 * unités, notifications toast).
 */
import { create } from "zustand";

export type ToastKind = "info" | "success" | "error";

export interface Toast {
  id: string;
  message: string;
  kind: ToastKind;
}

export type Units = "mm" | "mil";

interface UIStoreState {
  sidebarOpen: boolean;
  activeModal: string | null;
  units: Units;
  toasts: Toast[];
  toggleSidebar: () => void;
  openModal: (name: string) => void;
  closeModal: () => void;
  setUnits: (units: Units) => void;
  pushToast: (message: string, kind?: ToastKind) => void;
  dismissToast: (id: string) => void;
}

let toastSeq = 0;

export const useUIStore = create<UIStoreState>()((set, get) => ({
  sidebarOpen: true,
  activeModal: null,
  units: "mm",
  toasts: [],

  toggleSidebar: () => set((st) => ({ sidebarOpen: !st.sidebarOpen })),

  openModal: (name) => set({ activeModal: name }),

  closeModal: () => set({ activeModal: null }),

  setUnits: (units) => set({ units }),

  pushToast: (message, kind = "info") => {
    const id = `toast-${Date.now()}-${toastSeq++}`;
    set((st) => ({ toasts: [...st.toasts, { id, message, kind }] }));
    // Auto-fermeture au bout de 4,5 s.
    setTimeout(() => get().dismissToast(id), 4500);
  },

  dismissToast: (id) => set((st) => ({ toasts: st.toasts.filter((t) => t.id !== id) })),
}));
