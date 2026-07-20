import assert from 'node:assert/strict';
import { h, defineComponent } from 'vue';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { beforeEach, describe, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  routerPush: vi.fn(),
  messageAlert: vi.fn(),
  buildDatabaseProtocolURL: vi.fn((_input: unknown) => 'jianmen-db://connect/test-payload'),
  preferences: {
    loading: false,
    loaded: true,
    hasSSHClient: true,
    value: { ssh_client: 'default', ssh_client_path: '' },
    error: '',
    fetch: vi.fn(),
    update: vi.fn(),
  },
  databaseClient: {
    configured: false,
    directLaunchReady: false,
    value: {
      client: '',
      platform: 'windows',
      executablePath: '',
      caFilePath: '',
      protocolRegistered: false,
    },
  },
  apiClient: {
    createUserSession: vi.fn(),
    createConnectionPassword: vi.fn(),
    testTargetConnection: vi.fn(),
    testDBConnection: vi.fn(),
    getDBGateway: vi.fn(),
  },
}));

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: mocks.routerPush,
    currentRoute: { value: { fullPath: '/databases' } },
  }),
}));

vi.mock('@/stores/preferences', () => ({
  usePreferencesStore: () => mocks.preferences,
}));

vi.mock('@/stores/databaseClient', () => ({
  useDatabaseClientStore: () => mocks.databaseClient,
}));

vi.mock('@/api/client', () => ({
  apiClient: mocks.apiClient,
}));

vi.mock('@/config/databaseClients', () => ({
  buildDatabaseProtocolURL: mocks.buildDatabaseProtocolURL,
}));

vi.mock('@/utils/clipboard', () => ({
  writeClipboardText: vi.fn(),
}));

vi.mock('@element-plus/icons-vue', () => ({
  Loading: defineComponent({ setup: () => () => h('span', { 'data-testid': 'loading-icon' }) }),
}));

vi.mock('element-plus', async () => {
  const { defineComponent: define, h: createElement } = await import('vue');

  const ElButton = define({
    inheritAttrs: false,
    props: { disabled: Boolean, loading: Boolean },
    setup(props, { attrs, slots }) {
      return () => createElement('button', {
        ...attrs,
        disabled: props.disabled,
        'data-loading': props.loading ? 'true' : undefined,
      }, slots.default?.());
    },
  });

  const ElInput = define({
    inheritAttrs: false,
    props: { modelValue: { type: String, default: '' }, readonly: Boolean },
    emits: ['update:modelValue'],
    setup(props, { attrs, emit, slots }) {
      return () => createElement('div', { ...attrs, class: ['el-input', attrs.class] }, [
        createElement('input', {
          value: props.modelValue,
          readonly: props.readonly,
          onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
        }),
        slots.append?.(),
      ]);
    },
  });

  return {
    ElButton,
    ElInput,
    ElMessage: { success: vi.fn(), warning: vi.fn(), error: vi.fn() },
    ElMessageBox: { alert: mocks.messageAlert },
  };
});

const passthroughStub = (tag: string) => defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h(tag, attrs, [slots.default?.(), slots.footer?.()]);
  },
});

import ConnectionConfigDialog from './ConnectionConfigDialog.vue';

type DialogWrapper = VueWrapper<InstanceType<typeof ConnectionConfigDialog>>;

const target = {
  id: 'host-1',
  name: 'test-host',
  username: 'admin',
  host: '10.0.0.1',
  port: 22,
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.databaseClient.configured = false;
  mocks.databaseClient.directLaunchReady = false;
  Object.assign(mocks.databaseClient.value, {
    client: '',
    platform: 'windows',
    executablePath: '',
    caFilePath: '',
    protocolRegistered: false,
  });
  mocks.messageAlert.mockResolvedValue(undefined);
  mocks.apiClient.createUserSession.mockResolvedValue({ compact_username: 'admin@host-1' });
  mocks.apiClient.createConnectionPassword.mockResolvedValue({ password: 'temporary-password', expires_at: '' });
  mocks.apiClient.testTargetConnection.mockResolvedValue({ ok: true, latency_ms: 1 });
  mocks.apiClient.testDBConnection.mockResolvedValue({ ok: true, latency_ms: 1 });
  mocks.apiClient.getDBGateway.mockResolvedValue({
    enabled: true,
    connectable: true,
    mode: 'unified',
    protocol: 'postgresql',
    port: 33060,
    tls_enabled: true,
    tls_server_name: 'localhost',
    tls_ca_pem: '-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----',
    tls_cert_sha256: 'AA:BB',
  });
});

async function mountDialog(allowSsh: boolean, allowSftp: boolean): Promise<DialogWrapper> {
  const wrapper = mount(ConnectionConfigDialog, {
    props: {
      modelValue: false,
      resourceType: 'host',
      target,
      resourceName: 'test-host',
      sourceAddress: '10.0.0.1:22',
      sourceAccount: 'admin',
      allowSsh,
      allowSftp,
    },
    global: {
      stubs: {
        ElDialog: passthroughStub('section'),
        ElAlert: passthroughStub('div'),
        ElTag: passthroughStub('span'),
        ElForm: passthroughStub('form'),
        ElFormItem: passthroughStub('div'),
        ElSelect: passthroughStub('div'),
        ElOption: passthroughStub('option'),
        ElIcon: passthroughStub('span'),
      },
    },
  }) as DialogWrapper;

  await wrapper.setProps({ modelValue: true });
  await flushPromises();
  return wrapper;
}

async function mountDatabaseDialog(protocol = 'postgres'): Promise<DialogWrapper> {
  const wrapper = mount(ConnectionConfigDialog, {
    props: {
      modelValue: false,
      resourceType: 'database',
      target: {
        id: 'database-account-1',
        resource_id: 'D0001',
        username: 'reporter',
      },
      resourceName: 'reporting',
      sourceAddress: 'db.internal:5432',
      sourceAccount: 'reporter',
      protocol,
    },
    global: {
      stubs: {
        ElDialog: passthroughStub('section'),
        ElAlert: passthroughStub('div'),
        ElTag: passthroughStub('span'),
        ElForm: passthroughStub('form'),
        ElFormItem: passthroughStub('div'),
        ElSelect: passthroughStub('div'),
        ElOption: passthroughStub('option'),
        ElIcon: passthroughStub('span'),
      },
    },
  }) as DialogWrapper;

  await wrapper.setProps({ modelValue: true });
  await flushPromises();
  return wrapper;
}

function assertVisibility(wrapper: DialogWrapper, selector: string, expected: boolean): void {
  assert.equal(wrapper.find(`[data-testid="${selector}"]`).exists(), expected, selector);
}

describe('ConnectionConfigDialog permission controls', () => {
  it.each([
    { allowSsh: true, allowSftp: true },
    { allowSsh: true, allowSftp: false },
    { allowSsh: false, allowSftp: true },
    { allowSsh: false, allowSftp: false },
  ])('renders each control according to allowSsh=$allowSsh allowSftp=$allowSftp', async ({ allowSsh, allowSftp }) => {
    const wrapper = await mountDialog(allowSsh, allowSftp);

    assertVisibility(wrapper, 'ssh-local-client', allowSsh);
    assertVisibility(wrapper, 'ssh-browser', allowSsh);
    assertVisibility(wrapper, 'connection-command-ssh', allowSsh);
    assertVisibility(wrapper, 'connection-command-sftp', allowSftp);

    wrapper.unmount();
  });

  it('blocks SSH launch controls and emits refresh when the host key changes', async () => {
    mocks.apiClient.testTargetConnection.mockRejectedValue(Object.assign(
      new Error('host key changed'),
      {
        code: 'SSH_HOST_KEY_CHANGED',
        details: {
          host_id: 'host-1',
          old_fingerprint: 'SHA256:old',
          new_fingerprint: 'SHA256:new',
          host_disabled: true,
        },
      },
    ));

    const wrapper = await mountDialog(true, true);

    assert.equal(wrapper.get('[data-testid="ssh-local-client"]').attributes('disabled'), '');
    assert.equal(wrapper.get('[data-testid="ssh-browser"]').attributes('disabled'), '');
    assert.deepEqual(wrapper.emitted('hostIdentityChanged'), [['host-1']]);
    assert.equal(mocks.messageAlert.mock.calls.length, 1);
    wrapper.unmount();
  });
});

describe('ConnectionConfigDialog database local client', () => {
  it('redirects to local client settings when DBeaver is not configured', async () => {
    const wrapper = await mountDatabaseDialog();

    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    assert.deepEqual(mocks.routerPush.mock.calls.at(-1)?.[0], {
      path: '/settings',
      query: { tab: 'database', return_to: '/databases' },
    });
    wrapper.unmount();
  });

  it('launches DBeaver with the issued temporary password when local setup is ready', async () => {
    mocks.databaseClient.configured = true;
    mocks.databaseClient.directLaunchReady = true;
    Object.assign(mocks.databaseClient.value, {
      client: 'dbeaver',
      platform: 'windows',
      executablePath: 'C:\\DBeaver\\dbeaverc.exe',
      caFilePath: 'C:\\Users\\Alice\\Downloads\\jianmen-database-gateway-ca.pem',
      protocolRegistered: true,
    });
    const wrapper = await mountDatabaseDialog();

    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    assert.deepEqual(mocks.buildDatabaseProtocolURL.mock.calls.at(-1)?.[0], {
      protocol: 'postgres',
      host: 'localhost',
      port: 33060,
      username: 'admin@host-1',
      password: 'temporary-password',
      databaseName: 'postgres',
      connectionName: 'reporting',
    });
    wrapper.unmount();
  });

  it('does not force the PostgreSQL default database when launching MySQL', async () => {
    mocks.databaseClient.configured = true;
    mocks.databaseClient.directLaunchReady = true;
    Object.assign(mocks.databaseClient.value, {
      client: 'dbeaver',
      platform: 'windows',
      executablePath: 'C:\\DBeaver\\dbeaverc.exe',
      caFilePath: 'C:\\Users\\Alice\\Downloads\\jianmen-database-gateway-ca.pem',
      protocolRegistered: true,
    });
    const wrapper = await mountDatabaseDialog('mysql');

    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    const launchInput = mocks.buildDatabaseProtocolURL.mock.calls.at(-1)?.[0] as {
      databaseName?: string;
    } | undefined;
    assert.equal(launchInput?.databaseName, '');
    wrapper.unmount();
  });
});
