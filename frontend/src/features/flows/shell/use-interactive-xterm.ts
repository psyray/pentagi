import '@xterm/xterm/css/xterm.css';
import type { ILinkHandler } from '@xterm/xterm';

import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { WebglAddon } from '@xterm/addon-webgl';
import { Terminal } from '@xterm/xterm';
import { useCallback, useEffect, useRef, useState } from 'react';

import {
    getTerminalTheme,
    INTERACTIVE_TERMINAL_OPTIONS,
    isDarkMode,
} from '@/components/shared/terminal/terminal-config';
import { SAFE_PROTOCOLS } from '@/components/shared/terminal/terminal-sanitizer';
import { Log } from '@/lib/log';
import { isMac } from '@/lib/utils/platform';

const RESIZE_DEBOUNCE_MS = 150;

export interface InteractiveXtermCallbacks {
    /** Raw keystrokes from the user (already decoded by xterm). */
    onData: (data: string) => void;
    /** Fired after a fit/resize so the caller can push the new size to the PTY. */
    onResize: (cols: number, rows: number) => void;
}

export interface UseInteractiveXtermResult {
    clear: () => void;
    containerRef: React.RefObject<HTMLDivElement | null>;
    fit: () => void;
    focus: () => void;
    isReady: boolean;
    searchAddon: null | SearchAddon;
    write: (data: string | Uint8Array) => void;
}

export function useInteractiveXterm(
    theme: 'dark' | 'light' | 'system',
    callbacks: InteractiveXtermCallbacks,
): UseInteractiveXtermResult {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const terminalRef = useRef<null | Terminal>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const themeRef = useRef(theme);
    const callbacksRef = useRef(callbacks);
    const [isReady, setIsReady] = useState(false);
    const [searchAddon, setSearchAddon] = useState<null | SearchAddon>(null);

    useEffect(() => {
        themeRef.current = theme;
    }, [theme]);

    useEffect(() => {
        callbacksRef.current = callbacks;
    }, [callbacks]);

    const write = useCallback((data: string | Uint8Array) => {
        try {
            terminalRef.current?.write(data);
        } catch (error: unknown) {
            Log.error('Terminal write failed:', error);
        }
    }, []);

    const clear = useCallback(() => {
        try {
            terminalRef.current?.clear();
        } catch (error: unknown) {
            Log.error('Terminal clear failed:', error);
        }
    }, []);

    const focus = useCallback(() => {
        try {
            terminalRef.current?.focus();
        } catch (error: unknown) {
            Log.error('Terminal focus failed:', error);
        }
    }, []);

    const fit = useCallback(() => {
        const terminal = terminalRef.current;
        const fitAddon = fitAddonRef.current;

        if (!terminal || !fitAddon) {
            return;
        }

        try {
            if ((containerRef.current?.offsetHeight ?? 0) > 0) {
                fitAddon.fit();
                callbacksRef.current.onResize(terminal.cols, terminal.rows);
            }
        } catch (error: unknown) {
            Log.error('Terminal fit failed:', error);
        }
    }, []);

    useEffect(() => {
        const container = containerRef.current;

        if (!container) {
            return;
        }

        let mounted = true;

        const terminal = new Terminal({
            ...INTERACTIVE_TERMINAL_OPTIONS,
            theme: getTerminalTheme(isDarkMode(themeRef.current)),
        });

        const fitAddon = new FitAddon();
        const search = new SearchAddon();
        const unicodeAddon = new Unicode11Addon();
        const mac = isMac();

        const openLink = (event: MouseEvent, uri: string) => {
            const uriLower = uri.toLowerCase();

            if ((mac ? event.metaKey : event.ctrlKey) && SAFE_PROTOCOLS.some((p) => uriLower.startsWith(p))) {
                window.open(uri, '_blank', 'noopener,noreferrer');
            }
        };

        const linkHandler: ILinkHandler = {
            activate: (event, text) => openLink(event, text),
            allowNonHttpProtocols: true,
        };

        terminal.options.linkHandler = linkHandler;
        terminal.loadAddon(fitAddon);
        terminal.loadAddon(search);
        terminal.loadAddon(unicodeAddon);
        terminal.unicode.activeVersion = '11';
        terminal.loadAddon(new WebLinksAddon(openLink));

        const disposables: Array<{ dispose: () => void }> = [unicodeAddon];

        try {
            const webglAddon = new WebglAddon();
            terminal.loadAddon(webglAddon);
            disposables.push(webglAddon);
            disposables.push(webglAddon.onContextLoss(() => webglAddon.dispose()));
        } catch {
            /* WebGL unavailable — the canvas renderer is used as a fallback */
        }

        const onDataDisposable = terminal.onData((data) => callbacksRef.current.onData(data));

        let debounceTimer: null | ReturnType<typeof setTimeout> = null;
        const resizeObserver = new ResizeObserver(() => {
            if (debounceTimer) {
                clearTimeout(debounceTimer);
            }

            debounceTimer = setTimeout(() => {
                try {
                    if (container.offsetHeight > 0) {
                        fitAddon.fit();
                        callbacksRef.current.onResize(terminal.cols, terminal.rows);
                    }
                } catch (error: unknown) {
                    Log.error('Terminal fit failed:', error);
                }
            }, RESIZE_DEBOUNCE_MS);
        });

        terminalRef.current = terminal;
        fitAddonRef.current = fitAddon;

        const rafId = requestAnimationFrame(() => {
            if (!mounted) {
                return;
            }

            try {
                terminal.open(container);
                resizeObserver.observe(container);

                if (container.offsetHeight > 0) {
                    fitAddon.fit();
                }

                setSearchAddon(search);
                setIsReady(true);
            } catch (error: unknown) {
                Log.error('Failed to open terminal:', error);
            }
        });

        return () => {
            mounted = false;
            cancelAnimationFrame(rafId);

            if (debounceTimer) {
                clearTimeout(debounceTimer);
            }

            onDataDisposable.dispose();
            resizeObserver.disconnect();

            for (const d of disposables) {
                try {
                    d.dispose();
                } catch {
                    /* ignore */
                }
            }

            try {
                search.dispose();
            } catch {
                /* ignore */
            }

            try {
                fitAddon.dispose();
            } catch {
                /* ignore */
            }

            try {
                terminal.dispose();
            } catch {
                /* ignore */
            }

            terminalRef.current = null;
            fitAddonRef.current = null;
            setSearchAddon(null);
            setIsReady(false);
        };
    }, []);

    useEffect(() => {
        const terminal = terminalRef.current;

        if (!terminal) {
            return;
        }

        const applyTheme = () => {
            try {
                terminal.options.theme = getTerminalTheme(isDarkMode(theme));
            } catch (error: unknown) {
                Log.error('Terminal theme update failed:', error);
            }
        };

        applyTheme();

        if (theme === 'system') {
            const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
            const handler = () => applyTheme();
            mediaQuery.addEventListener('change', handler);

            return () => {
                mediaQuery.removeEventListener('change', handler);
            };
        }
    }, [theme]);

    return { clear, containerRef, fit, focus, isReady, searchAddon, write };
}
