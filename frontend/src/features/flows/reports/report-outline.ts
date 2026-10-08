import { type ReportSectionFragmentFragment, ReportSectionKind, ReportSectionStatusType } from '@/graphql/types';

// Sections the bundled template marks `numbered: false` (front matter). This
// mirrors backend/pkg/report/templates/pentagi-default-pentest-report.yaml;
// user-defined templates will source the flag from a template descriptor
// (plan § 6.9). Numbering itself is always derived here, never stored.
const UNNUMBERED_SECTION_KEYS: ReadonlySet<string> = new Set(['cover-page', 'limitations', 'page-header-footer']);

export interface ReportOutlineNode {
    children: ReportOutlineNode[];
    number: string;
    section: ReportSectionFragmentFragment;
}

export const isSectionHidden = (section: ReportSectionFragmentFragment): boolean => section.hidden;

export const isSectionSkipped = (section: ReportSectionFragmentFragment): boolean =>
    section.status === ReportSectionStatusType.Skipped;

export const isSectionNotWritten = (section: ReportSectionFragmentFragment): boolean =>
    section.status === ReportSectionStatusType.NotWritten;

export const isSectionActive = (section: ReportSectionFragmentFragment): boolean =>
    section.status === ReportSectionStatusType.Queued || section.status === ReportSectionStatusType.Writing;

// The section is part of the document outline (hidden and planner-skipped
// alternatives stay out of both the TOC and the body).
const isOutlineSection = (section: ReportSectionFragmentFragment): boolean =>
    !isSectionHidden(section) && !isSectionSkipped(section);

const byPosition = (left: ReportSectionFragmentFragment, right: ReportSectionFragmentFragment): number => {
    if (left.position !== right.position) {
        return left.position - right.position;
    }

    return Number.parseInt(left.id, 10) - Number.parseInt(right.id, 10);
};

const buildNode = (
    section: ReportSectionFragmentFragment,
    byParent: Map<string, ReportSectionFragmentFragment[]>,
): ReportOutlineNode => ({
    children: (byParent.get(section.id) ?? []).map((child) => buildNode(child, byParent)),
    number: '',
    section,
});

// buildReportOutline turns the flat section list into the document tree, in
// document order, and assigns the derived numbers (top-level `1.`, children
// `1.1.`; children of an unnumbered front-matter section stay unnumbered).
export const buildReportOutline = (sections: readonly ReportSectionFragmentFragment[]): ReportOutlineNode[] => {
    const visible = sections.filter(isOutlineSection);
    const byParent = new Map<string, ReportSectionFragmentFragment[]>();

    for (const section of visible) {
        if (!section.parentId) {
            continue;
        }

        const siblings = byParent.get(section.parentId) ?? [];
        siblings.push(section);
        byParent.set(section.parentId, siblings);
    }

    for (const siblings of byParent.values()) {
        siblings.sort(byPosition);
    }

    const roots = visible.filter((section) => !section.parentId).sort(byPosition);
    const outline = roots.map((section) => buildNode(section, byParent));

    let counter = 0;

    for (const node of outline) {
        if (UNNUMBERED_SECTION_KEYS.has(node.section.key)) {
            assignNumbers(node, '');
        } else {
            counter += 1;
            assignNumbers(node, `${counter}.`);
        }
    }

    return outline;
};

const assignNumbers = (node: ReportOutlineNode, number: string): void => {
    node.number = number;
    node.children.forEach((child, index) => {
        assignNumbers(child, number === '' ? '' : `${number}${index + 1}.`);
    });
};

// flattenReportOutline walks the tree in document order (a section before its
// children), the same order the server assembles the exported document in.
export const flattenReportOutline = (outline: readonly ReportOutlineNode[]): ReportOutlineNode[] =>
    outline.flatMap((node) => [node, ...flattenReportOutline(node.children)]);

export const numberedTitle = (node: ReportOutlineNode): string =>
    node.number ? `${node.number} ${node.section.title}` : node.section.title;

// A composed section body is computed server-side; the app shows the same
// placeholder the edition view does until the report is exported.
export const isComposedSection = (section: ReportSectionFragmentFragment): boolean =>
    section.kind === ReportSectionKind.Composed;
