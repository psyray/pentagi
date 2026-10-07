import { useQuery } from '@apollo/client/react';
import { Activity, ArrowDownToLine, ArrowUpFromLine, CircleDollarSign, GitFork, Users } from 'lucide-react';
import { useMemo } from 'react';

import { MetricCard, UsageStatsRow } from '@/components/dashboard';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import FlowAgentIcon from '@/features/flows/agents/flow-agent-icon';
import {
    AgentType,
    FlowStatsByFlowDocument,
    ToolcallsStatsByFlowDocument,
    ToolcallsStatsByFunctionForFlowDocument,
    UsageStatsByAgentTypeForFlowDocument,
    UsageStatsByFlowDocument,
    UsageStatsByModelAgentsForFlowDocument,
} from '@/graphql/types';
import { formatCost, formatDuration, formatNumber, formatTokenCount } from '@/lib/utils/format';

// SUB_AGENT_TYPES lists the delegated agent types (workers and their helpers),
// as opposed to the task-level orchestration agents. It drives the "Sub-agent
// runs" card, which must ignore primary_agent / generator / refiner / reporter.
const SUB_AGENT_TYPES = new Set<string>([
    'adviser',
    'coder',
    'enricher',
    'installer',
    'memorist',
    'pentester',
    'searcher',
    'summarizer',
    'tool_call_fixer',
]);

const pct = (part: number, total: number) => (total > 0 ? Math.round((part / total) * 100) : 0);

export function FlowDashboardOverview({ flowId }: { flowId: string }) {
    const {
        data: usageData,
        error: usageError,
        loading: usageLoading,
    } = useQuery(UsageStatsByFlowDocument, {
        variables: { flowId },
    });
    const { data: usageByAgentData } = useQuery(UsageStatsByAgentTypeForFlowDocument, {
        variables: { flowId },
    });
    const { data: usageByModelAgentsData } = useQuery(UsageStatsByModelAgentsForFlowDocument, {
        variables: { flowId },
    });
    const {
        data: toolcallsData,
        error: toolcallsError,
        loading: toolcallsLoading,
    } = useQuery(ToolcallsStatsByFlowDocument, {
        variables: { flowId },
    });
    const { data: toolcallsByFunctionData } = useQuery(ToolcallsStatsByFunctionForFlowDocument, {
        variables: { flowId },
    });
    const {
        data: flowStatsData,
        error: flowStatsError,
        loading: flowStatsLoading,
    } = useQuery(FlowStatsByFlowDocument, {
        variables: { flowId },
    });

    const usage = usageData?.usageStatsByFlow;
    const toolcalls = toolcallsData?.toolcallsStatsByFlow;
    const flowStats = flowStatsData?.flowStatsByFlow;

    const totalCost = usage ? usage.totalUsageCostIn + usage.totalUsageCostOut : 0;
    const totalTokens = usage ? usage.totalUsageIn + usage.totalUsageOut : 0;
    const totalUsageIn = usage?.totalUsageIn ?? 0;
    const totalUsageOut = usage?.totalUsageOut ?? 0;
    const totalCacheIn = usage?.totalUsageCacheIn ?? 0;
    const cacheReadPct = pct(totalCacheIn, totalUsageIn + totalCacheIn);

    const subAgentStats = useMemo(() => {
        const seen = new Set<string>();
        let cost = 0;
        let tokens = 0;

        for (const item of usageByAgentData?.usageStatsByAgentTypeForFlow ?? []) {
            if (!SUB_AGENT_TYPES.has(item.agentType) || seen.has(item.agentType)) {
                continue;
            }

            seen.add(item.agentType);
            tokens += item.stats.totalUsageIn + item.stats.totalUsageOut;
            cost += item.stats.totalUsageCostIn + item.stats.totalUsageCostOut;
        }

        return { cost, tokens };
    }, [usageByAgentData]);

    const subAgentRuns = useMemo(
        () =>
            (toolcallsByFunctionData?.toolcallsStatsByFunctionForFlow ?? [])
                .filter((item) => item.isSubAgent)
                .reduce((acc, item) => acc + item.totalCount, 0),
        [toolcallsByFunctionData],
    );

    const agentTypeRows = useMemo(() => {
        const seen = new Set<string>();

        return (usageByAgentData?.usageStatsByAgentTypeForFlow ?? [])
            .filter((item) => {
                if (seen.has(item.agentType)) {
                    return false;
                }

                seen.add(item.agentType);

                return true;
            })
            .map((item) => ({
                label: item.agentType,
                stats: item.stats,
            }));
    }, [usageByAgentData]);

    const modelAgentRows = useMemo(() => {
        const seen = new Set<string>();

        return (usageByModelAgentsData?.usageStatsByModelAgentsForFlow ?? []).filter((item) => {
            const key = `${item.model}|${item.provider}`;

            if (seen.has(key)) {
                return false;
            }

            seen.add(key);

            return true;
        });
    }, [usageByModelAgentsData]);

    const toolcallsByFunction = useMemo(() => {
        const seen = new Set<string>();

        return [...(toolcallsByFunctionData?.toolcallsStatsByFunctionForFlow ?? [])]
            .filter((item) => {
                if (seen.has(item.functionName)) {
                    return false;
                }

                seen.add(item.functionName);

                return true;
            })
            .sort((a, b) => b.totalCount - a.totalCount);
    }, [toolcallsByFunctionData]);

    const anyLoading =
        (usageLoading && !usageData) || (toolcallsLoading && !toolcallsData) || (flowStatsLoading && !flowStatsData);

    return (
        <div className="flex flex-col gap-6">
            <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
                <MetricCard
                    description={`Subtasks: ${flowStats?.totalSubtasksCount ?? 0} · Assistants: ${flowStats?.totalAssistantsCount ?? 0}`}
                    error={!!flowStatsError && !flowStatsData}
                    icon={<GitFork className="text-muted-foreground size-4" />}
                    loading={anyLoading}
                    title="Tasks"
                    value={flowStats ? formatNumber(flowStats.totalTasksCount) : '0'}
                />
                <MetricCard
                    description={`Duration: ${toolcalls ? formatDuration(toolcalls.totalDurationSeconds) : '—'}`}
                    error={!!toolcallsError && !toolcallsData}
                    icon={<Activity className="text-muted-foreground size-4" />}
                    loading={anyLoading}
                    title="Tool Calls"
                    value={toolcalls ? formatNumber(toolcalls.totalCount) : '0'}
                />
                <MetricCard
                    description={`${formatTokenCount(subAgentStats.tokens)} tokens · ${formatCost(subAgentStats.cost)} · ${pct(subAgentStats.tokens, totalTokens)}% of tokens`}
                    error={!!usageError && !usageData}
                    icon={<Users className="text-muted-foreground size-4" />}
                    loading={anyLoading}
                    title="Sub-agent runs"
                    value={formatNumber(subAgentRuns)}
                />
                <MetricCard
                    description={`${cacheReadPct}% read from cache`}
                    error={!!usageError && !usageData}
                    icon={<ArrowDownToLine className="text-muted-foreground size-4" />}
                    loading={anyLoading}
                    title="Tokens In"
                    value={formatTokenCount(totalUsageIn)}
                />
                <MetricCard
                    description="Generated by the models"
                    error={!!usageError && !usageData}
                    icon={<ArrowUpFromLine className="text-muted-foreground size-4" />}
                    loading={anyLoading}
                    title="Tokens Out"
                    value={formatTokenCount(totalUsageOut)}
                />
                <MetricCard
                    description="LLM spending for this flow"
                    error={!!usageError && !usageData}
                    icon={<CircleDollarSign className="text-muted-foreground size-4" />}
                    loading={anyLoading}
                    title="Cost"
                    value={formatCost(totalCost)}
                />
            </div>

            {!!modelAgentRows.length && (
                <Card>
                    <CardHeader>
                        <CardTitle>Usage by Model &amp; Provider</CardTitle>
                        <CardDescription>
                            LLM token usage and costs grouped by model and provider, with agent types used
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Table aria-label="Usage by Model & Provider">
                            <TableHeader>
                                <TableRow>
                                    <TableHead className="w-40 whitespace-nowrap">Model</TableHead>
                                    <TableHead className="whitespace-nowrap">Provider</TableHead>
                                    <TableHead className="w-36 whitespace-nowrap">Agents</TableHead>
                                    <TableHead className="text-right whitespace-nowrap">Tokens In</TableHead>
                                    <TableHead className="text-right whitespace-nowrap">Tokens Out</TableHead>
                                    <TableHead className="text-right whitespace-nowrap">Cache In</TableHead>
                                    <TableHead className="text-right whitespace-nowrap">Cache Out</TableHead>
                                    <TableHead className="text-right whitespace-nowrap">Cost In</TableHead>
                                    <TableHead className="text-right whitespace-nowrap">Cost Out</TableHead>
                                    <TableHead className="text-right whitespace-nowrap">Total Cost</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {modelAgentRows.map((row) => (
                                    <TableRow key={`${row.model}|${row.provider}`}>
                                        <TableCell className="font-medium">
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <span className="block w-40 truncate">{row.model}</span>
                                                </TooltipTrigger>
                                                <TooltipContent>{row.model}</TooltipContent>
                                            </Tooltip>
                                        </TableCell>
                                        <TableCell>{row.provider}</TableCell>
                                        <TableCell>
                                            <div className="flex w-36 flex-wrap gap-1">
                                                {row.agentTypes.map((agentType) => (
                                                    <FlowAgentIcon
                                                        className="size-3.5"
                                                        key={agentType}
                                                        tooltip={agentType}
                                                        type={agentType as AgentType}
                                                    />
                                                ))}
                                            </div>
                                        </TableCell>
                                        <TableCell className="text-right">
                                            {formatTokenCount(row.stats.totalUsageIn)}
                                        </TableCell>
                                        <TableCell className="text-right">
                                            {formatTokenCount(row.stats.totalUsageOut)}
                                        </TableCell>
                                        <TableCell className="text-right">
                                            {formatTokenCount(row.stats.totalUsageCacheIn)}
                                        </TableCell>
                                        <TableCell className="text-right">
                                            {formatTokenCount(row.stats.totalUsageCacheOut)}
                                        </TableCell>
                                        <TableCell className="text-right">
                                            {formatCost(row.stats.totalUsageCostIn)}
                                        </TableCell>
                                        <TableCell className="text-right">
                                            {formatCost(row.stats.totalUsageCostOut)}
                                        </TableCell>
                                        <TableCell className="text-right font-semibold">
                                            {formatCost(row.stats.totalUsageCostIn + row.stats.totalUsageCostOut)}
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </CardContent>
                </Card>
            )}

            {!!agentTypeRows.length && (
                <Card>
                    <CardHeader>
                        <CardTitle>Usage by Agent Type</CardTitle>
                        <CardDescription>LLM token usage and costs per agent type in this flow</CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Table aria-label="Usage by Agent Type">
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Agent Type</TableHead>
                                    <TableHead className="text-right">Tokens In</TableHead>
                                    <TableHead className="text-right">Tokens Out</TableHead>
                                    <TableHead className="text-right">Cache In</TableHead>
                                    <TableHead className="text-right">Cache Out</TableHead>
                                    <TableHead className="text-right">Cost In</TableHead>
                                    <TableHead className="text-right">Cost Out</TableHead>
                                    <TableHead className="text-right">Total Cost</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {agentTypeRows.map((row) => (
                                    <UsageStatsRow
                                        key={row.label}
                                        label={row.label}
                                        stats={row.stats}
                                    />
                                ))}
                            </TableBody>
                        </Table>
                    </CardContent>
                </Card>
            )}

            {!!toolcallsByFunction.length && (
                <Card>
                    <CardHeader>
                        <CardTitle>Tool Calls by Function</CardTitle>
                        <CardDescription>Execution statistics per tool function in this flow</CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Table aria-label="Tool Calls by Function">
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Function</TableHead>
                                    <TableHead>Type</TableHead>
                                    <TableHead className="text-right">Count</TableHead>
                                    <TableHead className="text-right">Total Duration</TableHead>
                                    <TableHead className="text-right">Avg Duration</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {toolcallsByFunction.map((item) => (
                                    <TableRow key={item.functionName}>
                                        <TableCell className="font-medium">{item.functionName}</TableCell>
                                        <TableCell>
                                            <Badge variant={item.isAgent ? 'secondary' : 'outline'}>
                                                {item.isAgent ? 'Agent' : 'Tool'}
                                            </Badge>
                                        </TableCell>
                                        <TableCell className="text-right">{formatNumber(item.totalCount)}</TableCell>
                                        <TableCell className="text-right">
                                            {formatDuration(item.totalDurationSeconds)}
                                        </TableCell>
                                        <TableCell className="text-right">
                                            {formatDuration(item.avgDurationSeconds)}
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </CardContent>
                </Card>
            )}
        </div>
    );
}
