import { useMutation, useQuery } from '@apollo/client/react';
import { useCallback } from 'react';

import type { ReportTemplateFragmentFragment } from '@/graphql/types';

import {
    CreateReportTemplateDocument,
    DeleteReportTemplateDocument,
    ReportTemplatesDocument,
    SetReportTemplateBrandingDocument,
    SetReportTemplateEnabledDocument,
    UpdateReportTemplateDocument,
} from '@/graphql/types';

export interface ReportTemplateBrandingValues {
    footer: string;
    header: string;
    // A data URL to store, an empty string to clear the stored logo, or null to keep it.
    logo?: null | string;
    logoName?: null | string;
    subtitle: string;
    title: string;
}

export interface ReportTemplateInputValues {
    global: boolean;
    yaml: string;
}

/**
 * Data access for Settings > Report templates (backlog v2, L3). The query already
 * returns the merged list the caller may use — own + global rows newest first,
 * the bundled ones after — so the page renders it as-is. Mutations reject on
 * failure (GraphQL error included) so the dialogs can surface it and stay open.
 */
export function useReportTemplates() {
    const { data, error, loading, refetch } = useQuery(ReportTemplatesDocument);
    const [createTemplateMutation] = useMutation(CreateReportTemplateDocument);
    const [updateTemplateMutation] = useMutation(UpdateReportTemplateDocument);
    const [setEnabledMutation] = useMutation(SetReportTemplateEnabledDocument);
    const [deleteTemplateMutation] = useMutation(DeleteReportTemplateDocument);
    const [saveBrandingMutation] = useMutation(SetReportTemplateBrandingDocument);

    const templates = data?.reportTemplates ?? [];

    const createTemplate = useCallback(
        async (values: ReportTemplateInputValues) => {
            await createTemplateMutation({
                refetchQueries: ['reportTemplates'],
                variables: { input: { enabled: true, global: values.global, yaml: values.yaml } },
            });
        },
        [createTemplateMutation],
    );

    const updateTemplate = useCallback(
        async (template: ReportTemplateFragmentFragment, yaml: string) => {
            await updateTemplateMutation({
                refetchQueries: ['reportTemplates'],
                variables: { input: { enabled: template.enabled, yaml }, templateId: template.id },
            });
        },
        [updateTemplateMutation],
    );

    const setEnabled = useCallback(
        async (template: ReportTemplateFragmentFragment, enabled: boolean) => {
            await setEnabledMutation({
                refetchQueries: ['reportTemplates'],
                variables: { enabled, templateId: template.id },
            });
        },
        [setEnabledMutation],
    );

    const deleteTemplate = useCallback(
        async (template: ReportTemplateFragmentFragment) => {
            await deleteTemplateMutation({
                refetchQueries: ['reportTemplates'],
                variables: { templateId: template.id },
            });
        },
        [deleteTemplateMutation],
    );

    const saveBranding = useCallback(
        async (template: ReportTemplateFragmentFragment, values: ReportTemplateBrandingValues) => {
            await saveBrandingMutation({
                refetchQueries: ['reportTemplates'],
                variables: {
                    input: {
                        footer: values.footer,
                        header: values.header,
                        logo: values.logo ?? null,
                        logoName: values.logoName ?? null,
                        subtitle: values.subtitle,
                        title: values.title,
                    },
                    templateId: template.id,
                },
            });
        },
        [saveBrandingMutation],
    );

    return {
        createTemplate,
        deleteTemplate,
        error,
        loading,
        refetch,
        saveBranding,
        setEnabled,
        templates,
        updateTemplate,
    };
}
