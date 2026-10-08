import { CheckCircle2, ChevronRight, CircleDashed, CircleSlash, XCircle } from 'lucide-react';
import { useState } from 'react';

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Spinner } from '@/components/ui/spinner';
import { ReportSectionStatusType } from '@/graphql/types';
import { cn } from '@/lib/utils';

import { isSectionNotWritten, numberedTitle, type ReportOutlineNode } from './report-outline';

const StatusIcon = ({ status }: { status: ReportSectionStatusType }) => {
    switch (status) {
        case ReportSectionStatusType.Done:
            return <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />;
        case ReportSectionStatusType.Failed:
            return <XCircle className="size-3.5 shrink-0 text-red-500" />;
        case ReportSectionStatusType.NotWritten:
            return <CircleSlash className="text-muted-foreground size-3.5 shrink-0" />;
        case ReportSectionStatusType.Writing:
            return <Spinner className="text-primary size-3.5 shrink-0" />;
        default:
            return <CircleDashed className="text-muted-foreground size-3.5 shrink-0" />;
    }
};

interface FlowReportTocProps {
    activeSectionId: null | string;
    onSelectSection: (sectionId: string) => void;
    outline: readonly ReportOutlineNode[];
}

interface TocListProps {
    activeSectionId: null | string;
    depth: number;
    nodes: readonly ReportOutlineNode[];
    onSelectSection: (sectionId: string) => void;
}

function FlowReportToc({ activeSectionId, onSelectSection, outline }: FlowReportTocProps) {
    const [notWrittenOpen, setNotWrittenOpen] = useState(false);

    const notWritten = outline
        .flatMap(function flatten(node): ReportOutlineNode[] {
            return [node, ...node.children.flatMap(flatten)];
        })
        .filter((node) => isSectionNotWritten(node.section));

    return (
        <div className="flex flex-col gap-2">
            <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">Contents</div>
            <TocList
                activeSectionId={activeSectionId}
                depth={0}
                nodes={outline}
                onSelectSection={onSelectSection}
            />

            {notWritten.length > 0 && (
                <Collapsible
                    onOpenChange={setNotWrittenOpen}
                    open={notWrittenOpen}
                >
                    <CollapsibleTrigger
                        className="hover:bg-muted/60 flex w-full items-center gap-1 rounded px-2 py-1 text-left text-xs font-medium tracking-wide uppercase"
                        type="button"
                    >
                        <ChevronRight className={cn('size-3.5 transition-transform', notWrittenOpen && 'rotate-90')} />
                        Not written ({notWritten.length})
                    </CollapsibleTrigger>
                    <CollapsibleContent>
                        <ul className="mt-0.5 flex flex-col gap-0.5">
                            {notWritten.map((node) => (
                                <li key={node.section.id}>
                                    <button
                                        className="hover:bg-muted/60 text-muted-foreground flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs"
                                        onClick={() => onSelectSection(node.section.id)}
                                        type="button"
                                    >
                                        <CircleSlash className="size-3.5 shrink-0" />
                                        <span className="min-w-0 flex-1 truncate">{numberedTitle(node)}</span>
                                    </button>
                                </li>
                            ))}
                        </ul>
                    </CollapsibleContent>
                </Collapsible>
            )}
        </div>
    );
}

function TocList({ activeSectionId, depth, nodes, onSelectSection }: TocListProps) {
    const visible = nodes.filter((node) => !isSectionNotWritten(node.section));

    return (
        <ul className="flex flex-col gap-0.5">
            {visible.map((node) => (
                <li key={node.section.id}>
                    <button
                        className={cn(
                            'hover:bg-muted/60 flex w-full items-start gap-2 rounded px-2 py-1 text-left text-xs transition-colors',
                            activeSectionId === node.section.id && 'bg-muted/60',
                        )}
                        onClick={() => onSelectSection(node.section.id)}
                        style={{ paddingLeft: `${8 + depth * 14}px` }}
                        type="button"
                    >
                        <span className="mt-0.5">
                            <StatusIcon status={node.section.status} />
                        </span>
                        <span className="min-w-0 flex-1 truncate">
                            {node.number && <span className="text-muted-foreground mr-1">{node.number}</span>}
                            {node.section.title}
                        </span>
                    </button>
                    {node.children.length > 0 && (
                        <TocList
                            activeSectionId={activeSectionId}
                            depth={depth + 1}
                            nodes={node.children}
                            onSelectSection={onSelectSection}
                        />
                    )}
                </li>
            ))}
        </ul>
    );
}

export default FlowReportToc;
