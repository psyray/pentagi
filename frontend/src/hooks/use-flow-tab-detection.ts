import { useCallback, useEffect, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

import { useSearchParamTab } from '@/hooks/use-search-param-tab';
import { type FlowTabSlot, recallTab, rememberTab } from '@/lib/flow-tab-memory';
import { getLatestSearchParams } from '@/lib/url-params';
import { useFlow } from '@/providers/flow-provider';

// Dashboard first: it is the tab a flow opens on by default.
export const CENTRAL_TAB_VALUES = ['dashboard', 'automation', 'assistant'];

export const SIDE_TAB_VALUES = [
    'terminal',
    'shell',
    'tasks',
    'agents',
    'tools',
    'vectorStores',
    'reports',
    'files',
    'screenshots',
];

export const FLOW_TAB_VALUES = [...CENTRAL_TAB_VALUES, ...SIDE_TAB_VALUES];

export const SIDE_TAB_PARAM = 'side';

const DEFAULT_CENTRAL_TAB = 'dashboard';
const DEFAULT_SIDE_TAB = 'terminal';

const readParam = (snapshot: URLSearchParams, key: string, values: readonly string[]): null | string => {
    const value = getLatestSearchParams(snapshot).get(key);

    return value && values.includes(value) ? value : null;
};

const writeParam = (key: string, value: string) => (previous: URLSearchParams) => {
    const params = getLatestSearchParams(previous);

    params.set(key, value);

    return params;
};

// rememberedTab resolves the tab to reopen for a flow: its own last tab first,
// then the last tab used on any flow, provided the consumer can render it.
const rememberedTab = (flowId: null | string, slot: FlowTabSlot, values: readonly string[]): null | string => {
    const own = recallTab(flowId, slot);

    if (own && values.includes(own)) {
        return own;
    }

    const other = recallTab(flowId, slot === 'central' ? 'side' : 'central');

    return other && values.includes(other) ? other : null;
};

export function useFlowSideTab() {
    const { flowData } = useFlow();
    const [searchParams] = useSearchParams();

    const flowId = flowData?.flow?.id ?? null;

    const linked = readParam(searchParams, 'tab', SIDE_TAB_VALUES);
    const named = readParam(searchParams, SIDE_TAB_PARAM, SIDE_TAB_VALUES);

    // A ?tab= deep link wins over memory; otherwise reopen this flow's side tab.
    const fallback = linked ?? rememberedTab(flowId, 'side', SIDE_TAB_VALUES) ?? DEFAULT_SIDE_TAB;

    const [resolvedTab, setSearchParamTab] = useSearchParamTab({
        fallback,
        key: SIDE_TAB_PARAM,
        resetKey: flowId,
        values: SIDE_TAB_VALUES,
    });

    // Persisting on the explicit pick (not on mount) keeps the default from being
    // written as if the user had chosen it.
    const handleTabChange = useCallback(
        (tab: string) => {
            setSearchParamTab(tab);

            if (flowId) {
                rememberTab(flowId, 'side', tab);
            }
        },
        [flowId, setSearchParamTab],
    );

    useEffect(() => {
        if (linked && !named) {
            handleTabChange(linked);
        }
    }, [handleTabChange, linked, named]);

    return { handleTabChange, resolvedTab };
}

export function useFlowTabDetection(tabValues: readonly string[] = CENTRAL_TAB_VALUES) {
    const { flowData } = useFlow();
    const [searchParams, setSearchParams] = useSearchParams();

    const flowId = flowData?.flow?.id ?? null;

    const resolvedTab = useMemo(() => {
        const tabParam = readParam(searchParams, 'tab', tabValues);

        if (tabParam) {
            return tabParam;
        }

        return rememberedTab(flowId, 'central', tabValues) ?? DEFAULT_CENTRAL_TAB;
    }, [searchParams, tabValues, flowId]);

    const handleTabChange = useCallback(
        (tab: string) => {
            setSearchParams((previous) => {
                const params = writeParam('tab', tab)(previous);

                if (tabValues.includes(tab) && SIDE_TAB_VALUES.includes(tab)) {
                    params.set(SIDE_TAB_PARAM, tab);
                }

                return params;
            });

            // Persist on the explicit pick, into the slot that matches the tab.
            if (flowId && tabValues.includes(tab)) {
                rememberTab(flowId, SIDE_TAB_VALUES.includes(tab) ? 'side' : 'central', tab);
            }
        },
        [flowId, setSearchParams, tabValues],
    );

    return { handleTabChange, resolvedTab };
}
