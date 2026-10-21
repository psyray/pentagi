import { beforeEach, describe, expect, it } from 'vitest';

import {
    automationViewStateKey,
    emptyAutomationViewState,
    loadAutomationViewState,
    saveAutomationViewState,
} from './flow-automation-view-state';

describe('automation view state', () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it('round-trips a saved state', () => {
        const state = {
            agentStates: { '9|pentester': { open: true, watched: true } },
            expandedSubtasks: ['9', '10'],
            lastExpandedSubtaskId: '10',
        };

        saveAutomationViewState('42', state);

        expect(loadAutomationViewState('42')).toEqual(state);
    });

    it('answers the empty state when nothing was stored', () => {
        expect(loadAutomationViewState('42')).toEqual(emptyAutomationViewState);
    });

    it('answers the empty state on invalid or malformed data', () => {
        localStorage.setItem(automationViewStateKey('42'), 'not json');
        expect(loadAutomationViewState('42')).toEqual(emptyAutomationViewState);

        localStorage.setItem(automationViewStateKey('42'), JSON.stringify({ expandedSubtasks: 'nope' }));
        expect(loadAutomationViewState('42')).toEqual(emptyAutomationViewState);
    });

    it('defaults the collections of a partial payload', () => {
        localStorage.setItem(automationViewStateKey('42'), JSON.stringify({ expandedSubtasks: ['9'] }));

        const loaded = loadAutomationViewState('42');

        expect(loaded.expandedSubtasks).toEqual(['9']);
        expect(loaded.agentStates).toEqual({});
    });
});
