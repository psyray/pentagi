import { ListCheck, ListTodo } from 'lucide-react';
import { memo, useMemo, useState } from 'react';

import type { SubtaskExecutionStatsFragmentFragment, SubtaskFragmentFragment } from '@/graphql/types';

import Markdown from '@/components/shared/markdown';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { StatusType } from '@/graphql/types';
import { matchesSearchTerm } from '@/lib/text-filter';

import FlowSubtaskBadges from './flow-subtask-badges';
import FlowSubtaskExecution from './flow-subtask-execution';
import FlowTaskStatusIcon from './flow-task-status-icon';

interface FlowSubtaskProps {
    searchValue?: string;
    stats?: SubtaskExecutionStatsFragmentFragment;
    subtask: SubtaskFragmentFragment;
}

const hasExecution = (status: StatusType) =>
    [StatusType.Failed, StatusType.Finished, StatusType.Running].includes(status);

function FlowSubtask({ searchValue = '', stats, subtask }: FlowSubtaskProps) {
    const { budgetSeconds, description, evidence, id, result, status, title, type } = subtask;
    const [isDetailsVisible, setIsDetailsVisible] = useState(false);
    const showExecution = hasExecution(status);
    const hasDetails = Boolean(description) || Boolean(result) || showExecution;

    const searchChecks = useMemo(() => {
        const trimmedSearch = searchValue.trim();

        if (!trimmedSearch) {
            return { hasDescriptionMatch: false, hasResultMatch: false };
        }

        return {
            hasDescriptionMatch: matchesSearchTerm(description, trimmedSearch),
            hasResultMatch: matchesSearchTerm(result, trimmedSearch),
        };
    }, [searchValue, description, result]);

    const [prevSearchValue, setPrevSearchValue] = useState(searchValue);
    const [prevHasMatch, setPrevHasMatch] = useState(searchChecks.hasDescriptionMatch || searchChecks.hasResultMatch);

    const hasMatch = searchChecks.hasDescriptionMatch || searchChecks.hasResultMatch;

    if (searchValue !== prevSearchValue || hasMatch !== prevHasMatch) {
        setPrevSearchValue(searchValue);
        setPrevHasMatch(hasMatch);

        const trimmedSearch = searchValue.trim();

        if (trimmedSearch) {
            if (hasMatch) {
                setIsDetailsVisible(true);
            }
        } else {
            setIsDetailsVisible(false);
        }
    }

    return (
        <div className="group relative flex gap-2.5 pb-4 pl-0.5">
            <FlowTaskStatusIcon
                className="bg-background ring-border ring-background relative z-1 mt-px rounded-full ring-3"
                status={status}
                tooltip={`Subtask ID: ${id}`}
            />
            <div className="flex flex-1 flex-col gap-2">
                <div className="text-sm">
                    <Markdown
                        className="prose-fixed prose-sm wrap-break-word *:m-0 [&>p]:leading-tight"
                        searchValue={searchValue}
                    >
                        {title}
                    </Markdown>
                </div>

                <FlowSubtaskBadges
                    budgetSeconds={budgetSeconds}
                    evidence={evidence}
                    type={type}
                />

                {hasDetails && (
                    <div className="text-muted-foreground text-xs">
                        <div
                            className="cursor-pointer hover:underline"
                            onClick={() => setIsDetailsVisible(!isDetailsVisible)}
                        >
                            {isDetailsVisible ? 'Hide details' : 'Show details'}
                        </div>
                        {isDetailsVisible && (
                            <div className="mt-4 flex flex-col gap-4">
                                {description && (
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
                                                {description}
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
                                {showExecution && (
                                    <FlowSubtaskExecution
                                        budgetSeconds={budgetSeconds}
                                        stats={stats}
                                        status={status}
                                    />
                                )}
                            </div>
                        )}
                    </div>
                )}
            </div>
            <div className="absolute top-0 left-[calc((--spacing(2.5))-0.5px)] h-full border-l group-last:hidden"></div>
        </div>
    );
}

export default memo(FlowSubtask);
