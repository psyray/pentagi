import { useEffect, useState } from 'react';

export interface VisualViewportBox {
    height: number;
    offsetTop: number;
}

/**
 * Tracks the visual viewport (the region actually visible, above the soft
 * keyboard). A fullscreen overlay sized from this stays on screen when the
 * keyboard opens, which the layout viewport (and `dvh` on some browsers) does
 * not reliably reflect.
 */
export function useVisualViewport(): VisualViewportBox {
    const [box, setBox] = useState<VisualViewportBox>(readVisualViewport);

    useEffect(() => {
        const viewport = window.visualViewport;

        if (!viewport) {
            return;
        }

        const update = () => setBox({ height: viewport.height, offsetTop: viewport.offsetTop });

        viewport.addEventListener('resize', update);
        viewport.addEventListener('scroll', update);
        update();

        return () => {
            viewport.removeEventListener('resize', update);
            viewport.removeEventListener('scroll', update);
        };
    }, []);

    return box;
}

function readVisualViewport(): VisualViewportBox {
    if (typeof window === 'undefined') {
        return { height: 0, offsetTop: 0 };
    }

    const viewport = window.visualViewport;

    return {
        height: viewport?.height ?? window.innerHeight,
        offsetTop: viewport?.offsetTop ?? 0,
    };
}
