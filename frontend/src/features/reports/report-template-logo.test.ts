import { describe, expect, it } from 'vitest';

import { reportTemplateLogoPath, reportTemplateLogoUrl } from './report-template-logo';

describe('reportTemplateLogoPath / reportTemplateLogoUrl', () => {
    it('points at the template logo endpoint', () => {
        expect(reportTemplateLogoPath('7')).toBe('/report-templates/7/branding/logo');
        expect(reportTemplateLogoPath(7)).toBe('/report-templates/7/branding/logo');
    });

    it('carries the API base for an <img> src', () => {
        expect(reportTemplateLogoUrl('7')).toBe('/api/v1/report-templates/7/branding/logo');
    });

    it('busts the cache after an upload without changing the URL path', () => {
        expect(reportTemplateLogoUrl('7', 1234)).toBe('/api/v1/report-templates/7/branding/logo?v=1234');
    });
});
