import { defineComponent, h, inject, provide } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  apiClient: {
    getSystemSettings: vi.fn(),
    getSystemSettingsRevisions: vi.fn(),
    updateSystemSettings: vi.fn(),
    testSystemSettingsGuacd: vi.fn(),
    testSystemSettingsObjectStorage: vi.fn(),
  },
  message: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
  confirm: vi.fn(),
}))

vi.mock('@/api/client', async importOriginal => {
  const actual = await importOriginal<typeof import('@/api/client')>()
  return {
    ...actual,
    apiClient: { ...actual.apiClient, ...mocks.apiClient },
  }
})

vi.mock('element-plus', async importOriginal => {
  const actual = await importOriginal<typeof import('element-plus')>()
  return {
    ...actual,
    ElMessage: mocks.message,
    ElMessageBox: { confirm: mocks.confirm },
  }
})

import SystemSettingsView from './SystemSettingsView.vue'

const slotHost = (tag = 'div') => defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h(tag, attrs, slots.default?.())
  },
})

const ElButtonStub = defineComponent({
  inheritAttrs: false,
  props: { disabled: Boolean, loading: Boolean },
  setup(props, { attrs, slots }) {
    return () => h('button', {
      ...attrs,
      disabled: props.disabled || props.loading,
    }, slots.default?.())
  },
})

const ElTagStub = defineComponent({
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag-stub' }, slots.default?.())
  },
})

const tableRowKey = Symbol('table-row')

const ElTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    provide(tableRowKey, () => props.data[0])
    return () => h('table', slots.default?.())
  },
})

const ElTableColumnStub = defineComponent({
  setup(_, { slots }) {
    const row = inject<() => unknown>(tableRowKey, () => undefined)
    return () => row() ? h('div', slots.default?.({ row: row() })) : null
  },
})

const ElTabsStub = slotHost('section')
const ElTabPaneStub = slotHost('section')
const ElCardStub = defineComponent({
  setup(_, { slots }) {
    return () => h('section', [
      h('header', slots.header?.()),
      slots.default?.(),
    ])
  },
})
const ElAlertStub = slotHost('div')
const ElDescriptionsStub = slotHost('dl')
const ElDescriptionsItemStub = slotHost('div')
const ElInputNumberStub = defineComponent({
  props: { modelValue: { type: Number, default: 0 } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', {
      type: 'number',
      value: props.modelValue,
      onInput: (event: Event) => emit('update:modelValue', Number((event.target as HTMLInputElement).value)),
    })
  },
})
const ElSwitchStub = defineComponent({
  props: { modelValue: Boolean },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', {
      type: 'checkbox',
      checked: props.modelValue,
      onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked),
    })
  },
})
const ElRadioGroupStub = slotHost('div')
const ElRadioButtonStub = slotHost('label')

function buildState(pendingRestart: boolean) {
  return {
    desired: {
      database_gateway_mode: 'unified',
      database_gateway_client_tls_mode: 'optional',
      web_rdp_enabled: false,
      web_rdp_connect_timeout_seconds: 15,
      web_rdp_allow_unrecorded: false,
      database_max_client_message_bytes: 10485760,
      recording_enabled: true,
      recording_record_input: false,
      recording_record_commands: true,
      recording_retention_days: 30,
      recording_max_replay_bytes: 0,
      recording_cleanup_batch_size: 100,
    },
    effective: {
      database_gateway_mode: 'unified',
      database_gateway_client_tls_mode: 'optional',
      web_rdp_enabled: false,
      web_rdp_connect_timeout_seconds: 15,
      web_rdp_allow_unrecorded: false,
      database_max_client_message_bytes: 10485760,
      recording_enabled: true,
      recording_record_input: false,
      recording_record_commands: true,
      recording_retention_days: 30,
      recording_max_replay_bytes: 0,
      recording_cleanup_batch_size: 100,
    },
    revision: pendingRestart ? 2 : 1,
    effective_revision: 1,
    pending_restart: pendingRestart,
    updated_by_username: 'system',
    updated_at: '2026-07-21T09:23:34Z',
    infrastructure: {
      guacd: { address: '127.0.0.1:4822' },
      directories: {
        spool_dir: '/tmp/spool',
        guacd_recording_root: '/tmp/recordings',
        local_drive_root: '/tmp/drive',
        guacd_drive_root: '/tmp/guacd-drive',
        replay_dir: '/tmp/replay',
      },
      object_storage: {
        provider: 'filesystem',
        local_dir: '/tmp/object-storage',
        endpoint: '',
        bucket: '',
        region: '',
        prefix: '',
        secure: false,
        path_style: false,
        auto_create_bucket: false,
        access_key_id_configured: false,
        secret_access_key_configured: false,
        session_token_configured: false,
        credentials_configured: false,
      },
    },
  }
}

function mountView() {
  return mount(SystemSettingsView, {
    global: {
      directives: {
        loading: {},
      },
      stubs: {
        ElCard: ElCardStub,
        ElButton: ElButtonStub,
        ElTag: ElTagStub,
        ElTabs: ElTabsStub,
        ElTabPane: ElTabPaneStub,
        ElAlert: ElAlertStub,
        ElDescriptions: ElDescriptionsStub,
        ElDescriptionsItem: ElDescriptionsItemStub,
        ElTable: ElTableStub,
        ElTableColumn: ElTableColumnStub,
        ElInputNumber: ElInputNumberStub,
        ElSwitch: ElSwitchStub,
        ElRadioGroup: ElRadioGroupStub,
        ElRadioButton: ElRadioButtonStub,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.confirm.mockResolvedValue(undefined)
  mocks.apiClient.getSystemSettingsRevisions.mockResolvedValue({
    items: [{
      revision: 2,
      changed_fields: ['database_gateway_mode'],
      created_at: '2026-07-21T09:23:34Z',
    }],
  })
  mocks.apiClient.testSystemSettingsGuacd.mockResolvedValue({ ok: true, message: '', latency_ms: 1 })
  mocks.apiClient.testSystemSettingsObjectStorage.mockResolvedValue({ ok: true, message: '', latency_ms: 1 })
})

describe('SystemSettingsView header', () => {
  it('shows a disabled restart button and no status strip when settings are running', async () => {
    mocks.apiClient.getSystemSettings.mockResolvedValue(buildState(false))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('运行中')
    expect(wrapper.text()).toContain('重启系统')
    expect(wrapper.find('.status-strip').exists()).toBe(false)
    const restartButton = wrapper.findAll('button').find(button => button.text() === '重启系统')
    expect(restartButton?.attributes('disabled')).toBeDefined()
  })

  it('shows 待重启生效 without rendering the removed status strip', async () => {
    mocks.apiClient.getSystemSettings.mockResolvedValue(buildState(true))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('待重启生效')
    expect(wrapper.text()).not.toContain('等待重启')
    expect(wrapper.find('.status-strip').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('变更明细')
  })

  it('keeps revision history field labels readable', async () => {
    mocks.apiClient.getSystemSettings.mockResolvedValue(buildState(false))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('数据库网关入口模式')
    expect(wrapper.text()).not.toContain('database_gateway_mode')
  })
})
