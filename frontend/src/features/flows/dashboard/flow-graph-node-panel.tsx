import { useQuery } from '@apollo/client/react';
import { Fragment, type ReactNode, useMemo } from 'react';

import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { colorForLabel, primaryLabel } from '@/features/flows/dashboard/flow-graph-labels';
import {
    type AttackGraphEdgeFragmentFragment,
    type AttackGraphNodeFragmentFragment,
    FlowGraphNodePropertiesDocument,
} from '@/graphql/types';

interface FlowGraphNodePanelProps {
    edges: AttackGraphEdgeFragmentFragment[];
    flowId: string;
    node: AttackGraphNodeFragmentFragment;
    nodes: AttackGraphNodeFragmentFragment[];
    onClose: () => void;
    onNavigate: (uuid: string) => void;
}

// FlowGraphNodePanel is the right-hand detail drawer opened when a graph node is
// clicked. It shows the node description, its Graphiti attributes (fetched on
// demand) and its navigable incoming/outgoing relations, mirroring the EE
// node-details panel. Clicking a relation selects that neighbour in place.
export function FlowGraphNodePanel({ edges, flowId, node, nodes, onClose, onNavigate }: FlowGraphNodePanelProps) {
    const { data, loading } = useQuery(FlowGraphNodePropertiesDocument, {
        fetchPolicy: 'cache-and-network',
        variables: { flowId, uuid: node.uuid },
    });

    const properties = data?.flowGraphNodeProperties ?? [];
    const nodeIndex = useMemo(() => new Map(nodes.map((n) => [n.uuid, n])), [nodes]);

    const outgoing = useMemo(() => edges.filter((e) => e.sourceUUID === node.uuid), [edges, node.uuid]);
    const incoming = useMemo(() => edges.filter((e) => e.targetUUID === node.uuid), [edges, node.uuid]);

    const type = primaryLabel(node.labels);
    const c = colorForLabel(type);

    return (
        <Sheet
            modal={false}
            onOpenChange={(open) => {
                if (!open) {
                    onClose();
                }
            }}
            open
        >
            <SheetContent
                className="flex w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-md"
                overlay={false}
            >
                <SheetHeader className="border-b px-4 py-3 text-left">
                    <div className="flex items-center gap-2">
                        <span
                            className="inline-block size-2.5 rounded-full"
                            style={{ background: c.dot }}
                        />
                        <span className="text-xs font-bold tracking-wider uppercase opacity-70">{type}</span>
                    </div>
                    <SheetTitle className="text-base">{node.name || type}</SheetTitle>
                    {node.summary ? <SheetDescription>{node.summary}</SheetDescription> : null}
                </SheetHeader>

                <div className="min-h-0 flex-1 overflow-auto px-4 py-3">
                    <SectionTitle>Properties</SectionTitle>
                    {loading && properties.length === 0 ? (
                        <Skeleton className="h-20 w-full" />
                    ) : properties.length === 0 ? (
                        <p className="text-muted-foreground text-xs">No attributes extracted.</p>
                    ) : (
                        <dl className="grid grid-cols-[minmax(0,8rem)_1fr] gap-x-3 gap-y-1 text-xs">
                            {properties.map((p) => (
                                <Fragment key={p.key}>
                                    <dt className="text-muted-foreground truncate">{p.key}</dt>
                                    <dd className="break-words text-gray-200">{p.value}</dd>
                                </Fragment>
                            ))}
                        </dl>
                    )}

                    <Relations
                        direction="in"
                        edges={incoming}
                        nodeIndex={nodeIndex}
                        onNavigate={onNavigate}
                        title={`Incoming (${incoming.length})`}
                    />
                    <Relations
                        direction="out"
                        edges={outgoing}
                        nodeIndex={nodeIndex}
                        onNavigate={onNavigate}
                        title={`Outgoing (${outgoing.length})`}
                    />
                </div>
            </SheetContent>
        </Sheet>
    );
}

function Relations({
    direction,
    edges,
    nodeIndex,
    onNavigate,
    title,
}: {
    direction: 'in' | 'out';
    edges: AttackGraphEdgeFragmentFragment[];
    nodeIndex: Map<string, AttackGraphNodeFragmentFragment>;
    onNavigate: (uuid: string) => void;
    title: string;
}) {
    if (edges.length === 0) {
        return null;
    }

    return (
        <div>
            <SectionTitle>{title}</SectionTitle>
            <ul className="flex flex-col gap-0.5">
                {edges.map((e) => {
                    const neighborUUID = direction === 'in' ? e.sourceUUID : e.targetUUID;
                    const neighbor = nodeIndex.get(neighborUUID);
                    const neighborType = neighbor ? primaryLabel(neighbor.labels) : 'Entity';
                    const nc = colorForLabel(neighborType);

                    return (
                        <li key={e.uuid}>
                            <button
                                className="hover:bg-accent/50 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs"
                                onClick={() => onNavigate(neighborUUID)}
                                type="button"
                            >
                                <span
                                    className="inline-block size-2 shrink-0 rounded-full"
                                    style={{ background: nc.dot }}
                                />
                                <span className="text-muted-foreground shrink-0 font-mono text-[10px] uppercase">
                                    {e.type}
                                </span>
                                <span className="truncate font-medium">{neighbor?.name || neighborType}</span>
                            </button>
                        </li>
                    );
                })}
            </ul>
        </div>
    );
}

function SectionTitle({ children }: { children: ReactNode }) {
    return (
        <div className="text-muted-foreground mt-4 mb-1.5 text-[11px] font-semibold tracking-wider uppercase">
            {children}
        </div>
    );
}
