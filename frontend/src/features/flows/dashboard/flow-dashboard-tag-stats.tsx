import { useQuery } from '@apollo/client/react';
import { useMemo } from 'react';

import { MetricCard } from '@/components/dashboard';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { FlowGraphitiTagStatsDocument } from '@/graphql/types';

// preferredOrder controls the display order of the tag stat blocks. Tags not
// in this list appear after, sorted alphabetically.
const preferredOrder = [
    'ValidAccess',
    'Account',
    'Credential',
    'Host',
    'Port',
    'Service',
    'Vulnerability',
    'Misconfiguration',
    'Endpoint',
    'WebApp',
    'Vhost',
    'PrivChange',
    'Capability',
    'Attempt',
    'Artifact',
    'Evidence',
    'AttackTechnique',
];

const tagIcon: Record<string, string> = {
    Account: '👤',
    Artifact: '📦',
    AttackTechnique: '⚔️',
    Attempt: '🎯',
    Capability: '🎓',
    Credential: '🔑',
    Endpoint: '🔗',
    Evidence: '🔬',
    Host: '🖥️',
    Misconfiguration: '⚠️',
    Port: '🔌',
    PrivChange: '⬆️',
    Service: '⚙️',
    ValidAccess: '🔓',
    Vhost: '💽',
    Vulnerability: '🛡️',
    WebApp: '🌐',
};

interface FlowDashboardTagStatsProps {
    flowId: string;
    pollInterval?: number;
}

export function FlowDashboardTagStats({ flowId, pollInterval = 10000 }: FlowDashboardTagStatsProps) {
    const { data, loading } = useQuery(FlowGraphitiTagStatsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval,
        variables: { flowId },
    });

    const stats = useMemo(() => {
        const rows = data?.flowGraphitiTagStats ?? [];
        const byTag = new Map<string, number>();

        for (const r of rows) {
            byTag.set(r.tag, (byTag.get(r.tag) ?? 0) + r.count);
        }

        return [...byTag.entries()]
            .map(([tag, count]) => ({ count, tag }))
            .sort((a, b) => {
                const ai = preferredOrder.indexOf(a.tag);
                const bi = preferredOrder.indexOf(b.tag);

                if (ai !== -1 || bi !== -1) {
                    if (ai === -1) {return 1;}

                    if (bi === -1) {return -1;}

                    return ai - bi;
                }

                return a.tag.localeCompare(b.tag);
            });
    }, [data]);

    if (!loading && stats.length === 0) {
        return null;
    }

    return (
        <Card>
            <CardHeader>
                <CardTitle>Knowledge-graph tag stats</CardTitle>
                <CardDescription>Entity counts per Graphiti taxonomy tag</CardDescription>
            </CardHeader>
            <CardContent>
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-6">
                    {loading && stats.length === 0 ? (
                        <>
                            {Array.from({ length: 6 }).map((_, i) => (
                                <MetricCard
                                    key={i}
                                    loading
                                    title=""
                                    value=""
                                />
                            ))}
                        </>
                    ) : (
                        stats.map(({ count, tag }) => (
                            <MetricCard
                                description={tag}
                                icon={<span className="text-base">{tagIcon[tag] ?? '•'}</span>}
                                key={tag}
                                title={tag}
                                value={count}
                            />
                        ))
                    )}
                </div>
            </CardContent>
        </Card>
    );
}