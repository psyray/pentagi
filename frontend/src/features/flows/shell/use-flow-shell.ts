import { useCallback, useEffect, useMemo, useState } from 'react';

import { getApiErrorMessage, getApiErrorStatusCode } from '@/lib/axios';
import { Log } from '@/lib/log';

import { createFlowShell, deleteFlowShell, type FlowShellInfo, listFlowShells } from './flow-shell-api';

export interface UseFlowShellResult {
    active: FlowShellInfo | null;
    close: (terminalId: string) => Promise<void>;
    create: (shell: string, cols: number, rows: number) => Promise<FlowShellInfo | null>;
    creating: boolean;
    error: null | string;
    handleExit: (terminalId: string, code?: number) => void;
    loading: boolean;
    refresh: () => Promise<void>;
    select: (terminalId: string) => void;
    shells: FlowShellInfo[];
}

/**
 * Owns the flow's terminal list and the selected terminal. The backend keeps
 * the shells alive, so this hook only reconciles the list with the server; the
 * websocket itself lives in each terminal component.
 */
export function useFlowShell(flowId: null | string): UseFlowShellResult {
    const [shells, setShells] = useState<FlowShellInfo[]>([]);
    const [activeId, setActiveId] = useState<null | string>(null);
    const [loading, setLoading] = useState(false);
    const [creating, setCreating] = useState(false);
    const [error, setError] = useState<null | string>(null);

    const refresh = useCallback(async () => {
        if (!flowId) {
            setShells([]);

            return;
        }

        setLoading(true);

        try {
            const list = await listFlowShells(flowId);
            setShells(list);
            setError(null);
        } catch (err: unknown) {
            Log.error('Failed to list flow shells:', err);
            setError(getApiErrorMessage(err, 'Unable to load terminals'));
        } finally {
            setLoading(false);
        }
    }, [flowId]);

    useEffect(() => {
        // refresh reconciles the terminal list from the REST endpoint; it only
        // sets state around the awaited fetch, never during render.
        // eslint-disable-next-line react-hooks/set-state-in-effect
        void refresh();
    }, [refresh]);

    const active = useMemo(
        () => shells.find((shell) => shell.id === activeId) ?? shells[0] ?? null,
        [shells, activeId],
    );

    const select = useCallback((terminalId: string) => setActiveId(terminalId), []);

    const create = useCallback(
        async (shell: string, cols: number, rows: number) => {
            if (!flowId) {
                return null;
            }

            setCreating(true);

            try {
                const created = await createFlowShell(flowId, shell, cols, rows);
                setShells((previous) => [...previous, created]);
                setActiveId(created.id);
                setError(null);

                return created;
            } catch (err: unknown) {
                const status = getApiErrorStatusCode(err);
                setError(
                    status === 409
                        ? 'Too many terminals for this flow. Close one first.'
                        : getApiErrorMessage(err, 'Unable to create a terminal'),
                );

                return null;
            } finally {
                setCreating(false);
            }
        },
        [flowId],
    );

    const close = useCallback(
        async (terminalId: string) => {
            if (!flowId) {
                return;
            }

            try {
                await deleteFlowShell(flowId, terminalId);
            } catch (err: unknown) {
                // A terminal that already exited server-side is gone from the
                // list a moment later anyway; surface real failures only.
                Log.error('Failed to close flow shell:', err);
            } finally {
                setShells((previous) => previous.filter((shell) => shell.id !== terminalId));
            }
        },
        [flowId],
    );

    // The server told us the shell process exited (container stopped, `exit`,
    // ...) — drop it from the list so the UI stays truthful.
    const handleExit = useCallback((terminalId: string) => {
        setShells((previous) => previous.filter((shell) => shell.id !== terminalId));
    }, []);

    return { active, close, create, creating, error, handleExit, loading, refresh, select, shells };
}
