import { defineComponent, h } from 'vue';
import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';

import type { SQLConsoleResult } from '@/api/client';

import SQLResultPanel from './SQLResultPanel.vue';

// Element Plus 组件 stub:挂载环境未注册全局组件。
// 注意 el-collapse-transition 不 stub,保留真实过渡组件以验证 v-show 折叠行为。
const ElTableStub = defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h('div', attrs, slots.default?.());
  },
});

const ElTableColumnStub = defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    // 真实 el-table-column 会以 { row } 作用域渲染 default 插槽,
    // 未解析的组件不会传作用域,导致模板 #default="{ row }" 解构 undefined 抛错
    return () => h('div', attrs, slots.default?.({ row: {} }));
  },
});

const ElButtonStub = defineComponent({
  inheritAttrs: false,
  // 不透发 emit:attrs 中的 onClick(含 .stop 修饰)已挂到原生 button 上,
  // 若再 emit('click') 会经 props.onClick 二次触发父级 handler,造成双重切换
  setup(_, { attrs, slots }) {
    return () => h('button', attrs, slots.default?.());
  },
});

const ElAlertStub = defineComponent({
  inheritAttrs: false,
  props: { title: { type: String, default: '' } },
  setup(props, { attrs }) {
    return () => h('div', attrs, props.title);
  },
});

const ElTagStub = defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h('span', attrs, slots.default?.());
  },
});

const ElEmptyStub = defineComponent({
  inheritAttrs: false,
  setup(_, { attrs }) {
    return () => h('div', attrs);
  },
});

function mountResultPanel(result: SQLConsoleResult | null, error = '', executing = false) {
  return mount(SQLResultPanel, {
    props: { result, error, executing },
    // happy-dom 对未挂载到文档的元素 getComputedStyle 返回空串,isVisible() 无法区分折叠状态
    attachTo: document.body,
    global: {
      stubs: {
        ElTable: ElTableStub,
        ElTableColumn: ElTableColumnStub,
        ElButton: ElButtonStub,
        ElAlert: ElAlertStub,
        ElTag: ElTagStub,
        ElEmpty: ElEmptyStub,
      },
    },
  });
}

/**
 * 等待 Vue Transition 的离开动画完成(happy-dom 下 rAF 基于 setImmediate,宏任务,
 * `await trigger` 只排微任务;vShow 的 display:none 需等双 rAF 后才会应用)。
 * 生产环境有真实 CSS 过渡,这里仅是测试环境的时序补偿。
 */
async function flushFrames(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));
}

const baseResult: SQLConsoleResult = {
  audit_session_id: 'aud-1',
  query_kind: 'select',
  read_only: true,
  columns: ['id', 'name'],
  rows: [['1', 'a']],
  row_count: 1,
  rows_affected: 0,
  truncated: false,
  duration_ms: 5,
};

describe('SQLResultPanel 折叠', () => {
  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('默认折叠:内容区隐藏,header 可见', () => {
    const wrapper = mountResultPanel(null);
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    expect(wrapper.find('.result-header').exists()).toBe(true);
    expect(wrapper.find('.result-collapse-btn').exists()).toBe(true);
  });

  it('传入 result 后自动展开', async () => {
    const wrapper = mountResultPanel(null);
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    await wrapper.setProps({ result: baseResult });
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });

  it('error 非空时自动展开', async () => {
    const wrapper = mountResultPanel(null);
    await wrapper.setProps({ error: '执行失败' });
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });

  it('点击折叠按钮可手动切换', async () => {
    const wrapper = mountResultPanel(baseResult);
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
    await wrapper.find('.result-collapse-btn').trigger('click');
    await flushFrames();
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    await wrapper.find('.result-collapse-btn').trigger('click');
    await flushFrames();
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });

  it('手动折叠后传入新 result 自动重新展开', async () => {
    const wrapper = mountResultPanel(baseResult);
    await wrapper.find('.result-collapse-btn').trigger('click');
    await flushFrames();
    expect(wrapper.find('.result-body').isVisible()).toBe(false);
    await wrapper.setProps({ result: { ...baseResult, duration_ms: 9 } });
    expect(wrapper.find('.result-body').isVisible()).toBe(true);
  });
});
