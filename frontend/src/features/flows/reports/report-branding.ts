// Per-report export branding (plan § 6.6 / P6). The logo is served by the API
// with the session cookie, so the reader renders it in an <img> and the
// exporters fetch it as bytes — it never travels through GraphQL.

export const BRANDING_LOGO_MAX_BYTES = 256 * 1024;
export const BRANDING_LOGO_TYPES = ['image/jpeg', 'image/png'];
export const BRANDING_LOGO_MAX_WIDTH = 150;

export interface BrandingLogoFile {
    dataUrl: string;
    mediaType: string;
    name: string;
}

export const reportBrandingLogoPath = (flowId: null | number | string, reportId: null | number | string): string =>
    `/flows/${flowId}/reports/${reportId}/branding/logo`;

// validateBrandingLogoFile mirrors the server's rules so the user learns about a
// rejected file before the round trip; the server still sniffs the real type.
export const validateBrandingLogoFile = (file: { size: number; type: string }): null | string => {
    if (!BRANDING_LOGO_TYPES.includes(file.type)) {
        return 'Choose a PNG or JPEG image.';
    }

    if (file.size > BRANDING_LOGO_MAX_BYTES) {
        return 'The logo must stay under 256 KB.';
    }

    return null;
};

export const readFileAsDataUrl = (file: File): Promise<string> =>
    new Promise((resolve, reject) => {
        const reader = new FileReader();

        reader.onload = () => resolve(String(reader.result ?? ''));
        reader.onerror = () => reject(new Error('the file could not be read'));
        reader.readAsDataURL(file);
    });

// measureImage reads the intrinsic size of an image; the Word renderer needs
// both dimensions. Browser-only (jsdom never fires load).
export const measureImage = (dataUrl: string): Promise<{ height: number; width: number }> =>
    new Promise((resolve, reject) => {
        const image = new Image();

        image.onload = () => resolve({ height: image.naturalHeight, width: image.naturalWidth });
        image.onerror = () => reject(new Error('the logo could not be decoded'));
        image.src = dataUrl;
    });
