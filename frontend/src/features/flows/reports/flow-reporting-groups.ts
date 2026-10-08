import { type ReportLogFragmentFragment, type ReportSectionFragmentFragment } from '@/graphql/types';

// Logs the engine did not attach to a section (run status, planner decisions)
// share this leading group.
export const REPORT_GROUP_KEY = 'report';
export const REPORT_GROUP_TITLE = 'Report run';

export interface ReportLogGroup {
    key: string;
    logs: ReportLogFragmentFragment[];
    sectionId: null | string;
    title: null | string;
}

// The writer lifecycle logs carry the section id in their payload; the
// `report_logs.section_id` column stays NULL for them (only operator notes use
// it), so the payload is the source of truth.
export const logSectionId = (log: ReportLogFragmentFragment): null | string => {
    const { payload } = log;

    if (payload && typeof payload === 'object' && 'section_id' in payload) {
        const value = (payload as { section_id?: unknown }).section_id;

        if (value !== null && value !== undefined && value !== '') {
            return String(value);
        }
    }

    return log.sectionId === null ? null : String(log.sectionId);
};

const logId = (log: ReportLogFragmentFragment): number => {
    const id = Number.parseInt(log.id, 10);

    return Number.isFinite(id) ? id : 0;
};

// groupReportLogs folds the flat log list into the console's section groups, in
// first-appearance order: a section becomes a group when its first log arrives,
// and the ungrouped logs share one leading group. Sections the list does not
// know (not loaded yet, or since removed) keep a null title.
export const groupReportLogs = (
    logs: readonly ReportLogFragmentFragment[],
    sections: readonly ReportSectionFragmentFragment[],
): ReportLogGroup[] => {
    const titleBySectionId = new Map(sections.map((section) => [String(section.id), section.title]));
    const groups = new Map<string, ReportLogGroup>();

    for (const log of [...logs].sort((left, right) => logId(left) - logId(right))) {
        const sectionId = logSectionId(log);
        const key = sectionId ?? REPORT_GROUP_KEY;

        let group = groups.get(key);

        if (!group) {
            group = {
                key,
                logs: [],
                sectionId,
                title: sectionId === null ? null : (titleBySectionId.get(sectionId) ?? null),
            };
            groups.set(key, group);
        }

        group.logs.push(log);
    }

    return [...groups.values()];
};

export const groupTitle = (group: ReportLogGroup): string => {
    if (group.title) {
        return group.title;
    }

    return group.sectionId === null ? REPORT_GROUP_TITLE : `Section ${group.sectionId}`;
};
