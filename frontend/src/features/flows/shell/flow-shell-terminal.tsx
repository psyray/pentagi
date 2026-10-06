import { useCallback, useEffect, useRef, useState } from 'react';

import { useTheme } from '@/hooks/use-theme';
import { cn } from '@/lib/utils';

import { type FlowShellInfo, flowShellSocketUrl } from './flow-shell-api';
import { useInteractiveXterm } from './use-interactive-xterm';

type ConnectionStatus = 'closed' | 'connecting' | 'error' | 'open';

interface ControlMessage {
    code?: number;
    type: string;
}

interface FlowShellTerminalProps {
    className?: string;
    flowId: string;
    onExit: (terminalId: string, code?: number) => void;
    terminal: FlowShellInfo;
}

/**
 * A single interactive terminal. It owns one websocket for its whole lifetime;
 * because the backend keeps the shell alive independently, unmounting (tab
 * switch, navigation) only detaches — output produced meanwhile is replayed on
 * the next attach.
 */
function FlowShellTerminal({ className, flowId, onExit, terminal }: FlowShellTerminalProps) {
    const { theme } = useTheme();
    const socketRef = useRef<null | WebSocket>(null);
    const sizeRef = useRef<{ cols: number; rows: number }>({ cols: 80, rows: 24 });
    const onExitRef = useRef(onExit);
    const [status, setStatus] = useState<ConnectionStatus>('connecting');

    useEffect(() => {
        onExitRef.current = onExit;
    }, [onExit]);

    const sendResize = useCallback((cols: number, rows: number) => {
        const socket = socketRef.current;

        if (socket && socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify({ cols, rows, type: 'resize' }));
        }
    }, []);

    const { containerRef, focus, isReady, write } = useInteractiveXterm(theme, {
        onData: (data) => {
            const socket = socketRef.current;

            if (socket && socket.readyState === WebSocket.OPEN) {
                socket.send(new TextEncoder().encode(data));
            }
        },
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
        const socket = new WebSocket(flowShellSocketUrl(flowId, terminal.id));
        socket.binaryType = 'arraybuffer';
        socketRef.current = socket;

        socket.onopen = () => {
            if (disposed) {
                return;
            }

            setStatus('open');
            sendResize(sizeRef.current.cols, sizeRef.current.rows);
            focus();
        };

        socket.onmessage = (event: MessageEvent) => {
            if (typeof event.data === 'string') {
                try {
                    const message = JSON.parse(event.data) as ControlMessage;

                    if (message.type === 'exit') {
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
            if (!disposed) {
                setStatus('error');
            }
        };

        socket.onclose = () => {
            socketRef.current = null;

            if (!disposed) {
                setStatus((previous) => (previous === 'error' ? 'error' : 'closed'));
            }
        };

        return () => {
            disposed = true;
            socket.close();
            socketRef.current = null;
        };
    }, [flowId, terminal.id, isReady, write, focus, sendResize]);

    return (
        <div
            className={cn('relative size-full overflow-hidden rounded-md bg-[#050c13]', className)}
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
                    {status === 'error' && 'connection error'}
                </div>
            )}
        </div>
    );
}

export default FlowShellTerminal;
