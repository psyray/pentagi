/**
 * True on touch-first devices (phones, tablets, and desktop browsers emulating
 * touch). Used to switch the terminal to an input path that copes with soft
 * keyboards and their IMEs.
 */
export function isCoarsePointer(): boolean {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
        return false;
    }

    return window.matchMedia('(pointer: coarse)').matches;
}

export function isMac(): boolean {
    if (typeof navigator === 'undefined') {
        return false;
    }

    const platform =
        'userAgentData' in navigator
            ? ((navigator as { userAgentData?: { platform?: string } }).userAgentData?.platform ?? '')
            : (navigator.platform ?? '');

    return /Mac|iPhone|iPad/i.test(platform) || /Mac/i.test(navigator.userAgent);
}
