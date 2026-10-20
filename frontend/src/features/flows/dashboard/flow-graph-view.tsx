import { useQuery } from '@apollo/client/react';
import {
    Background,
    BackgroundVariant,
    BaseEdge,
    Controls,
    type Edge,
    EdgeLabelRenderer,
    type EdgeMouseHandler,
    type EdgeProps,
    getBezierPath,
    Handle,
    MarkerType,
    type Node,
    type NodeMouseHandler,
    Position,
    ReactFlow,
} from '@xyflow/react';
import { Expand, Loader2, Maximize2, Pause, Play, Shrink } from 'lucide-react';
import {
    type ChangeEvent,
    type MouseEvent as ReactMouseEvent,
    useCallback,
    useEffect,
    useMemo,
    useRef,
    useState,
} from 'react';

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
// WebApp and Vhost share column 5. Vhost/VHost are Graphiti 0.9 legacy labels
// (1.1 folded the vhost into Host.aliases and WebApp.vhost). Labels not in the
// map land in a trailing column, ordered alphabetically.
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
    VHost: 5, // legacy Graphiti 0.9 spelling
    Vulnerability: 6,
    WebApp: 5,
};

const maxMappedColumn = 9;

const COLUMN_WIDTH = 340;
const NODE_WIDTH = 220;
const ROW_HEIGHT = 110;
const TOP_PADDING = 60;
// Width of the empty lane between two adjacent columns, and a rough node height
// used to place the top/bottom routing buses around the whole graph.
const GUTTER_GAP = COLUMN_WIDTH - NODE_WIDTH;
const NODE_HEIGHT_ESTIMATE = 84;
const BUS_MARGIN = 50;
// Delay before a node hover applies its highlight (see handleNodeEnter).
const HOVER_DELAY_MS = 160;

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

// BoundaryNode is an invisible 1x1 node placed at the corners of the content
// box (including the routing buses, which lie outside the node bounding box).
// ReactFlow's fitView only measures nodes, so these markers pull the buses into
// the fitted viewport.
function BoundaryNode() {
    return <span style={{ display: 'none' }} />;
}

function EntityNode({ data }: { data: EntityNodeData }) {
    const c = colorForLabel(data.type);

    return (
        <div
            className="w-full rounded-xl border-2 px-3 py-2 backdrop-blur-sm"
            data-kg-node="root"
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

const nodeTypes = { boundary: BoundaryNode, entityNode: EntityNode };
const edgeTypes = { routed: RoutedEdge };

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
    // Vertical extent of the graph, used to route long edges on a top/bottom bus.
    route: RouteGeometry;
}

interface NodePosition {
    col: number;
    x: number;
    y: number;
}

interface RouteGeometry {
    busBottomY: number;
    busTopY: number;
    centerY: number;
}

const emptyLayout: Layout = {
    positions: new Map(),
    rfNodes: [],
    route: { busBottomY: 0, busTopY: 0, centerY: 0 },
};

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
    let maxRows = 1;

    cols.forEach((col, colIdx) => {
        const items = byColumn.get(col)!;

        maxRows = Math.max(maxRows, items.length);
        items.forEach((n, rowIdx) => {
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

    // Graph vertical span, so long edges can be routed above or below every node.
    const minY = TOP_PADDING;
    const maxY = TOP_PADDING + (maxRows - 1) * ROW_HEIGHT + NODE_HEIGHT_ESTIMATE;

    return {
        positions,
        rfNodes,
        route: { busBottomY: maxY + BUS_MARGIN, busTopY: minY - BUS_MARGIN, centerY: (minY + maxY) / 2 },
    };
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

// ─── Related-node computation ────────────────────────────────────────────

// relatedNodes returns the set of nodes tied to root in the attack-chain sense:
// every ancestor (a node that can reach root by following edges forward) and
// every descendant (a node reachable from root). Sibling branches of an ancestor
// (for example another port of the same Host) are therefore NOT included, which
// keeps the focus on the chain that actually passes through root.
export function relatedNodes(graph: { edges: AttackGraphEdgeFragmentFragment[] }, rootUUID: string): Set<string> {
    const outAdj = new Map<string, string[]>();
    const inAdj = new Map<string, string[]>();

    const push = (m: Map<string, string[]>, key: string, value: string) => {
        const arr = m.get(key);

        if (arr) {
            arr.push(value);
        } else {
            m.set(key, [value]);
        }
    };

    for (const e of graph.edges) {
        push(outAdj, e.sourceUUID, e.targetUUID);
        push(inAdj, e.targetUUID, e.sourceUUID);
    }

    const result = new Set<string>([rootUUID]);

    for (const id of walk(rootUUID, outAdj)) {
        result.add(id);
    }

    for (const id of walk(rootUUID, inAdj)) {
        result.add(id);
    }

    return result;
}

// walk returns every node reachable from start along the adjacency map (BFS,
// each node visited once, so cycles are handled).
function walk(start: string, adj: Map<string, string[]>): Set<string> {
    const seen = new Set<string>();
    const queue = [start];

    while (queue.length > 0) {
        const current = queue.shift()!;

        for (const next of adj.get(current) ?? []) {
            if (!seen.has(next)) {
                seen.add(next);
                queue.push(next);
            }
        }
    }

    return seen;
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

// EdgeRoute describes how to draw one edge: a single gutter (adjacent columns),
// a top/bottom bus (columns far apart, or a backward edge) or a plain bezier
// fallback. gA is the gutter right after the source column, gB the gutter just
// before the target column; busY is the shared bus line used by the bus kind.
interface EdgeRoute {
    busY: number;
    gA: number;
    gB: number;
    kind: 'bezier' | 'bus' | 'gutter';
}

interface EdgeRouteData {
    [key: string]: unknown;
    route: EdgeRoute;
}

// ─── Edge routing ────────────────────────────────────────────────────────
interface FlowEdgeOptions {
    hoveredEdgeId: null | string;
    positions: Map<string, NodePosition>;
    related: null | Set<string>;
    route: RouteGeometry;
}

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
    const [hoveredNodeId, setHoveredNodeId] = useState<null | string>(null);
    const hoverTimer = useRef<null | number>(null);
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

    // Nodes to keep lit. Precedence: hovered edge, then hovered node, then the
    // clicked selection. The related set is the union of the anchor's ancestors
    // and descendants, so the whole chain through it stays lit while unrelated
    // branches (e.g. sibling ports) dim.
    const related = useMemo(() => {
        if (!graph) {
            return null;
        }

        if (hoveredEdgeId) {
            const edge = graph.edges.find((e) => e.uuid === hoveredEdgeId);

            if (edge) {
                return new Set([...relatedNodes(graph, edge.sourceUUID), ...relatedNodes(graph, edge.targetUUID)]);
            }
        }

        const anchor = hoveredNodeId ?? selectedNodeId;

        if (anchor) {
            return relatedNodes(graph, anchor);
        }

        return null;
    }, [graph, hoveredEdgeId, hoveredNodeId, selectedNodeId]);

    const dimmedUUIDs = useMemo(() => {
        if (!graph || !related) {
            return null;
        }

        const dimmed = new Set<string>();

        for (const n of graph.nodes) {
            if (!related.has(n.uuid)) {
                dimmed.add(n.uuid);
            }
        }

        return dimmed;
    }, [graph, related]);

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
        () => (graph ? buildLayout(graph.nodes, hiddenLabels, revealedBefore) : emptyLayout),
        [graph, hiddenLabels, revealedBefore],
    );

    const rfNodes = useMemo(
        () =>
            layout.rfNodes.map((node) => ({
                ...node,
                data: {
                    ...node.data,
                    // dimmedUUIDs holds the nodes to grey out, so a node is dimmed
                    // when it belongs to that set (the chain through the anchor
                    // is NOT in it and stays lit).
                    dimmed: dimmedUUIDs ? dimmedUUIDs.has(node.id) : false,
                    selected: node.id === selectedNodeId,
                },
            })),
        [layout, dimmedUUIDs, selectedNodeId],
    );

    // Invisible corner markers so fitView includes the routing buses, which lie
    // above/below the node bounding box and would otherwise be cropped.
    const boundaryNodes = useMemo<Node[]>(() => {
        if (layout.positions.size === 0 || layout.rfNodes.length === 0) {
            return [];
        }

        let maxX = -Infinity;
        let maxY = -Infinity;
        let minX = Infinity;
        let minY = Infinity;

        for (const p of layout.positions.values()) {
            minX = Math.min(minX, p.x);
            maxX = Math.max(maxX, p.x + NODE_WIDTH);
            minY = Math.min(minY, p.y);
            maxY = Math.max(maxY, p.y + NODE_HEIGHT_ESTIMATE);
        }

        minY = Math.min(minY, layout.route.busTopY);
        maxY = Math.max(maxY, layout.route.busBottomY);

        return [
            {
                data: {},
                draggable: false,
                id: '__boundary-top-left',
                position: { x: minX, y: minY },
                selectable: false,
                style: { height: 1, width: 1 },
                type: 'boundary',
            },
            {
                data: {},
                draggable: false,
                id: '__boundary-bottom-right',
                position: { x: maxX, y: maxY },
                selectable: false,
                style: { height: 1, width: 1 },
                type: 'boundary',
            },
        ];
    }, [layout]);

    const rfEdges = useMemo(
        () =>
            graph
                ? toFlowEdges(graph.edges, {
                      hoveredEdgeId,
                      positions: layout.positions,
                      related,
                      route: layout.route,
                  })
                : [],
        [graph, layout.positions, layout.route, hoveredEdgeId, related],
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
        // Cancel a pending hover highlight so a click that follows a hover is not
        // racing a node re-render (which would remount the node between mousedown
        // and mouseup and swallow the click).
        if (hoverTimer.current !== null) {
            window.clearTimeout(hoverTimer.current);
            hoverTimer.current = null;
        }

        setSelectedNodeId(node.id);
    }, []);

    const handleNodeEnter: NodeMouseHandler = useCallback((_, node) => {
        // Defer the hover highlight: applying it synchronously re-renders the
        // nodes, and ReactFlow then swaps the node element mid-click, cancelling
        // the click. A short delay lets a click fire first; the timer is cleared
        // on click/leave.
        if (hoverTimer.current !== null) {
            window.clearTimeout(hoverTimer.current);
        }

        hoverTimer.current = window.setTimeout(() => {
            hoverTimer.current = null;
            setHoveredNodeId(node.id);
            setHoveredEdgeId(null);
        }, HOVER_DELAY_MS);
    }, []);

    const handleNodeLeave: NodeMouseHandler = useCallback(() => {
        if (hoverTimer.current !== null) {
            window.clearTimeout(hoverTimer.current);
            hoverTimer.current = null;
        }

        setHoveredNodeId(null);
    }, []);

    const handleEdgeEnter: EdgeMouseHandler = useCallback((_, edge) => {
        setHoveredEdgeId(edge.id);
        setHoveredNodeId(null);
    }, []);

    const handleEdgeLeave: EdgeMouseHandler = useCallback(() => {
        setHoveredEdgeId(null);
    }, []);

    // Clicking the empty pane clears the selection, which closes the detail
    // panel and returns to the fully-lit general view. ReactFlow binds
    // onPaneClick to the container that also wraps the nodes and edges, so the
    // event bubbles up from them too: ignore those and only react to a genuine
    // background click.
    const handlePaneClick = useCallback((event: ReactMouseEvent) => {
        const target = event.target as HTMLElement | null;

        if (target?.closest('.react-flow__node') || target?.closest('.react-flow__edge')) {
            return;
        }

        setSelectedNodeId(null);
    }, []);

    // Escape clears the current selection (same as clicking the empty pane).
    useEffect(() => {
        if (!selectedNodeId) {
            return;
        }

        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                setSelectedNodeId(null);
            }
        };

        window.addEventListener('keydown', onKeyDown);

        return () => window.removeEventListener('keydown', onKeyDown);
    }, [selectedNodeId]);

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

    const allNodes = useMemo(() => [...rfNodes, ...boundaryNodes], [rfNodes, boundaryNodes]);

    const flowElement = (
        <ReactFlow
            colorMode="dark"
            edges={rfEdges}
            edgeTypes={edgeTypes}
            fitView
            fitViewOptions={{ padding: 0.08 }}
            // key forces a fresh fitView when the view or fullscreen changes.
            key={`${view}-${isFullscreen}`}
            minZoom={0.05}
            nodes={allNodes}
            nodesConnectable={false}
            nodesDraggable={view === AttackGraphView.Full}
            nodeTypes={nodeTypes}
            onEdgeMouseEnter={handleEdgeEnter}
            onEdgeMouseLeave={handleEdgeLeave}
            onNodeClick={handleNodeClick}
            onNodeMouseEnter={handleNodeEnter}
            onNodeMouseLeave={handleNodeLeave}
            onPaneClick={handlePaneClick}
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

    const canvas = (extraClassName: string, withPanel: boolean) => (
        <div className={cn('relative w-full overflow-hidden rounded-lg bg-black', extraClassName)}>
            {flowElement}
            <EdgeHoverCard hovered={hovered} />
            {withPanel && selectedNode ? (
                <FlowGraphNodePanel
                    edges={graph?.edges ?? []}
                    flowId={flowId}
                    node={selectedNode}
                    nodes={graph?.nodes ?? []}
                    onClose={() => setSelectedNodeId(null)}
                    onNavigate={(uuid) => setSelectedNodeId(uuid)}
                />
            ) : null}
        </div>
    );

    const graphContainer = (
        <>
            <GraphLegend
                hidden={hiddenLabels}
                labels={presentLabels}
                onToggle={handleToggleLabel}
            />
            {canvas('h-[420px]', !isFullscreen)}
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
            {canvas('min-h-0 flex-1', isFullscreen)}
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
    const { hoveredEdgeId, positions, related, route } = options;

    // Nudge truly parallel edges (same source -> same target) apart so their
    // labels do not stack. Edges to different targets keep the shared trunk.
    const pairCounter = new Map<string, number>();
    const out: Edge[] = [];

    for (const e of edges) {
        const src = positions.get(e.sourceUUID);
        const tgt = positions.get(e.targetUUID);

        if (!src || !tgt) {
            continue;
        }

        const isAttack = isAttackEdge(e.type);
        // An edge is lit when both its endpoints belong to the related set (or
        // when nothing is selected, so the whole graph stays lit); otherwise it
        // is dimmed. The hovered edge is emphasised regardless.
        const inRelated = related === null || (related.has(e.sourceUUID) && related.has(e.targetUUID));
        const dimmed = !inRelated;
        const emphasized = related !== null && inRelated;
        const thick = hoveredEdgeId === e.uuid || emphasized;
        const edgeColor = dimmed ? '#374151' : isAttack ? '#f97316' : '#9ca3af';

        const key = `${e.sourceUUID}->${e.targetUUID}`;
        const idx = pairCounter.get(key) ?? 0;
        pairCounter.set(key, idx + 1);

        // Orthogonal routing. Edges between adjacent columns run on the single
        // gutter that separates them (they share a trunk, git-tree style);
        // multi-column jumps and backward edges are routed on a top/bottom bus
        // so the long horizontal run travels above or below every node and
        // never crosses a block.
        const gA = src.x + NODE_WIDTH + GUTTER_GAP / 2;
        const gB = tgt.x - GUTTER_GAP / 2;
        const adjacent = Math.abs(gA - gB) < 1;
        const centerY = (src.y + tgt.y) / 2 + NODE_HEIGHT_ESTIMATE / 2;
        const busY = centerY < route.centerY ? route.busTopY : route.busBottomY;
        const offset = idx * 12;

        const edgeRoute: EdgeRoute = adjacent
            ? { busY, gA: gA + offset, gB: gB + offset, kind: 'gutter' }
            : { busY: busY + offset, gA: gA + offset, gB: gB + offset, kind: 'bus' };

        out.push({
            data: { route: edgeRoute } satisfies EdgeRouteData,
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
            source: e.sourceUUID,
            style: {
                opacity: dimmed ? 0.15 : 1,
                stroke: edgeColor,
                strokeWidth: thick ? (isAttack ? 3 : 2.5) : isAttack ? 2 : 1.5,
            },
            target: e.targetUUID,
            type: 'routed',
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
            className="pointer-events-none absolute bottom-3 left-3 z-10 max-w-sm rounded-lg border-2 px-3 py-2 shadow-lg"
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

// roundedPath builds an SVG path through the points, rounding each corner by up
// to `radius` (clamped to half of the shorter adjacent segment).
function roundedPath(points: [number, number][], radius = 8): string {
    if (points.length < 2) {
        return '';
    }

    const [startX, startY] = points[0]!;

    let d = `M ${startX},${startY}`;

    for (let i = 1; i < points.length - 1; i++) {
        const [px, py] = points[i - 1]!;
        const [cx, cy] = points[i]!;
        const [nx, ny] = points[i + 1]!;
        const inLen = Math.hypot(cx - px, cy - py);
        const outLen = Math.hypot(nx - cx, ny - cy);
        const r = Math.min(radius, inLen / 2, outLen / 2);
        const inX = inLen === 0 ? 0 : (cx - px) / inLen;
        const inY = inLen === 0 ? 0 : (cy - py) / inLen;
        const outX = outLen === 0 ? 0 : (nx - cx) / outLen;
        const outY = outLen === 0 ? 0 : (ny - cy) / outLen;

        d += ` L ${cx - inX * r},${cy - inY * r} Q ${cx},${cy} ${cx + outX * r},${cy + outY * r}`;
    }

    const [lx, ly] = points[points.length - 1]!;

    return `${d} L ${lx},${ly}`;
}

// ─── Custom edge component ───────────────────────────────────────────────
// RoutedEdge draws the orthogonal path computed by toFlowEdges (single gutter
// or top/bottom bus) and renders the relation label near the target entry.
function RoutedEdge({
    data,
    id,
    label,
    labelBgBorderRadius,
    labelBgStyle,
    labelStyle,
    markerEnd,
    sourceX,
    sourceY,
    style,
    targetX,
    targetY,
}: EdgeProps) {
    const route = (data as EdgeRouteData | undefined)?.route;
    const { labelX, labelY, path } = useMemo(
        () => routePath(route, sourceX, sourceY, targetX, targetY),
        [route, sourceX, sourceY, targetX, targetY],
    );

    return (
        <>
            <BaseEdge
                id={id}
                markerEnd={markerEnd}
                path={path}
                style={style}
            />
            {label ? (
                <EdgeLabelRenderer>
                    <div
                        className="nodrag nopan pointer-events-none absolute text-[10px] font-semibold"
                        style={{
                            background: (labelBgStyle?.fill as string | undefined) ?? '#151523',
                            border: `1px solid ${(labelBgStyle?.stroke as string | undefined) ?? '#2a2a40'}`,
                            borderRadius: labelBgBorderRadius ?? 4,
                            color: (labelStyle?.fill as string | undefined) ?? '#c4c4d4',
                            padding: '2px 6px',
                            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
                        }}
                    >
                        {label}
                    </div>
                </EdgeLabelRenderer>
            ) : null}
        </>
    );
}

function routePath(
    route: EdgeRoute | undefined,
    sourceX: number,
    sourceY: number,
    targetX: number,
    targetY: number,
): { labelX: number; labelY: number; path: string } {
    if (!route || route.kind === 'bezier') {
        const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, targetX, targetY });

        return { labelX, labelY, path };
    }

    const points: [number, number][] =
        route.kind === 'gutter'
            ? [
                  [sourceX, sourceY],
                  [route.gA, sourceY],
                  [route.gA, targetY],
                  [targetX, targetY],
              ]
            : [
                  [sourceX, sourceY],
                  [route.gA, sourceY],
                  [route.gA, route.busY],
                  [route.gB, route.busY],
                  [route.gB, targetY],
                  [targetX, targetY],
              ];

    const penultimate = points[points.length - 2]!;

    return {
        labelX: (penultimate[0] + targetX) / 2,
        labelY: targetY,
        path: roundedPath(points),
    };
}

function truncate(s: string, n: number): string {
    if (s.length <= n) {
        return s;
    }

    return s.slice(0, n - 1) + '…';
}
