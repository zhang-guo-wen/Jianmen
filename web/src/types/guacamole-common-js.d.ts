declare module 'guacamole-common-js' {
  export interface GuacamoleStatus {
    code: number;
    message: string;
    isError(): boolean;
  }

  export interface GuacamoleInputStream {
    index: number;
    onblob: ((data: string) => void) | null;
    onend: (() => void) | null;
    sendAck(message: string, code: number): void;
  }

  export interface GuacamoleOutputStream {
    index: number;
    onack: ((status: GuacamoleStatus) => void) | null;
    sendBlob(data: string): void;
    sendEnd(): void;
  }

  export interface GuacamoleTunnel {
    state: number;
    uuid: string | null;
    connect(data?: string): void;
    disconnect(): void;
    isConnected(): boolean;
    sendMessage(...elements: Array<string | number | boolean>): void;
    setState(state: number): void;
    setUUID(uuid: string): void;
    onerror: ((status: GuacamoleStatus) => void) | null;
    oninstruction: ((opcode: string, parameters: string[]) => void) | null;
    onstatechange: ((state: number) => void) | null;
    onuuid: ((uuid: string) => void) | null;
  }

  export interface GuacamoleDisplay {
    getElement(): HTMLDivElement;
    getHeight(): number;
    getScale(): number;
    getWidth(): number;
    scale(scale: number): void;
    onresize: ((width: number, height: number) => void) | null;
  }

  export interface GuacamoleMouseState {
    x: number;
    y: number;
    left: boolean;
    middle: boolean;
    right: boolean;
    up: boolean;
    down: boolean;
  }

  export interface GuacamoleMouseEvent {
    state: GuacamoleMouseState;
  }

  export interface GuacamoleMouse {
    onEach(events: string[], listener: (event: GuacamoleMouseEvent) => void): void;
  }

  export interface GuacamoleKeyboard {
    onkeydown: ((keysym: number) => boolean | void) | null;
    onkeyup: ((keysym: number) => void) | null;
    reset(): void;
  }

  export interface GuacamoleObject {
    index: number;
  }

  export interface GuacamoleClient {
    connect(data?: string): void;
    createClipboardStream(mimetype: string): GuacamoleOutputStream;
    createFileStream(mimetype: string, filename: string): GuacamoleOutputStream;
    disconnect(): void;
    getDisplay(): GuacamoleDisplay;
    sendKeyEvent(pressed: boolean, keysym: number): void;
    sendMouseState(mouseState: GuacamoleMouseState, applyDisplayScale?: boolean): void;
    sendSize(width: number, height: number): void;
    onclipboard: ((stream: GuacamoleInputStream, mimetype: string) => void) | null;
    onerror: ((status: GuacamoleStatus) => void) | null;
    onfile: ((stream: GuacamoleInputStream, mimetype: string, filename: string) => void) | null;
    onfilesystem: ((object: GuacamoleObject, name: string) => void) | null;
    onname: ((name: string) => void) | null;
    onstatechange: ((state: number) => void) | null;
  }

  interface GuacamoleParser {
    receive(packet: string): void;
    oninstruction: ((opcode: string, parameters: string[]) => void) | null;
  }

  interface GuacamoleBlobReader {
    getBlob(): Blob;
    onend: (() => void) | null;
    onprogress: ((length: number) => void) | null;
  }

  interface GuacamoleBlobWriter {
    sendBlob(blob: Blob): void;
    sendEnd(): void;
    onack: ((status: GuacamoleStatus) => void) | null;
    oncomplete: ((blob: Blob) => void) | null;
    onerror: ((blob: Blob, offset: number, error: DOMException | null) => void) | null;
    onprogress: ((blob: Blob, offset: number) => void) | null;
  }

  interface GuacamoleStringReader {
    onend: (() => void) | null;
    ontext: ((text: string) => void) | null;
  }

  interface GuacamoleStringWriter {
    sendEnd(): void;
    sendText(text: string): void;
  }

  interface TunnelConstructor {
    new(): GuacamoleTunnel;
    State: {
      CONNECTING: number;
      OPEN: number;
      CLOSED: number;
      UNSTABLE: number;
    };
  }

  interface ClientConstructor {
    new(tunnel: GuacamoleTunnel): GuacamoleClient;
    State: {
      IDLE: number;
      CONNECTING: number;
      WAITING: number;
      CONNECTED: number;
      DISCONNECTING: number;
      DISCONNECTED: number;
    };
  }

  interface StatusConstructor {
    new(code: number, message: string): GuacamoleStatus;
    Code: {
      SUCCESS: number;
      UNSUPPORTED: number;
      SERVER_ERROR: number;
      SERVER_BUSY: number;
      UPSTREAM_TIMEOUT: number;
      UPSTREAM_ERROR: number;
      RESOURCE_NOT_FOUND: number;
      RESOURCE_CONFLICT: number;
      CLIENT_BAD_REQUEST: number;
      CLIENT_UNAUTHORIZED: number;
      CLIENT_FORBIDDEN: number;
      CLIENT_TIMEOUT: number;
      CLIENT_OVERRUN: number;
      CLIENT_BAD_TYPE: number;
      CLIENT_TOO_MANY: number;
    };
  }

  const Guacamole: {
    BlobReader: new(stream: GuacamoleInputStream, mimetype: string) => GuacamoleBlobReader;
    BlobWriter: new(stream: GuacamoleOutputStream) => GuacamoleBlobWriter;
    Client: ClientConstructor;
    Keyboard: new(element: Element | Document) => GuacamoleKeyboard;
    Mouse: new(element: Element) => GuacamoleMouse;
    Parser: new() => GuacamoleParser;
    Status: StatusConstructor;
    StringReader: new(stream: GuacamoleInputStream) => GuacamoleStringReader;
    StringWriter: new(stream: GuacamoleOutputStream) => GuacamoleStringWriter;
    Tunnel: TunnelConstructor;
  };

  export default Guacamole;
}
