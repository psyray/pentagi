import { useQuery } from '@apollo/client/react';
import { X } from 'lucide-react';
import { Fragment, type ReactNode, useMemo } from 'react';

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

// FlowGraphNodePanel is the detail drawer shown over the graph when a node is
// clicked. It is a plain absolutely-positioned panel (not a Radix dialog) so it
// can live inside the same container as the graph: inside the fullscreen dialog
// it stays within that dialog's DOM subtree, so closing it never dismisses the
// fullscreen view. It shows the node description, its Graphiti attributes
// (fetched on demand) and its navigable incoming/outgoing relations.
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
        <div
            className="bg-background absolute inset-y-0 right-0 z-20 flex w-full max-w-md flex-col overflow-hidden border-l shadow-xl"
            data-testid="flow-node-panel"
        >
            <div className="flex items-start justify-between gap-2 border-b px-4 py-3">
                <div className="flex min-w-0 flex-col gap-1">
                    <div className="flex items-center gap-2">
                        <span
                            className="inline-block size-2.5 shrink-0 rounded-full"
                            style={{ background: c.dot }}
                        />
                        <span className="text-muted-foreground text-xs font-bold tracking-wider uppercase">{type}</span>
                    </div>
                    <div className="truncate text-base font-semibold">{node.name || type}</div>
                </div>
                <button
                    aria-label="Close node details"
                    className="hover:bg-accent rounded-sm p-1 opacity-70 transition-opacity hover:opacity-100"
                    onClick={onClose}
                    type="button"
                >
                    <X className="size-4" />
                </button>
            </div>

            {node.summary ? <p className="text-muted-foreground border-b px-4 py-3 text-sm">{node.summary}</p> : null}

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
        </div>
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
