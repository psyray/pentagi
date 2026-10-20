import { useEffect } from 'react';

/**
 * Mobile browsers scroll the nearest scrollable ancestor to reveal the focused
 * element when the soft keyboard opens. For an interactive terminal that pushes
 * the pinned header (and its key row) out of view, because a sticky element
 * sticks to its scrollport, not to the visual viewport.
 *
 * When the visual viewport resizes (the keyboard opening or closing), snap those
 * ancestors back to the top so the pinned header stays on screen. The terminal
 * itself resizes through its own ResizeObserver, so nothing else is needed.
 */
export function useResetScrollOnKeyboard(ref: React.RefObject<HTMLElement | null>, enabled: boolean) {
    useEffect(() => {
        if (!enabled) {
            return;
        }

        const viewport = window.visualViewport;

        if (!viewport) {
            return;
        }

        let timer: null | ReturnType<typeof setTimeout> = null;

        const reset = () => {
            if (timer) {
                clearTimeout(timer);
            }

            // Wait for the keyboard animation and the browser's own scroll to
            // settle, otherwise our reset lands before the browser's and is undone.
            timer = setTimeout(() => {
                let element: HTMLElement | null = ref.current?.parentElement ?? null;

                while (element) {
                    if (element.scrollTop > 0) {
                        element.scrollTop = 0;
                    }

                    element = element.parentElement;
                }

                if (window.scrollY > 0) {
                    window.scrollTo(0, 0);
                }
            }, 120);
        };

        viewport.addEventListener('resize', reset);

        return () => {
            if (timer) {
                clearTimeout(timer);
            }

            viewport.removeEventListener('resize', reset);
        };
    }, [ref, enabled]);
}
