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

export interface InteractiveXtermOptions {
    /**
     * Route input through a dedicated field so soft-keyboard IMEs (autocorrect,
     * word suggestions) cannot corrupt what reaches the terminal.
     */
    mobileInput?: boolean;
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
    options: InteractiveXtermOptions = {},
): UseInteractiveXtermResult {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const terminalRef = useRef<null | Terminal>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const mobileInputElRef = useRef<HTMLTextAreaElement | null>(null);
    const mobileInputEnabledRef = useRef(options.mobileInput ?? false);
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

    useEffect(() => {
        mobileInputEnabledRef.current = options.mobileInput ?? false;
    }, [options.mobileInput]);

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
        const mobileInput = mobileInputElRef.current;

        if (mobileInput) {
            try {
                mobileInput.focus({ preventScroll: true });

                return;
            } catch (error: unknown) {
                Log.error('Terminal focus failed:', error);
            }
        }

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
        let teardownMobileInput: (() => void) | null = null;

        const terminal = new Terminal({
            ...INTERACTIVE_TERMINAL_OPTIONS,
            disableStdin: mobileInputEnabledRef.current,
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

                if (mobileInputEnabledRef.current) {
                    // xterm would otherwise send the IME's autocorrected commit on
                    // top of what was typed; own the input instead. Its textarea is
                    // disabled so it cannot steal focus back (readOnly still can).
                    const textarea = terminal.textarea;

                    if (textarea) {
                        textarea.disabled = true;
                        textarea.tabIndex = -1;
                    }

                    teardownMobileInput = setupMobileInput(
                        container,
                        terminal,
                        (data) => callbacksRef.current.onData(data),
                        mobileInputElRef,
                    );
                }

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

            if (teardownMobileInput) {
                teardownMobileInput();
            }

            mobileInputElRef.current = null;

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

/**
 * Builds the terminal's mobile input field and wires it to `emit`.
 *
 * The field is kept to just a sentinel after every `input`, so the keyboard's
 * IME never holds a composing buffer to autocorrect (and `insertReplacementText`
 * is dropped outright), while the sentinel keeps backspace working. Every
 * keystroke therefore reaches the terminal exactly as typed.
 */
function setupMobileInput(
    container: HTMLDivElement,
    terminal: Terminal,
    emit: (data: string) => void,
    elementRef: React.RefObject<HTMLTextAreaElement | null>,
): () => void {
    const input = document.createElement('textarea');
    input.setAttribute('aria-label', 'Terminal input');
    input.setAttribute('autocapitalize', 'none');
    input.setAttribute('autocomplete', 'off');
    input.setAttribute('autocorrect', 'off');
    input.setAttribute('enterkeyhint', 'send');
    input.setAttribute('rows', '1');
    input.setAttribute('spellcheck', 'false');
    input.className = 'xterm-mobile-input';
    input.style.cssText = [
        'position:absolute',
        'top:0',
        'left:0',
        'width:1px',
        'height:1px',
        'padding:0',
        'border:0',
        'margin:0',
        'opacity:0',
        'overflow:hidden',
        'background:transparent',
        'color:transparent',
        'caret-color:transparent',
        'font-size:16px',
        'line-height:1',
        'resize:none',
        'z-index:-1',
    ].join(';');

    // A zero-width sentinel keeps the field non-empty: soft keyboards implement
    // backspace as a text edit, so on an empty field they emit nothing and the
    // terminal would never see the delete. The caret always sits after it.
    const SENTINEL = '\u200b';

    const resetInput = () => {
        input.value = SENTINEL;

        try {
            input.setSelectionRange(SENTINEL.length, SENTINEL.length);
        } catch {
            /* ignore */
        }
    };

    resetInput();

    const send = (data: string) => {
        if (data) {
            emit(data);
        }
    };

    const onBeforeInput = (event: Event) => {
        const inputType = (event as InputEvent).inputType;

        // Autocorrect/autofill replacements edit already-typed text; the terminal
        // is append-only, so accepting them would duplicate the word.
        if (inputType === 'insertReplacementText') {
            event.preventDefault();

            return;
        }

        // The IME deletes around the cursor; keep the sentinel and map it to a
        // real backspace for the terminal.
        if (inputType === 'deleteContentBackward') {
            event.preventDefault();
            resetInput();
            send('\x7f');
        }
    };

    const onKeyDown = (event: KeyboardEvent) => {
        switch (event.key) {
            case 'ArrowDown':
                event.preventDefault();
                send('\x1b[B');
                break;
            case 'ArrowLeft':
                event.preventDefault();
                send('\x1b[D');
                break;
            case 'ArrowRight':
                event.preventDefault();
                send('\x1b[C');
                break;
            case 'ArrowUp':
                event.preventDefault();
                send('\x1b[A');
                break;
            case 'Enter':
                event.preventDefault();
                send('\r');
                break;
            case 'Escape':
                send('\x1b');
                break;
            case 'Tab':
                event.preventDefault();
                send('\t');
                break;
            default:
                break;
        }
    };

    const onInput = () => {
        const value = input.value;

        // The sentinel was consumed by a deletion the beforeinput path did not
        // intercept (e.g. a hardware keyboard): still a backspace.
        if (value.length < SENTINEL.length) {
            resetInput();
            send('\x7f');

            return;
        }

        const inserted = value.startsWith(SENTINEL) ? value.slice(SENTINEL.length) : value.split(SENTINEL).join('');

        resetInput();
        send(inserted.replace(/\r?\n/g, '\r'));
    };

    const onPaste = (event: ClipboardEvent) => {
        event.preventDefault();
        const text = event.clipboardData?.getData('text/plain') ?? '';
        resetInput();
        send(text.replace(/\r?\n/g, '\r'));
    };

    const onFocus = () => {
        resetInput();
        terminal.element?.classList.add('focus');
    };

    const onBlur = () => terminal.element?.classList.remove('focus');

    input.addEventListener('beforeinput', onBeforeInput);
    input.addEventListener('blur', onBlur);
    input.addEventListener('focus', onFocus);
    input.addEventListener('input', onInput);
    input.addEventListener('keydown', onKeyDown);
    input.addEventListener('paste', onPaste);
    container.appendChild(input);

    elementRef.current = input;

    return () => {
        input.removeEventListener('beforeinput', onBeforeInput);
        input.removeEventListener('blur', onBlur);
        input.removeEventListener('focus', onFocus);
        input.removeEventListener('input', onInput);
        input.removeEventListener('keydown', onKeyDown);
        input.removeEventListener('paste', onPaste);
        input.remove();

        if (elementRef.current === input) {
            elementRef.current = null;
        }
    };
}
