import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { createFlowShell, deleteFlowShell, type FlowShellInfo, listFlowShells } from './flow-shell-api';
import { useFlowShell } from './use-flow-shell';

vi.mock('./flow-shell-api', () => ({
    createFlowShell: vi.fn(),
    deleteFlowShell: vi.fn(),
    flowShellSocketUrl: vi.fn(() => 'ws://test/ws'),
    listFlowShells: vi.fn(),
}));

const makeShell = (id: string, name: string): FlowShellInfo => ({
    createdAt: '2026-01-01T00:00:00Z',
    id,
    name,
    running: true,
    shell: 'zsh',
});

describe('useFlowShell', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('lists the terminals and selects the first', async () => {
        vi.mocked(listFlowShells).mockResolvedValue([makeShell('a', 'terminal 1'), makeShell('b', 'terminal 2')]);

        const { result } = renderHook(() => useFlowShell('1'));

        await waitFor(() => expect(result.current.shells).toHaveLength(2));
        expect(result.current.active?.id).toBe('a');
    });

    it('creates a terminal and selects it', async () => {
        vi.mocked(listFlowShells).mockResolvedValue([]);
        vi.mocked(createFlowShell).mockResolvedValue(makeShell('c', 'terminal 1'));

        const { result } = renderHook(() => useFlowShell('1'));
        await waitFor(() => expect(listFlowShells).toHaveBeenCalledWith('1'));

        await act(async () => {
            await result.current.create('zsh', 80, 24);
        });

        expect(createFlowShell).toHaveBeenCalledWith('1', 'zsh', 80, 24);
        expect(result.current.shells).toHaveLength(1);
        expect(result.current.active?.id).toBe('c');
    });

    it('forwards a too-many-terminals failure as a clear error', async () => {
        vi.mocked(listFlowShells).mockResolvedValue([]);
        vi.mocked(createFlowShell).mockRejectedValue({ statusCode: 409 });

        const { result } = renderHook(() => useFlowShell('1'));
        await waitFor(() => expect(listFlowShells).toHaveBeenCalled());

        await act(async () => {
            await result.current.create('bash', 80, 24);
        });

        expect(result.current.shells).toHaveLength(0);
        expect(result.current.error).toMatch(/too many terminals/i);
    });

    it('closes a terminal and removes it from the list', async () => {
        vi.mocked(listFlowShells).mockResolvedValue([makeShell('a', 'terminal 1'), makeShell('b', 'terminal 2')]);
        vi.mocked(deleteFlowShell).mockResolvedValue(undefined);

        const { result } = renderHook(() => useFlowShell('1'));
        await waitFor(() => expect(result.current.shells).toHaveLength(2));

        await act(async () => {
            await result.current.close('a');
        });

        expect(deleteFlowShell).toHaveBeenCalledWith('1', 'a');
        expect(result.current.shells.map((shell) => shell.id)).toEqual(['b']);
        expect(result.current.active?.id).toBe('b');
    });

    it('drops a terminal whose process exited', async () => {
        vi.mocked(listFlowShells).mockResolvedValue([makeShell('a', 'terminal 1'), makeShell('b', 'terminal 2')]);

        const { result } = renderHook(() => useFlowShell('1'));
        await waitFor(() => expect(result.current.shells).toHaveLength(2));

        act(() => {
            result.current.handleExit('a', 0);
        });

        expect(result.current.shells.map((shell) => shell.id)).toEqual(['b']);
    });
});
