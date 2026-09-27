import { defineComponent, h } from 'vue';
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import ClientActivationDialog from './ClientActivationDialog.vue';

const ElDialogStub = defineComponent({
  props: { modelValue: Boolean },
  emits: ['update:modelValue'],
  setup(props, { slots }) {
    return () => props.modelValue ? h('div', { 'data-testid': 'client-activation-dialog' }, [slots.default?.(), slots.footer?.()]) : null;
  },
});

const ElInputStub = defineComponent({
  props: { modelValue: { type: String, default: '' }, readonly: Boolean },
  setup(props, { attrs }) {
    return () => h('textarea', {
      ...attrs,
      value: props.modelValue,
      readonly: props.readonly,
      'data-testid': 'client-activation-command',
    });
  },
});

const ElButtonStub = defineComponent({
  props: { disabled: Boolean, loading: Boolean },
  emits: ['click'],
  setup(props, { attrs, slots, emit }) {
    return () => h('button', {
      ...attrs,
      disabled: props.disabled,
      'data-loading': props.loading ? 'true' : undefined,
      onClick: () => emit('click'),
    }, slots.default?.());
  },
});

describe('ClientActivationDialog', () => {
  it('shows the activation copy and confirm actions', async () => {
    const wrapper = mount(ClientActivationDialog, {
      props: {
        modelValue: true,
        title: '激活本地 SSH 客户端',
        command: 'reg add HKCR\\ssh ...',
      },
      global: {
        stubs: {
          ElDialog: ElDialogStub,
          ElInput: ElInputStub,
          ElButton: ElButtonStub,
        },
      },
    });

    expect(wrapper.text()).toContain('请在CMD终端执行协议注册命令，激活本地客户端');
    expect(wrapper.get('[data-testid="client-activation-command"]').attributes('readonly')).toBeDefined();

    await wrapper.get('[data-testid="client-activation-copy"]').trigger('click');
    await wrapper.get('[data-testid="client-activation-confirm"]').trigger('click');

    expect(wrapper.emitted('copy')).toHaveLength(1);
    expect(wrapper.emitted('confirm')).toHaveLength(1);
  });
});
