// Per-flow and global memory of the last screen (tab) the user was on, so
// switching flows reopens the same screen and coming back reopens the screen last
// shown in that flow. Best-effort: storage may be unavailable (private mode,
// quota) and every access is guarded.

export type FlowTabSlot = 'central' | 'side';

interface FlowTabMemory {
    byFlow: Record<string, Partial<Record<FlowTabSlot, string>>>;
    last: Partial<Record<FlowTabSlot, string>>;
}

const STORAGE_KEY = 'pentagi:flow-tab-memory';

// Keep the per-flow map bounded: drop the oldest inserted flows past this count.
const MAX_TRACKED_FLOWS = 100;

const emptyMemory = (): FlowTabMemory => ({ byFlow: {}, last: {} });

// recallTab returns the tab remembered for this flow, falling back to the last
// tab used anywhere when the flow has none.
export function recallTab(flowId: null | string, slot: FlowTabSlot): string | undefined {
    const memory = readMemory();

    if (flowId) {
        const perFlow = memory.byFlow[flowId]?.[slot];

        if (perFlow) {
            return perFlow;
        }
    }

    return memory.last[slot];
}

// rememberTab records the tab for this flow and as the global last-used tab.
export function rememberTab(flowId: null | string, slot: FlowTabSlot, value: string): void {
    const memory = readMemory();

    memory.last[slot] = value;

    if (flowId) {
        memory.byFlow[flowId] = { ...memory.byFlow[flowId], [slot]: value };

        const ids = Object.keys(memory.byFlow);

        for (const id of ids.slice(0, Math.max(0, ids.length - MAX_TRACKED_FLOWS))) {
            delete memory.byFlow[id];
        }
    }

    writeMemory(memory);
}

function readMemory(): FlowTabMemory {
    try {
        const raw = window.localStorage.getItem(STORAGE_KEY);

        if (!raw) {
            return emptyMemory();
        }

        const parsed = JSON.parse(raw) as Partial<FlowTabMemory>;

        return { byFlow: parsed.byFlow ?? {}, last: parsed.last ?? {} };
    } catch {
        return emptyMemory();
    }
}

function writeMemory(memory: FlowTabMemory): void {
    try {
        window.localStorage.setItem(STORAGE_KEY, JSON.stringify(memory));
    } catch {
        // Storage full or unavailable: the memory is best-effort.
    }
}
