/**
 * client/stores/ui-store — État d'interface (vue active, sélection, outils).
 */
import { create } from 'zustand'

export type ViewId = 'projects' | 'schematic' | 'pcb' | 'viewer3d' | 'verify' | 'export'
export type SchematicTool = 'select' | 'wire' | 'add'
export type PcbTool = 'select' | 'move' | 'rotate' | 'flip'

interface UiState {
  view: ViewId
  setView: (v: ViewId) => void

  schematicTool: SchematicTool
  setSchematicTool: (t: SchematicTool) => void
  selectedComponentId: string | null
  setSelectedComponentId: (id: string | null) => void

  pcbTool: PcbTool
  setPcbTool: (t: PcbTool) => void
  selectedFootprintId: string | null
  setSelectedFootprintId: (id: string | null) => void
  activeLayer: 'F.Cu' | 'B.Cu'
  setActiveLayer: (l: 'F.Cu' | 'B.Cu') => void

  showRatsnest: boolean
  toggleRatsnest: () => void
  showFront: boolean
  showBack: boolean
  toggleLayer: (l: 'F.Cu' | 'B.Cu') => void
}

export const useUiStore = create<UiState>((set) => ({
  view: 'projects',
  setView: (view) => set({ view }),

  schematicTool: 'select',
  setSchematicTool: (schematicTool) => set({ schematicTool }),
  selectedComponentId: null,
  setSelectedComponentId: (selectedComponentId) => set({ selectedComponentId }),

  pcbTool: 'select',
  setPcbTool: (pcbTool) => set({ pcbTool }),
  selectedFootprintId: null,
  setSelectedFootprintId: (selectedFootprintId) => set({ selectedFootprintId }),
  activeLayer: 'F.Cu',
  setActiveLayer: (activeLayer) => set({ activeLayer }),

  showRatsnest: true,
  toggleRatsnest: () => set((s) => ({ showRatsnest: !s.showRatsnest })),
  showFront: true,
  showBack: true,
  toggleLayer: (l) =>
    set((s) =>
      l === 'F.Cu' ? { showFront: !s.showFront } : { showBack: !s.showBack },
    ),
}))
