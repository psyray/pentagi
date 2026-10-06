import { ListCheck, ListTodo } from 'lucide-react';
import { memo, useMemo, useState } from 'react';

import type {
    SubtaskExecutionStatsFragmentFragment,
    TaskExecutionStatsFragmentFragment,
    TaskFragmentFragment,
} from '@/graphql/types';

import Markdown from '@/components/shared/markdown';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { StatusType } from '@/graphql/types';
import { matchesSearchTerm } from '@/lib/text-filter';
import { formatDuration } from '@/lib/utils/format';

import FlowSubtask from './flow-subtask';
import FlowTaskStatusIcon from './flow-task-status-icon';

interface FlowTaskProps {
    searchValue?: string;
    stats?: TaskExecutionStatsFragmentFragment;
    subtaskStats?: Map<string, SubtaskExecutionStatsFragmentFragment>;
    task: TaskFragmentFragment;
}

const describeMissingSubtasks = (status: StatusType) => {
    switch (status) {
        case StatusType.Failed: {
            return 'The task failed before any subtasks were created.';
        }

        case StatusType.Finished: {
            return 'The task ended before any subtasks were created.';
        }

        default: {
            return 'Waiting for subtasks to be created...';
        }
    }
};

function FlowTask({ searchValue = '', stats, subtaskStats, task }: FlowTaskProps) {
    const { id, input, result, status, subtasks, title } = task;
    const [isDetailsVisible, setIsDetailsVisible] = useState(false);
    const hasDetails = Boolean(input) || Boolean(result);

    const searchChecks = useMemo(() => {
        const trimmedSearch = searchValue.trim();

        if (!trimmedSearch) {
            return { hasResultMatch: false };
        }

        return {
            hasResultMatch: matchesSearchTerm(result, trimmedSearch),
        };
    }, [searchValue, result]);

    const [prevSearchValue, setPrevSearchValue] = useState(searchValue);
    const [prevHasResultMatch, setPrevHasResultMatch] = useState(searchChecks.hasResultMatch);

    if (searchValue !== prevSearchValue || searchChecks.hasResultMatch !== prevHasResultMatch) {
        setPrevSearchValue(searchValue);
        setPrevHasResultMatch(searchChecks.hasResultMatch);

        const trimmedSearch = searchValue.trim();

        if (trimmedSearch) {
            if (searchChecks.hasResultMatch) {
                setIsDetailsVisible(true);
            }
        } else {
            setIsDetailsVisible(false);
        }
    }

    const sortedSubtasks = [...(subtasks || [])].sort((a, b) => +a.id - +b.id);
    const hasSubtasks = Boolean(subtasks && subtasks.length > 0);

    const completedSubtasksCount = useMemo(() => {
        if (!subtasks?.length) {
            return 0;
        }

        return subtasks.filter((subtask) => [StatusType.Failed, StatusType.Finished].includes(subtask.status)).length;
    }, [subtasks]);

    const subtaskProgress = useMemo(() => {
        if (!subtasks?.length) {
            return 0;
        }

        return Math.round((completedSubtasksCount / subtasks.length) * 100);
    }, [subtasks, completedSubtasksCount]);

    const totalBudgetSeconds = useMemo(
        () => (subtasks || []).reduce((total, subtask) => total + (subtask.budgetSeconds ?? 0), 0),
        [subtasks],
    );

    const elapsedSeconds = stats?.totalDurationSeconds ?? 0;
    const timeProgress =
        totalBudgetSeconds > 0 ? Math.min(100, Math.round((elapsedSeconds / totalBudgetSeconds) * 100)) : 0;

    return (
        <div className="flex flex-col">
            <div className="relative flex gap-2 pb-4">
                <FlowTaskStatusIcon
                    className="bg-background ring-border ring-background relative z-1 -mt-px size-5 rounded-full ring-3"
                    status={status}
                    tooltip={`Task ID: ${id}`}
                />
                <div className="flex flex-1 flex-col gap-2">
                    <div className="font-semibold">
                        <Markdown
                            className="prose-fixed prose-sm wrap-break-word *:m-0 [&>p]:leading-tight"
                            searchValue={searchValue}
                        >
                            {title}
                        </Markdown>
                    </div>

                    {hasSubtasks && (
                        <div className="flex flex-col gap-1.5">
                            <div className="flex items-center gap-2">
                                <Progress
                                    className="h-1.5 flex-1"
                                    value={subtaskProgress}
                                />
                                <div className="text-muted-foreground shrink-0 text-xs text-nowrap">
                                    {completedSubtasksCount} of {subtasks?.length} subtasks
                                </div>
                            </div>
                            {totalBudgetSeconds > 0 && (
                                <div className="flex items-center gap-2">
                                    <Progress
                                        className="h-1.5 flex-1"
                                        value={timeProgress}
                                    />
                                    <div className="text-muted-foreground shrink-0 text-xs text-nowrap">
                                        {formatDuration(elapsedSeconds)} of {formatDuration(totalBudgetSeconds)}
                                    </div>
                                </div>
                            )}
                        </div>
                    )}

                    {hasDetails && (
                        <div className="text-muted-foreground text-xs">
                            <div
                                className="cursor-pointer"
                                onClick={() => setIsDetailsVisible(!isDetailsVisible)}
                            >
                                {isDetailsVisible ? 'Hide details' : 'Show details'}
                            </div>
                            {isDetailsVisible && (
                                <div className="mt-4 flex flex-col gap-4">
                                    {input && (
                                        <Card>
                                            <CardHeader className="p-3">
                                                <CardTitle className="flex items-center gap-2">
                                                    <ListTodo className="size-4 shrink-0" /> Description
                                                </CardTitle>
                                            </CardHeader>
                                            <CardContent className="p-3 pt-0">
                                                <hr className="mt-0 mb-3" />
                                                <Markdown
                                                    className="prose-xs prose-fixed wrap-break-word"
                                                    searchValue={searchValue}
                                                >
                                                    {input}
                                                </Markdown>
                                            </CardContent>
                                        </Card>
                                    )}
                                    {result && (
                                        <Card>
                                            <CardHeader className="p-3">
                                                <CardTitle className="flex items-center gap-2">
                                                    <ListCheck className="size-4 shrink-0" /> Result
                                                </CardTitle>
                                            </CardHeader>
                                            <CardContent className="p-3 pt-0">
                                                <hr className="mt-0 mb-3" />
                                                <Markdown
                                                    className="prose-xs prose-fixed wrap-break-word"
                                                    searchValue={searchValue}
                                                >
                                                    {result}
                                                </Markdown>
                                            </CardContent>
                                        </Card>
                                    )}
                                </div>
                            )}
                        </div>
                    )}
                </div>
                <div className="border-red absolute top-0 left-[calc((--spacing(2.5))-0.5px)] h-full border-l"></div>
            </div>

            {hasSubtasks ? (
                <div className="flex flex-col">
                    {sortedSubtasks.map((subtask) => (
                        <FlowSubtask
                            key={subtask.id}
                            searchValue={searchValue}
                            stats={subtaskStats?.get(subtask.id)}
                            subtask={subtask}
                        />
                    ))}
                </div>
            ) : (
                <div className="text-muted-foreground mt-2 ml-6 text-xs">{describeMissingSubtasks(status)}</div>
            )}
        </div>
    );
}

export default memo(FlowTask);
