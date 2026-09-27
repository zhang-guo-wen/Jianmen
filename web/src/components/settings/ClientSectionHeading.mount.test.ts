import { defineComponent, h } from 'vue';
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import ClientSectionHeading from './ClientSectionHeading.vue';

const ElTagStub = defineComponent({
  props: { type: String, effect: String },
  setup(_, { attrs, slots }) {
    return () => h('span', attrs, slots.default?.());
  },
});

describe('ClientSectionHeading', () => {
  it('keeps status tag and actions inside the same right-side toolbar', () => {
    const wrapper = mount(ClientSectionHeading, {
      props: {
        title: '本地 SSH 客户端',
        desc: '设置快速连接默认使用的 SSH 工具。',
        configured: true,
        registered: false,
      },
      slots: {
        actions: '<button data-testid="save-button">保存配置</button>',
      },
      global: {
        stubs: {
          ElTag: ElTagStub,
        },
      },
    });

    const toolbar = wrapper.get('.section-heading__toolbar');
    expect(toolbar.text()).toContain('待注册协议');
    expect(toolbar.get('[data-testid="save-button"]').text()).toBe('保存配置');
  });
});
