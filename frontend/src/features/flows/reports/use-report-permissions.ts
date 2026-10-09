import { useMemo } from 'react';

import { useUser } from '@/providers/user-provider';

export interface ReportPermissions {
    canCreate: boolean;
    canDelete: boolean;
    canEdit: boolean;
    canRead: boolean;
}

/**
 * Mirrors the reports.* privileges the backend enforces (plan reports backlog
 * v2, L2). reports.admin widens every report right, like the GraphQL helper.
 */
export function useReportPermissions(): ReportPermissions {
    const { authInfo } = useUser();
    const privileges = authInfo?.privileges;

    return useMemo(() => {
        const list = privileges ?? [];
        const admin = list.includes('reports.admin');

        return {
            canCreate: admin || list.includes('reports.create'),
            canDelete: admin || list.includes('reports.delete'),
            canEdit: admin || list.includes('reports.edit'),
            canRead: admin || list.includes('reports.read'),
        };
    }, [privileges]);
}
