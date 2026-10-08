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

// GraphQL `ID` values reach the client as numbers while route params are
// strings; compare them as text so a lookup never misses on type alone.
export const sameReportId = (
    left: null | number | string | undefined,
    right: null | number | string | undefined,
): boolean => left != null && right != null && String(left) === String(right);

export const findReportById = <T extends { id: number | string }>(
    reports: readonly T[],
    reportId: null | number | string | undefined,
): null | T => reports.find((report) => sameReportId(report.id, reportId)) ?? null;

export const isSectionHidden = (section: ReportSectionFragmentFragment): boolean => section.hidden;

export const isSectionSkipped = (section: ReportSectionFragmentFragment): boolean =>
    section.status === ReportSectionStatusType.Skipped;

export const isSectionNotWritten = (section: ReportSectionFragmentFragment): boolean =>
    section.status === ReportSectionStatusType.NotWritten;

export const isSectionActive = (section: ReportSectionFragmentFragment): boolean =>
    section.status === ReportSectionStatusType.Queued || section.status === ReportSectionStatusType.Writing;

// The section is part of the editable outline (planner-skipped alternatives are
// the only ones dropped). A hidden section stays in the tree so the editor can
// show it dimmed and offer to unhide it; it is kept out of the numbering.
const isOutlineSection = (section: ReportSectionFragmentFragment): boolean => !isSectionSkipped(section);

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
// `1.1.`; children of an unnumbered front-matter section stay unnumbered, and a
// hidden section is never numbered).
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
        if (UNNUMBERED_SECTION_KEYS.has(node.section.key) || isSectionHidden(node.section)) {
            assignNumbers(node, '');
        } else {
            counter += 1;
            assignNumbers(node, `${counter}.`);
        }
    }

    return outline;
};

const assignNumbers = (node: ReportOutlineNode, prefix: string): void => {
    if (prefix === '' || isSectionHidden(node.section)) {
        node.number = '';
        node.children.forEach((child) => assignNumbers(child, ''));

        return;
    }

    node.number = prefix;

    // Visible children are numbered on their own counter so hiding one never
    // shifts the numbers of its siblings.
    let index = 0;

    for (const child of node.children) {
        if (isSectionHidden(child.section)) {
            assignNumbers(child, '');

            continue;
        }

        index += 1;
        assignNumbers(child, `${prefix}${index}.`);
    }
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
