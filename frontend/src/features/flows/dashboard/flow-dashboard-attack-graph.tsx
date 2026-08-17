import { useQuery } from '@apollo/client/react';
import { Background, BackgroundVariant, Controls, MiniMap, ReactFlow } from '@xyflow/react';
import { Expand, Loader2, Maximize2, Shrink } from 'lucide-react';
import { useMemo, useState } from 'react';

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

// labelColumnOrder fixes the horizontal reading order of the attack chain:
// Host → Port → Service → Vulnerability / Misconfiguration → ValidAccess →
// Account / Credential. Labels outside the list land in a trailing column,
// ordered alphabetically.
const labelColumnOrder: string[] = [
    'Host',
    'Port',
    'Service',
    'Endpoint',
    'WebApp',
    'Vhost',
    'Vulnerability',
    'Misconfiguration',
    'Capability',
    'Attempt',
    'PrivChange',
    'ValidAccess',
    'Account',
    'Credential',
    'Artifact',
    'Evidence',
    'Agent',
    'Episodic',
];

// labelStyles maps an entity label to a Tailwind background/border/text color
// so the attack chain reads at a glance. Unknown labels fall back to muted.
const labelStyles: Record<string, string> = {
    Account: 'bg-cyan-50 border-cyan-300 text-cyan-900 dark:bg-cyan-950 dark:border-cyan-800 dark:text-cyan-100',
    Agent: 'bg-slate-50 border-slate-300 text-slate-900 dark:bg-slate-900 dark:border-slate-700 dark:text-slate-100',
    Artifact: 'bg-stone-50 border-stone-300 text-stone-900 dark:bg-stone-900 dark:border-stone-700 dark:text-stone-100',
    Attempt: 'bg-yellow-50 border-yellow-300 text-yellow-900 dark:bg-yellow-950 dark:border-yellow-800 dark:text-yellow-100',
    Capability: 'bg-lime-50 border-lime-300 text-lime-900 dark:bg-lime-950 dark:border-lime-800 dark:text-lime-100',
    Credential: 'bg-amber-50 border-amber-300 text-amber-900 dark:bg-amber-950 dark:border-amber-800 dark:text-amber-100',
    Endpoint: 'bg-blue-50 border-blue-300 text-blue-900 dark:bg-blue-950 dark:border-blue-800 dark:text-blue-100',
    Episodic: 'bg-muted border-border text-muted-foreground',
    Evidence: 'bg-teal-50 border-teal-300 text-teal-900 dark:bg-teal-950 dark:border-teal-800 dark:text-teal-100',
    Host: 'bg-sky-50 border-sky-300 text-sky-900 dark:bg-sky-950 dark:border-sky-800 dark:text-sky-100',
    Misconfiguration: 'bg-orange-50 border-orange-300 text-orange-900 dark:bg-orange-950 dark:border-orange-800 dark:text-orange-100',
    Port: 'bg-indigo-50 border-indigo-300 text-indigo-900 dark:bg-indigo-950 dark:border-indigo-800 dark:text-indigo-100',
    PrivChange: 'bg-rose-50 border-rose-300 text-rose-900 dark:bg-rose-950 dark:border-rose-800 dark:text-rose-100',
    Service: 'bg-violet-50 border-violet-300 text-violet-900 dark:bg-violet-950 dark:border-violet-800 dark:text-violet-100',
    ValidAccess: 'bg-emerald-50 border-emerald-300 text-emerald-900 dark:bg-emerald-950 dark:border-emerald-800 dark:text-emerald-100',
    Vhost: 'bg-pink-50 border-pink-300 text-pink-900 dark:bg-pink-950 dark:border-pink-800 dark:text-pink-100',
    Vulnerability: 'bg-red-50 border-red-300 text-red-900 dark:bg-red-950 dark:border-red-800 dark:text-red-100',
    WebApp: 'bg-fuchsia-50 border-fuchsia-300 text-fuchsia-900 dark:bg-fuchsia-950 dark:border-fuchsia-800 dark:text-fuchsia-100',
};

function columnForLabel(label: string): number {
    const idx = labelColumnOrder.indexOf(label);

    if (idx >= 0) {return idx;}

    // Unknown labels: append after the known columns, ordered alphabetically.
    return labelColumnOrder.length + label.charCodeAt(0) % 16;
}

function primaryLabel(labels: string[]): string {
    for (const l of labels) {
        if (l && l !== 'Entity') {return l;}
    }

    return labels[0] ?? 'Entity';
}

const COLUMN_WIDTH = 240;
const ROW_HEIGHT = 72;
const TOP_PADDING = 40;

interface FlowDashboardAttackGraphProps {
    flowId: string;
    pollInterval?: number;
}

export function FlowDashboardAttackGraph({ flowId, pollInterval = 10000 }: FlowDashboardAttackGraphProps) {
    const [view, setView] = useState<AttackGraphView>(AttackGraphView.Main);
    const [isFullscreen, setIsFullscreen] = useState(false);

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

    const flowElement = (
        <ReactFlow
            edges={rfEdges}
            fitView
            // key forces a fresh fitView when the view (MAIN/FULL) changes.
            key={`${view}-${isFullscreen}`}
            minZoom={0.05}
            nodes={rfNodes}
            nodesConnectable={false}
            proOptions={{ hideAttribution: true }}
        >
            <Background color="hsl(var(--border))" gap={20} variant={BackgroundVariant.Dots} />
            <Controls position="bottom-right" showInteractive={false} />
            <MiniMap
                className="bg-background/80"
                nodeColor={(n) => (typeof n.className === 'string' && n.className.includes('red-') ? '#fca5a5' : '#cbd5e1')}
                pannable
                zoomable
            />
        </ReactFlow>
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
                    <div className="h-[420px] w-full overflow-hidden rounded-md border">{flowElement}</div>
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
                    <div className="relative h-[calc(95vh-3.5rem)] w-full">{flowElement}</div>
                </DialogContent>
            </Dialog>
        </Card>
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

// layoutNodes assigns a deterministic column/row position per node so the
// attack chain reads left → right without pulling in a layout engine. The
// user can still drag nodes and zoom/pan freely.
function layoutNodes(
    nodes: AttackGraphNodeFragmentFragment[],
): Array<{ className: string; data: { label: string }; id: string; position: { x: number; y: number }; }> {
    const byColumn = new Map<number, AttackGraphNodeFragmentFragment[]>();

    for (const n of nodes) {
        const col = columnForLabel(primaryLabel(n.labels));
        const arr = byColumn.get(col) ?? [];
        arr.push(n);
        byColumn.set(col, arr);
    }

    const cols = [...byColumn.keys()].sort((a, b) => a - b);

    const out: Array<{
        className: string;
        data: { label: string };
        id: string;
        position: { x: number; y: number };
    }> = [];
    cols.forEach((col, colIdx) => {
        const items = byColumn.get(col)!;
        items.forEach((n, rowIdx) => {
            const label = primaryLabel(n.labels);
            const style = labelStyles[label] ?? 'bg-muted border-border text-muted-foreground';
            const subtitle = n.summary ? truncate(n.summary, 60) : '';
            const labelLine = n.name ? truncate(n.name, 28) : label;
            out.push({
                className: cn(
                    'whitespace-pre-wrap rounded-md border px-3 py-2 text-xs font-medium shadow-sm',
                    style,
                ),
                data: { label: `${labelLine}${subtitle ? `\n${subtitle}` : ''}` },
                id: n.uuid,
                position: {
                    x: colIdx * COLUMN_WIDTH,
                    y: TOP_PADDING + rowIdx * ROW_HEIGHT,
                },
            });
        });
    });

    return out;
}

function toFlowEdges(
    edges: AttackGraphEdgeFragmentFragment[],
): Array<{ animated: boolean; id: string; label?: string; source: string; target: string; }> {
    return edges.map((e) => ({
        animated: isAttackEdge(e.type),
        id: e.uuid,
        // Show the relation type (shorter than the fact prose) so the graph
        // stays readable at default zoom; hover tooltips are overkill here.
        label: e.type,
        source: e.sourceUUID,
        target: e.targetUUID,
    }));
}

function truncate(s: string, n: number): string {
    if (s.length <= n) {return s;}

    return s.slice(0, n - 1) + '…';
}