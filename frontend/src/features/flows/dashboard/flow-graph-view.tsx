import { useQuery } from '@apollo/client/react';
import {
    Background,
    BackgroundVariant,
    Controls,
    type Edge,
    type EdgeMouseHandler,
    Handle,
    MarkerType,
    type Node,
    type NodeMouseHandler,
    Position,
    ReactFlow,
} from '@xyflow/react';
import { Expand, Loader2, Maximize2, Pause, Play, Shrink } from 'lucide-react';
import { type ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Skeleton } from '@/components/ui/skeleton';
import { colorForLabel, primaryLabel } from '@/features/flows/dashboard/flow-graph-labels';
import { FlowGraphNodePanel } from '@/features/flows/dashboard/flow-graph-node-panel';

import '@xyflow/react/dist/style.css';

import {
    type AttackGraphEdgeFragmentFragment,
    type AttackGraphNodeFragmentFragment,
    AttackGraphView,
    FlowAttackGraphDocument,
} from '@/graphql/types';
import { cn } from '@/lib/utils';

// ─── Layout ─────────────────────────────────────────────────────────────
// labelColumnMap controls the left-to-right column order of entity types
// in the graph. Labels sharing the same column index appear in the same
// vertical lane. Host and Account share column 2 (same attack-chain depth);
// WebApp and Vhost share column 5. Labels not in the map land in a
// trailing column, ordered alphabetically.
const labelColumnMap: Record<string, number> = {
    Account: 2,
    Agent: 8,
    Artifact: 8,
    Attempt: 7,
    Capability: 7,
    Credential: 1,
    Endpoint: 7,
    Episodic: 9,
    Evidence: 8,
    Host: 2,
    Misconfiguration: 7,
    Port: 3,
    PrivChange: 7,
    Service: 4,
    ValidAccess: 0,
    Vhost: 5,
    Vulnerability: 6,
    WebApp: 5,
};

const maxMappedColumn = 9;

const COLUMN_WIDTH = 340;
const NODE_WIDTH = 220;
const ROW_HEIGHT = 110;
const TOP_PADDING = 60;

// ─── Custom node component ───────────────────────────────────────────────
interface EntityNodeData {
    [key: string]: unknown;
    dimmed: boolean;
    label: string;
    name: string;
    selected: boolean;
    summary: string;
    type: string;
    uuid: string;
}

function EntityNode({ data }: { data: EntityNodeData }) {
    const c = colorForLabel(data.type);

    return (
        <div
            className="w-full rounded-xl border-2 px-3 py-2 backdrop-blur-sm"
            style={{
                background: data.selected ? c.border : c.bg,
                borderColor: data.selected ? c.text : c.border,
                boxShadow: data.selected ? `0 0 14px 3px ${c.dot}` : '0 4px 6px rgba(0,0,0,0.4)',
                color: c.text,
                opacity: data.dimmed ? 0.25 : 1,
                transition: 'opacity 0.2s, box-shadow 0.2s',
            }}
        >
            {/* One hidden target on the left, one hidden source on the right.
                Orthogonal routing (see toFlowEdges) handles edge separation, so
                multiple handles per side are no longer needed. */}
            <Handle
                position={Position.Left}
                style={{ opacity: 0 }}
                type="target"
            />
            <div className="text-[10px] font-bold tracking-wider uppercase opacity-70">{data.type}</div>
            <div className="text-sm leading-tight font-semibold">{data.name || data.type}</div>
            {data.summary ? <div className="mt-1 line-clamp-2 text-xs opacity-60">{data.summary}</div> : null}
            <Handle
                position={Position.Right}
                style={{ opacity: 0 }}
                type="source"
            />
        </div>
    );
}

const nodeTypes = { entityNode: EntityNode };

export interface FlowGraphViewProps {
    // When true, a toggle offers the FULL (every entity) view next to the base
    // view. Used by the Action Chain tab; the other Analytics tabs are fixed.
    allowFullToggle?: boolean;
    flowId: string;
    pollInterval?: number;
    view: AttackGraphView;
}

interface Layout {
    // uuid -> lane/position, used by the edge router.
    positions: Map<string, NodePosition>;
    // ReactFlow nodes, one per visible entity.
    rfNodes: Node[];
}

interface NodePosition {
    col: number;
    x: number;
    y: number;
}

// buildLayout assigns each visible entity a deterministic lane (column) and row
// and returns the bare ReactFlow nodes. Entities whose label is in hiddenLabels
// are dropped from the graph (legend filter); when revealedBefore is set, an
// entity with a createdAt past that instant is also dropped (replay timeline).
// Entities without a createdAt are always kept (they cannot be ordered).
export function buildLayout(
    nodes: AttackGraphNodeFragmentFragment[],
    hiddenLabels: Set<string>,
    revealedBefore: null | number = null,
): Layout {
    const byColumn = new Map<number, AttackGraphNodeFragmentFragment[]>();

    for (const n of nodes) {
        if (hiddenLabels.has(primaryLabel(n.labels))) {
            continue;
        }

        if (revealedBefore !== null && n.createdAt) {
            const t = new Date(n.createdAt).getTime();

            if (!Number.isNaN(t) && t > revealedBefore) {
                continue;
            }
        }

        const col = columnForLabel(primaryLabel(n.labels));
        const arr = byColumn.get(col) ?? [];
        arr.push(n);
        byColumn.set(col, arr);
    }

    const cols = [...byColumn.keys()].sort((a, b) => a - b);
    const positions = new Map<string, NodePosition>();
    const rfNodes: Node[] = [];

    cols.forEach((col, colIdx) => {
        byColumn.get(col)!.forEach((n, rowIdx) => {
            const type = primaryLabel(n.labels);
            const x = colIdx * COLUMN_WIDTH;
            const y = TOP_PADDING + rowIdx * ROW_HEIGHT;

            positions.set(n.uuid, { col: colIdx, x, y });
            rfNodes.push({
                data: {
                    dimmed: false,
                    label: n.name ? truncate(n.name, 40) : type,
                    name: n.name ? truncate(n.name, 40) : type,
                    selected: false,
                    summary: n.summary ? truncate(n.summary, 80) : '',
                    type,
                    uuid: n.uuid,
                } satisfies EntityNodeData,
                id: n.uuid,
                position: { x, y },
                style: { width: NODE_WIDTH },
                type: 'entityNode',
            });
        });
    });

    return { positions, rfNodes };
}

export function columnForLabel(label: string): number {
    const col = labelColumnMap[label];

    if (col !== undefined) {
        return col;
    }

    // Unknown labels: append after the known columns, ordered alphabetically.
    return maxMappedColumn + 1 + (label.charCodeAt(0) % 16);
}

// ─── Main component ──────────────────────────────────────────────────────

// connectedToSelection walks the graph from the selected node up to the Host(s)
// so the attack chain that led to it stays lit while its siblings dim. It stops
// expanding at Host nodes (the chain root), so the Host's other children stay
// dimmed instead of flooding the highlight.
function connectedToSelection(
    graph: { edges: AttackGraphEdgeFragmentFragment[]; nodes: AttackGraphNodeFragmentFragment[] },
    selectedNodeId: string,
): Set<string> {
    const hostUUIDs = new Set<string>();

    for (const n of graph.nodes) {
        if (n.labels.includes('Host')) {
            hostUUIDs.add(n.uuid);
        }
    }

    const connected = new Set([selectedNodeId]);

    if (hostUUIDs.has(selectedNodeId)) {
        for (const e of graph.edges) {
            if (e.sourceUUID === selectedNodeId) {
                connected.add(e.targetUUID);
            }

            if (e.targetUUID === selectedNodeId) {
                connected.add(e.sourceUUID);
            }
        }

        return connected;
    }

    const queue = [selectedNodeId];

    while (queue.length > 0) {
        const current = queue.shift()!;

        for (const e of graph.edges) {
            const neighbor = e.sourceUUID === current ? e.targetUUID : e.targetUUID === current ? e.sourceUUID : null;

            if (neighbor === null || connected.has(neighbor)) {
                continue;
            }

            connected.add(neighbor);

            if (!hostUUIDs.has(neighbor)) {
                queue.push(neighbor);
            }
        }
    }

    return connected;
}

const viewTitles: Record<AttackGraphView, string> = {
    [AttackGraphView.Action]: 'Action chain',
    [AttackGraphView.Full]: 'Full knowledge graph',
    [AttackGraphView.Infra]: 'Infrastructure',
    [AttackGraphView.Main]: 'Main attack chain',
    [AttackGraphView.Privesc]: 'Privilege-escalation chain',
};

const viewShortLabels: Record<AttackGraphView, string> = {
    [AttackGraphView.Action]: 'Action',
    [AttackGraphView.Full]: 'Full',
    [AttackGraphView.Infra]: 'Infrastructure',
    [AttackGraphView.Main]: 'Main',
    [AttackGraphView.Privesc]: 'Priv esc',
};

const viewDescriptions: Record<AttackGraphView, string> = {
    [AttackGraphView.Action]:
        'The backbone plus the attempts, artifacts and evidence collected while the agents worked.',
    [AttackGraphView.Full]: 'Every entity and edge Graphiti extracted for this flow.',
    [AttackGraphView.Infra]: 'Hosts, their open ports and the services running on them.',
    [AttackGraphView.Main]:
        'The attack-chain backbone discovered by Graphiti: host, port, service, web app, endpoint and vulnerability.',
    [AttackGraphView.Privesc]: 'Credentials, validated accesses, accounts and privilege changes.',
};

// ─── Edge routing ────────────────────────────────────────────────────────
interface FlowEdgeOptions {
    hoveredEdgeId: null | string;
    positions: Map<string, NodePosition>;
    selectedNodeId: null | string;
}

// RoutedEdge widens the ReactFlow Edge type with the smoothstep path options
// (centerX/borderRadius) used to pin the vertical run to a shared gutter.
type RoutedEdge = Edge & { pathOptions?: { borderRadius: number; centerX?: number } };

export function FlowGraphView({
    allowFullToggle = false,
    flowId,
    pollInterval = 10000,
    view: baseView,
}: FlowGraphViewProps) {
    const [showFull, setShowFull] = useState(false);
    const view = showFull ? AttackGraphView.Full : baseView;
    const [isFullscreen, setIsFullscreen] = useState(false);
    const [selectedNodeId, setSelectedNodeId] = useState<null | string>(null);
    const [hoveredEdgeId, setHoveredEdgeId] = useState<null | string>(null);
    const [hiddenLabels, setHiddenLabels] = useState<Set<string>>(() => new Set());
    // Replay timeline: revealActive off means "show everything"; when on, only
    // entities created before min + frac*(max-min) are shown.
    const [revealActive, setRevealActive] = useState(false);
    const [revealFrac, setRevealFrac] = useState(1);
    const [playing, setPlaying] = useState(false);

    const { data, error, loading } = useQuery(FlowAttackGraphDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval,
        variables: { flowId, view },
    });

    const graph = data?.flowAttackGraph;

    // Nodes to keep lit: an edge hover wins over a node selection.
    const activeEndpoints = useMemo(() => {
        if (!graph) {
            return null;
        }

        if (hoveredEdgeId) {
            const edge = graph.edges.find((e) => e.uuid === hoveredEdgeId);

            if (edge) {
                return new Set([edge.sourceUUID, edge.targetUUID]);
            }
        }

        if (selectedNodeId) {
            return connectedToSelection(graph, selectedNodeId);
        }

        return null;
    }, [graph, hoveredEdgeId, selectedNodeId]);

    const dimmedUUIDs = useMemo(() => {
        if (!graph || !activeEndpoints) {
            return null;
        }

        const dimmed = new Set<string>();

        for (const n of graph.nodes) {
            if (!activeEndpoints.has(n.uuid)) {
                dimmed.add(n.uuid);
            }
        }

        return dimmed;
    }, [graph, activeEndpoints]);

    // Time domain of the revealed entities, derived from their createdAt.
    // Null when there is nothing to animate (no timestamps or a single instant).
    const timeDomain = useMemo(() => {
        if (!graph) {
            return null;
        }

        let min = Infinity;
        let max = -Infinity;

        for (const n of graph.nodes) {
            if (!n.createdAt) {
                continue;
            }

            const t = new Date(n.createdAt).getTime();

            if (Number.isNaN(t)) {
                continue;
            }

            min = Math.min(min, t);
            max = Math.max(max, t);
        }

        if (!Number.isFinite(min) || min === max) {
            return null;
        }

        return { max, min };
    }, [graph]);

    const revealedBefore =
        revealActive && timeDomain ? timeDomain.min + revealFrac * (timeDomain.max - timeDomain.min) : null;

    const layout = useMemo(
        () => (graph ? buildLayout(graph.nodes, hiddenLabels, revealedBefore) : { positions: new Map(), rfNodes: [] }),
        [graph, hiddenLabels, revealedBefore],
    );

    const rfNodes = useMemo(
        () =>
            layout.rfNodes.map((node) => ({
                ...node,
                data: {
                    ...node.data,
                    dimmed: dimmedUUIDs ? !dimmedUUIDs.has(node.id) : false,
                    selected: node.id === selectedNodeId,
                },
            })),
        [layout, dimmedUUIDs, selectedNodeId],
    );

    const rfEdges = useMemo(
        () =>
            graph
                ? toFlowEdges(graph.edges, {
                      hoveredEdgeId,
                      positions: layout.positions,
                      selectedNodeId,
                  })
                : [],
        [graph, layout.positions, hoveredEdgeId, selectedNodeId],
    );

    const hasNodes = (graph?.nodes?.length ?? 0) > 0;

    // Find the full entity data for the selected node (the ReactFlow node only
    // carries display data; we look up the original entity by UUID).
    const selectedNode = useMemo(() => {
        if (!selectedNodeId || !graph) {
            return null;
        }

        return graph.nodes.find((n) => n.uuid === selectedNodeId) ?? null;
    }, [selectedNodeId, graph]);

    const hovered = useMemo(() => {
        if (!graph || !hoveredEdgeId) {
            return null;
        }

        const edge = graph.edges.find((e) => e.uuid === hoveredEdgeId);

        if (!edge) {
            return null;
        }

        return {
            edge,
            source: graph.nodes.find((n) => n.uuid === edge.sourceUUID) ?? null,
            target: graph.nodes.find((n) => n.uuid === edge.targetUUID) ?? null,
        };
    }, [graph, hoveredEdgeId]);

    const presentLabels = useMemo(() => {
        if (!graph) {
            return [];
        }

        const labels = [...new Set(graph.nodes.map((n) => primaryLabel(n.labels)))];

        // Sort by their column index so the legend matches the graph layout.
        return labels.sort((a, b) => {
            const colA = labelColumnMap[a] ?? maxMappedColumn + 1;
            const colB = labelColumnMap[b] ?? maxMappedColumn + 1;

            if (colA !== colB) {
                return colA - colB;
            }

            return a.localeCompare(b);
        });
    }, [graph]);

    const handleNodeClick: NodeMouseHandler = useCallback((_, node) => {
        setSelectedNodeId(node.id);
    }, []);

    const handleEdgeEnter: EdgeMouseHandler = useCallback((_, edge) => {
        setHoveredEdgeId(edge.id);
    }, []);

    const handleEdgeLeave: EdgeMouseHandler = useCallback(() => {
        setHoveredEdgeId(null);
    }, []);

    const handleToggleLabel = useCallback((label: string) => {
        setHiddenLabels((previous) => {
            const next = new Set(previous);

            if (next.has(label)) {
                next.delete(label);
            } else {
                next.add(label);
            }

            return next;
        });
    }, []);

    // Number of entities currently drawn vs. the label-visible total, for the
    // timeline "Visible n/total" readout.
    const labelVisibleTotal = useMemo(
        () => (graph ? graph.nodes.filter((n) => !hiddenLabels.has(primaryLabel(n.labels))).length : 0),
        [graph, hiddenLabels],
    );
    const revealedCount = layout.rfNodes.length;

    // Replay animation: advance the cursor until it reaches the end. The stop
    // happens inside the timer callback (not the effect body) to keep the
    // render free of synchronous cascading updates.
    const revealFracRef = useRef(revealFrac);

    useEffect(() => {
        if (!playing || !timeDomain) {
            return;
        }

        const id = setInterval(() => {
            const next = Math.min(1, revealFracRef.current + 0.02);

            revealFracRef.current = next;
            setRevealFrac(next);

            if (next >= 1) {
                setPlaying(false);
                clearInterval(id);
            }
        }, 120);

        return () => clearInterval(id);
    }, [playing, timeDomain]);

    const handleToggleReplay = useCallback(() => {
        setRevealActive(true);

        // Starting a fresh replay from the end rewinds to the beginning.
        if (!playing && revealFracRef.current >= 1) {
            revealFracRef.current = 0;
            setRevealFrac(0);
        }

        setPlaying((p) => !p);
    }, [playing]);

    const handleScrub = useCallback((event: ChangeEvent<HTMLInputElement>) => {
        const value = Number(event.target.value) / 100;

        setRevealActive(true);
        setPlaying(false);
        revealFracRef.current = value;
        setRevealFrac(value);
    }, []);

    const timelineBar = timeDomain ? (
        <div className="mt-2 flex items-center gap-3 rounded-lg border border-white/10 bg-black/40 px-3 py-2">
            <Button
                className="h-7 w-7 p-0"
                onClick={handleToggleReplay}
                size="sm"
                variant="ghost"
            >
                {playing ? <Pause className="size-3.5" /> : <Play className="size-3.5" />}
                <span className="sr-only">{playing ? 'Pause replay' : 'Play replay'}</span>
            </Button>
            <div className="flex min-w-0 flex-1 flex-col gap-1">
                <input
                    aria-label="Replay timeline"
                    className="w-full accent-sky-500"
                    max={100}
                    min={0}
                    onChange={handleScrub}
                    type="range"
                    value={Math.round(revealFrac * 100)}
                />
                <div className="text-muted-foreground flex justify-between text-[10px]">
                    <span>{fmtClock(timeDomain.min)}</span>
                    <span>{fmtClock(timeDomain.max)}</span>
                </div>
            </div>
            <div className="text-right text-xs">
                <div className="font-mono font-semibold">{fmtClock(revealedBefore ?? timeDomain.max)}</div>
                <div className="text-muted-foreground text-[10px]">
                    {fmtDate(revealedBefore ?? timeDomain.max)} ·{' '}
                    {fmtOffset((revealedBefore ?? timeDomain.max) - timeDomain.min)}
                </div>
            </div>
            <div className="text-right text-xs">
                <div className="text-muted-foreground text-[10px] uppercase">Visible</div>
                <div className="font-mono font-semibold">
                    {revealedCount}/{labelVisibleTotal}
                </div>
            </div>
        </div>
    ) : null;

    const flowElement = (
        <ReactFlow
            colorMode="dark"
            edges={rfEdges}
            fitView
            // key forces a fresh fitView when the view or fullscreen changes.
            key={`${view}-${isFullscreen}`}
            minZoom={0.05}
            nodes={rfNodes}
            nodesConnectable={false}
            nodesDraggable={view === AttackGraphView.Full}
            nodeTypes={nodeTypes}
            onEdgeMouseEnter={handleEdgeEnter}
            onEdgeMouseLeave={handleEdgeLeave}
            onNodeClick={handleNodeClick}
            proOptions={{ hideAttribution: true }}
        >
            <Background
                color="#0d0d17"
                gap={999}
                variant={BackgroundVariant.Dots}
            />
            <Controls
                position="bottom-right"
                showInteractive={false}
            />
        </ReactFlow>
    );

    const canvas = (extraClassName: string) => (
        <div className={cn('relative w-full overflow-hidden rounded-lg bg-black', extraClassName)}>
            {flowElement}
            <EdgeHoverCard hovered={hovered} />
        </div>
    );

    const graphContainer = (
        <>
            <GraphLegend
                hidden={hiddenLabels}
                labels={presentLabels}
                onToggle={handleToggleLabel}
            />
            {canvas('h-[420px]')}
            {timelineBar}
            <p className="text-muted-foreground mt-2 text-xs">
                Hover a link to see the action; click a node for its full details.
            </p>
        </>
    );

    const fullscreenContainer = (
        <div className="flex h-[calc(95vh-3.5rem)] flex-col gap-2 px-4 pb-3">
            <GraphLegend
                hidden={hiddenLabels}
                labels={presentLabels}
                onToggle={handleToggleLabel}
            />
            {canvas('min-h-0 flex-1')}
            {timelineBar}
        </div>
    );

    return (
        <Card>
            <CardHeader className="flex flex-row items-center justify-between gap-2">
                <div className="flex flex-col gap-1">
                    <CardTitle className="flex items-center gap-2">
                        {viewTitles[baseView]}
                        {graph?.truncated ? (
                            <span className="text-muted-foreground text-xs font-normal">
                                (truncated: {graph.nodes.length}/{graph.totalNodes} nodes)
                            </span>
                        ) : null}
                    </CardTitle>
                    <CardDescription>{viewDescriptions[baseView]}</CardDescription>
                </div>
                <div className="flex items-center gap-2">
                    {allowFullToggle ? (
                        <div className="bg-muted inline-flex rounded-md p-0.5">
                            <Button
                                className={cn('h-7 px-3 py-1 text-xs', !showFull && 'shadow-sm')}
                                onClick={() => setShowFull(false)}
                                size="sm"
                                variant={!showFull ? 'secondary' : 'ghost'}
                            >
                                {viewShortLabels[baseView]}
                            </Button>
                            <Button
                                className={cn('h-7 px-3 py-1 text-xs', showFull && 'shadow-sm')}
                                onClick={() => setShowFull(true)}
                                size="sm"
                                variant={showFull ? 'secondary' : 'ghost'}
                            >
                                Full
                            </Button>
                        </div>
                    ) : null}
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

            <Dialog
                onOpenChange={setIsFullscreen}
                open={isFullscreen}
            >
                <DialogContent className="h-[95vh] max-w-[95vw] gap-0 p-0 sm:max-w-[95vw]">
                    <DialogHeader className="flex flex-row items-center justify-between border-b px-4 py-3 sm:flex-row">
                        <div>
                            <DialogTitle className="text-base">{viewTitles[view]}</DialogTitle>
                            <DialogDescription className="sr-only">
                                Fullscreen knowledge-graph view of the attack chain
                            </DialogDescription>
                        </div>
                        <div className="flex items-center gap-2">
                            {allowFullToggle ? (
                                <div className="bg-muted inline-flex rounded-md p-0.5">
                                    <Button
                                        className={cn('h-7 px-3 py-1 text-xs', !showFull && 'shadow-sm')}
                                        onClick={() => setShowFull(false)}
                                        size="sm"
                                        variant={!showFull ? 'secondary' : 'ghost'}
                                    >
                                        {viewShortLabels[baseView]}
                                    </Button>
                                    <Button
                                        className={cn('h-7 px-3 py-1 text-xs', showFull && 'shadow-sm')}
                                        onClick={() => setShowFull(true)}
                                        size="sm"
                                        variant={showFull ? 'secondary' : 'ghost'}
                                    >
                                        Full
                                    </Button>
                                </div>
                            ) : null}
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

            {selectedNode ? (
                <FlowGraphNodePanel
                    edges={graph?.edges ?? []}
                    flowId={flowId}
                    node={selectedNode}
                    nodes={graph?.nodes ?? []}
                    onClose={() => setSelectedNodeId(null)}
                    onNavigate={(uuid) => setSelectedNodeId(uuid)}
                />
            ) : null}
        </Card>
    );
}

// toFlowEdges routes every edge as an orthogonal (smoothstep) path whose
// vertical run sits in the gutter right after the source column. Edges leaving
// the same column therefore share a single trunk line (their vertical segments
// overlap and read as one line, git-tree style) and branch out into their
// targets. Because the vertical run stays in the inter-column gutter, edges
// never cross a node box. Edges whose endpoint was filtered out are dropped.
export function toFlowEdges(edges: AttackGraphEdgeFragmentFragment[], options: FlowEdgeOptions): Edge[] {
    const { hoveredEdgeId, positions, selectedNodeId } = options;

    // Nudge truly parallel edges (same source -> same target) apart so their
    // labels do not stack. Edges to different targets keep the shared trunk.
    const pairCounter = new Map<string, number>();
    const out: RoutedEdge[] = [];

    for (const e of edges) {
        const src = positions.get(e.sourceUUID);
        const tgt = positions.get(e.targetUUID);

        if (!src || !tgt) {
            continue;
        }

        const isAttack = isAttackEdge(e.type);
        const isActive =
            hoveredEdgeId !== null
                ? e.uuid === hoveredEdgeId
                : selectedNodeId !== null && (e.sourceUUID === selectedNodeId || e.targetUUID === selectedNodeId);
        const dimmed =
            (hoveredEdgeId !== null && e.uuid !== hoveredEdgeId) ||
            (hoveredEdgeId === null && selectedNodeId !== null && !isActive);

        const edgeColor = dimmed ? '#374151' : isAttack ? '#f97316' : '#9ca3af';
        const thick = isActive && (hoveredEdgeId !== null || selectedNodeId !== null);

        const key = `${e.sourceUUID}->${e.targetUUID}`;
        const idx = pairCounter.get(key) ?? 0;
        pairCounter.set(key, idx + 1);

        let pathOptions: { borderRadius: number; centerX?: number } = { borderRadius: 8 };

        if (tgt.col > src.col) {
            const gutter = src.x + NODE_WIDTH + (COLUMN_WIDTH - NODE_WIDTH) / 2;
            pathOptions = { borderRadius: 8, centerX: gutter + idx * 14 };
        }

        out.push({
            animated: isAttack && isActive,
            id: e.uuid,
            label: e.type,
            labelBgBorderRadius: 4,
            labelBgPadding: [6, 3] as [number, number],
            labelBgStyle: { fill: '#151523', stroke: '#2a2a40', strokeWidth: 1 },
            labelShowBg: true,
            labelStyle: {
                fill: dimmed ? '#4b5563' : '#c4c4d4',
                fontSize: 10,
                fontWeight: 600,
            },
            markerEnd: {
                color: edgeColor,
                height: 18,
                type: MarkerType.ArrowClosed,
                width: 18,
            },
            pathOptions,
            source: e.sourceUUID,
            style: {
                opacity: dimmed ? 0.15 : 1,
                stroke: edgeColor,
                strokeWidth: thick ? (isAttack ? 3 : 2.5) : isAttack ? 2 : 1.5,
            },
            target: e.targetUUID,
            type: 'smoothstep',
        });
    }

    return out;
}

// ─── Edge hover detail ────────────────────────────────────────────────────
function EdgeHoverCard({
    hovered,
}: {
    hovered: null | {
        edge: AttackGraphEdgeFragmentFragment;
        source: AttackGraphNodeFragmentFragment | null;
        target: AttackGraphNodeFragmentFragment | null;
    };
}) {
    if (!hovered) {
        return null;
    }

    const { edge, source, target } = hovered;
    const sourceType = source ? primaryLabel(source.labels) : 'Entity';
    const c = colorForLabel(sourceType);
    const sourceName = source?.name || sourceType;
    const targetName = target?.name || (target ? primaryLabel(target.labels) : 'Entity');

    return (
        <div
            className="pointer-events-none absolute top-3 left-3 z-10 max-w-sm rounded-lg border-2 px-3 py-2 shadow-lg"
            style={{ background: '#0b0b14ee', borderColor: c.border, color: c.text }}
        >
            <div className="flex items-center gap-2 text-xs">
                <span
                    className="inline-block size-2.5 rounded-full"
                    style={{ background: c.dot }}
                />
                <span className="font-bold tracking-wider uppercase opacity-70">{sourceType}</span>
                <span className="truncate font-semibold text-gray-200">{sourceName}</span>
            </div>
            <div className="mt-1 text-xs text-gray-400">
                <span className="font-semibold text-gray-200">{edge.type}</span> → {targetName}
            </div>
            {edge.fact ? <div className="mt-1 line-clamp-3 text-xs text-gray-400 italic">{edge.fact}</div> : null}
        </div>
    );
}

// ─── Detail panel ────────────────────────────────────────────────────────
// ─── Replay timeline formatting ──────────────────────────────────────────
function fmtClock(t: number): string {
    return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function fmtDate(t: number): string {
    return new Date(t).toLocaleDateString([], { day: 'numeric', month: 'short' });
}

function fmtOffset(ms: number): string {
    const totalSeconds = Math.max(0, Math.round(ms / 1000));
    const minutes = Math.floor(totalSeconds / 60);

    return `+${minutes}m ${totalSeconds % 60}s`;
}

// ─── Legend (clickable type filter) ───────────────────────────────────────
function GraphLegend({
    hidden,
    labels,
    onToggle,
}: {
    hidden: Set<string>;
    labels: string[];
    onToggle: (label: string) => void;
}) {
    if (labels.length === 0) {
        return null;
    }

    return (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5 px-1 pb-2">
            {labels.map((label) => {
                const c = colorForLabel(label);
                const isHidden = hidden.has(label);

                return (
                    <button
                        className={cn(
                            'flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs transition-opacity',
                            isHidden ? 'border-transparent opacity-40' : 'border-white/10',
                        )}
                        key={label}
                        onClick={() => onToggle(label)}
                        style={{ background: isHidden ? 'transparent' : c.bg }}
                        type="button"
                    >
                        <span
                            className="inline-block size-2.5 rounded-full border"
                            style={{ background: c.dot, borderColor: c.border }}
                        />
                        <span className="font-medium text-gray-300">{label}</span>
                    </button>
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

function truncate(s: string, n: number): string {
    if (s.length <= n) {
        return s;
    }

    return s.slice(0, n - 1) + '…';
}
