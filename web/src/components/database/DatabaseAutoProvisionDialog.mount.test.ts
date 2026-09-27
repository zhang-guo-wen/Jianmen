import assert from 'node:assert/strict'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  getDBAccounts: vi.fn(),
  listDBDatabases: vi.fn(),
  provisionDBAccount: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    getDBAccounts: mocks.getDBAccounts,
    listDBDatabases: mocks.listDBDatabases,
    provisionDBAccount: mocks.provisionDBAccount,
  },
}))

vi.mock('@element-plus/icons-vue', () => ({
  Loading: defineComponent({
    setup: () => () => h('span', { 'data-testid': 'loading-icon' }),
  }),
}))

import DatabaseAutoProvisionDialog from './DatabaseAutoProvisionDialog.vue'

interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => {
    resolve = done
  })
  return { promise, resolve }
}

const passthrough = (tag: string) => defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h(tag, attrs, [slots.default?.(), slots.footer?.()])
  },
})

const ElDialogStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: Boolean },
  emits: ['update:modelValue'],
  setup(props, { attrs, slots }) {
    return () => props.modelValue
      ? h('section', attrs, [slots.default?.(), slots.footer?.()])
      : null
  },
})

const ElSelectStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { attrs, emit, slots }) {
    return () => h('select', {
      ...attrs,
      'data-testid': 'admin-select',
      value: props.modelValue,
      onChange: (event: Event) => {
        emit('update:modelValue', (event.target as HTMLSelectElement).value)
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

const ElTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
  },
  setup(props) {
    return () => h(
      'div',
      { 'data-testid': 'database-list' },
      props.data.map(row => h(
        'span',
        { class: 'database-row' },
        String((row as { database?: string }).database || ''),
      )),
    )
  },
})

const ElButtonStub = defineComponent({
  inheritAttrs: false,
  props: { disabled: Boolean, loading: Boolean },
  setup(props, { attrs, slots }) {
    return () => h('button', {
      ...attrs,
      disabled: props.disabled,
      'data-loading': props.loading ? 'true' : undefined,
    }, slots.default?.())
  },
})

function mountDialog() {
  return mount(DatabaseAutoProvisionDialog, {
    props: {
      modelValue: true,
      instance: { id: 'instance-1', protocol: 'mysql' },
      'onUpdate:modelValue': () => undefined,
    },
    global: {
      stubs: {
        ElDialog: ElDialogStub,
        ElForm: passthrough('form'),
        ElFormItem: passthrough('div'),
        ElSelect: ElSelectStub,
        ElOption: ElOptionStub,
        ElTable: ElTableStub,
        ElTableColumn: passthrough('div'),
        ElButton: ElButtonStub,
        ElAlert: passthrough('div'),
        ElEmpty: passthrough('div'),
        ElIcon: passthrough('span'),
        ElRadioGroup: passthrough('div'),
        ElRadioButton: passthrough('button'),
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.getDBAccounts.mockResolvedValue({
    items: [
      {
        id: 'admin-1',
        username: 'root',
        unique_name: 'database-admin-1',
        status: 'active',
      },
    ],
    total: 1,
  })
  mocks.listDBDatabases.mockResolvedValue({ databases: ['orders', 'audit'] })
  mocks.provisionDBAccount.mockResolvedValue({
    ok: true,
    account: { id: 'created-1', resource_id: 'D001' },
  })
})

describe('DatabaseAutoProvisionDialog', () => {
  it('loads databases immediately for the default administrator credential', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    assert.deepEqual(mocks.listDBDatabases.mock.calls[0], ['instance-1', 'admin-1'])
    assert.match(wrapper.get('[data-testid="database-list"]').text(), /orders/)
    assert.match(wrapper.get('[data-testid="database-list"]').text(), /audit/)
    wrapper.unmount()
  })

  it('requires a database grant and creates without a client-supplied MySQL account host', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    let createButton = wrapper.findAll('button').find(button => button.text().trim() === '创建')
    assert.ok(createButton)
    assert.equal(createButton.attributes('disabled'), '')

    const selectRead = wrapper.findAll('button').find(button => button.text().trim() === '全部只读')
    assert.ok(selectRead)
    await selectRead.trigger('click')
    await flushPromises()

    createButton = wrapper.findAll('button').find(button => button.text().trim() === '创建')
    assert.ok(createButton)
    assert.equal(createButton.attributes('disabled'), undefined)
    await createButton.trigger('click')
    await flushPromises()

    const payload = mocks.provisionDBAccount.mock.calls[0]?.[1] as Record<string, unknown>
    assert.equal(payload.admin_account_id, 'admin-1')
    assert.deepEqual(payload.grants, [
      { database: 'orders', privilege: 'read' },
      { database: 'audit', privilege: 'read' },
    ])
    assert.equal('host' in payload, false)
    assert.deepEqual(wrapper.emitted('created'), [[]])
    wrapper.unmount()
  })

  it('keeps the dialog locked while account creation is in flight', async () => {
    const request = deferred<{
      ok: boolean
      account: { id: string; resource_id: string }
    }>()
    mocks.provisionDBAccount.mockReturnValue(request.promise)
    const wrapper = mountDialog()
    await flushPromises()

    const selectRead = wrapper.findAll('button').find(button => button.text().trim() === '全部只读')
    assert.ok(selectRead)
    await selectRead.trigger('click')
    const createButton = wrapper.findAll('button').find(button => button.text().trim() === '创建')
    assert.ok(createButton)
    await createButton.trigger('click')
    await flushPromises()

    const cancelButton = wrapper.findAll('button').find(button => button.text().trim() === '取消')
    assert.ok(cancelButton)
    assert.equal(cancelButton.attributes('disabled'), '')
    assert.equal(wrapper.get<HTMLSelectElement>('[data-testid="admin-select"]').attributes('disabled'), '')
    await cancelButton.trigger('click')
    assert.equal(wrapper.emitted('update:modelValue'), undefined)

    await wrapper.setProps({ modelValue: false })
    await flushPromises()
    assert.deepEqual(wrapper.emitted('update:modelValue'), [[true]])

    request.resolve({
      ok: true,
      account: { id: 'created-1', resource_id: 'D001' },
    })
    await flushPromises()
    assert.deepEqual(wrapper.emitted('created'), [[]])
    wrapper.unmount()
  })

  it('keeps the newest database list when credential requests finish out of order', async () => {
    const first = deferred<{ databases: string[] }>()
    const second = deferred<{ databases: string[] }>()
    mocks.getDBAccounts.mockResolvedValue({
      items: [
        { id: 'admin-1', username: 'root', unique_name: 'admin-1', status: 'active' },
        { id: 'admin-2', username: 'dba', unique_name: 'admin-2', status: 'active' },
      ],
      total: 2,
    })
    mocks.listDBDatabases.mockImplementation(
      (_instanceID: string, accountID: string) => (
        accountID === 'admin-1' ? first.promise : second.promise
      ),
    )

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get<HTMLSelectElement>('[data-testid="admin-select"]').setValue('admin-2')
    await flushPromises()

    second.resolve({ databases: ['newest'] })
    await flushPromises()
    assert.equal(wrapper.get('[data-testid="database-list"]').text(), 'newest')

    first.resolve({ databases: ['stale'] })
    await flushPromises()
    assert.equal(wrapper.get('[data-testid="database-list"]').text(), 'newest')
    wrapper.unmount()
  })
})
