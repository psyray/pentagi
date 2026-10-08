import Markdown from '@/components/shared/markdown';
import { Badge } from '@/components/ui/badge';
import { ReportSectionStatusType } from '@/graphql/types';
import { cn } from '@/lib/utils';

import { sectionStatusLabel } from './report-format';
import { isComposedSection, numberedTitle, type ReportOutlineNode } from './report-outline';

const HEADING_CLASSES = ['text-xl font-semibold', 'text-lg font-semibold', 'text-base font-semibold'] as const;

const headingClass = (depth: number): string => HEADING_CLASSES[Math.min(depth, HEADING_CLASSES.length - 1)] ?? '';

interface FlowReportSectionProps {
    depth: number;
    node: ReportOutlineNode;
}

function FlowReportSection({ depth, node }: FlowReportSectionProps) {
    const { section } = node;
    const Heading = (['h2', 'h3', 'h4'] as const)[Math.min(depth, 2)] ?? 'h4';

    return (
        <section
            className="scroll-mt-4"
            id={`report-section-${section.id}`}
        >
            <div className={cn('flex items-center gap-2', depth === 0 ? 'mt-8 mb-3 first:mt-0' : 'mt-6 mb-2')}>
                <Heading className={cn('min-w-0', headingClass(depth))}>{numberedTitle(node)}</Heading>
                {sectionStatusBadge(section.status)}
            </div>

            <div className="text-sm">{sectionBody(node)}</div>

            {node.children.map((child) => (
                <FlowReportSection
                    depth={depth + 1}
                    key={child.section.id}
                    node={child}
                />
            ))}
        </section>
    );
}

function sectionBody(node: ReportOutlineNode) {
    const { section } = node;

    if (isComposedSection(section)) {
        return (
            <p className="text-muted-foreground text-sm italic">
                Composed from the document when the report is exported.
            </p>
        );
    }

    if (section.status === ReportSectionStatusType.Queued) {
        return <p className="text-muted-foreground text-sm italic">Not written yet.</p>;
    }

    if (section.status === ReportSectionStatusType.NotWritten) {
        return <p className="text-muted-foreground text-sm italic">Not written.</p>;
    }

    return (
        <>
            {section.content.trim() ? <Markdown>{section.content}</Markdown> : null}
            {section.status === ReportSectionStatusType.Writing && (
                <p className="text-muted-foreground mt-2 text-sm italic">Being written…</p>
            )}
        </>
    );
}

function sectionStatusBadge(status: ReportSectionStatusType) {
    if (status !== ReportSectionStatusType.Queued && status !== ReportSectionStatusType.Writing) {
        return null;
    }

    return (
        <Badge
            className="shrink-0 text-xs"
            variant="secondary"
        >
            {sectionStatusLabel(status)}
        </Badge>
    );
}

export default FlowReportSection;
