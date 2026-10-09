import { useApolloClient, useMutation } from '@apollo/client/react';
import { useCallback, useState } from 'react';
import { toast } from 'sonner';

import {
    AddReportSectionDocument,
    DeleteReportSectionDocument,
    HideReportSectionDocument,
    MoveReportSectionDocument,
    type ReportSectionFragmentFragment,
    ReportSectionsDocument,
    SectionDirection,
    SetReportSectionContentDocument,
    UpdateReportSectionDocument,
    WriteReportSectionDocument,
} from '@/graphql/types';
import { Log } from '@/lib/log';

import { collectSubtreeIds, type ReportVariableRow, serializeVariables } from './report-section-edit';

export interface AddReportSectionInput {
    afterSectionId?: null | string;
    parentId?: null | string;
    title: string;
}

export interface ReportSectionEditor {
    addSection: (input: AddReportSectionInput) => Promise<boolean>;
    busy: boolean;
    deleteSection: (sectionId: string) => Promise<boolean>;
    moveSection: (sectionId: string, direction: 'down' | 'up') => Promise<boolean>;
    saveSection: (input: SaveReportSectionInput) => Promise<boolean>;
    setSectionHidden: (sectionId: string, hidden: boolean) => Promise<boolean>;
    writeSection: (sectionId: string) => Promise<boolean>;
}

export interface SaveReportSectionInput {
    content: string;
    instructions: null | string;
    originalContent: string;
    sectionId: string;
    title: string;
    variables: readonly ReportVariableRow[];
    warnings: null | readonly string[];
}

// upsertSection replaces the matching row in the report's section list or
// appends it. The flat order does not matter: the outline sorts by position.

/**
 * Section-edit mutations for the full-page report view. Every call updates the
 * `reportSections(reportId)` cache from its own result so the document reflects
 * the change immediately; the flow-scoped subscriptions fill in the rest (and
 * the delete, which the server cannot publish per section).
 */
export function useFlowReportEditor(reportId: null | string): ReportSectionEditor {
    const client = useApolloClient();
    const [busy, setBusy] = useState(false);
    const [updateSection] = useMutation(UpdateReportSectionDocument);
    const [setContent] = useMutation(SetReportSectionContentDocument);
    const [moveMutation] = useMutation(MoveReportSectionDocument);
    const [hideMutation] = useMutation(HideReportSectionDocument);
    const [addMutation] = useMutation(AddReportSectionDocument);
    const [deleteMutation] = useMutation(DeleteReportSectionDocument);
    const [writeMutation] = useMutation(WriteReportSectionDocument);

    const upsertSection = useCallback(
        (section: ReportSectionFragmentFragment): void => {
            if (!reportId) {
                return;
            }

            client.cache.updateQuery({ query: ReportSectionsDocument, variables: { reportId } }, (data) => {
                const sections = data?.reportSections ?? [];
                const without = sections.filter((row) => String(row.id) !== String(section.id));

                return { reportSections: [...without, section] };
            });
        },
        [client, reportId],
    );

    const run = useCallback(async (action: () => Promise<boolean>): Promise<boolean> => {
        setBusy(true);

        try {
            return await action();
        } finally {
            setBusy(false);
        }
    }, []);

    const saveSection = useCallback(
        (input: SaveReportSectionInput): Promise<boolean> =>
            run(async () => {
                if (!reportId) {
                    return false;
                }

                try {
                    const response = await updateSection({
                        variables: {
                            input: {
                                instructions: input.instructions,
                                title: input.title,
                                variables: serializeVariables(input.variables),
                                warnings: input.warnings ? [...input.warnings] : null,
                            },
                            reportId,
                            sectionId: input.sectionId,
                        },
                    });
                    const updated = response.data?.updateReportSection;

                    if (updated) {
                        upsertSection(updated);
                    }

                    if (input.content !== input.originalContent) {
                        const contentResponse = await setContent({
                            variables: { content: input.content, reportId, sectionId: input.sectionId },
                        });
                        const contentUpdated = contentResponse.data?.setReportSectionContent;

                        if (contentUpdated) {
                            upsertSection(contentUpdated);
                        }
                    }

                    return true;
                } catch (error) {
                    Log.error('Failed to save the report section:', error);
                    toast.error('Failed to save the section');

                    return false;
                }
            }),
        [reportId, run, setContent, updateSection, upsertSection],
    );

    const moveSection = useCallback(
        (sectionId: string, direction: 'down' | 'up'): Promise<boolean> =>
            run(async () => {
                if (!reportId) {
                    return false;
                }

                try {
                    await moveMutation({
                        variables: {
                            direction: direction === 'up' ? SectionDirection.Up : SectionDirection.Down,
                            reportId,
                            sectionId,
                        },
                    });

                    return true;
                } catch (error) {
                    Log.error('Failed to move the report section:', error);
                    toast.error('Failed to move the section');

                    return false;
                }
            }),
        [moveMutation, reportId, run],
    );

    const setSectionHidden = useCallback(
        (sectionId: string, hidden: boolean): Promise<boolean> =>
            run(async () => {
                if (!reportId) {
                    return false;
                }

                try {
                    const response = await hideMutation({ variables: { hidden, reportId, sectionId } });
                    const updated = response.data?.hideReportSection;

                    if (updated) {
                        upsertSection(updated);
                    }

                    return true;
                } catch (error) {
                    Log.error('Failed to hide the report section:', error);
                    toast.error('Failed to update the section');

                    return false;
                }
            }),
        [hideMutation, reportId, run, upsertSection],
    );

    const addSection = useCallback(
        (input: AddReportSectionInput): Promise<boolean> =>
            run(async () => {
                if (!reportId) {
                    return false;
                }

                try {
                    const response = await addMutation({
                        variables: {
                            afterSectionId: input.afterSectionId ?? null,
                            parentId: input.parentId ?? null,
                            reportId,
                            title: input.title,
                        },
                    });
                    const created = response.data?.addReportSection;

                    if (created) {
                        upsertSection(created);
                    }

                    return true;
                } catch (error) {
                    Log.error('Failed to add the report section:', error);
                    toast.error('Failed to add the section');

                    return false;
                }
            }),
        [addMutation, reportId, run, upsertSection],
    );

    const deleteSection = useCallback(
        (sectionId: string): Promise<boolean> =>
            run(async () => {
                if (!reportId) {
                    return false;
                }

                // The subtree is evicted optimistically: the server cascades the
                // delete but only republishes the report row.
                const removed = new Set(
                    collectSubtreeIds(
                        client.cache.readQuery({
                            query: ReportSectionsDocument,
                            variables: { reportId },
                        })?.reportSections ?? [],
                        sectionId,
                    ),
                );

                try {
                    await deleteMutation({ variables: { reportId, sectionId } });
                    client.cache.updateQuery({ query: ReportSectionsDocument, variables: { reportId } }, (data) => ({
                        reportSections: (data?.reportSections ?? []).filter((row) => !removed.has(String(row.id))),
                    }));

                    return true;
                } catch (error) {
                    Log.error('Failed to delete the report section:', error);
                    toast.error('Failed to delete the section');

                    return false;
                }
            }),
        [client, deleteMutation, reportId, run],
    );

    // writeSection runs the writer on one section on demand (after a hand-added
    // section, or a section left unwritten by a budget stop). The mutation
    // returns ResultType: the live reportSectionUpdated frames carry the state.
    const writeSection = useCallback(
        (sectionId: string): Promise<boolean> =>
            run(async () => {
                if (!reportId) {
                    return false;
                }

                try {
                    await writeMutation({ variables: { reportId, sectionId } });
                    toast.success('Section queued for writing');

                    return true;
                } catch (error) {
                    Log.error('Failed to write the report section:', error);
                    toast.error('Failed to write the section');

                    return false;
                }
            }),
        [reportId, run, writeMutation],
    );

    return { addSection, busy, deleteSection, moveSection, saveSection, setSectionHidden, writeSection };
}
