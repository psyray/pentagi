import { ChevronRight, Clock, Copy, ExternalLink, FileText, Gauge, MessageSquare, TriangleAlert } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { toast } from 'sonner';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import {
    type ReportLogFragmentFragment,
    ReportLogKind,
    type ReportSectionFragmentFragment,
    ReportSectionStatusType,
} from '@/graphql/types';
import { copyToClipboard } from '@/lib/clipboard';
import { cn } from '@/lib/utils';

import { groupTitle, type ReportLogGroup } from './flow-reporting-groups';
import { sectionStatusLabel } from './report-format';

interface FlowReportingConversationProps {
    groups: readonly ReportLogGroup[];
    onOpenReport: () => void;
    sections: readonly ReportSectionFragmentFragment[];
}

interface FlowReportingGroupProps {
    group: ReportLogGroup;
    onOpenReport: () => void;
    status: null | ReportSectionStatusType;
}

function FlowReportingConversation({ groups, onOpenReport, sections }: FlowReportingConversationProps) {
    const statusBySectionId = useMemo(
        () => new Map(sections.map((section) => [String(section.id), section.status])),
        [sections],
    );

    return (
        <div className="flex flex-col gap-3 pb-4">
            {groups.map((group) => (
                <FlowReportingGroup
                    group={group}
                    key={group.key}
                    onOpenReport={onOpenReport}
                    status={group.sectionId === null ? null : (statusBySectionId.get(group.sectionId) ?? null)}
                />
            ))}
        </div>
    );
}

function FlowReportingGroup({ group, onOpenReport, status }: FlowReportingGroupProps) {
    const [open, setOpen] = useState(true);
    const title = groupTitle(group);
    const isSection = group.sectionId !== null;

    return (
        <Collapsible
            className="bg-card overflow-hidden rounded-lg border"
            onOpenChange={setOpen}
            open={open}
        >
            <div className="flex items-center gap-2 px-3 py-2">
                <CollapsibleTrigger asChild>
                    <button
                        className="hover:bg-muted/60 flex min-w-0 flex-1 items-center gap-2 rounded px-1 py-0.5 text-left"
                        type="button"
                    >
                        <ChevronRight className={cn('size-4 shrink-0 transition-transform', open && 'rotate-90')} />
                        {isSection ? <FileText className="text-muted-foreground size-4 shrink-0" /> : null}
                        <span className="truncate text-sm font-medium">{title}</span>
                        <span className="text-muted-foreground shrink-0 text-xs">
                            {group.logs.length} message{group.logs.length === 1 ? '' : 's'}
                        </span>
                    </button>
                </CollapsibleTrigger>

                {status !== null && (
                    <Badge
                        className="shrink-0 text-xs"
                        variant="secondary"
                    >
                        {sectionStatusLabel(status)}
                    </Badge>
                )}
                {isSection && (
                    <Button
                        aria-label="Open the report document"
                        onClick={onOpenReport}
                        size="icon-xs"
                        title="Open the report document"
                        variant="ghost"
                    >
                        <ExternalLink />
                    </Button>
                )}
            </div>

            <CollapsibleContent>
                <div className="flex flex-col gap-2 border-t px-3 py-3">
                    {group.logs.map((log) => (
                        <FlowReportingLog
                            key={log.id}
                            log={log}
                        />
                    ))}
                </div>
            </CollapsibleContent>
        </Collapsible>
    );
}

function FlowReportingLog({ log }: { log: ReportLogFragmentFragment }) {
    const [showDetails, setShowDetails] = useState(false);
    const details = useMemo(() => {
        if (log.payload === null || log.payload === undefined) {
            return null;
        }

        if (typeof log.payload === 'string') {
            return log.payload;
        }

        try {
            return JSON.stringify(log.payload, null, 2);
        } catch {
            return String(log.payload);
        }
    }, [log.payload]);

    const handleCopy = useCallback(async () => {
        if (await copyToClipboard(log.message)) {
            toast.success('Copied to clipboard');
        } else {
            toast.error('Failed to copy to clipboard');
        }
    }, [log.message]);

    return (
        <div className="bg-muted/40 rounded-md border px-3 py-2">
            <div className="flex items-start gap-2">
                <span className="mt-0.5 shrink-0">
                    <LogKindIcon kind={log.kind} />
                </span>
                <div className="min-w-0 flex-1">
                    <div className="text-sm break-words whitespace-pre-wrap">{log.message}</div>
                    {details !== null && (
                        <>
                            <button
                                className="text-muted-foreground mt-1 text-xs underline"
                                onClick={() => setShowDetails((value) => !value)}
                                type="button"
                            >
                                {showDetails ? 'Hide details' : 'Show details'}
                            </button>
                            {showDetails && (
                                <pre className="bg-background/60 mt-1 max-h-64 overflow-auto rounded border p-2 text-xs">
                                    {details}
                                </pre>
                            )}
                        </>
                    )}
                </div>
            </div>

            <div className="text-muted-foreground mt-1.5 flex items-center gap-2 text-xs">
                <Clock className="size-3" />
                <span>{formatClock(log.createdAt)}</span>
                <span className="font-mono">{log.id}</span>
                <Button
                    aria-label="Copy this message"
                    className="ml-auto size-6"
                    onClick={() => {
                        void handleCopy();
                    }}
                    size="icon-xs"
                    title="Copy"
                    variant="ghost"
                >
                    <Copy />
                </Button>
            </div>
        </div>
    );
}

function LogKindIcon({ kind }: { kind: ReportLogKind }) {
    switch (kind) {
        case ReportLogKind.Error:
            return <TriangleAlert className="text-destructive size-3.5" />;
        case ReportLogKind.Note:
            return <MessageSquare className="text-primary size-3.5" />;
        case ReportLogKind.Section:
            return <FileText className="size-3.5" />;
        default:
            return <Gauge className="text-muted-foreground size-3.5" />;
    }
}

const formatClock = (value: string): string => {
    const date = new Date(value);

    if (Number.isNaN(date.getTime())) {
        return '';
    }

    return date.toLocaleTimeString([], { hour12: false });
};

export default FlowReportingConversation;
