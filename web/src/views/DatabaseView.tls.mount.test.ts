import { defineComponent, h, inject, nextTick, provide } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  apiClient: {
    getDBInstances: vi.fn(),
    getDBConnections: vi.fn(),
    getResourceGroups: vi.fn(),
    preflightDBInstanceTLS: vi.fn(),
    createDBInstance: vi.fn(),
    updateDBInstance: vi.fn(),
  },
  confirm: vi.fn(),
  message: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}))

vi.mock('@/api/client', async importOriginal => {
  const actual = await importOriginal<typeof import('@/api/client')>()
  return {
    ...actual,
    apiClient: { ...actual.apiClient, ...mocks.apiClient },
  }
})

vi.mock('@/stores/permission', () => ({
  usePermissionStore: () => ({
    canDo: () => true,
    canAccessMenu: () => true,
  }),
}))

vi.mock('vue-router', async importOriginal => {
  const actual = await importOriginal<typeof import('vue-router')>()
  return {
    ...actual,
    useRouter: () => ({ push: vi.fn() }),
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

import DatabaseView from './DatabaseView.vue'

const slotHost = (tag = 'div') => defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h(tag, attrs, slots.default?.())
  },
})

const tableRowKey = Symbol('table-row')

const DataTableCardStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    provide(tableRowKey, () => props.data[0])
    return () => h('section', [
      slots['toolbar-filter']?.(),
      slots['toolbar-extra']?.(),
      props.data.length > 0 ? slots.default?.() : null,
    ])
  },
})

const ElTableColumnStub = defineComponent({
  setup(_, { slots }) {
    const row = inject<() => unknown>(tableRowKey, () => undefined)
    return () => row() ? h('div', slots.default?.({ row: row() })) : null
  },
})

const FormDialogStub = defineComponent({
  props: { visible: Boolean, loading: Boolean },
  emits: ['update:visible', 'submit'],
  setup(props, { emit, slots }) {
    return () => props.visible
      ? h('section', { 'data-testid': 'instance-dialog' }, [
          slots.default?.(),
          h('button', {
            'data-testid': 'save-instance',
            disabled: props.loading,
            onClick: () => emit('submit'),
          }, '保存'),
        ])
      : null
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

const ElSelectStub = defineComponent({
  inheritAttrs: false,
  props: {
    modelValue: { type: String, default: '' },
    disabled: Boolean,
  },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit, slots }) {
    return () => h('select', {
      ...attrs,
      value: props.modelValue,
      disabled: props.disabled,
      onChange: (event: Event) => {
        const value = (event.target as HTMLSelectElement).value
        emit('update:modelValue', value)
        emit('change', value)
      },
    }, slots.default?.())
  },
})

const ElOptionStub = defineComponent({
  props: {
    label: { type: String, default: '' },
    value: { type: String, default: '' },
  },
  setup(props) {
    return () => h('option', { value: props.value }, props.label)
  },
})

const ElInputStub = defineComponent({
  inheritAttrs: false,
  props: {
    modelValue: { type: String, default: '' },
    type: { type: String, default: 'text' },
    disabled: Boolean,
  },
  emits: ['update:modelValue', 'input'],
  setup(props, { attrs, emit }) {
    return () => h(props.type === 'textarea' ? 'textarea' : 'input', {
      ...attrs,
      value: props.modelValue,
      disabled: props.disabled,
      onInput: (event: Event) => {
        const value = (event.target as HTMLInputElement).value
        emit('update:modelValue', value)
        emit('input', value)
      },
    })
  },
})

const ElInputNumberStub = defineComponent({
  props: { modelValue: { type: Number, default: 0 } },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit }) {
    return () => h('input', {
      type: 'number',
      value: props.modelValue,
      onChange: (event: Event) => {
        const value = Number((event.target as HTMLInputElement).value)
        emit('update:modelValue', value)
        emit('change', value)
      },
    })
  },
})

const ElAlertStub = defineComponent({
  props: { title: { type: String, default: '' } },
  setup(props) {
    return () => h('p', { class: 'alert' }, props.title)
  },
})

function mountDatabaseView() {
  return mount(DatabaseView, {
    global: {
      stubs: {
        DataTableCard: DataTableCardStub,
        FormDialog: FormDialogStub,
        ConnectionConfigDialog: true,
        DatabaseAccountFormDialog: true,
        DatabaseAutoProvisionDialog: true,
        ResourceFilterBar: true,
        StatusSwitch: true,
        ElButton: ElButtonStub,
        ElSelect: ElSelectStub,
        ElOption: ElOptionStub,
        ElInput: ElInputStub,
        ElInputNumber: ElInputNumberStub,
        ElAlert: ElAlertStub,
        ElForm: slotHost('form'),
        ElFormItem: slotHost(),
        ElCollapse: slotHost(),
        ElCollapseItem: slotHost(),
        ElDropdown: true,
        ElDropdownMenu: true,
        ElDropdownItem: true,
        ElTableColumn: ElTableColumnStub,
        ElTag: true,
        ElIcon: true,
        ElDialog: true,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.confirm.mockResolvedValue(undefined)
  mocks.apiClient.getDBInstances.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 50 })
  mocks.apiClient.getDBConnections.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 200 })
  mocks.apiClient.getResourceGroups.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 200 })
  mocks.apiClient.createDBInstance.mockResolvedValue({ id: 'db-1' })
  mocks.apiClient.updateDBInstance.mockResolvedValue({ id: 'db-1' })
})

describe('DatabaseView TLS activation', () => {
  it('keeps a failed requested mode visible and lets the user cancel it', async () => {
    mocks.apiClient.preflightDBInstanceTLS.mockResolvedValueOnce({
      ok: false,
      code: 'tls_unsupported',
      error: '远程数据库未启用 TLS',
      latency_ms: 4,
    })
    const wrapper = mountDatabaseView()
    await flushPromises()
    await buttonByText(wrapper, '新增实例').trigger('click')
    await wrapper.get('input[placeholder="host:port 或 IP"]').setValue('db.example.com')
    const tlsSelect = selectByValue(wrapper, 'disable')

    await tlsSelect.setValue('verify-full')
    await flushPromises()

    expect((tlsSelect.element as HTMLSelectElement).value).toBe('verify-full')
    expect(wrapper.text()).toContain('远程数据库未启用 TLS')
    await buttonByText(wrapper, '取消开启').trigger('click')
    await nextTick()
    expect((tlsSelect.element as HTMLSelectElement).value).toBe('disable')

    await wrapper.get('[data-testid="save-instance"]').trigger('click')
    await flushPromises()
    expect(mocks.apiClient.preflightDBInstanceTLS).toHaveBeenCalledTimes(1)
    expect(mocks.apiClient.createDBInstance).toHaveBeenCalledWith(expect.objectContaining({ tls_mode: 'disable' }))
  })

  it('allows a private CA to be added after failure and enables only after retry succeeds', async () => {
    mocks.apiClient.preflightDBInstanceTLS
      .mockResolvedValueOnce({ ok: false, code: 'ca_untrusted', error: '证书不受信任', latency_ms: 5 })
      .mockResolvedValueOnce({ ok: true, latency_ms: 3 })
    const wrapper = mountDatabaseView()
    await flushPromises()
    await buttonByText(wrapper, '新增实例').trigger('click')
    await wrapper.get('input[placeholder="host:port 或 IP"]').setValue('db.example.com')
    const tlsSelect = selectByValue(wrapper, 'disable')

    await tlsSelect.setValue('verify-full')
    await flushPromises()
    await wrapper.get('textarea[placeholder*="手动粘贴 PEM"]').setValue('PRIVATE CA PEM')
    await buttonByText(wrapper, '重新检测并开启').trigger('click')
    await flushPromises()

    expect((tlsSelect.element as HTMLSelectElement).value).toBe('verify-full')
    expect(mocks.apiClient.preflightDBInstanceTLS.mock.calls[1]?.[0]).toEqual(expect.objectContaining({
      tls_mode: 'verify-full',
      tls_ca_pem: 'PRIVATE CA PEM',
    }))
    await wrapper.get('[data-testid="save-instance"]').trigger('click')
    await flushPromises()
    expect(mocks.apiClient.createDBInstance).toHaveBeenCalledWith(expect.objectContaining({
      tls_mode: 'verify-full',
      tls_ca_pem: 'PRIVATE CA PEM',
    }))
  })

  it('rechecks edited TLS fields while retaining the saved instance CA', async () => {
    mocks.apiClient.getDBInstances.mockResolvedValue({
      items: [{
        id: 'db-existing',
        name: 'orders',
        protocol: 'mysql',
        address: 'db.example.com',
        port: 3306,
        tls_mode: 'verify-full',
        tls_server_name: 'db.example.com',
        has_tls_ca: true,
        status: 'active',
        can_manage: true,
      }],
      total: 1,
      page: 1,
      page_size: 50,
    })
    mocks.apiClient.preflightDBInstanceTLS.mockResolvedValueOnce({ ok: true, latency_ms: 3 })
    const wrapper = mountDatabaseView()
    await flushPromises()
    await buttonByText(wrapper, '编辑').trigger('click')
    await wrapper.get('input[placeholder="host:port 或 IP"]').setValue('db-new.example.com')
    await buttonByText(wrapper, '保留当前模式').trigger('click')

    await wrapper.get('[data-testid="save-instance"]').trigger('click')
    await flushPromises()

    expect(mocks.apiClient.preflightDBInstanceTLS).toHaveBeenCalledWith(expect.objectContaining({
      instance_id: 'db-existing',
      address: 'db-new.example.com',
      tls_mode: 'verify-full',
    }), expect.any(AbortSignal))
    const preflightPayload = mocks.apiClient.preflightDBInstanceTLS.mock.calls[0]?.[0]
    expect(preflightPayload).not.toHaveProperty('tls_ca_pem')
    expect(mocks.apiClient.updateDBInstance).toHaveBeenCalledWith('db-existing', expect.objectContaining({
      address: 'db-new.example.com',
      tls_mode: 'verify-full',
    }))
  })
})

function buttonByText(wrapper: ReturnType<typeof mountDatabaseView>, text: string) {
  const button = wrapper.findAll('button').find(candidate => candidate.text().trim() === text)
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

function selectByValue(wrapper: ReturnType<typeof mountDatabaseView>, value: string) {
  const select = wrapper.findAll('select').find(candidate => (candidate.element as HTMLSelectElement).value === value)
  if (!select) throw new Error(`select not found: ${value}`)
  return select
}
