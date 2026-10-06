import { ChevronDown, ChevronUp, Search, X } from 'lucide-react';
import { useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useBreakpoint } from '@/hooks/use-breakpoint';
import { useTheme } from '@/hooks/use-theme';
import { cn } from '@/lib/utils';

import { type FlowShellInfo, flowShellSocketUrl } from './flow-shell-api';
import { useInteractiveXterm } from './use-interactive-xterm';

// Imperative surface the parent uses to forward the on-screen keys row, which
// lives in the sticky header rather than inside the terminal.
export interface FlowShellTerminalHandle {
    sendKey: (sequence: string) => void;
}

type ConnectionStatus = 'closed' | 'connecting' | 'error' | 'open';

interface ControlMessage {
    code?: number;
    type: string;
}

interface FlowShellTerminalProps {
    className?: string;
    flowId: string;
    onExit: (terminalId: string, code?: number) => void;
    ref?: React.RefObject<FlowShellTerminalHandle | null>;
    terminal: FlowShellInfo;
}

const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 15000;

/**
 * A single interactive terminal: a websocket bridge to a server-side shell plus
 * a light search bar. Because the backend keeps the shell alive independently,
 * unmounting only detaches — output produced meanwhile is replayed on the next
 * attach, and a dropped connection reconnects with backoff.
 */
function FlowShellTerminal({ className, flowId, onExit, ref, terminal }: FlowShellTerminalProps) {
    const { theme } = useTheme();
    const { isDesktop } = useBreakpoint();
    const socketRef = useRef<null | WebSocket>(null);
    const sizeRef = useRef<{ cols: number; rows: number }>({ cols: 80, rows: 24 });
    const onExitRef = useRef(onExit);
    const [status, setStatus] = useState<ConnectionStatus>('connecting');
    const [search, setSearch] = useState('');

    useEffect(() => {
        onExitRef.current = onExit;
    }, [onExit]);

    const send = useCallback((data: string) => {
        const socket = socketRef.current;

        if (socket && socket.readyState === WebSocket.OPEN) {
            socket.send(new TextEncoder().encode(data));
        }
    }, []);

    useImperativeHandle(ref, () => ({ sendKey: send }), [send]);

    const sendResize = useCallback((cols: number, rows: number) => {
        const socket = socketRef.current;

        if (socket && socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify({ cols, rows, type: 'resize' }));
        }
    }, []);

    const { containerRef, focus, isReady, searchAddon, write } = useInteractiveXterm(theme, {
        onData: (data) => send(data),
        onResize: (cols, rows) => {
            sizeRef.current = { cols, rows };
            sendResize(cols, rows);
        },
    });

    useEffect(() => {
        if (!isReady) {
            return;
        }

        let disposed = false;
        let exited = false;
        let attempts = 0;
        let reconnectTimer: null | ReturnType<typeof setTimeout> = null;
        let socket: null | WebSocket = null;

        const connect = () => {
            if (disposed) {
                return;
            }

            socket = new WebSocket(flowShellSocketUrl(flowId, terminal.id));
            socket.binaryType = 'arraybuffer';
            socketRef.current = socket;

            socket.onopen = () => {
                if (disposed) {
                    return;
                }

                attempts = 0;
                setStatus('open');
                sendResize(sizeRef.current.cols, sizeRef.current.rows);

                // Auto-focus steals the soft keyboard on mobile; only do it on
                // a real keyboard.
                if (isDesktop) {
                    focus();
                }
            };

            socket.onmessage = (event: MessageEvent) => {
                if (typeof event.data === 'string') {
                    try {
                        const message = JSON.parse(event.data) as ControlMessage;

                        if (message.type === 'exit') {
                            exited = true;
                            setStatus('closed');
                            onExitRef.current(terminal.id, message.code);
                        }
                    } catch {
                        /* ignore malformed control frames */
                    }

                    return;
                }

                write(new Uint8Array(event.data as ArrayBuffer));
            };

            socket.onerror = () => {
                // The close event carries the retry; only surface the failure.
                if (!disposed) {
                    setStatus('error');
                }
            };

            socket.onclose = () => {
                socketRef.current = null;

                if (disposed || exited) {
                    return;
                }

                setStatus('connecting');
                const delay = Math.min(RECONNECT_BASE_MS * 2 ** attempts, RECONNECT_MAX_MS);
                attempts += 1;
                reconnectTimer = setTimeout(connect, delay);
            };
        };

        connect();

        return () => {
            disposed = true;

            if (reconnectTimer) {
                clearTimeout(reconnectTimer);
            }

            socket?.close();
            socketRef.current = null;
        };
    }, [flowId, terminal.id, isReady, write, focus, sendResize, isDesktop]);

    const runSearch = useCallback(
        (direction: 'next' | 'prev') => {
            const term = search.trim();

            if (!term || !searchAddon) {
                return;
            }

            if (direction === 'next') {
                searchAddon.findNext(term);
            } else {
                searchAddon.findPrevious(term);
            }
        },
        [search, searchAddon],
    );

    const clearSearch = useCallback(() => {
        setSearch('');
        searchAddon?.clearDecorations();
    }, [searchAddon]);

    return (
        <div className={cn('flex size-full min-h-0 flex-col gap-2', className)}>
            <div className="flex items-center gap-2">
                <div className="relative flex-1">
                    <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-4 -translate-y-1/2" />
                    <Input
                        className="h-8 pl-8"
                        onChange={(event) => setSearch(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === 'Enter') {
                                event.preventDefault();
                                runSearch(event.shiftKey ? 'prev' : 'next');
                            }
                        }}
                        placeholder="Search terminal…"
                        value={search}
                    />
                </div>
                <Button
                    aria-label="Previous match"
                    disabled={!search.trim()}
                    onClick={() => runSearch('prev')}
                    size="icon-sm"
                    title="Previous match"
                    type="button"
                    variant="ghost"
                >
                    <ChevronUp />
                </Button>
                <Button
                    aria-label="Next match"
                    disabled={!search.trim()}
                    onClick={() => runSearch('next')}
                    size="icon-sm"
                    title="Next match"
                    type="button"
                    variant="ghost"
                >
                    <ChevronDown />
                </Button>
                {search && (
                    <Button
                        aria-label="Clear search"
                        onClick={clearSearch}
                        size="icon-sm"
                        title="Clear search"
                        type="button"
                        variant="ghost"
                    >
                        <X />
                    </Button>
                )}
            </div>

            <div
                className="relative min-h-0 flex-1 overflow-hidden rounded-md bg-[#050c13]"
                onClick={focus}
            >
                <div
                    className="size-full"
                    ref={containerRef}
                    style={{ contain: 'strict' }}
                />
                {status !== 'open' && (
                    <div className="text-muted-foreground pointer-events-none absolute top-2 right-3 text-xs">
                        {status === 'connecting' && 'connecting…'}
                        {status === 'closed' && 'disconnected'}
                        {status === 'error' && 'reconnecting…'}
                    </div>
                )}
            </div>
        </div>
    );
}

export default FlowShellTerminal;
