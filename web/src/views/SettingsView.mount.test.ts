import { defineComponent, h } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  preferences: {
    loading: false,
    saving: false,
    loaded: true,
    error: '',
    sshProtocolRegistered: false,
    value: {
      theme: 'light',
      terminal_font_family: 'Cascadia Mono',
      terminal_font_size: 14,
      ssh_client: 'xshell',
      ssh_client_platform: 'windows',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
      db_client: 'dbeaver',
      db_client_platform: 'windows',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
      db_client_ca_file_path: '',
    },
    fetch: vi.fn(async () => mocks.preferences.value),
    update: vi.fn(async patch => ({ ...mocks.preferences.value, ...patch })),
    persistPartialToBrowser: vi.fn(),
    markSSHProtocolRegistered: vi.fn(),
  },
  databaseClient: {
    protocolRegistered: false,
    configured: true,
    directLaunchReady: false,
    markRegistered: vi.fn(),
    markUnregistered: vi.fn(),
  },
  writeClipboardText: vi.fn(),
  getDBGateway: vi.fn(),
  routerReplace: vi.fn(),
  route: { name: 'settings', query: { tab: 'appearance' } },
  messageSuccess: vi.fn(),
  messageWarning: vi.fn(),
  messageError: vi.fn(),
}));

vi.mock('vue-router', () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.routerReplace }),
}));

vi.mock('@/stores/preferences', () => ({
  usePreferencesStore: () => mocks.preferences,
}));

vi.mock('@/stores/databaseClient', () => ({
  useDatabaseClientStore: () => mocks.databaseClient,
}));

vi.mock('@/api/client', () => ({
  apiClient: { getDBGateway: mocks.getDBGateway },
}));

vi.mock('@/utils/clipboard', () => ({
  writeClipboardText: mocks.writeClipboardText,
}));

vi.mock('element-plus', async () => {
  const { defineComponent: define, h: createElement } = await import('vue');

  const ElButton = define({
    inheritAttrs: false,
    props: { disabled: Boolean, loading: Boolean, type: String },
    emits: ['click'],
    setup(props, { attrs, slots, emit }) {
      return () => createElement('button', {
        ...attrs,
        disabled: props.disabled,
        'data-loading': props.loading ? 'true' : undefined,
        onClick: () => emit('click'),
      }, slots.default?.());
    },
  });

  const ElInput = define({
    inheritAttrs: false,
    props: { modelValue: { type: String, default: '' }, readonly: Boolean },
    emits: ['update:modelValue'],
    setup(props, { attrs, emit, slots }) {
      return () => createElement('div', attrs, [
        createElement('input', {
          value: props.modelValue,
          readonly: props.readonly,
          onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
        }),
        slots.append?.(),
      ]);
    },
  });

  const ElInputNumber = define({
    inheritAttrs: false,
    props: { modelValue: { type: Number, default: 0 } },
    emits: ['update:modelValue'],
    setup(props, { attrs, emit }) {
      return () => createElement('input', {
        ...attrs,
        type: 'number',
        value: String(props.modelValue),
        onInput: (event: Event) => emit('update:modelValue', Number((event.target as HTMLInputElement).value)),
      });
    },
  });

  const ElOption = define({
    props: { label: String, value: String, disabled: Boolean },
    setup(props) {
      return () => createElement('option', { value: props.value, disabled: props.disabled }, props.label);
    },
  });

  const ElMessage = {
    success: mocks.messageSuccess,
    warning: mocks.messageWarning,
    error: mocks.messageError,
  };

  return { ElButton, ElInput, ElInputNumber, ElOption, ElMessage };
});

const passthroughStub = (tag: string) => defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h(tag, attrs, [slots.default?.(), slots.footer?.()]);
  },
});

const ElSelectStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { attrs, slots, emit }) {
    return () => h('select', {
      ...attrs,
      value: props.modelValue,
      onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLSelectElement).value),
    }, slots.default?.());
  },
});

const ElOptionStub = defineComponent({
  props: { label: String, value: String, disabled: Boolean },
  setup(props) {
    return () => h('option', { value: props.value, disabled: props.disabled }, props.label);
  },
});

const ClientSectionHeadingStub = defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h('section', attrs, [slots.default?.(), slots.actions?.()]);
  },
});

const ClientActivationDialogStub = defineComponent({
  props: {
    modelValue: Boolean,
    title: String,
    command: String,
    loading: Boolean,
  },
  emits: ['update:modelValue', 'copy', 'confirm'],
  setup(props, { emit }) {
    return () => props.modelValue ? h('section', { 'data-testid': 'client-activation-dialog' }, [
      h('p', '请执行协议注册命令，激活本地客户端'),
      h('pre', props.command || ''),
      h('button', {
        'data-testid': 'client-activation-copy',
        onClick: () => emit('copy'),
      }, '复制命令'),
      h('button', {
        'data-testid': 'client-activation-confirm',
        onClick: () => emit('confirm'),
      }, '已激活'),
    ]) : null;
  },
});

import SettingsView from './SettingsView.vue';

describe('SettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('saves appearance fields directly without opening an activation dialog', async () => {
    const wrapper = mount(SettingsView, {
      attachTo: document.body,
      global: {
        stubs: {
          ElCard: passthroughStub('section'),
          ElTabs: passthroughStub('div'),
          ElTabPane: passthroughStub('section'),
          ElForm: passthroughStub('form'),
          ElFormItem: passthroughStub('div'),
          ElSelect: ElSelectStub,
          ElOption: ElOptionStub,
          ElTag: passthroughStub('span'),
          ClientSectionHeading: ClientSectionHeadingStub,
          ClientActivationDialog: ClientActivationDialogStub,
        },
      },
    });
    await flushPromises();

    await wrapper.get('[data-testid="settings-save-appearance"]').trigger('click');

    expect(mocks.preferences.update).toHaveBeenCalledWith({
      theme: 'light',
      terminal_font_family: 'Cascadia Mono',
      terminal_font_size: 14,
    });
    expect(wrapper.find('[data-testid="client-activation-dialog"]').exists()).toBe(false);
  });

  it('opens the SSH activation dialog and saves only ssh fields after confirmation', async () => {
    const wrapper = mount(SettingsView, {
      attachTo: document.body,
      global: {
        stubs: {
          ElCard: passthroughStub('section'),
          ElTabs: passthroughStub('div'),
          ElTabPane: passthroughStub('section'),
          ElForm: passthroughStub('form'),
          ElFormItem: passthroughStub('div'),
          ElSelect: ElSelectStub,
          ElOption: ElOptionStub,
          ElTag: passthroughStub('span'),
          ClientSectionHeading: ClientSectionHeadingStub,
          ClientActivationDialog: ClientActivationDialogStub,
        },
      },
    });
    await flushPromises();

    await wrapper.get('[data-testid="settings-save-ssh"]').trigger('click');
    expect(wrapper.get('[data-testid="client-activation-dialog"]').text()).toContain('请执行协议注册命令，激活本地客户端');
    expect(mocks.preferences.update).not.toHaveBeenCalled();

    await wrapper.get('[data-testid="client-activation-confirm"]').trigger('click');

    expect(mocks.preferences.update).toHaveBeenCalledWith({
      ssh_client: 'xshell',
      ssh_client_platform: 'windows',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
    });
    expect(mocks.preferences.persistPartialToBrowser).toHaveBeenCalledWith({
      ssh_client: 'xshell',
      ssh_client_platform: 'windows',
      ssh_client_path: 'C:\\Tools\\Xshell\\Xshell.exe',
    });
    expect(mocks.preferences.markSSHProtocolRegistered).toHaveBeenCalledWith(true);
  });

  it('opens the database activation dialog and saves only database fields after confirmation', async () => {
    mocks.route.query.tab = 'database';
    const wrapper = mount(SettingsView, {
      attachTo: document.body,
      global: {
        stubs: {
          ElCard: passthroughStub('section'),
          ElTabs: passthroughStub('div'),
          ElTabPane: passthroughStub('section'),
          ElForm: passthroughStub('form'),
          ElFormItem: passthroughStub('div'),
          ElSelect: ElSelectStub,
          ElOption: ElOptionStub,
          ElTag: passthroughStub('span'),
          ClientSectionHeading: ClientSectionHeadingStub,
          ClientActivationDialog: ClientActivationDialogStub,
        },
      },
    });
    await flushPromises();

    await wrapper.get('[data-testid="settings-save-database"]').trigger('click');
    await wrapper.get('[data-testid="client-activation-confirm"]').trigger('click');

    expect(mocks.preferences.update).toHaveBeenCalledWith({
      db_client: 'dbeaver',
      db_client_platform: 'windows',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
      db_client_ca_file_path: '',
    });
    expect(mocks.preferences.persistPartialToBrowser).toHaveBeenCalledWith({
      db_client: 'dbeaver',
      db_client_platform: 'windows',
      db_client_path: 'C:\\Program Files\\DBeaver\\dbeaverc.exe',
      db_client_ca_file_path: '',
    });
    expect(mocks.databaseClient.markRegistered).toHaveBeenCalled();
  });
});
