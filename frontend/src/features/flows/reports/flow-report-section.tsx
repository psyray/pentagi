import { TriangleAlert } from 'lucide-react';
import { useMemo, useState } from 'react';

import Markdown from '@/components/shared/markdown';
import { Badge } from '@/components/ui/badge';
import { type ReportSectionFragmentFragment, ReportSectionStatusType } from '@/graphql/types';
import { cn } from '@/lib/utils';

import type { ReportSectionEditor } from './use-flow-report-editor';

import FlowReportSectionActions from './flow-report-section-actions';
import FlowReportSectionEditor from './flow-report-section-editor';
import { sectionStatusLabel } from './report-format';
import { isComposedSection, isSectionHidden, numberedTitle, type ReportOutlineNode } from './report-outline';
import { siblingMoveBoundaries } from './report-section-edit';

const HEADING_CLASSES = ['text-xl font-semibold', 'text-lg font-semibold', 'text-base font-semibold'] as const;

const headingClass = (depth: number): string => HEADING_CLASSES[Math.min(depth, HEADING_CLASSES.length - 1)] ?? '';

interface FlowReportSectionProps {
    canEdit: boolean;
    depth: number;
    editor: ReportSectionEditor;
    node: ReportOutlineNode;
    sections: readonly ReportSectionFragmentFragment[];
}

function FlowReportSection({ canEdit, depth, editor, node, sections }: FlowReportSectionProps) {
    const { section } = node;
    const [editing, setEditing] = useState(false);
    const boundaries = useMemo(() => siblingMoveBoundaries(sections, section.id), [sections, section.id]);
    const Heading = (['h2', 'h3', 'h4'] as const)[Math.min(depth, 2)] ?? 'h4';
    const hidden = isSectionHidden(section);

    return (
        <section
            className={cn('scroll-mt-4', hidden && 'opacity-60')}
            id={`report-section-${section.id}`}
        >
            <div
                className={cn(
                    'group/report-section flex items-start gap-2',
                    depth === 0 ? 'mt-12 mb-4 first:mt-0' : 'mt-10 mb-3',
                )}
            >
                <Heading className={cn('min-w-0 flex-1', headingClass(depth))}>{numberedTitle(node)}</Heading>
                {hidden && (
                    <Badge
                        className="shrink-0 text-xs"
                        variant="outline"
                    >
                        Hidden
                    </Badge>
                )}
                {sectionStatusBadge(section.status)}
                {section.warnings && section.warnings.length > 0 && (
                    <span
                        className="flex shrink-0 items-center gap-1 text-xs text-amber-500"
                        title={section.warnings.join('\n')}
                    >
                        <TriangleAlert className="size-3.5" />
                        {section.warnings.length}
                    </span>
                )}
                {canEdit && (
                    <FlowReportSectionActions
                        boundaries={boundaries}
                        busy={editor.busy}
                        editor={editor}
                        onEdit={() => setEditing(true)}
                        section={section}
                    />
                )}
            </div>

            {editing ? (
                <FlowReportSectionEditor
                    onCancel={() => setEditing(false)}
                    onSaved={() => setEditing(false)}
                    save={editor.saveSection}
                    saving={editor.busy}
                    section={section}
                />
            ) : (
                <>
                    {variablesTable(section)}
                    <div className="text-sm">{sectionBody(node)}</div>
                </>
            )}

            {node.children.length > 0 && (
                <div className="border-border/60 mt-3 flex flex-col border-l pl-5">
                    {node.children.map((child) => (
                        <FlowReportSection
                            canEdit={canEdit}
                            depth={depth + 1}
                            editor={editor}
                            key={child.section.id}
                            node={child}
                            sections={sections}
                        />
                    ))}
                </div>
            )}
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

function variablesTable(section: ReportSectionFragmentFragment) {
    if (section.variables.length === 0) {
        return null;
    }

    return (
        <dl className="bg-muted/40 mb-2 flex flex-col gap-0.5 rounded-md border px-2 py-1.5 text-xs">
            {section.variables.map((variable, index) => (
                <div
                    className="flex gap-2"
                    key={`${variable.name}-${index}`}
                >
                    <dt className="text-muted-foreground shrink-0 font-medium">{variable.name}</dt>
                    <dd className="min-w-0 font-mono break-all">{variable.value}</dd>
                </div>
            ))}
        </dl>
    );
}

export default FlowReportSection;
