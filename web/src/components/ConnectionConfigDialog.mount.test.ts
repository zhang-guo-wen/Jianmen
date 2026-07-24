import assert from 'node:assert/strict';
import { h, defineComponent } from 'vue';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { beforeEach, describe, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  routerPush: vi.fn(),
  messageConfirm: vi.fn(),
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
    refreshSSHHostIdentity: vi.fn(),
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
  DATABASE_CLIENT_CA_FILE_NAME: 'jianmen-database-gateway-ca.pem',
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

  const ElSwitch = define({
    inheritAttrs: false,
    props: { modelValue: Boolean, disabled: Boolean },
    emits: ['update:modelValue'],
    setup(props, { attrs, emit }) {
      return () => createElement('button', {
        ...attrs,
        type: 'button',
        disabled: props.disabled,
        'data-testid': 'database-tls-switch',
        'aria-pressed': props.modelValue ? 'true' : 'false',
        onClick: () => {
          if (!props.disabled) emit('update:modelValue', !props.modelValue);
        },
      });
    },
  });

  return {
    ElButton,
    ElInput,
    ElSwitch,
    ElMessage: { success: vi.fn(), warning: vi.fn(), error: vi.fn() },
    ElMessageBox: { confirm: mocks.messageConfirm },
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
  mocks.messageConfirm.mockResolvedValue('confirm');
  mocks.apiClient.createUserSession.mockResolvedValue({ compact_username: 'admin@host-1' });
  mocks.apiClient.createConnectionPassword.mockResolvedValue({ password: 'temporary-password', expires_at: '' });
  mocks.apiClient.testTargetConnection.mockResolvedValue({ ok: true, latency_ms: 1 });
  mocks.apiClient.refreshSSHHostIdentity.mockResolvedValue({
    id: 'host-1',
    name: 'test-host',
    address: '10.0.0.1',
    port: 22,
    status: 'active',
    identity_status: 'available',
  });
  mocks.apiClient.testDBConnection.mockResolvedValue({ ok: true, latency_ms: 1 });
  mocks.apiClient.getDBGateway.mockResolvedValue({
    enabled: true,
    connectable: true,
    mode: 'unified',
    protocol: 'postgresql',
    host: 'gateway.internal',
    port: 33060,
    client_tls_mode: 'optional',
    tls_enabled: true,
    tls_trust_mode: 'custom',
    tls_server_name: 'database.example',
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

async function mountDatabaseDialog(protocol = 'postgres', allowWebSql = true): Promise<DialogWrapper> {
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
      allowWebSql,
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

  it('confirms, refreshes, and retests when the host key changes', async () => {
    mocks.apiClient.testTargetConnection
      .mockRejectedValueOnce(Object.assign(
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
      ))
      .mockResolvedValueOnce({ ok: true, latency_ms: 2 });

    const wrapper = await mountDialog(true, true);

    assert.equal(wrapper.get('[data-testid="ssh-local-client"]').attributes('disabled'), undefined);
    assert.equal(wrapper.get('[data-testid="ssh-browser"]').attributes('disabled'), undefined);
    assert.deepEqual(mocks.apiClient.refreshSSHHostIdentity.mock.calls, [['host-1', 'SHA256:new']]);
    assert.equal(mocks.apiClient.testTargetConnection.mock.calls.length, 2);
    assert.equal(wrapper.emitted('hostIdentityChanged')?.[0]?.[0]?.status, 'active');
    assert.equal(mocks.messageConfirm.mock.calls.length, 1);
    assert.equal(mocks.messageConfirm.mock.calls[0]?.[1], '连接确认');
    assert.equal(mocks.messageConfirm.mock.calls[0]?.[2]?.type, 'warning');
    wrapper.unmount();
  });

  it('does not confirm or refresh an identity result after the component unmounts', async () => {
    let rejectConnectionTest: (error: unknown) => void = () => undefined;
    mocks.apiClient.testTargetConnection.mockImplementationOnce(
      () => new Promise((_resolve, reject) => {
        rejectConnectionTest = reject;
      }),
    );
    const wrapper = await mountDialog(true, true);

    wrapper.unmount();
    rejectConnectionTest(Object.assign(
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
    await flushPromises();

    assert.equal(mocks.messageConfirm.mock.calls.length, 0);
    assert.equal(mocks.apiClient.refreshSSHHostIdentity.mock.calls.length, 0);
  });

  it('invalidates the visible host state without trusting a key when confirmation is cancelled', async () => {
    mocks.messageConfirm.mockRejectedValueOnce('cancel');
    mocks.apiClient.testTargetConnection.mockRejectedValueOnce(Object.assign(
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

    assert.deepEqual(wrapper.emitted('hostIdentityInvalidated'), [['host-1']]);
    assert.equal(mocks.apiClient.refreshSSHHostIdentity.mock.calls.length, 0);
    assert.equal(mocks.apiClient.testTargetConnection.mock.calls.length, 1);
    wrapper.unmount();
  });
});

describe('ConnectionConfigDialog database local client', () => {
  it('opens the Web SQL console with the selected database account', async () => {
    const wrapper = await mountDatabaseDialog();

    await wrapper.get('[data-testid="database-web-sql"]').trigger('click');

    assert.deepEqual(mocks.routerPush.mock.calls.at(-1)?.[0], {
      path: '/sql-console',
      query: { database_account_id: 'database-account-1' },
    });
    assert.deepEqual(wrapper.emitted('update:modelValue')?.at(-1), [false]);
    wrapper.unmount();
  });

  it('hides the Web SQL entry without permission or for Redis accounts', async () => {
    const unauthorizedWrapper = await mountDatabaseDialog('postgres', false);
    assertVisibility(unauthorizedWrapper, 'database-web-sql', false);
    unauthorizedWrapper.unmount();

    const redisWrapper = await mountDatabaseDialog('redis', true);
    assertVisibility(redisWrapper, 'database-web-sql', false);
    redisWrapper.unmount();
  });

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
      host: 'gateway.internal',
      port: 33060,
      username: 'admin@host-1',
      password: 'temporary-password',
      databaseName: 'postgres',
      connectionName: 'reporting',
      tls: 'disable',
    });
    wrapper.unmount();
  });

  it('uses the certificate server name when optional TLS is enabled manually', async () => {
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

    await wrapper.get('[data-testid="database-tls-switch"]').trigger('click');
    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    const launchInput = mocks.buildDatabaseProtocolURL.mock.calls.at(-1)?.[0] as {
      host?: string;
      tls?: string;
      tlsTrust?: string;
    } | undefined;
    assert.equal(launchInput?.host, 'database.example');
    assert.equal(launchInput?.tls, 'verify-full');
    assert.equal(launchInput?.tlsTrust, 'custom');
    wrapper.unmount();
  });

  it('allows optional TLS with client default trust and no CA path', async () => {
    mocks.databaseClient.configured = true;
    mocks.databaseClient.directLaunchReady = true;
    Object.assign(mocks.databaseClient.value, {
      client: 'dbeaver',
      platform: 'windows',
      executablePath: 'C:\\DBeaver\\dbeaverc.exe',
      caFilePath: '',
      protocolRegistered: true,
    });
    mocks.apiClient.getDBGateway.mockResolvedValue({
      enabled: true,
      connectable: true,
      mode: 'unified',
      protocol: 'postgresql',
      host: 'gateway.internal',
      port: 33060,
      client_tls_mode: 'optional',
      tls_enabled: true,
      tls_trust_mode: 'system',
      tls_server_name: 'database.example',
      tls_cert_sha256: 'AA:BB',
    });
    const wrapper = await mountDatabaseDialog();

    await wrapper.get('[data-testid="database-tls-switch"]').trigger('click');
    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    const launchInput = mocks.buildDatabaseProtocolURL.mock.calls.at(-1)?.[0] as {
      host?: string;
      tls?: string;
      tlsTrust?: string;
    } | undefined;
    assert.equal(launchInput?.host, 'database.example');
    assert.equal(launchInput?.tls, 'verify-full');
    assert.equal(launchInput?.tlsTrust, 'system');
    assert.equal(mocks.routerPush.mock.calls.length, 0);
    wrapper.unmount();
  });

  it('forces verified TLS when the gateway policy is required', async () => {
    mocks.databaseClient.configured = true;
    mocks.databaseClient.directLaunchReady = true;
    Object.assign(mocks.databaseClient.value, {
      client: 'dbeaver',
      platform: 'windows',
      executablePath: 'C:\\DBeaver\\dbeaverc.exe',
      caFilePath: 'C:\\Users\\Alice\\Downloads\\jianmen-database-gateway-ca.pem',
      protocolRegistered: true,
    });
    mocks.apiClient.getDBGateway.mockResolvedValue({
      enabled: true,
      connectable: true,
      mode: 'unified',
      protocol: 'postgresql',
      host: 'gateway.internal',
      port: 33060,
      client_tls_mode: 'required',
      tls_enabled: true,
      tls_trust_mode: 'custom',
      tls_server_name: 'database.example',
      tls_ca_pem: '-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----',
      tls_cert_sha256: 'AA:BB',
    });
    const wrapper = await mountDatabaseDialog();

    assert.equal(wrapper.get('[data-testid="database-tls-switch"]').attributes('disabled'), '');
    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    const launchInput = mocks.buildDatabaseProtocolURL.mock.calls.at(-1)?.[0] as {
      host?: string;
      tls?: string;
      tlsTrust?: string;
    } | undefined;
    assert.equal(launchInput?.host, 'database.example');
    assert.equal(launchInput?.tls, 'verify-full');
    assert.equal(launchInput?.tlsTrust, 'custom');
    wrapper.unmount();
  });

  it('redirects to client settings before required TLS launch when no CA path is configured', async () => {
    mocks.databaseClient.configured = true;
    mocks.databaseClient.directLaunchReady = true;
    mocks.apiClient.getDBGateway.mockResolvedValue({
      enabled: true,
      connectable: true,
      mode: 'unified',
      protocol: 'postgresql',
      host: 'gateway.internal',
      port: 33060,
      client_tls_mode: 'required',
      tls_enabled: true,
      tls_trust_mode: 'custom',
      tls_server_name: 'database.example',
      tls_ca_pem: '-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----',
      tls_cert_sha256: 'AA:BB',
    });
    const wrapper = await mountDatabaseDialog();

    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    assert.equal(mocks.buildDatabaseProtocolURL.mock.calls.length, 0);
    assert.deepEqual(mocks.routerPush.mock.calls.at(-1)?.[0], {
      path: '/settings',
      query: { tab: 'database', return_to: '/databases' },
    });
    wrapper.unmount();
  });

  it('launches required TLS with client default trust when no CA path is configured', async () => {
    mocks.databaseClient.configured = true;
    mocks.databaseClient.directLaunchReady = true;
    Object.assign(mocks.databaseClient.value, {
      client: 'dbeaver',
      platform: 'windows',
      executablePath: 'C:\\DBeaver\\dbeaverc.exe',
      caFilePath: '',
      protocolRegistered: true,
    });
    mocks.apiClient.getDBGateway.mockResolvedValue({
      enabled: true,
      connectable: true,
      mode: 'unified',
      protocol: 'postgresql',
      host: 'gateway.internal',
      port: 33060,
      client_tls_mode: 'required',
      tls_enabled: true,
      tls_trust_mode: 'system',
      tls_server_name: 'database.example',
      tls_cert_sha256: 'AA:BB',
    });
    const wrapper = await mountDatabaseDialog();

    await wrapper.get('[data-testid="database-local-client"]').trigger('click');

    const launchInput = mocks.buildDatabaseProtocolURL.mock.calls.at(-1)?.[0] as {
      host?: string;
      tls?: string;
      tlsTrust?: string;
    } | undefined;
    assert.equal(launchInput?.host, 'database.example');
    assert.equal(launchInput?.tls, 'verify-full');
    assert.equal(launchInput?.tlsTrust, 'system');
    assert.equal(mocks.routerPush.mock.calls.length, 0);
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
