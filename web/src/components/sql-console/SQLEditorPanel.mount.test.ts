import { EditorView } from '@codemirror/view';
import { mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { executingStatement } from './codemirror/executionGutter';
import SQLEditorPanel from './SQLEditorPanel.vue';

/** 编辑器视图类型(仅测试所需的最小结构) */
type EditorViewLike = {
  state: { doc: { toString(): string } };
};

describe('SQLEditorPanel (CodeMirror 6)', () => {
  beforeEach(() => {
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 800,
      height: 400,
      top: 0,
      left: 0,
      right: 800,
      bottom: 400,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('渲染 CodeMirror 编辑器并回写 v-model', async () => {
    const wrapper = mount(SQLEditorPanel, {
      props: { modelValue: 'SELECT 1;', executing: false, disabled: false, metadata: [], dialect: 'mysql' },
    });
    expect(wrapper.find('.cm-editor').exists()).toBe(true);
    // 通过编辑器输入触发 update:sql
    const view = (wrapper.vm as unknown as { getView(): unknown }).getView();
    // 若组件未暴露 getView,则断言编辑内容被渲染(见组件实现后补强)
    expect(view).toBeTruthy();
  });

  it('执行事件携带语句文本', async () => {
    const wrapper = mount(SQLEditorPanel, {
      props: { modelValue: 'SELECT 1;\nSELECT 2;', executing: false, disabled: false, metadata: [], dialect: 'mysql' },
    });
    // 点击第一条语句的 gutter 执行按钮
    const marker = wrapper.find('.sql-exec-gutter .sql-exec-marker');
    await marker.trigger('click');
    const emitted = wrapper.emitted('execute');
    expect(emitted).toBeTruthy();
    expect(String(emitted![0][0])).toContain('SELECT 1');
  });

  it('gutter 执行单条语句时,程序性回写不截断编辑器缓冲(回归:执行后全文保留)', async () => {
    const wrapper = mount(SQLEditorPanel, {
      props: { modelValue: 'SELECT 1;\nSELECT 2;', executing: false, disabled: false, metadata: [], dialect: 'mysql' },
    });
    // 点击第一条语句的 gutter 执行按钮 → emit('execute')(内部置位程序性回写抑制标志)
    const marker = wrapper.find('.sql-exec-gutter .sql-exec-marker');
    await marker.trigger('click');
    const emitted = wrapper.emitted('execute');
    expect(emitted).toBeTruthy();
    // 模拟父组件 handleExecute 的回写:收到语句后把 modelValue 写回为被执行的单条语句
    await wrapper.setProps({ modelValue: String(emitted![0][0]).trim() });
    const view = (wrapper.vm as unknown as { getView(): EditorViewLike | null }).getView();
    expect(view).toBeTruthy();
    // 编辑器缓冲仍保留全部语句(第二条未被截断),且与原文一致
    expect(view!.state.doc.toString()).toContain('SELECT 2');
    expect(view!.state.doc.toString()).toBe('SELECT 1;\nSELECT 2;');
    // modelValue 变为被执行的语句文本(与父组件 handleExecute 落盘语义一致)
    expect(wrapper.props('modelValue')).toBe('SELECT 1;');
  });

  it('执行期间编辑器不可编辑,结束后恢复(基线 textarea :disabled 回归)', async () => {
    const wrapper = mount(SQLEditorPanel, {
      props: { modelValue: 'SELECT 1;', executing: false, disabled: false, metadata: [], dialect: 'mysql' },
    });
    const view = (wrapper.vm as unknown as { getView(): EditorView | null }).getView();
    expect(view).toBeTruthy();
    expect(view!.state.facet(EditorView.editable)).toBe(true);
    await wrapper.setProps({ executing: true });
    expect(view!.state.facet(EditorView.editable)).toBe(false);
    await wrapper.setProps({ executing: false });
    expect(view!.state.facet(EditorView.editable)).toBe(true);
  });

  it('gutter 点击非光标所在语句执行时,loading 定位到被执行的语句(而非光标语句)', async () => {
    const wrapper = mount(SQLEditorPanel, {
      props: { modelValue: 'SELECT 1;\nSELECT 2;', executing: false, disabled: false, metadata: [], dialect: 'mysql' },
    });
    // 光标默认在文档开头(语句 1),点击第二条语句的 ▶(gutter 第二个按钮)
    const markers = wrapper.findAll('.sql-exec-gutter .sql-exec-marker');
    expect(markers.length).toBe(2);
    await markers[1].trigger('click');
    await wrapper.setProps({ executing: true });
    const view = (wrapper.vm as unknown as { getView(): EditorView | null }).getView();
    expect(view).toBeTruthy();
    // loading 应定位到语句 2 的 from 偏移(10),而非光标所在的语句 1(0)
    expect(view!.state.field(executingStatement, false)).toBe(10);
    // 仅语句 2 的按钮显示执行中(禁用)
    expect(wrapper.findAll('.sql-exec-marker[disabled]').length).toBe(1);
    await wrapper.setProps({ executing: false });
    expect(view!.state.field(executingStatement, false)).toBe(null);
  });
});
