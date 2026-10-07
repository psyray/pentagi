import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import {
    assertDisjoint,
    assertNoDuplicates,
    attachIdSet,
    extractMessageIds,
    MESSAGE_ID_SELECTOR,
} from '../../helpers/subscriptions.ts';
import {
    FLOW_A_INITIAL_IDS,
    FLOW_A_STREAMED_IDS,
    FLOW_B_INITIAL_IDS,
    FLOW_B_SENTINEL_ID,
    FLOW_B_STREAMED_IDS,
    flowsCassette,
    PAGER_SWITCH_FLAG,
    pagerStreamsCassette,
} from '../../mocks/cassettes/flows.ts';
import { subscriptionStreamKey } from '../../mocks/world.ts';

const FLOW_A_IDS = [...FLOW_A_INITIAL_IDS, ...FLOW_A_STREAMED_IDS];
const FLOW_B_IDS = [...FLOW_B_INITIAL_IDS, ...FLOW_B_STREAMED_IDS];

test.describe('flow subscriptions', { tag: ['@flows', '@smoke'] }, () => {
    test.use({ cassette: flowsCassette() });

    test('streams messages without duplicates within a flow', async ({ page, pageErrorLog }, testInfo) => {
        await page.goto('/flows');
        await page.getByRole('row', { name: /E2E Alpha/ }).click();
        // The flow opens on Dashboard; the message log under test lives on Automation.
        await page.getByRole('tab', { name: 'Automation' }).click();

        await expect(page.locator(MESSAGE_ID_SELECTOR)).toHaveCount(FLOW_A_IDS.length);

        const ids = await extractMessageIds(page.locator('body'));

        assertNoDuplicates(ids);
        expect([...ids].sort()).toEqual([...FLOW_A_IDS].sort());
        await attachIdSet(testInfo, 'flow-5-message-ids', ids);
        expectCleanPage(pageErrorLog);
    });

    test('keeps concurrent flows exact-set disjoint across a round trip', async ({ page, pageErrorLog }, testInfo) => {
        await page.goto('/flows');
        await page.getByRole('row', { name: /E2E Alpha/ }).click();
        await page.getByRole('tab', { name: 'Automation' }).click();
        await expect(page.locator(MESSAGE_ID_SELECTOR)).toHaveCount(FLOW_A_IDS.length);

        const idsA = await extractMessageIds(page.locator('body'));

        // Back to the list via its own link: the explicit Automation pick above wrote a history
        // entry, so `goBack()` would reopen the flow rather than leave it.
        await page.getByRole('link', { exact: true, name: 'Flows' }).click();
        await page.getByRole('row', { name: /E2E Beta/ }).click();
        await page.getByRole('tab', { name: 'Automation' }).click();
        await expect(page.locator(MESSAGE_ID_SELECTOR)).toHaveCount(FLOW_B_IDS.length);

        const idsB = await extractMessageIds(page.locator('body'));

        assertNoDuplicates(idsB);
        assertDisjoint(idsA, idsB);
        await attachIdSet(testInfo, 'flow-5-message-ids', idsA);
        await attachIdSet(testInfo, 'flow-6-message-ids', idsB);

        await page.getByRole('link', { exact: true, name: 'Flows' }).click();
        await page.getByRole('row', { name: /E2E Alpha/ }).click();
        await page.getByRole('tab', { name: 'Automation' }).click();
        await expect(page.locator(MESSAGE_ID_SELECTOR)).toHaveCount(FLOW_A_IDS.length);

        const idsAgain = await extractMessageIds(page.locator('body'));

        expect([...idsAgain].sort(), 'round trip must not change the set').toEqual([...idsA].sort());
        expectCleanPage(pageErrorLog);
    });
});

/**
 * Leaving via `/flows` unmounts FlowProvider (app.tsx wraps only `flows/:flowId`), so stream A is
 * already closed there. Only the pager keeps the provider mounted and merely swaps the subscription
 * variables — the one path where a superseded stream can still write into the flow on screen.
 */
test.describe('flow subscriptions across a pager switch', { tag: '@flows' }, () => {
    test.use({ cassette: pagerStreamsCassette() });

    test('drops the stream it switched away from', async ({ page, pageErrorLog, world }, testInfo) => {
        const streamA = subscriptionStreamKey('messageLogAdded', { flowId: '5' });
        const streamB = subscriptionStreamKey('messageLogAdded', { flowId: '6' });

        await page.goto('/flows/5');
        // The flow opens on Dashboard; the message log under test lives on Automation.
        await page.getByRole('tab', { name: 'Automation' }).click();
        await expect(page.locator(MESSAGE_ID_SELECTOR)).toHaveCount(FLOW_A_IDS.length);
        expect(world.subscriberCount(streamA), 'flow A must be streaming before the switch').toBe(1);

        const idsA = await extractMessageIds(page.locator('body'));

        await page.locator('header').getByRole('button', { name: 'Next' }).click();
        // The pager steps to the sibling preserving the current search, so the tab it carries rides along.
        await expect(page).toHaveURL(/\/flows\/6(\?|$)/);
        await expect(page.locator(MESSAGE_ID_SELECTOR)).toHaveCount(FLOW_B_IDS.length);

        // The DOM cannot witness this leak: `messageLogs` is keyed by flowId, so a frame carrying
        // {flowId:'5'} is written to flow A's cache slot and never rendered under flow 6 however long
        // the stale stream stays open. The live subscriber set is the only place it shows.
        await expect.poll(() => world.subscriberCount(streamA), 'the superseded stream must be torn down').toBe(0);
        expect(world.subscriberCount(streamB), 'the switched-to flow must be streaming').toBe(1);

        world.raiseFlag(PAGER_SWITCH_FLAG);

        await expect(page.locator(MESSAGE_ID_SELECTOR).filter({ hasText: FLOW_B_SENTINEL_ID })).toHaveCount(1);

        const idsB = await extractMessageIds(page.locator('body'));

        expect([...idsB].sort(), 'the switched-to flow renders exactly its own stream').toEqual(
            [...FLOW_B_IDS, FLOW_B_SENTINEL_ID].sort(),
        );
        assertNoDuplicates(idsB);
        assertDisjoint(idsA, idsB);
        await attachIdSet(testInfo, 'pager-flow-6-message-ids', idsB);
        expectCleanPage(pageErrorLog);
    });
});
