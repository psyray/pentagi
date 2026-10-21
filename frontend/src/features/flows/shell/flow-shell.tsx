import { Maximize2, Minimize2, Plus, Terminal as TerminalIcon, X } from 'lucide-react';
import { useRef, useState } from 'react';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { StatusType, TerminalType } from '@/graphql/types';
import { useBreakpoint } from '@/hooks/use-breakpoint';
import { cn } from '@/lib/utils';
import { useFlow } from '@/providers/flow-provider';

import { SHELL_OPTIONS, type ShellName } from './flow-shell-api';
import FlowShellTerminal, { type FlowShellTerminalHandle } from './flow-shell-terminal';
import { useFlowShell } from './use-flow-shell';
import { useResetScrollOnKeyboard } from './use-reset-scroll-on-keyboard';
import { useVisualViewport } from './use-visual-viewport';

const DEFAULT_COLS = 80;
const DEFAULT_ROWS = 24;

// Keys a mobile soft keyboard cannot produce, sent as raw terminal input. Kept
// in the sticky header so they are always reachable while using the shell.
const SPECIAL_KEYS: Array<{ label: string; sequence: string }> = [
    { label: 'Esc', sequence: '\x1b' },
    { label: 'Tab', sequence: '\t' },
    { label: '^C', sequence: '\x03' },
    { label: '^D', sequence: '\x04' },
    { label: '^Z', sequence: '\x1a' },
    { label: '↑', sequence: '\x1b[A' },
    { label: '↓', sequence: '\x1b[B' },
    { label: '→', sequence: '\x1b[C' },
    { label: '←', sequence: '\x1b[D' },
];

function FlowShell() {
    const { flowData, flowId } = useFlow();
    const { isDesktop, isMobile } = useBreakpoint();
    const { active, close, create, creating, error, handleExit, loading, select, shells } = useFlowShell(flowId);
    const [shell, setShell] = useState<ShellName>('zsh');
    const [fullscreen, setFullscreen] = useState(false);
    const terminalRef = useRef<FlowShellTerminalHandle | null>(null);
    const rootRef = useRef<HTMLDivElement>(null);
    const visualViewport = useVisualViewport();

    // Keep the pinned header (and its key row) on screen when the soft keyboard
    // pushes the scroll container up on mobile.
    useResetScrollOnKeyboard(rootRef, !isDesktop);

    const enterFullscreen = () => {
        setFullscreen(true);
        // Focus within the user gesture so the soft keyboard opens right away.
        terminalRef.current?.focus();
    };

    // Tapping the terminal auto-enters fullscreen on mobile only; on desktop (and
    // tablets) it only focuses. The fullscreen button stays available everywhere.
    const handleActivate = () => {
        if (isMobile && !fullscreen) {
            enterFullscreen();

            return;
        }

        terminalRef.current?.focus();
    };

    const toggleFullscreen = () => {
        if (fullscreen) {
            setFullscreen(false);

            return;
        }

        enterFullscreen();
    };

    // The shell targets the flow's primary container. Once the flow finishes its
    // container is removed, so no interactive terminal can be opened; grey the
    // whole section out rather than let the user hit a failed create.
    const status = flowData?.flow?.status;
    const primaryTerminal = flowData?.flow?.terminals?.find((terminal) => terminal.type === TerminalType.Primary);
    const flowEnded = status === StatusType.Finished || status === StatusType.Failed;
    const containerAvailable = flowData ? !flowEnded && primaryTerminal?.connected === true : true;

    const handleCreate = () => {
        void create(shell, DEFAULT_COLS, DEFAULT_ROWS);
    };

    const handleClose = () => {
        if (active) {
            void close(active.id);
        }
    };

    return (
        <div
            className={cn(
                fullscreen
                    ? 'bg-background fixed z-50 flex flex-col gap-2 overflow-hidden p-3'
                    : 'flex size-full min-h-0 flex-col gap-3',
            )}
            ref={rootRef}
            style={
                fullscreen
                    ? {
                          height: visualViewport.height,
                          left: 0,
                          right: 0,
                          top: visualViewport.offsetTop,
                      }
                    : undefined
            }
        >
            <div className="bg-background sticky top-0 z-10 flex flex-col gap-2 pr-4">
                <div className="flex flex-wrap items-center gap-2">
                    <Select
                        disabled={!containerAvailable || shells.length === 0}
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
                            disabled={!containerAvailable}
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
                            disabled={creating || !containerAvailable}
                            onClick={handleCreate}
                            size="sm"
                        >
                            <Plus />
                            New terminal
                        </Button>
                        <Button
                            aria-label={fullscreen ? 'Exit full screen' : 'Full screen'}
                            className={
                                fullscreen ? 'bg-primary text-primary-foreground hover:bg-primary/90' : undefined
                            }
                            onClick={toggleFullscreen}
                            size="icon-sm"
                            title={fullscreen ? 'Exit full screen' : 'Full screen'}
                            variant="ghost"
                        >
                            {fullscreen ? <Minimize2 /> : <Maximize2 />}
                        </Button>
                    </div>
                </div>

                {!isDesktop && active && containerAvailable && (
                    <div className="flex gap-1 overflow-x-auto pb-1">
                        {SPECIAL_KEYS.map((key) => (
                            // A real <button> receives the tap's click (a plain div
                            // does not in Gecko), and refocusing the terminal right
                            // after keeps the soft keyboard open.
                            <Button
                                key={key.label}
                                onClick={() => {
                                    terminalRef.current?.sendKey(key.sequence);
                                    terminalRef.current?.focus();
                                }}
                                size="xs"
                                type="button"
                                variant="outline"
                            >
                                {key.label}
                            </Button>
                        ))}
                    </div>
                )}
            </div>

            {error && <p className="text-destructive pr-4 text-sm">{error}</p>}

            {!containerAvailable ? (
                <div className="flex flex-1 items-center justify-center">
                    <Empty className="opacity-60">
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <TerminalIcon />
                            </EmptyMedia>
                            <EmptyTitle>Interactive terminals unavailable</EmptyTitle>
                            <EmptyDescription>
                                This flow has finished and its container has been removed. A shell is only available
                                while the flow&apos;s container is running.
                            </EmptyDescription>
                        </EmptyHeader>
                    </Empty>
                </div>
            ) : loading && shells.length === 0 ? (
                <div className="flex flex-1 items-center justify-center">
                    <Spinner />
                </div>
            ) : active && flowId ? (
                <div className="min-h-0 flex-1">
                    <FlowShellTerminal
                        flowId={flowId}
                        key={active.id}
                        onActivate={handleActivate}
                        onExit={handleExit}
                        ref={terminalRef}
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
                        <Button onClick={handleCreate}>
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
