// Shared color scheme and label helpers for the knowledge-graph dashboard.
// Extracted from flow-graph-view so the node detail panel can reuse them
// without a circular import.
//
// Each entity type gets a hex triplet: node background (20% alpha), border
// (60% alpha), text, and legend/dot swatch. Designed for a black graph
// background.

export interface LabelColor {
    bg: string;
    border: string;
    dot: string;
    text: string;
}

export const labelColors: Record<string, LabelColor> = {
    Account: { bg: '#7c3aed20', border: '#7c3aed99', dot: '#7c3aed', text: '#c4b5fd' }, // purple
    Agent: { bg: '#64748b20', border: '#64748b99', dot: '#64748b', text: '#cbd5e1' }, // slate
    Artifact: { bg: '#78716c20', border: '#78716c99', dot: '#78716c', text: '#d6d3d1' }, // stone
    Attempt: { bg: '#eab30820', border: '#eab30899', dot: '#eab308', text: '#fde047' }, // yellow
    Capability: { bg: '#84cc1620', border: '#84cc1699', dot: '#84cc16', text: '#bef264' }, // lime
    Credential: { bg: '#f59e0b20', border: '#f59e0b99', dot: '#f59e0b', text: '#fcd34d' }, // amber
    Endpoint: { bg: '#6366f120', border: '#6366f199', dot: '#6366f1', text: '#a5b4fc' }, // indigo
    Episodic: { bg: '#40404020', border: '#52525299', dot: '#525252', text: '#a3a3a3' }, // neutral
    Evidence: { bg: '#14b8a620', border: '#14b8a699', dot: '#14b8a6', text: '#5eead4' }, // teal
    Host: { bg: '#3b82f620', border: '#3b82f699', dot: '#3b82f6', text: '#93c5fd' }, // blue
    Misconfiguration: { bg: '#fb923c20', border: '#fb923c99', dot: '#fb923c', text: '#fdba74' }, // orange-light
    Port: { bg: '#06b6d420', border: '#06b6d499', dot: '#06b6d4', text: '#67e8f9' }, // cyan
    PrivChange: { bg: '#f43f5e20', border: '#f43f5e99', dot: '#f43f5e', text: '#fda4af' }, // rose
    Service: { bg: '#22c55e20', border: '#22c55e99', dot: '#22c55e', text: '#86efac' }, // green
    ValidAccess: { bg: '#f9731620', border: '#f9731699', dot: '#f97316', text: '#fdba74' }, // orange
    Vhost: { bg: '#ec489920', border: '#ec489999', dot: '#ec4899', text: '#f9a8d4' }, // pink
    Vulnerability: { bg: '#ef444420', border: '#ef444499', dot: '#ef4444', text: '#fca5a5' }, // red
    WebApp: { bg: '#d946ef20', border: '#d946ef99', dot: '#d946ef', text: '#e879f9' }, // fuchsia
};

export const fallbackColor: LabelColor = { bg: '#52525220', border: '#52525299', dot: '#525252', text: '#a3a3a3' };

export function colorForLabel(label: string): LabelColor {
    return labelColors[label] ?? fallbackColor;
}

// primaryLabel returns the most specific label of an entity, skipping the
// generic `Entity` label Graphiti attaches to every node.
export function primaryLabel(labels: string[]): string {
    for (const l of labels) {
        if (l && l !== 'Entity') {
            return l;
        }
    }

    return labels[0] ?? 'Entity';
}
