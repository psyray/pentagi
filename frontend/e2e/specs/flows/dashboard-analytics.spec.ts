import type { Page } from '@playwright/test';

import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { flowDashboardCassette } from '../../mocks/cassettes/flows.ts';

// Opens a flow's central Dashboard tab, then its Analytics sub-tab. The cassette
// mocks kgDashboardEnabled, the four attack-graph views and every metric block.
const openAnalytics = async (page: Page) => {
    await page.goto('/flows/5');
    await expect(page.locator('header').getByRole('button', { name: 'Toggle favorite' })).toBeEnabled();

    await page.getByRole('tab', { name: 'Dashboard' }).click();
    await expect(page.getByRole('heading', { name: 'Usage by Agent Type' })).toBeVisible();

    await page.getByRole('tab', { name: 'Analytics' }).click();
};

test.describe('flow dashboard analytics', { tag: '@flows' }, () => {
    test.use({ cassette: flowDashboardCassette() });

    test('renders the main attack chain, its legend and the metric blocks', async ({ page, pageErrorLog }) => {
        await openAnalytics(page);

        // Main Attack Chain is the default view.
        await expect(page.getByRole('heading', { name: 'Main attack chain' })).toBeVisible();

        // Legend chips (one per entity type present in the mocked graph).
        await expect(page.getByRole('button', { exact: true, name: 'Host' })).toBeVisible();
        await expect(page.getByRole('button', { exact: true, name: 'Port' })).toBeVisible();
        await expect(page.getByRole('button', { exact: true, name: 'Endpoint' })).toBeVisible();

        // Replay timeline.
        await expect(page.getByLabel('Replay timeline')).toBeVisible();
        await expect(page.getByText('Visible', { exact: true })).toBeVisible();

        // Textual metric blocks below the graph.
        await expect(page.getByRole('heading', { name: 'Attack surface overview' })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Credentials status' })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Infrastructure map' })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Vulnerability breakdown' })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('switching graph views re-renders the matching chain', async ({ page, pageErrorLog }) => {
        await openAnalytics(page);

        await page.getByRole('tab', { name: 'Action Chain' }).click();
        await expect(page.getByRole('heading', { name: 'Action chain' })).toBeVisible();

        await page.getByRole('tab', { name: 'Infrastructure' }).click();
        await expect(page.getByRole('heading', { exact: true, name: 'Infrastructure' })).toBeVisible();

        await page.getByRole('tab', { name: 'Priv Esc Chain' }).click();
        await expect(page.getByRole('heading', { name: 'Privilege-escalation chain' })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('clicking a node opens the detail panel with its properties', async ({ page, pageErrorLog }) => {
        await openAnalytics(page);

        const endpoint = page.locator('.react-flow__node', { hasText: '/flag' });

        await expect(endpoint).toBeVisible();
        await endpoint.click();

        await expect(page.getByText('Properties', { exact: true })).toBeVisible();
        await expect(page.getByText('method', { exact: true })).toBeVisible();
        await expect(page.getByText('POST', { exact: true })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });
});
