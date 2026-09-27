import assert from 'node:assert/strict';
import { describe, it, vi } from 'vitest';

import { handleTerminalKeyEvent } from './useWebTerminal';

/** 构造一个最小化的键盘事件对象（node 环境无真实 KeyboardEvent） */
function keyEvent(partial: {
  key?: string;
  type?: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
}): KeyboardEvent {
  const event = {
    key: 'a',
    type: 'keydown',
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    shiftKey: false,
    preventDefault: vi.fn(),
    ...partial,
  };
  return event as unknown as KeyboardEvent;
}

describe('handleTerminalKeyEvent', () => {
  it('普通按键不做拦截，交给 xterm 默认处理', () => {
    const send = vi.fn();
    const handled = handleTerminalKeyEvent(keyEvent({ key: 'a' }), send);

    assert.equal(handled, true);
    assert.equal(send.mock.calls.length, 0);
  });

  it('Tab 补全：阻止默认行为并发送制表符', () => {
    const send = vi.fn();
    const event = keyEvent({ key: 'Tab' });
    const handled = handleTerminalKeyEvent(event, send);

    assert.equal(handled, false);
    assert.equal(send.mock.calls.length, 1);
    assert.equal(send.mock.calls[0][0], '\t');
    assert.equal((event.preventDefault as ReturnType<typeof vi.fn>).mock.calls.length, 1);
  });

  it('Shift+Tab 发送反向补全序列', () => {
    const send = vi.fn();
    const handled = handleTerminalKeyEvent(keyEvent({ key: 'Tab', shiftKey: true }), send);

    assert.equal(handled, false);
    assert.equal(send.mock.calls.length, 1);
    assert.equal(send.mock.calls[0][0], '[Z');
  });

  it('Ctrl+V 不拦截也不发送 ^V，让浏览器派发原生粘贴事件', () => {
    const send = vi.fn();
    const event = keyEvent({ key: 'v', ctrlKey: true });
    const handled = handleTerminalKeyEvent(event, send);

    assert.equal(handled, false);
    assert.equal(send.mock.calls.length, 0);
    assert.equal((event.preventDefault as ReturnType<typeof vi.fn>).mock.calls.length, 0);
  });

  it('Cmd+V (macOS) 同样走原生粘贴路径', () => {
    const send = vi.fn();
    const handled = handleTerminalKeyEvent(keyEvent({ key: 'v', metaKey: true }), send);

    assert.equal(handled, false);
    assert.equal(send.mock.calls.length, 0);
  });

  it('Ctrl+Shift+V 不发送数据', () => {
    const send = vi.fn();
    const handled = handleTerminalKeyEvent(keyEvent({ key: 'V', ctrlKey: true, shiftKey: true }), send);

    assert.equal(handled, false);
    assert.equal(send.mock.calls.length, 0);
  });

  it('Ctrl+Alt+V 属于 Alt 组合键，不做拦截', () => {
    const send = vi.fn();
    const handled = handleTerminalKeyEvent(keyEvent({ key: 'v', ctrlKey: true, altKey: true }), send);

    assert.equal(handled, true);
    assert.equal(send.mock.calls.length, 0);
  });

  it('keyup 事件不做拦截', () => {
    const send = vi.fn();
    const handled = handleTerminalKeyEvent(keyEvent({ key: 'v', ctrlKey: true, type: 'keyup' }), send);

    assert.equal(handled, true);
  });
});
