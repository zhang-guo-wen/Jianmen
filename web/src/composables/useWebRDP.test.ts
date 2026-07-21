import assert from 'node:assert/strict';
import { shallowRef } from 'vue';
import { afterEach, beforeEach, describe, it, vi } from 'vitest';

import { apiClient } from '@/api/client';
import { encodeGuacamoleInstruction } from '@/utils/guacamoleProtocol';

import { useWebRDP } from './useWebRDP';

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static readonly instances: FakeWebSocket[] = [];

  readonly sent: string[] = [];
  readyState = FakeWebSocket.CONNECTING;
  binaryType: BinaryType = 'blob';
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;

  constructor(readonly url: string | URL) {
    FakeWebSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  receive(data: string) {
    this.onmessage?.(new MessageEvent('message', { data }));
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView) {
    this.sent.push(String(data));
  }

  close(code = 1000, reason = '') {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.(new CloseEvent('close', { code, reason }));
  }
}

class FakeResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeEach(() => {
  FakeWebSocket.instances.length = 0;
  vi.stubGlobal('WebSocket', FakeWebSocket);
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  const canvasContext = new Proxy<Record<PropertyKey, unknown>>({}, {
    get(target, property) {
      if (!(property in target)) target[property] = vi.fn();
      return target[property];
    },
  });
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(
    () => canvasContext as unknown as CanvasRenderingContext2D,
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('useWebRDP downloads', () => {
  it('acknowledges a file stream before waiting for its first blob', async () => {
    vi.spyOn(apiClient, 'createWebRDPTicket').mockResolvedValue({
      ticket: 'ticket-1',
      target_id: 'target-1',
      effective_policy: {
        clipboard_read: false,
        clipboard_write: false,
        file_upload: false,
        file_download: true,
        drive_mapping: true,
      },
    });

    const state = useWebRDP({ targetId: shallowRef('target-1') });
    await state.connect(document.createElement('div'));

    const socket = FakeWebSocket.instances[0];
    assert.ok(socket);
    socket.open();
    socket.receive(encodeGuacamoleInstruction([
      'file',
      7,
      'application/octet-stream',
      'report.bin',
    ]));

    assert.equal(state.downloadStatus.value, 'receiving');
    assert.equal(state.downloadedFilename.value, 'report.bin');
    assert.equal(state.downloadBytes.value, 0);
    assert.ok(socket.sent.includes(encodeGuacamoleInstruction([
      'ack',
      7,
      'Ready',
      0,
    ])));

    state.disconnect();
  });
});
