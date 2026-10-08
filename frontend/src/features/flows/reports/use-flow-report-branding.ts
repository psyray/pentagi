import { useApolloClient, useMutation, useQuery } from '@apollo/client/react';
import { useCallback, useState } from 'react';
import { toast } from 'sonner';

import {
    ReportBrandingDocument,
    type ReportBrandingFragmentFragment,
    UpdateReportBrandingDocument,
} from '@/graphql/types';
import { Log } from '@/lib/log';

import { reportBrandingLogoPath } from './report-branding';

export interface SaveBrandingInput {
    footer: string;
    header: string;
    // A data URL to store, an empty string to clear, null to keep the stored logo.
    logo?: null | string;
    logoName?: null | string;
    subtitle: string;
    title: string;
}

/**
 * The per-report branding behind the export (plan § 6.6 / P6): read it, save it.
 * The logo bytes stay out of GraphQL — the reader points an <img> at the API
 * endpoint and the exporters fetch it from the same place.
 */
export function useFlowReportBranding(reportId: null | string, flowId: null | string) {
    const client = useApolloClient();
    const [saving, setSaving] = useState(false);
    const [updateBranding] = useMutation(UpdateReportBrandingDocument);

    const { data, loading } = useQuery(ReportBrandingDocument, {
        skip: !reportId,
        variables: { reportId: reportId ?? '' },
    });

    const branding: null | ReportBrandingFragmentFragment = data?.reportBranding ?? null;

    const save = useCallback(
        async (input: SaveBrandingInput): Promise<boolean> => {
            if (!reportId) {
                return false;
            }

            setSaving(true);

            try {
                const response = await updateBranding({
                    variables: {
                        input: {
                            footer: input.footer,
                            header: input.header,
                            logo: input.logo ?? null,
                            logoName: input.logoName ?? null,
                            subtitle: input.subtitle,
                            title: input.title,
                        },
                        reportId,
                    },
                });
                const updated = response.data?.updateReportBranding;

                if (updated) {
                    client.cache.writeQuery({
                        data: { reportBranding: updated },
                        query: ReportBrandingDocument,
                        variables: { reportId },
                    });
                }

                return true;
            } catch (error) {
                Log.error('Failed to save the report branding:', error);
                toast.error(error instanceof Error ? error.message : 'Failed to save the branding');

                return false;
            } finally {
                setSaving(false);
            }
        },
        [client, reportId, updateBranding],
    );

    return {
        branding,
        loading,
        logoUrl: reportId && flowId ? reportBrandingLogoPath(flowId, reportId) : '',
        save,
        saving,
    };
}
