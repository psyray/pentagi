import type { ReactNode } from 'react';

import { useQuery } from '@apollo/client/react';
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';

import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import {
    CredentialStatus,
    FlowArtifactsDocument,
    FlowAttackSurfaceDocument,
    FlowCredentialsStatusDocument,
    FlowDetectedCvEsDocument,
    FlowGraphitiToolUsageDocument,
    FlowInfrastructureMapDocument,
    FlowOpenPortsDocument,
    FlowValidAccessesDocument,
    FlowVulnerabilityBreakdownDocument,
    VulnCategory,
} from '@/graphql/types';
import { formatNumber } from '@/lib/utils/format';

const POLL_INTERVAL = 10000;

// ─── Shared helpers ──────────────────────────────────────────────────────

interface DashboardTableCardProps {
    children: ReactNode;
    description?: string;
    loading?: boolean;
    title: string;
}

export function FlowDashboardAttackSurface({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowAttackSurfaceDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowAttackSurface ?? [];

    return (
        <DashboardTableCard
            description="Entities discovered by Graphiti, newest first"
            loading={loading && rows.length === 0}
            title="Attack surface overview"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No entities extracted yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead className="w-32">Type</TableHead>
                            <TableHead className="w-48">Name</TableHead>
                            <TableHead>Summary</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r) => (
                            <TableRow key={`${r.type}-${r.name}`}>
                                <TableCell>
                                    <Badge variant="outline">{r.type}</Badge>
                                </TableCell>
                                <TableCell className="font-medium">{r.name || '—'}</TableCell>
                                <TableCell className="text-muted-foreground">{r.summary || '—'}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

function DashboardTableCard({ children, description, loading, title }: DashboardTableCardProps) {
    return (
        <Card>
            <CardHeader>
                <CardTitle>{title}</CardTitle>
                {description ? <CardDescription>{description}</CardDescription> : null}
            </CardHeader>
            <CardContent>{loading ? <LoadingRows /> : children}</CardContent>
        </Card>
    );
}

function EmptyTable({ message }: { message: string }) {
    return (
        <Empty className="py-6">
            <EmptyHeader>
                <EmptyTitle>Nothing yet</EmptyTitle>
                <EmptyDescription>{message}</EmptyDescription>
            </EmptyHeader>
        </Empty>
    );
}

function ExamplesCell({ examples }: { examples: string[] }) {
    if (!examples.length) {return <TableCell className="text-muted-foreground">—</TableCell>;}

    return (
        <TableCell>
            <ul className="flex flex-col gap-1">
                {examples.map((ex, i) => (
                    <li className="text-muted-foreground truncate" key={i} title={ex}>
                    {ex}
                    </li>
                ))}
            </ul>
        </TableCell>
    );
}

// ─── Attack surface overview ────────────────────────────────────────────

function LoadingRows() {
    return (
        <div className="flex flex-col gap-2">
            {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton className="h-6 w-full" key={i} />
            ))}
        </div>
    );
}

// ─── Credentials status ─────────────────────────────────────────────────

const credentialStatusBadge: Record<CredentialStatus, { label: string; variant: 'red' | 'secondary' | 'yellow' }> = {
    [CredentialStatus.Compromised]: { label: 'Compromised', variant: 'red' },
    [CredentialStatus.Discovered]: { label: 'Discovered', variant: 'yellow' },
    [CredentialStatus.Unknown]: { label: 'Unknown', variant: 'secondary' },
};

export function FlowDashboardCredentialsStatus({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowCredentialsStatusDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowCredentialsStatus ?? [];

    return (
        <DashboardTableCard
            description="Inferred from YIELDED_ACCESS / AUTHENTICATES_TO / BELONGS_TO_ACCOUNT"
            loading={loading && rows.length === 0}
            title="Credentials status"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No credentials extracted yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Status</TableHead>
                            <TableHead className="text-right">Count</TableHead>
                            <TableHead>Examples</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r) => {
                            const meta = credentialStatusBadge[r.status] ?? credentialStatusBadge[CredentialStatus.Unknown];

                            return (
                                <TableRow key={r.status}>
                                    <TableCell>
                                        <Badge variant={meta.variant}>{meta.label}</Badge>
                                    </TableCell>
                                    <TableCell className="text-right font-semibold">
                                        {formatNumber(r.count)}
                                    </TableCell>
                                    <ExamplesCell examples={r.examples} />
                                </TableRow>
                            );
                        })}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

// ─── Valid accesses ─────────────────────────────────────────────────────

export function FlowDashboardInfrastructureMap({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowInfrastructureMapDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowInfrastructureMap ?? [];

    return (
        <DashboardTableCard
            description="Host → Port → Service extracted from scans"
            loading={loading && rows.length === 0}
            title="Infrastructure map"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No infrastructure extracted yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Host</TableHead>
                            <TableHead>Port</TableHead>
                            <TableHead>Service</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r, i) => (
                            <TableRow key={`${r.host}-${r.port}-${i}`}>
                                <TableCell className="font-medium">{r.host || '—'}</TableCell>
                                <TableCell>{r.port || '—'}</TableCell>
                                <TableCell className="text-muted-foreground">{r.service || '—'}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

// ─── Infrastructure map ──────────────────────────────────────────────────

export function FlowDashboardOpenPorts({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowOpenPortsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowOpenPorts ?? [];

    return (
        <DashboardTableCard
            description="One row per discovered port"
            loading={loading && rows.length === 0}
            title="Open ports"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No ports extracted yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Port</TableHead>
                            <TableHead>Service</TableHead>
                            <TableHead>Host</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r, i) => (
                            <TableRow key={`${r.port}-${r.host}-${i}`}>
                                <TableCell className="font-medium">{r.port || '—'}</TableCell>
                                <TableCell className="text-muted-foreground">{r.service || '—'}</TableCell>
                                <TableCell>{r.host || '—'}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

// ─── Open ports ─────────────────────────────────────────────────────────

export function FlowDashboardValidAccesses({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowValidAccessesDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowValidAccesses ?? [];

    return (
        <DashboardTableCard
            description="Validated access paths (ValidAccess → Account / Service / Host)"
            loading={loading && rows.length === 0}
            title="Valid accesses"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No validated access discovered yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Access</TableHead>
                            <TableHead>Account</TableHead>
                            <TableHead>Host</TableHead>
                            <TableHead>Service</TableHead>
                            <TableHead>Summary</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r, i) => (
                            <TableRow key={`${r.access}-${i}`}>
                                <TableCell className="text-muted-foreground">{r.access || '—'}</TableCell>
                                <TableCell className="font-medium">{r.account || '—'}</TableCell>
                                <TableCell>{r.host || '—'}</TableCell>
                                <TableCell>{r.service || '—'}</TableCell>
                                <TableCell className="text-muted-foreground">{r.summary || '—'}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

// ─── Vulnerability breakdown (table + bar chart) ────────────────────────

const vulnCategoryBadge: Record<VulnCategory, 'blue' | 'orange' | 'outline' | 'red' | 'secondary' | 'yellow'> = {
    [VulnCategory.Critical]: 'red',
    [VulnCategory.Cve]: 'red',
    [VulnCategory.High]: 'orange',
    [VulnCategory.Info]: 'secondary',
    [VulnCategory.Low]: 'blue',
    [VulnCategory.Medium]: 'yellow',
};

export function FlowDashboardArtifacts({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowArtifactsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowArtifacts ?? [];

    return (
        <DashboardTableCard
            description="Files / outputs produced during the flow (Artifact ← PRODUCED ← Episodic)"
            loading={loading && rows.length === 0}
            title="Artifacts"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No artifacts produced yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Artifact</TableHead>
                            <TableHead>Produced by</TableHead>
                            <TableHead>Summary</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r, i) => (
                            <TableRow key={`${r.artifact}-${i}`}>
                                <TableCell className="font-medium">{r.artifact || '—'}</TableCell>
                                <TableCell className="text-muted-foreground">{r.producedBy || '—'}</TableCell>
                                <TableCell className="text-muted-foreground">{r.summary || '—'}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

// ─── Detected CVEs ──────────────────────────────────────────────────────

export function FlowDashboardDetectedCVEs({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowDetectedCvEsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowDetectedCVEs ?? [];

    return (
        <DashboardTableCard
            description="Vulnerabilities whose name or summary contains a CVE id"
            loading={loading && rows.length === 0}
            title="Detected CVEs"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No CVE detected yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>CVE</TableHead>
                            <TableHead>Found on</TableHead>
                            <TableHead>Source</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r) => (
                            <TableRow key={r.cve}>
                                <TableCell className="font-mono font-medium">{r.cve}</TableCell>
                                <TableCell>{r.foundOn || '—'}</TableCell>
                                <TableCell className="text-muted-foreground">{r.source || '—'}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

// ─── Tool usage ─────────────────────────────────────────────────────────

export function FlowDashboardToolUsage({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowGraphitiToolUsageDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowGraphitiToolUsage ?? [];

    return (
        <DashboardTableCard
            description="Tool executions observed by Graphiti (episodes tool_execution_*)"
            loading={loading && rows.length === 0}
            title="Tool usage"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No tool executions recorded yet." />
            ) : (
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Tool</TableHead>
                            <TableHead className="text-right">Executions</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((r) => (
                            <TableRow key={r.tool}>
                                <TableCell className="font-mono font-medium">{r.tool || '—'}</TableCell>
                                <TableCell className="text-right">{formatNumber(r.executions)}</TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            )}
        </DashboardTableCard>
    );
}

// ─── Artifacts ──────────────────────────────────────────────────────────

export function FlowDashboardVulnerabilityBreakdown({ flowId }: { flowId: string }) {
    const { data, loading } = useQuery(FlowVulnerabilityBreakdownDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-and-network',
        notifyOnNetworkStatusChange: true,
        pollInterval: POLL_INTERVAL,
        variables: { flowId },
    });
    const rows = data?.flowVulnerabilityBreakdown ?? [];

    return (
        <DashboardTableCard
            description="Bucketed by CVE / severity (inferred from name + summary)"
            loading={loading && rows.length === 0}
            title="Vulnerability breakdown"
        >
            {rows.length === 0 ? (
                <EmptyTable message="No vulnerabilities extracted yet." />
            ) : (
                <div className="flex flex-col gap-4">
                    <div className="h-48">
                        <ResponsiveContainer height="100%" width="100%">
                            <BarChart data={rows} layout="vertical" margin={{ bottom: 8, left: 16, right: 16, top: 8 }}>
                                <XAxis allowDecimals={false} type="number" />
                                <YAxis dataKey="category" type="category" width={72} />
                                <Tooltip
                                    cursor={{ fill: 'hsl(var(--muted))', fillOpacity: 0.3 }}
                                    formatter={(v) => [formatNumber(Number(v)), 'count']}
                                />
                                <Bar dataKey="count" fill="hsl(var(--primary))" radius={[0, 4, 4, 0]} />
                            </BarChart>
                        </ResponsiveContainer>
                    </div>
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Category</TableHead>
                                <TableHead className="text-right">Count</TableHead>
                                <TableHead>Examples</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {rows.map((r) => (
                                <TableRow key={r.category}>
                                    <TableCell>
                                        <Badge variant={vulnCategoryBadge[r.category] ?? 'outline'}>
                                            {r.category}
                                        </Badge>
                                    </TableCell>
                                    <TableCell className="text-right font-semibold">
                                        {formatNumber(r.count)}
                                    </TableCell>
                                    <ExamplesCell examples={r.examples} />
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </div>
            )}
        </DashboardTableCard>
    );
}