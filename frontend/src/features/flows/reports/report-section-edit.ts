import { type ReportSectionFragmentFragment } from '@/graphql/types';

// A section variable row as the editor holds it. The backend stores the whole
// array as JSON (`[{name, value}]`, plan § 6.5.1) and resolves `{{.Name}}`
// placeholders with the raw value, so the value is kept verbatim.
export interface ReportVariableRow {
    name: string;
    value: string;
}

export const serializeVariables = (rows: readonly ReportVariableRow[]): string =>
    JSON.stringify(rows.map((row) => ({ name: row.name.trim(), value: row.value })).filter((row) => row.name !== ''));

export const parseVariableRows = (
    variables: null | readonly { name: string; value: string }[] | undefined,
): ReportVariableRow[] => (variables ?? []).map((variable) => ({ name: variable.name, value: variable.value }));

const byPosition = (left: ReportSectionFragmentFragment, right: ReportSectionFragmentFragment): number => {
    if (left.position !== right.position) {
        return left.position - right.position;
    }

    return Number.parseInt(left.id, 10) - Number.parseInt(right.id, 10);
};

// orderedSiblings mirrors the server's `orderedSections` (engine.go): the
// sections sharing a parent, in document order. Hidden and skipped sections are
// kept so a move swaps against the same neighbour the server would pick.
export const orderedSiblings = (
    sections: readonly ReportSectionFragmentFragment[],
    parentId: null | number | string | undefined,
): ReportSectionFragmentFragment[] =>
    sections.filter((section) => String(section.parentId ?? '') === String(parentId ?? '')).sort(byPosition);

export interface SiblingMoveBoundaries {
    canMoveDown: boolean;
    canMoveUp: boolean;
}

export const siblingMoveBoundaries = (
    sections: readonly ReportSectionFragmentFragment[],
    sectionId: null | number | string | undefined,
): SiblingMoveBoundaries => {
    const target = sections.find((section) => String(section.id) === String(sectionId));

    if (!target) {
        return { canMoveDown: false, canMoveUp: false };
    }

    const siblings = orderedSiblings(sections, target.parentId);
    const index = siblings.findIndex((section) => String(section.id) === String(sectionId));

    return {
        canMoveDown: index >= 0 && index < siblings.length - 1,
        canMoveUp: index > 0,
    };
};

// collectSubtreeIds returns the section and its descendants (the DB cascades
// the delete), so the client can evict the whole subtree from the cache: the
// server publishes no per-section event on delete.
export const collectSubtreeIds = (
    sections: readonly ReportSectionFragmentFragment[],
    sectionId: null | number | string | undefined,
): string[] => {
    const childrenByParent = new Map<string, ReportSectionFragmentFragment[]>();

    for (const section of sections) {
        if (!section.parentId) {
            continue;
        }

        const siblings = childrenByParent.get(String(section.parentId)) ?? [];
        siblings.push(section);
        childrenByParent.set(String(section.parentId), siblings);
    }

    const ids: string[] = [];

    const walk = (id: string): void => {
        ids.push(id);

        for (const child of childrenByParent.get(id) ?? []) {
            walk(String(child.id));
        }
    };

    if (sectionId === null || sectionId === undefined) {
        return ids;
    }

    // An id that is not in the list removes nothing (the caller may race a
    // section the cache already dropped).
    if (!sections.some((section) => String(section.id) === String(sectionId))) {
        return ids;
    }

    walk(String(sectionId));

    return ids;
};

// sectionVariablesDiffer tells whether the editor would change the stored JSON,
// so an untouched row never rewrites it (the GraphQL `String!` round-trip would
// otherwise coerce a numeric value to text).
export const sectionVariablesDiffer = (
    rows: readonly ReportVariableRow[],
    variables: null | readonly { name: string; value: string }[] | undefined,
): boolean => serializeVariables(rows) !== serializeVariables(parseVariableRows(variables));
