import { api, unwrapApiResponse } from '@/lib/axios';
import { baseUrl } from '@/models/api';

/** Supported shells, first entry is the default (matches the backend). */
export const SHELL_OPTIONS = ['zsh', 'bash', 'sh'] as const;
export interface FlowShellInfo {
    createdAt: string;
    id: string;
    name: string;
    running: boolean;
    shell: string;
}

export type ShellName = (typeof SHELL_OPTIONS)[number];

interface FlowShellListResponse {
    shells: FlowShellInfo[];
    total: number;
}

export async function createFlowShell(
    flowId: string,
    shell: string,
    cols: number,
    rows: number,
): Promise<FlowShellInfo> {
    const response = await api.post<FlowShellInfo>(`/flows/${flowId}/shell/`, { cols, rows, shell });

    return unwrapApiResponse(response);
}

export async function deleteFlowShell(flowId: string, terminalId: string): Promise<void> {
    await api.delete(`/flows/${flowId}/shell/${terminalId}`);
}

/** Builds the websocket URL for a terminal (session cookie auth). */
export function flowShellSocketUrl(flowId: string, terminalId: string): string {
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';

    return `${scheme}//${window.location.host}${baseUrl}/flows/${flowId}/shell/${terminalId}/ws`;
}

export async function listFlowShells(flowId: string): Promise<FlowShellInfo[]> {
    const response = await api.get<FlowShellListResponse>(`/flows/${flowId}/shell/`);

    return unwrapApiResponse(response).shells ?? [];
}
