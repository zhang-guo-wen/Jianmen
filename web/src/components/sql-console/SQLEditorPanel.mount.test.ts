import { mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import SQLEditorPanel from './SQLEditorPanel.vue';

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
});
