import { ref, type Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';

export type TerminalStatus = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'error';

export interface UseWebTerminalOptions {
  targetId: Ref<string>;
  cols?: number;
  rows?: number;
}

export interface UseWebTerminalReturn {
  terminal: Ref<Terminal | null>;
  status: Ref<TerminalStatus>;
  error: Ref<string>;
  connect(container: HTMLElement): Promise<void>;
  disconnect(): void;
}

function buildWsUrl(targetId: string, cols: number, rows: number): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const token = localStorage.getItem('jianmen_token') || '';
  return (
    `${protocol}//${window.location.host}/api/web-terminal` +
    `?target_id=${encodeURIComponent(targetId)}` +
    `&token=${encodeURIComponent(token)}` +
    `&cols=${cols}&rows=${rows}` +
    `&term=xterm-256color`
  );
}

function defaultTerminalOptions(cols: number, rows: number) {
  const rootStyle = getComputedStyle(document.documentElement);
  const configuredSize = Number.parseInt(rootStyle.getPropertyValue('--terminal-font-size'), 10);
  const configuredFamily = rootStyle.getPropertyValue('--terminal-font-family').trim();
  return {
    cursorBlink: true,
    fontSize: Number.isFinite(configuredSize) ? configuredSize : 14,
    fontFamily: configuredFamily || '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
    theme: {
      background: '#1e1e2e',
      foreground: '#cdd6f4',
      cursor: '#f5e0dc',
      selectionBackground: '#585b70',
      black: '#45475a',
      red: '#f38ba8',
      green: '#a6e3a1',
      yellow: '#f9e2af',
      blue: '#89b4fa',
      magenta: '#f5c2e7',
      cyan: '#94e2d5',
      white: '#bac2de',
      brightBlack: '#585b70',
    },
    cols,
    rows,
  };
}

export function useWebTerminal(opts: UseWebTerminalOptions): UseWebTerminalReturn {
  const terminal = ref<Terminal | null>(null);
  const status = ref<TerminalStatus>('idle');
  const error = ref('');

  const cols = opts.cols || 80;
  const rows = opts.rows || 24;

  let ws: WebSocket | null = null;
  let fitAddon: FitAddon | null = null;
  let resizeObserver: ResizeObserver | null = null;

  async function connect(container: HTMLElement): Promise<void> {
    if (status.value === 'connected' || status.value === 'connecting') return;

    status.value = 'connecting';
    error.value = '';

    const term = new Terminal(defaultTerminalOptions(cols, rows));

    fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new WebLinksAddon());

    term.open(container);
    term.attachCustomKeyEventHandler(event => {
      if (event.key !== 'Tab') return true;
      event.preventDefault();
      if (event.type === 'keydown' && ws?.readyState === WebSocket.OPEN) {
        ws.send(event.shiftKey ? '\u001b[Z' : '\t');
      }
      return false;
    });
    fitAddon.fit();
    terminal.value = term;

    try {
      await new Promise<void>((resolve, reject) => {
        const url = buildWsUrl(opts.targetId.value, cols, rows);
        console.debug('[WebTerminal] connecting to', url.replace(/token=[^&]+/, 'token=***'));
        ws = new WebSocket(url);
        ws.binaryType = 'arraybuffer';

        ws.onopen = () => {
          console.debug('[WebTerminal] connected');
          status.value = 'connected';
          resolve();
        };

        ws.onerror = (event) => {
          console.debug('[WebTerminal] onerror', event);
        };

        ws.onclose = (event) => {
          console.debug('[WebTerminal] onclose', { code: event.code, reason: event.reason, wasClean: event.wasClean, readyState: ws?.readyState, status: status.value });
          if (status.value === 'connecting') {
            const msg = event.code === 4001
              ? '认证失败，请重新登录'
              : event.code === 1006
                ? '连接异常关闭，请检查目标主机是否可达'
                : `WebSocket 连接关闭 (code: ${event.code}${event.reason ? `, ${event.reason}` : ''})`;
            error.value = msg;
            status.value = 'error';
            reject(new Error(msg));
          } else if (status.value === 'connected') {
            status.value = 'disconnected';
            if (terminal.value) {
              terminal.value.options.disableStdin = true;
              terminal.value.write('\r\n\x1b[33m[连接已断开]\x1b[0m\r\n');
            }
          }
        };

        ws.onmessage = (event) => {
          if (status.value !== 'connected' || !terminal.value) return;
          if (event.data instanceof ArrayBuffer) {
            terminal.value.write(new Uint8Array(event.data));
          } else if (typeof event.data === 'string') {
            terminal.value.write(event.data);
          }
        };

        term.onData((data) => {
          if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(data);
          }
        });
      });

      // ResizeObserver — fit terminal to container + send resize to server
      resizeObserver = new ResizeObserver(() => {
        if (!fitAddon || !terminal.value) return;
        fitAddon.fit();
        if (ws && ws.readyState === WebSocket.OPEN) {
          const t = terminal.value;
          if (t.cols > 0 && t.rows > 0) {
            ws.send(JSON.stringify({ type: 'resize', cols: t.cols, rows: t.rows }));
          }
        }
      });
      resizeObserver.observe(container);
    } catch (e) {
      // If status wasn't already set by onclose, mark as error
      if ((status.value as TerminalStatus) !== 'error') {
        error.value = e instanceof Error ? e.message : '连接失败';
        status.value = 'error';
      }
      throw e;
    }
  }

  function disconnect(): void {
    if (resizeObserver) {
      resizeObserver.disconnect();
      resizeObserver = null;
    }
    if (ws) {
      ws.onclose = null; // 防止 onclose 再次修改 status
      ws.close(1000, 'user disconnected');
      ws = null;
    }
    if (terminal.value) {
      terminal.value.dispose();
      terminal.value = null;
    }
    status.value = 'disconnected';
    fitAddon = null;
  }

  return { terminal, status, error, connect, disconnect };
}
