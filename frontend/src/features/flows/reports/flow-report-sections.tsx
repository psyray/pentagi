import { useQuery } from '@apollo/client/react';

import { Badge } from '@/components/ui/badge';
import { Spinner } from '@/components/ui/spinner';
import {
    type ReportSectionFragmentFragment,
    ReportSectionKind,
    ReportSectionsDocument,
    ReportSectionStatusType,
} from '@/graphql/types';
import { cn } from '@/lib/utils';

import { formatBytes, sectionStatusLabel } from './report-format';
import { buildReportOutline, isSectionHidden, isSectionSkipped, type ReportOutlineNode } from './report-outline';

interface FlowReportSectionsProps {
    reportId: string;
}

function FlowReportSectionRow({ depth, node }: { depth: number; node: ReportOutlineNode }) {
    const { section } = node;

    return (
        <>
            <div
                className="flex items-center gap-2 text-xs"
                style={{ paddingLeft: `${8 + depth * 14}px` }}
            >
                <span className="text-muted-foreground w-6 shrink-0 text-right font-mono">
                    {depth === 0 ? node.number.replace(/\.$/, '') : ''}
                </span>
                <span
                    className={cn(
                        'min-w-0 flex-1 truncate',
                        isSectionHidden(section) && 'opacity-60',
                        isSectionSkipped(section) && 'text-muted-foreground line-through',
                    )}
                >
                    {section.title}
                </span>
                <SectionTrailing
                    childCount={node.children.length}
                    section={section}
                />
            </div>

            {node.children.map((child) => (
                <FlowReportSectionRow
                    depth={depth + 1}
                    key={child.section.id}
                    node={child}
                />
            ))}
        </>
    );
}

// The report card's expanded body: the document outline with each section's
// progress (content size once written, structure for the aggregates, status
// while pending). Skipped alternatives are kept, dimmed and unnumbered, so the
// list shows the whole structure the planner produced.
function FlowReportSections({ reportId }: FlowReportSectionsProps) {
    const { data, loading } = useQuery(ReportSectionsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-first',
        variables: { reportId },
    });

    const sections = data?.reportSections ?? [];
    const outline = buildReportOutline(sections, { includeSkipped: true });

    if (loading && sections.length === 0) {
        return (
            <div className="text-muted-foreground flex items-center gap-2 px-2 py-2 text-xs">
                <Spinner className="size-3" />
                Loading sections…
            </div>
        );
    }

    if (outline.length === 0) {
        return <div className="text-muted-foreground px-2 py-2 text-xs">No sections yet.</div>;
    }

    return (
        <div className="flex flex-col gap-1 px-1 py-2">
            {outline.map((node) => (
                <FlowReportSectionRow
                    depth={0}
                    key={node.section.id}
                    node={node}
                />
            ))}
        </div>
    );
}

function SectionTrailing({ childCount, section }: { childCount: number; section: ReportSectionFragmentFragment }) {
    if (isSectionSkipped(section) || isSectionHidden(section)) {
        return (
            <Badge
                className="shrink-0 text-xs"
                variant="outline"
            >
                {isSectionSkipped(section) ? 'Skipped' : 'Hidden'}
            </Badge>
        );
    }

    if (section.kind === ReportSectionKind.Aggregate) {
        return (
            <span className="text-muted-foreground shrink-0">
                {childCount} element{childCount === 1 ? '' : 's'}
            </span>
        );
    }

    if (
        section.kind === ReportSectionKind.Llm &&
        (section.status === ReportSectionStatusType.Done || section.status === ReportSectionStatusType.Failed) &&
        section.content.length > 0
    ) {
        return <span className="text-muted-foreground shrink-0">{formatBytes(section.content)}</span>;
    }

    if (section.kind === ReportSectionKind.Composed) {
        return <span className="text-muted-foreground shrink-0">composed</span>;
    }

    if (section.status === ReportSectionStatusType.Done) {
        return null;
    }

    return (
        <Badge
            className="shrink-0 text-xs"
            variant="secondary"
        >
            {sectionStatusLabel(section.status)}
        </Badge>
    );
}

export default FlowReportSections;
