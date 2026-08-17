import { useQuery } from '@apollo/client/react';
import {
    Background,
    BackgroundVariant,
    Controls,
    type Edge,
    MarkerType,
    MiniMap,
    type Node,
    type NodeMouseHandler,
    ReactFlow,
} from '@xyflow/react';
import { Expand, Loader2, Maximize2, Shrink } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Skeleton } from '@/components/ui/skeleton';
import {
    type AttackGraphEdgeFragmentFragment,
    type AttackGraphNodeFragmentFragment,
    AttackGraphView,
    FlowAttackGraphDocument,
} from '@/graphql/types';
import { cn } from '@/lib/utils';

import '@xyflow/react/dist/style.css';

// ─── Color scheme ───────────────────────────────────────────────────────
// Each entity type gets a hex triplet used for the node background (at 20%
// alpha), border (at 60% alpha), text, and MiniMap swatch. The scheme is
// designed for a black graph background.
interface LabelColor {
    bg: string;
    border: string;
    dot: string;
    text: string;
}

const labelColors: Record<string, LabelColor> = {
    Account:       { bg: '#7c3aed20', border: '#7c3aed99', dot: '#7c3aed', text: '#c4b5fd' }, // purple
    Agent:          { bg: '#64748b20', border: '#64748b99', dot: '#64748b', text: '#cbd5e1' }, // slate
    Artifact:       { bg: '#78716c20', border: '#78716c99', dot: '#78716c', text: '#d6d3d1' }, // stone
    Attempt:        { bg: '#eab30820', border: '#eab30899', dot: '#eab308', text: '#fde047' }, // yellow
    Capability:     { bg: '#84cc1620', border: '#84cc1699', dot: '#84cc16', text: '#bef264' }, // lime
    Credential:     { bg: '#f59e0b20', border: '#f59e0b99', dot: '#f59e0b', text: '#fcd34d' }, // amber
    Endpoint:       { bg: '#6366f120', border: '#6366f199', dot: '#6366f1', text: '#a5b4fc' }, // indigo
    Episodic:       { bg: '#40404020', border: '#52525299', dot: '#525252', text: '#a3a3a3' }, // neutral
    Evidence:       { bg: '#14b8a620', border: '#14b8a699', dot: '#14b8a6', text: '#5eead4' }, // teal
    Host:           { bg: '#3b82f620', border: '#3b82f699', dot: '#3b82f6', text: '#93c5fd' }, // blue
    Misconfiguration:{ bg: '#fb923c20', border: '#fb923c99', dot: '#fb923c', text: '#fdba74' }, // orange-light
    Port:           { bg: '#06b6d420', border: '#06b6d499', dot: '#06b6d4', text: '#67e8f9' }, // cyan
    PrivChange:     { bg: '#f43f5e20', border: '#f43f5e99', dot: '#f43f5e', text: '#fda4af' }, // rose
    Service:        { bg: '#22c55e20', border: '#22c55e99', dot: '#22c55e', text: '#86efac' }, // green
    ValidAccess:    { bg: '#f9731620', border: '#f9731699', dot: '#f97316', text: '#fdba74' }, // orange
    Vhost:          { bg: '#ec489920', border: '#ec489999', dot: '#ec4899', text: '#f9a8d4' }, // pink
    Vulnerability:  { bg: '#ef444420', border: '#ef444499', dot: '#ef4444', text: '#fca5a5' }, // red
    WebApp:         { bg: '#d946ef20', border: '#d946ef99', dot: '#d946ef', text: '#e879f9' }, // fuchsia
};

const fallbackColor: LabelColor = { bg: '#52525220', border: '#52525299', dot: '#525252', text: '#a3a3a3' };

function colorForLabel(label: string): LabelColor {
    return labelColors[label] ?? fallbackColor;
}

// ─── Layout ─────────────────────────────────────────────────────────────
const labelColumnOrder: string[] = [
    'Host', 'Port', 'Service', 'Endpoint', 'WebApp', 'Vhost',
    'Vulnerability', 'Misconfiguration', 'Capability', 'Attempt', 'PrivChange',
    'ValidAccess', 'Account', 'Credential', 'Artifact', 'Evidence', 'Agent',
    'Episodic',
];

const COLUMN_WIDTH = 260;
const ROW_HEIGHT = 80;
const TOP_PADDING = 40;

// ─── Custom node component ───────────────────────────────────────────────
interface EntityNodeData {
    [key: string]: unknown;
    createdAt: null | string;
    label: string;
    labels: string[];
    name: string;
    summary: string;
    type: string;
    uuid: string;
}

function columnForLabel(label: string): number {
    const idx = labelColumnOrder.indexOf(label);

    if (idx >= 0) {return idx;}

    return labelColumnOrder.length + label.charCodeAt(0) % 16;
}

function EntityNode({ data }: { data: EntityNodeData }) {
    const c = colorForLabel(data.type);

    return (
        <div
            className="rounded-xl border-2 px-3 py-2 shadow-lg backdrop-blur-sm"
            style={{
                background: c.bg,
                borderColor: c.border,
                color: c.text,
                maxWidth: 220,
                minWidth: 140,
            }}
        >
            <div className="text-[10px] font-bold uppercase tracking-wider opacity-70">
                {data.type}
            </div>
            <div className="text-sm font-semibold leading-tight">
                {data.name || data.type}
            </div>
            {data.summary ? (
                <div className="mt-1 text-xs opacity-60 line-clamp-2">
                    {data.summary}
                </div>
            ) : null}
        </div>
    );
}

function primaryLabel(labels: string[]): string {
    for (const l of labels) {
        if (l && l !== 'Entity') {return l;}
    }

    return labels[0] ?? 'Entity';
}

const nodeTypes = { entityNode: EntityNode };

// ─── Main component ──────────────────────────────────────────────────────
interface FlowDashboardAttackGraphProps {
    flowId: string;
    pollInterval?: number;
}

export function FlowDashboardAttackGraph({ flowId, pollInterval = 10000 }: FlowDashboardAttackGraphProps) {
    const [view, setView] = useState<AttackGraphView>(AttackGraphView.Main);
    const [isFullscreen, setIsFullscreen] = useState(false);
    const [selectedNodeId, setSelectedNodeId] = useState<null | string>(null);

    const { data, error, loading } = useQuery(FlowAttackGraphDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval,
        variables: { flowId, view },
    });

    const graph = data?.flowAttackGraph;

    const rfNodes = useMemo(() => (graph ? layoutNodes(graph.nodes) : []), [graph]);
    const rfEdges = useMemo(() => (graph ? toFlowEdges(graph.edges) : []), [graph]);

    const hasNodes = (graph?.nodes?.length ?? 0) > 0;

    // Find the full entity data for the selected node (the ReactFlow node only
    // carries display data; we look up the original entity by UUID).
    const selectedNode = useMemo(() => {
        if (!selectedNodeId || !graph) {return null;}

        return graph.nodes.find((n) => n.uuid === selectedNodeId) ?? null;
    }, [selectedNodeId, graph]);

    const presentLabels = useMemo(() => {
        if (!graph) {return [];}

        return [...new Set(graph.nodes.map((n) => primaryLabel(n.labels)))];
    }, [graph]);

    const handleNodeClick: NodeMouseHandler = useCallback((_, node) => {
        setSelectedNodeId(node.id);
    }, []);

    const flowElement = (
        <ReactFlow
            colorMode="dark"
            edges={rfEdges}
            fitView
            // key forces a fresh fitView when the view (MAIN/FULL) changes.
            key={`${view}-${isFullscreen}`}
            minZoom={0.05}
            nodes={rfNodes}
            nodesConnectable={false}
            nodeTypes={nodeTypes}
            onNodeClick={handleNodeClick}
            proOptions={{ hideAttribution: true }}
        >
            <Background color="#1a1a2e" gap={24} variant={BackgroundVariant.Dots} />
            <Controls position="bottom-right" showInteractive={false} />
            <MiniMap
                className="bg-gray-900/80"
                maskColor="rgba(0,0,0,0.6)"
                nodeColor={(n) => {
                    if (n.data && typeof n.data === 'object' && 'type' in n.data) {
                        return colorForLabel(String(n.data.type)).dot;
                    }

                    return '#525252';
                }}
                pannable
                zoomable
            />
        </ReactFlow>
    );

    const graphContainer = (
        <>
            <GraphLegend labels={presentLabels} />
            <div className="h-[420px] w-full overflow-hidden rounded-lg bg-black">
                {flowElement}
            </div>
            <div className="mt-2">
                <NodeDetailPanel node={selectedNode} />
            </div>
        </>
    );

    const fullscreenContainer = (
        <>
            <GraphLegend labels={presentLabels} />
            <div className="relative h-[calc(95vh-7rem)] w-full rounded-lg bg-black">
                {flowElement}
            </div>
            <div className="mt-2 px-4 pb-2">
                <NodeDetailPanel node={selectedNode} />
            </div>
        </>
    );

    return (
        <Card>
            <CardHeader className="flex flex-row items-center justify-between gap-2">
                <div className="flex flex-col gap-1">
                    <CardTitle className="flex items-center gap-2">
                        Attack chain
                        {graph?.truncated ? (
                            <span className="text-muted-foreground text-xs font-normal">
                                (truncated: {graph.nodes.length}/{graph.totalNodes} nodes)
                            </span>
                        ) : null}
                    </CardTitle>
                    <CardDescription>
                        Knowledge-graph view of the attack surface discovered by Graphiti
                    </CardDescription>
                </div>
                <div className="flex items-center gap-2">
                    <div className="bg-muted inline-flex rounded-md p-0.5">
                        <Button
                            className={cn('h-7 px-3 py-1 text-xs', view === AttackGraphView.Main && 'shadow-sm')}
                            onClick={() => setView(AttackGraphView.Main)}
                            size="sm"
                            variant={view === AttackGraphView.Main ? 'secondary' : 'ghost'}
                        >
                            Main
                        </Button>
                        <Button
                            className={cn('h-7 px-3 py-1 text-xs', view === AttackGraphView.Full && 'shadow-sm')}
                            onClick={() => setView(AttackGraphView.Full)}
                            size="sm"
                            variant={view === AttackGraphView.Full ? 'secondary' : 'ghost'}
                        >
                            Full
                        </Button>
                    </div>
                    <Button
                        className="h-7 px-2 py-1"
                        onClick={() => setIsFullscreen(true)}
                        size="sm"
                        variant="outline"
                    >
                        <Maximize2 className="size-3.5" />
                        <span className="sr-only">Fullscreen</span>
                    </Button>
                </div>
            </CardHeader>
            <CardContent>
                {loading && !graph ? (
                    <div className="flex h-[420px] items-center justify-center">
                        <Skeleton className="h-full w-full" />
                        <span className="text-muted-foreground absolute flex items-center gap-2 text-sm">
                            <Loader2 className="size-4 animate-spin" />
                            Loading knowledge graph…
                        </span>
                    </div>
                ) : error ? (
                    <Empty>
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <Expand className="size-5" />
                            </EmptyMedia>
                            <EmptyTitle>Graph unavailable</EmptyTitle>
                            <EmptyDescription>
                                Graphiti / Neo4j is not reachable. The dashboard will retry automatically.
                            </EmptyDescription>
                        </EmptyHeader>
                    </Empty>
                ) : !hasNodes ? (
                    <Empty>
                        <EmptyHeader>
                            <EmptyTitle>No attack-chain data yet</EmptyTitle>
                            <EmptyDescription>
                                Graphiti has not extracted any entities for this flow. The graph will populate as the
                                flow progresses.
                            </EmptyDescription>
                        </EmptyHeader>
                    </Empty>
                ) : (
                    graphContainer
                )}
            </CardContent>

            <Dialog onOpenChange={setIsFullscreen} open={isFullscreen}>
                <DialogContent className="h-[95vh] max-w-[95vw] gap-0 p-0 sm:max-w-[95vw]">
                    <DialogHeader className="flex flex-row items-center justify-between border-b px-4 py-3 sm:flex-row">
                        <div>
                            <DialogTitle className="text-base">
                                Attack chain — {view === AttackGraphView.Main ? 'Main' : 'Full'}
                            </DialogTitle>
                            <DialogDescription className="sr-only">
                                Fullscreen knowledge-graph view of the attack chain
                            </DialogDescription>
                        </div>
                        <div className="flex items-center gap-2">
                            <div className="bg-muted inline-flex rounded-md p-0.5">
                                <Button
                                    className={cn(
                                        'h-7 px-3 py-1 text-xs',
                                        view === AttackGraphView.Main && 'shadow-sm',
                                    )}
                                    onClick={() => setView(AttackGraphView.Main)}
                                    size="sm"
                                    variant={view === AttackGraphView.Main ? 'secondary' : 'ghost'}
                                >
                                    Main
                                </Button>
                                <Button
                                    className={cn(
                                        'h-7 px-3 py-1 text-xs',
                                        view === AttackGraphView.Full && 'shadow-sm',
                                    )}
                                    onClick={() => setView(AttackGraphView.Full)}
                                    size="sm"
                                    variant={view === AttackGraphView.Full ? 'secondary' : 'ghost'}
                                >
                                    Full
                                </Button>
                            </div>
                            <Button
                                className="h-7 px-2 py-1"
                                onClick={() => setIsFullscreen(false)}
                                size="sm"
                                variant="outline"
                            >
                                <Shrink className="size-3.5" />
                                <span className="sr-only">Exit fullscreen</span>
                            </Button>
                        </div>
                    </DialogHeader>
                    {fullscreenContainer}
                </DialogContent>
            </Dialog>
        </Card>
    );
}

// ─── Legend ──────────────────────────────────────────────────────────────
function GraphLegend({ labels }: { labels: string[] }) {
    const present = labelColumnOrder.filter((l) => labels.includes(l));
    // Add any labels not in the predefined order
    const extra = labels.filter((l) => !labelColumnOrder.includes(l)).sort();
    const all = [...present, ...extra];

    if (all.length === 0) {return null;}

    return (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 px-1 pb-2">
            {all.map((label) => {
                const c = colorForLabel(label);

                return (
                    <div className="flex items-center gap-1.5" key={label}>
                        <span
                            className="inline-block size-3 rounded-full border"
                            style={{ background: c.dot, borderColor: c.border }}
                        />
                        <span className="text-xs font-medium text-gray-300">{label}</span>
                    </div>
                );
            })}
        </div>
    );
}

// isAttackEdge marks the edges that represent forward attack progress so they
// animate and draw the eye along the chain.
function isAttackEdge(type: string): boolean {
    return (
        type === 'YIELDED_ACCESS' ||
        type === 'YIELDED_PRIV_ACCESS' ||
        type === 'AUTHENTICATES_TO' ||
        type === 'AS_ACCOUNT' ||
        type === 'ESCALATED_VIA' ||
        type === 'PIVOTED_TO' ||
        type === 'BELONGS_TO_ACCOUNT'
    );
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// layoutNodes assigns a deterministic column/row position per node and wraps
// each entity in a custom entityNode with full display data.
function layoutNodes(nodes: AttackGraphNodeFragmentFragment[]): Node[] {
    const byColumn = new Map<number, AttackGraphNodeFragmentFragment[]>();

    for (const n of nodes) {
        const col = columnForLabel(primaryLabel(n.labels));
        const arr = byColumn.get(col) ?? [];
        arr.push(n);
        byColumn.set(col, arr);
    }

    const cols = [...byColumn.keys()].sort((a, b) => a - b);

    const out: Node[] = [];
    cols.forEach((col, colIdx) => {
        const items = byColumn.get(col)!;
        items.forEach((n, rowIdx) => {
            const type = primaryLabel(n.labels);
            const summary = n.summary ? truncate(n.summary, 80) : '';
            const name = n.name ? truncate(n.name, 40) : type;
            out.push({
                data: {
                    createdAt: n.createdAt,
                    label: name,
                    labels: n.labels,
                    name,
                    summary,
                    type,
                    uuid: n.uuid,
                } satisfies EntityNodeData,
                id: n.uuid,
                position: {
                    x: colIdx * COLUMN_WIDTH,
                    y: TOP_PADDING + rowIdx * ROW_HEIGHT,
                },
                type: 'entityNode',
            });
        });
    });

    return out;
}

// ─── Detail panel ────────────────────────────────────────────────────────
function NodeDetailPanel({ node }: { node: AttackGraphNodeFragmentFragment | null }) {
    if (!node) {
        return (
            <div className="flex items-center justify-center rounded-lg border border-gray-700 bg-gray-900/60 px-4 py-3 text-sm text-gray-500">
                Click a node to see its details
            </div>
        );
    }

    const type = primaryLabel(node.labels);
    const c = colorForLabel(type);

    return (
        <div
            className="rounded-lg border-2 px-4 py-3"
            style={{ background: c.bg, borderColor: c.border, color: c.text }}
        >
            <div className="flex items-center gap-2">
                <span
                    className="inline-block size-2.5 rounded-full"
                    style={{ background: c.dot }}
                />
                <span className="text-xs font-bold uppercase tracking-wider opacity-70">{type}</span>
            </div>
            <div className="mt-1 text-base font-semibold">{node.name || type}</div>
            {node.summary ? (
                <div className="mt-1.5 text-sm opacity-70">{node.summary}</div>
            ) : null}
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-0.5 text-xs opacity-50">
                <span>UUID: {node.uuid.slice(0, 12)}…</span>
                {node.labels.length > 0 ? <span>Labels: {node.labels.join(', ')}</span> : null}
                {node.createdAt ? <span>Created: {new Date(node.createdAt).toLocaleString()}</span> : null}
            </div>
        </div>
    );
}

function toFlowEdges(edges: AttackGraphEdgeFragmentFragment[]): Edge[] {
    return edges.map((e) => {
        const isAttack = isAttackEdge(e.type);

        return {
            animated: isAttack,
            id: e.uuid,
            label: e.type,
            labelBgBorderRadius: 4,
            labelBgPadding: [4, 2] as [number, number],
            labelBgStyle: { fill: '#1a1a2e' },
            labelShowBg: true,
            labelStyle: { fill: '#9ca3af', fontSize: 9, fontWeight: 500 },
            // Arrow on every edge to show the direction of the relationship.
            markerEnd: {
                color: isAttack ? '#f97316' : '#9ca3af',
                height: 16,
                type: MarkerType.ArrowClosed,
                width: 16,
            },
            source: e.sourceUUID,
            style: {
                stroke: isAttack ? '#f97316' : '#9ca3af',
                strokeWidth: isAttack ? 2 : 1.5,
            },
            target: e.targetUUID,
        };
    });
}

function truncate(s: string, n: number): string {
    if (s.length <= n) {return s;}

    return s.slice(0, n - 1) + '…';
}