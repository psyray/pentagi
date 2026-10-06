import { Plus, Terminal as TerminalIcon, X } from 'lucide-react';
import { useState } from 'react';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { useFlow } from '@/providers/flow-provider';

import { SHELL_OPTIONS, type ShellName } from './flow-shell-api';
import FlowShellTerminal from './flow-shell-terminal';
import { useFlowShell } from './use-flow-shell';

const DEFAULT_COLS = 80;
const DEFAULT_ROWS = 24;

function FlowShell() {
    const { flowId } = useFlow();
    const { active, close, create, creating, error, handleExit, loading, select, shells } = useFlowShell(flowId);
    const [shell, setShell] = useState<ShellName>('zsh');

    const handleCreate = () => {
        void create(shell, DEFAULT_COLS, DEFAULT_ROWS);
    };

    const handleClose = () => {
        if (active) {
            void close(active.id);
        }
    };

    return (
        <div className="flex size-full min-h-0 flex-col gap-3">
            <div className="bg-background sticky top-0 z-10 flex flex-wrap items-center gap-2 pr-4">
                <Select
                    disabled={shells.length === 0}
                    onValueChange={select}
                    value={active?.id ?? ''}
                >
                    <SelectTrigger className="w-[200px]">
                        <TerminalIcon className="size-4" />
                        <SelectValue placeholder="No terminals" />
                    </SelectTrigger>
                    <SelectContent>
                        {shells.map((terminal) => (
                            <SelectItem
                                key={terminal.id}
                                value={terminal.id}
                            >
                                {terminal.name}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                {active && (
                    <Button
                        aria-label="Close terminal"
                        onClick={handleClose}
                        size="icon-sm"
                        title="Close terminal"
                        variant="ghost"
                    >
                        <X />
                    </Button>
                )}
                <div className="ml-auto flex items-center gap-2">
                    <Select
                        onValueChange={(value) => setShell(value as ShellName)}
                        value={shell}
                    >
                        <SelectTrigger className="w-[110px]">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {SHELL_OPTIONS.map((option) => (
                                <SelectItem
                                    key={option}
                                    value={option}
                                >
                                    {option}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <Button
                        disabled={creating}
                        onClick={handleCreate}
                        size="sm"
                    >
                        <Plus />
                        New terminal
                    </Button>
                </div>
            </div>

            {error && <p className="text-destructive pr-4 text-sm">{error}</p>}

            {loading && shells.length === 0 ? (
                <div className="flex flex-1 items-center justify-center">
                    <Spinner />
                </div>
            ) : active && flowId ? (
                <div className="min-h-0 flex-1">
                    <FlowShellTerminal
                        flowId={flowId}
                        onExit={handleExit}
                        terminal={active}
                    />
                </div>
            ) : (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <TerminalIcon />
                        </EmptyMedia>
                        <EmptyTitle>No interactive terminals</EmptyTitle>
                        <EmptyDescription>
                            Open a shell of your own inside this flow&apos;s container to look around while the agents
                            work. Terminals close when the flow finishes and its container is removed.
                        </EmptyDescription>
                    </EmptyHeader>
                    <EmptyContent>
                        <Button
                            disabled={creating}
                            onClick={handleCreate}
                        >
                            <Plus />
                            New terminal
                        </Button>
                    </EmptyContent>
                </Empty>
            )}
        </div>
    );
}

export default FlowShell;
