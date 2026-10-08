import { describe, expect, it } from 'vitest';

import { BRANDING_LOGO_MAX_BYTES, reportBrandingLogoPath, validateBrandingLogoFile } from './report-branding';

describe('validateBrandingLogoFile', () => {
    it('accepts a small png or jpeg', () => {
        expect(validateBrandingLogoFile({ size: 1024, type: 'image/png' })).toBeNull();
        expect(validateBrandingLogoFile({ size: 1024, type: 'image/jpeg' })).toBeNull();
    });

    it('rejects a format the renderers cannot draw', () => {
        expect(validateBrandingLogoFile({ size: 1024, type: 'image/svg+xml' })).toContain('PNG or JPEG');
    });

    it('rejects a logo over the cap', () => {
        expect(validateBrandingLogoFile({ size: BRANDING_LOGO_MAX_BYTES + 1, type: 'image/png' })).toContain('256 KB');
    });
});

describe('reportBrandingLogoPath', () => {
    it('points at the API endpoint the reader and the exporters share', () => {
        expect(reportBrandingLogoPath('152', 11)).toBe('/flows/152/reports/11/branding/logo');
        expect(reportBrandingLogoPath(152, 11)).toBe('/flows/152/reports/11/branding/logo');
    });
});
