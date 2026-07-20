import { ref, type Ref } from 'vue';
import Guacamole, {
  type GuacamoleClient,
  type GuacamoleDisplay,
  type GuacamoleKeyboard,
  type GuacamoleStatus,
} from 'guacamole-common-js';

import {
  ApiError,
  apiClient,
  type WebRDPEffectivePolicy,
  type WebRDPTicketResponse,
} from '@/api/client';
import {
  encodeGuacamoleInstruction,
  type GuacamoleInstructionElement,
  UnicodeGuacamoleParser,
} from '@/utils/guacamoleProtocol';

export type WebRDPStatus =
  | 'idle'
  | 'requesting-ticket'
  | 'connecting'
  | 'connected'
  | 'disconnected'
  | 'error';

export type WebRDPDriveStatus = 'disabled' | 'waiting' | 'available';
export type WebRDPDownloadStatus = 'disabled' | 'ready' | 'receiving' | 'saved';

interface UseWebRDPOptions {
  targetId: Ref<string>;
}

const MIN_SCALE = 0.25;
const MAX_SCALE = 3;

function defaultPolicy(): WebRDPEffectivePolicy {
  return {
    clipboard_read: false,
    clipboard_write: false,
    file_upload: false,
    file_download: false,
    drive_mapping: false,
  };
}

function clamp(value: number, minimum: number, maximum: number) {
  return Math.min(maximum, Math.max(minimum, value));
}

function statusError(status: GuacamoleStatus, fallback: string) {
  return status.message || `${fallback}（错误码 ${status.code}）`;
}

/**
 * Guacamole tunnel over a plain WebSocket carrying raw Guacamole instructions.
 *
 * The upstream WebSocketTunnel adds a "guacamole" subprotocol and expects an
 * HTTP-style handshake. The Jianmen endpoint already completes the guacd
 * handshake server-side, so this tunnel intentionally does neither.
 */
export class RawWebSocketTunnel extends Guacamole.Tunnel {
  private readonly endpoint: string;
  private readonly parser = new UnicodeGuacamoleParser();
  private socket: WebSocket | null = null;
  private manuallyClosed = false;
  private errorReported = false;

  constructor(endpoint: string) {
    super();
    this.endpoint = endpoint;

    this.parser.oninstruction = (opcode, parameters) => {
      this.oninstruction?.(opcode, parameters);
    };

    // Guacamole.Tunnel defines these as instance properties. Assigning here is
    // required because prototype overrides would otherwise be shadowed.
    this.connect = () => this.openSocket();
    this.disconnect = () => this.closeSocket();
    this.sendMessage = (...elements) => this.sendInstruction(elements);
  }

  private reportError(message: string) {
    if (this.errorReported) return;
    this.errorReported = true;
    this.onerror?.(
      new Guacamole.Status(Guacamole.Status.Code.SERVER_ERROR, message),
    );
  }

  private openSocket() {
    if (
      this.socket
      && (this.socket.readyState === WebSocket.CONNECTING
        || this.socket.readyState === WebSocket.OPEN)
    ) {
      return;
    }

    this.manuallyClosed = false;
    this.errorReported = false;
    this.setState(Guacamole.Tunnel.State.CONNECTING);

    const socket = new WebSocket(this.endpoint);
    this.socket = socket;
    socket.binaryType = 'arraybuffer';

    socket.onopen = () => {
      if (this.socket !== socket) return;
      this.setState(Guacamole.Tunnel.State.OPEN);
    };

    socket.onmessage = (event) => {
      if (this.socket !== socket) return;

      if (typeof event.data === 'string') {
        this.receivePacket(event.data);
        return;
      }

      if (event.data instanceof ArrayBuffer) {
        this.receivePacket(new TextDecoder().decode(event.data));
        return;
      }

      if (event.data instanceof Blob) {
        void event.data.text().then((packet) => {
          if (this.socket === socket) this.receivePacket(packet);
        });
      }
    };

    socket.onerror = () => {
      if (!this.manuallyClosed) this.reportError('Web RDP 连接失败');
    };

    socket.onclose = (event) => {
      if (this.socket !== socket) return;
      this.socket = null;

      if (!this.manuallyClosed && event.code !== 1000) {
        const suffix = event.reason ? `：${event.reason}` : `（${event.code}）`;
        this.reportError(`Web RDP 连接已异常关闭${suffix}`);
      }

      this.setState(Guacamole.Tunnel.State.CLOSED);
    };
  }

  private closeSocket() {
    this.manuallyClosed = true;
    const socket = this.socket;
    this.socket = null;

    if (
      socket
      && (socket.readyState === WebSocket.CONNECTING
        || socket.readyState === WebSocket.OPEN)
    ) {
      socket.close(1000, 'client disconnect');
    }

    this.setState(Guacamole.Tunnel.State.CLOSED);
  }

  private sendInstruction(elements: GuacamoleInstructionElement[]) {
    const socket = this.socket;
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(encodeGuacamoleInstruction(elements));
  }

  private receivePacket(packet: string) {
    try {
      this.parser.receive(packet);
    } catch {
      this.reportError('服务端返回了无效的 Guacamole 指令');
      this.socket?.close(1011, 'invalid Guacamole instruction');
    }
  }
}

function buildWebSocketURL(
  targetId: string,
  ticket: WebRDPTicketResponse,
  width: number,
  height: number,
  dpi: number,
) {
  const endpoint = new URL('/api/web-rdp', window.location.href);
  endpoint.protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  endpoint.search = '';
  endpoint.searchParams.set('target_id', targetId);
  endpoint.searchParams.set('ticket', ticket.ticket);
  endpoint.searchParams.set('width', String(width));
  endpoint.searchParams.set('height', String(height));
  endpoint.searchParams.set('dpi', String(dpi));
  return endpoint.toString();
}

function measureViewport(container: HTMLElement) {
  const fallbackWidth = Math.max(320, window.innerWidth);
  const fallbackHeight = Math.max(200, window.innerHeight - 100);
  const width = Math.max(320, Math.round(container.clientWidth || fallbackWidth));
  const height = Math.max(200, Math.round(container.clientHeight || fallbackHeight));
  const dpi = clamp(Math.round(96 * (window.devicePixelRatio || 1)), 96, 240);
  return { width, height, dpi };
}

function safeFilename(filename: string) {
  const leaf = filename.split(/[\\/]/).pop()?.trim();
  return leaf || 'remote-download';
}

export function useWebRDP({ targetId }: UseWebRDPOptions) {
  const status = ref<WebRDPStatus>('idle');
  const error = ref('');
  const approvalRequired = ref(false);
  const policy = ref<WebRDPEffectivePolicy>(defaultPolicy());
  const approvalId = ref('');
  const remoteName = ref('');
  const scale = ref(1);
  const autoFit = ref(true);
  const remoteWidth = ref(0);
  const remoteHeight = ref(0);
  const remoteClipboardAvailable = ref(false);
  const uploading = ref(false);
  const uploadProgress = ref(0);
  const downloadStatus = ref<WebRDPDownloadStatus>('disabled');
  const downloadedFilename = ref('');
  const downloadBytes = ref(0);
  const driveStatus = ref<WebRDPDriveStatus>('disabled');
  const driveName = ref('');

  let client: GuacamoleClient | null = null;
  let tunnel: RawWebSocketTunnel | null = null;
  let display: GuacamoleDisplay | null = null;
  let keyboard: GuacamoleKeyboard | null = null;
  let displayContainer: HTMLElement | null = null;
  let resizeObserver: ResizeObserver | null = null;
  let resizeFrame = 0;
  let connectionGeneration = 0;
  let manualDisconnect = false;
  let remoteClipboardText = '';
  let activeUploadReject: ((reason?: unknown) => void) | null = null;

  function applyFit() {
    if (!display || !displayContainer) return;
    const width = display.getWidth();
    const height = display.getHeight();
    if (!width || !height) return;

    const nextScale = clamp(
      Math.min(
        displayContainer.clientWidth / width,
        displayContainer.clientHeight / height,
      ),
      MIN_SCALE,
      MAX_SCALE,
    );

    display.scale(nextScale);
    scale.value = nextScale;
  }

  function setScale(nextScale: number) {
    if (!display) return;
    autoFit.value = false;
    scale.value = clamp(nextScale, MIN_SCALE, MAX_SCALE);
    display.scale(scale.value);
  }

  function fitDisplay() {
    autoFit.value = true;
    applyFit();
  }

  function sendResize() {
    if (!client || !displayContainer || status.value !== 'connected') return;
    const { width, height } = measureViewport(displayContainer);
    client.sendSize(width, height);
    if (autoFit.value) applyFit();
  }

  function resize() {
    if (resizeFrame) window.cancelAnimationFrame(resizeFrame);
    resizeFrame = window.requestAnimationFrame(() => {
      resizeFrame = 0;
      sendResize();
    });
  }

  function destroyConnection() {
    if (resizeFrame) {
      window.cancelAnimationFrame(resizeFrame);
      resizeFrame = 0;
    }
    resizeObserver?.disconnect();
    resizeObserver = null;
    keyboard?.reset();
    keyboard = null;

    if (activeUploadReject) {
      activeUploadReject(new Error('连接已断开，文件上传已取消'));
      activeUploadReject = null;
    }
    uploading.value = false;

    if (client) {
      client.onstatechange = null;
      client.onerror = null;
      client.onclipboard = null;
      client.onfile = null;
      client.onfilesystem = null;
      client.onname = null;
      try {
        client.disconnect();
      } catch {
        // The underlying socket may already be closed.
      }
    } else {
      tunnel?.disconnect();
    }

    if (tunnel) {
      tunnel.onerror = null;
      tunnel.onstatechange = null;
      tunnel.oninstruction = null;
    }

    client = null;
    tunnel = null;
    display = null;
    if (displayContainer) displayContainer.replaceChildren();
    displayContainer = null;
  }

  function disconnect() {
    connectionGeneration += 1;
    manualDisconnect = true;
    destroyConnection();
    status.value = 'disconnected';
  }

  function bindClipboard(sessionClient: GuacamoleClient) {
    sessionClient.onclipboard = (stream, mimetype) => {
      if (!policy.value.clipboard_read) {
        stream.sendAck(
          'Remote clipboard is disabled by policy',
          Guacamole.Status.Code.CLIENT_FORBIDDEN,
        );
        return;
      }

      if (!mimetype.toLowerCase().startsWith('text/')) {
        stream.sendAck(
          'Only text clipboard data is supported',
          Guacamole.Status.Code.UNSUPPORTED,
        );
        return;
      }

      const reader = new Guacamole.StringReader(stream);
      let text = '';
      reader.ontext = (chunk) => {
        text += chunk;
      };
      reader.onend = () => {
        remoteClipboardText = text;
        remoteClipboardAvailable.value = true;
      };
    };
  }

  function bindDownloads(sessionClient: GuacamoleClient) {
    sessionClient.onfile = (stream, mimetype, filename) => {
      if (!policy.value.file_download) {
        stream.sendAck(
          'File download is disabled by policy',
          Guacamole.Status.Code.CLIENT_FORBIDDEN,
        );
        return;
      }

      const resolvedName = safeFilename(filename);
      const reader = new Guacamole.BlobReader(
        stream,
        mimetype || 'application/octet-stream',
      );
      downloadStatus.value = 'receiving';
      downloadedFilename.value = resolvedName;
      downloadBytes.value = 0;

      reader.onprogress = (chunkLength) => {
        downloadBytes.value += chunkLength;
      };
      reader.onend = () => {
        const blob = reader.getBlob();
        const objectURL = URL.createObjectURL(blob);
        const anchor = document.createElement('a');
        anchor.href = objectURL;
        anchor.download = resolvedName;
        anchor.style.display = 'none';
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
        window.setTimeout(() => URL.revokeObjectURL(objectURL), 1000);
        downloadBytes.value = blob.size;
        downloadStatus.value = 'saved';
      };
    };
  }

  function bindDrive(sessionClient: GuacamoleClient) {
    if (!policy.value.drive_mapping) {
      sessionClient.onfilesystem = null;
      return;
    }

    sessionClient.onfilesystem = (_object, name) => {
      driveName.value = name || '远程虚拟盘';
      driveStatus.value = 'available';
    };
  }

  async function connect(container: HTMLElement) {
    const requestedTargetId = targetId.value.trim();
    const generation = ++connectionGeneration;
    manualDisconnect = false;
    destroyConnection();

    error.value = '';
    approvalRequired.value = false;
    remoteName.value = '';
    remoteClipboardText = '';
    remoteClipboardAvailable.value = false;
    uploadProgress.value = 0;
    downloadedFilename.value = '';
    downloadBytes.value = 0;
    approvalId.value = '';
    policy.value = defaultPolicy();
    driveStatus.value = 'disabled';
    driveName.value = '';
    downloadStatus.value = 'disabled';
    autoFit.value = true;
    scale.value = 1;

    if (!requestedTargetId) {
      status.value = 'error';
      error.value = '缺少 RDP 目标 ID';
      throw new Error(error.value);
    }

    displayContainer = container;
    const viewport = measureViewport(container);
    status.value = 'requesting-ticket';

    try {
      const ticket = await apiClient.createWebRDPTicket({
        target_id: requestedTargetId,
        ...viewport,
      });
      if (generation !== connectionGeneration) return;
      if (!ticket.ticket) throw new Error('服务端未返回 Web RDP 连接票据');
      if (ticket.target_id && ticket.target_id !== requestedTargetId) {
        throw new Error('Web RDP 票据目标不匹配');
      }

      policy.value = {
        clipboard_read: ticket.effective_policy?.clipboard_read === true,
        clipboard_write: ticket.effective_policy?.clipboard_write === true,
        file_upload: ticket.effective_policy?.file_upload === true,
        file_download: ticket.effective_policy?.file_download === true,
        drive_mapping: ticket.effective_policy?.drive_mapping === true,
      };
      approvalId.value = ticket.approval_id || '';
      driveStatus.value = policy.value.drive_mapping ? 'waiting' : 'disabled';
      downloadStatus.value = policy.value.file_download ? 'ready' : 'disabled';

      tunnel = new RawWebSocketTunnel(
        buildWebSocketURL(
          requestedTargetId,
          ticket,
          viewport.width,
          viewport.height,
          viewport.dpi,
        ),
      );
      client = new Guacamole.Client(tunnel);
      display = client.getDisplay();

      const sessionClient = client;
      const sessionTunnel = tunnel;
      const sessionDisplay = display;
      const displayElement = sessionDisplay.getElement();
      displayElement.tabIndex = 0;
      displayElement.setAttribute('role', 'application');
      displayElement.setAttribute('aria-label', 'Web RDP 远程桌面');
      displayElement.addEventListener('pointerdown', () => displayElement.focus());
      displayElement.addEventListener('contextmenu', (event) => event.preventDefault());
      container.replaceChildren(displayElement);

      const mouse = new Guacamole.Mouse(displayElement);
      mouse.onEach(['mousedown', 'mousemove', 'mouseup'], (event) => {
        sessionClient.sendMouseState(event.state, true);
      });

      keyboard = new Guacamole.Keyboard(displayElement);
      keyboard.onkeydown = (keysym) => {
        sessionClient.sendKeyEvent(true, keysym);
        return false;
      };
      keyboard.onkeyup = (keysym) => {
        sessionClient.sendKeyEvent(false, keysym);
      };

      sessionDisplay.onresize = (width, height) => {
        remoteWidth.value = width;
        remoteHeight.value = height;
        if (autoFit.value) applyFit();
      };

      bindClipboard(sessionClient);
      bindDownloads(sessionClient);
      bindDrive(sessionClient);

      sessionClient.onname = (name) => {
        remoteName.value = name;
      };
      sessionClient.onerror = (clientStatus) => {
        if (generation !== connectionGeneration || manualDisconnect) return;
        error.value = statusError(clientStatus, '远程桌面连接错误');
        status.value = 'error';
      };
      sessionClient.onstatechange = (state) => {
        if (generation !== connectionGeneration || manualDisconnect) return;
        if (
          state === Guacamole.Client.State.CONNECTING
          || state === Guacamole.Client.State.WAITING
        ) {
          status.value = 'connecting';
        } else if (state === Guacamole.Client.State.CONNECTED) {
          status.value = 'connected';
          displayElement.focus();
          resize();
        } else if (
          state === Guacamole.Client.State.DISCONNECTED
          && status.value !== 'error'
        ) {
          status.value = 'disconnected';
        }
      };

      sessionTunnel.onerror = (tunnelStatus) => {
        if (generation !== connectionGeneration || manualDisconnect) return;
        error.value = statusError(tunnelStatus, 'Web RDP 通道错误');
        status.value = 'error';
      };
      sessionTunnel.onstatechange = (state) => {
        if (generation !== connectionGeneration || manualDisconnect) return;
        if (
          state === Guacamole.Tunnel.State.CLOSED
          && status.value !== 'error'
        ) {
          status.value = 'disconnected';
        }
      };

      resizeObserver = new ResizeObserver(() => resize());
      resizeObserver.observe(container);
      status.value = 'connecting';
      sessionClient.connect('');
    } catch (caught) {
      if (generation !== connectionGeneration) return;
      destroyConnection();
      if (caught instanceof ApiError && caught.code === 'RDP_APPROVAL_REQUIRED') {
        approvalRequired.value = true;
      }
      const message = approvalRequired.value
        ? '该主机账号需要先完成 RDP 访问审批'
        : caught instanceof Error
          ? caught.message
          : 'Web RDP 连接失败';
      error.value = message;
      status.value = 'error';
      throw caught;
    }
  }

  async function copyRemoteClipboard() {
    if (!policy.value.clipboard_read) {
      throw new Error('当前策略禁止从远程桌面读取剪贴板');
    }
    if (!remoteClipboardAvailable.value) {
      throw new Error('远程剪贴板尚无可复制的文本');
    }
    if (!navigator.clipboard?.writeText) {
      throw new Error('当前浏览器或页面环境不支持写入本机剪贴板');
    }
    await navigator.clipboard.writeText(remoteClipboardText);
  }

  async function pasteLocalClipboard() {
    if (!policy.value.clipboard_write) {
      throw new Error('当前策略禁止向远程桌面写入剪贴板');
    }
    if (!client || status.value !== 'connected') {
      throw new Error('远程桌面尚未连接');
    }
    if (!navigator.clipboard?.readText) {
      throw new Error('当前浏览器或页面环境不支持读取本机剪贴板');
    }

    const text = await navigator.clipboard.readText();
    const stream = client.createClipboardStream('text/plain');
    const writer = new Guacamole.StringWriter(stream);
    writer.sendText(text);
    writer.sendEnd();
  }

  function uploadFile(file: File) {
    if (!policy.value.file_upload) {
      return Promise.reject(new Error('当前策略禁止向远程桌面上传文件'));
    }
    if (!client || status.value !== 'connected') {
      return Promise.reject(new Error('远程桌面尚未连接'));
    }
    if (uploading.value) {
      return Promise.reject(new Error('已有文件正在上传'));
    }

    const stream = client.createFileStream(
      file.type || 'application/octet-stream',
      file.name,
    );
    const writer = new Guacamole.BlobWriter(stream);
    uploading.value = true;
    uploadProgress.value = 0;

    return new Promise<void>((resolve, reject) => {
      let settled = false;
      const fail = (reason: unknown) => {
        if (settled) return;
        settled = true;
        activeUploadReject = null;
        uploading.value = false;
        reject(reason);
      };

      activeUploadReject = fail;
      writer.onack = (ackStatus) => {
        if (ackStatus.isError()) {
          fail(new Error(statusError(ackStatus, '远程端拒绝接收文件')));
        }
      };
      writer.onprogress = (_blob, offset) => {
        uploadProgress.value = file.size
          ? Math.min(99, Math.round((offset / file.size) * 100))
          : 99;
      };
      writer.onerror = (_blob, _offset, uploadError) => {
        fail(new Error(uploadError?.message || '读取本地文件失败'));
      };
      writer.oncomplete = () => {
        if (settled) return;
        settled = true;
        writer.sendEnd();
        activeUploadReject = null;
        uploadProgress.value = 100;
        uploading.value = false;
        resolve();
      };
      writer.sendBlob(file);
    });
  }

  return {
    status,
    error,
    approvalRequired,
    policy,
    approvalId,
    remoteName,
    scale,
    autoFit,
    remoteWidth,
    remoteHeight,
    remoteClipboardAvailable,
    uploading,
    uploadProgress,
    downloadStatus,
    downloadedFilename,
    downloadBytes,
    driveStatus,
    driveName,
    connect,
    disconnect,
    resize,
    setScale,
    fitDisplay,
    copyRemoteClipboard,
    pasteLocalClipboard,
    uploadFile,
  };
}
