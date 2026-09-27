import assert from 'node:assert/strict'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  getDBAccountPrivileges: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    getDBAccountPrivileges: mocks.getDBAccountPrivileges,
  },
}))

vi.mock('@element-plus/icons-vue', () => ({
  Loading: defineComponent({
    setup: () => () => h('span', { 'data-testid': 'loading-icon' }),
  }),
}))

import DatabaseAccountPrivilegesDialog from './DatabaseAccountPrivilegesDialog.vue'

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

const ElTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
  },
  setup(props) {
    return () => h(
      'div',
      { 'data-testid': 'privileges-table' },
      props.data.map(row => {
        const item = row as { database?: string; privilege?: string; privileges?: string[] }
        return h(
          'div',
          { class: 'privilege-row', 'data-database': item.database },
          [
            h('span', { 'data-testid': 'privilege-db' }, String(item.database || '')),
            h('span', { 'data-testid': 'privilege-label' }, item.privilege || ''),
            h('span', { 'data-testid': 'privilege-detail' }, (item.privileges ?? []).join(',')),
          ],
        )
      }),
    )
  },
})

const ElDescriptionsStub = defineComponent({
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h('dl', attrs, [slots.default?.()])
  },
})

const ElAlertStub = defineComponent({
  inheritAttrs: false,
  props: { title: { type: String, default: '' } },
  setup(props, { attrs }) {
    return () => h('div', { ...attrs, class: 'alert' }, props.title)
  },
})

function mountDialog(props: {
  modelValue?: boolean
  account?: Record<string, unknown>
  instance?: Record<string, unknown>
} = {}) {
  return mount(DatabaseAccountPrivilegesDialog, {
    props: {
      modelValue: props.modelValue ?? true,
      account: props.account ?? { id: 'account-1', username: 'app' },
      instance: props.instance ?? { id: 'instance-1', name: 'orders' },
      'onUpdate:modelValue': () => undefined,
    },
    global: {
      stubs: {
        ElDialog: ElDialogStub,
        ElForm: passthrough('form'),
        ElFormItem: passthrough('div'),
        ElDescriptions: ElDescriptionsStub,
        ElDescriptionsItem: passthrough('div'),
        ElTable: ElTableStub,
        ElTableColumn: passthrough('div'),
        ElAlert: ElAlertStub,
        ElEmpty: passthrough('div'),
        ElTag: passthrough('span'),
        ElIcon: passthrough('span'),
        ElButton: passthrough('button'),
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.getDBAccountPrivileges.mockResolvedValue({
    account_id: 'account-1',
    instance_id: 'instance-1',
    username: 'app',
    protocol: 'mysql',
    grants: [
      { database: 'orders', privilege: 'readwrite', privileges: ['SELECT', 'INSERT', 'UPDATE', 'DELETE'] },
      { database: 'audit', privilege: 'read', privileges: ['SELECT'] },
    ],
  })
})

describe('DatabaseAccountPrivilegesDialog', () => {
  it('queries privileges for the account on open', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    assert.deepEqual(mocks.getDBAccountPrivileges.mock.calls[0], ['account-1'])
    const rows = wrapper.findAll('.privilege-row')
    assert.equal(rows.length, 2)
    assert.equal(rows[0].attributes('data-database'), 'orders')
    assert.match(rows[0].text(), /readwrite/)
    assert.match(rows[0].text(), /INSERT/)
    assert.match(rows[1].text(), /read/)
    wrapper.unmount()
  })

  it('surfaces a query error without crashing', async () => {
    mocks.getDBAccountPrivileges.mockRejectedValue(new Error('shutdown'))
    const wrapper = mountDialog()
    await flushPromises()

    assert.match(wrapper.text(), /shutdown/)
    wrapper.unmount()
  })

  it('shows warnings returned by the upstream parse', async () => {
    mocks.getDBAccountPrivileges.mockResolvedValue({
      account_id: 'account-1',
      instance_id: 'instance-1',
      username: 'app',
      protocol: 'mysql',
      grants: [],
      warnings: ['存在全局（*.*）授权，未按数据库展示：SELECT'],
    })
    const wrapper = mountDialog()
    await flushPromises()

    assert.match(wrapper.text(), /全局（\*\.\*）授权/)
    wrapper.unmount()
  })

  it('re-opens and refreshes when the account changes', async () => {
    const wrapper = mountDialog({ modelValue: false })
    await wrapper.setProps({ modelValue: true })
    await flushPromises()
    assert.equal(mocks.getDBAccountPrivileges.mock.calls.length, 1)

    await wrapper.setProps({ account: { id: 'account-2', username: 'dba' } })
    await flushPromises()
    assert.equal(mocks.getDBAccountPrivileges.mock.calls.length, 2)
    assert.deepEqual(mocks.getDBAccountPrivileges.mock.calls[1], ['account-2'])
    wrapper.unmount()
  })

  it('keeps the newest result when requests finish out of order', async () => {
    const stale = deferred<{
      account_id: string
      instance_id: string
      username: string
      protocol: string
      grants: { database: string; privilege: string; privileges: string[] }[]
    }>()
    const fresh = deferred<{
      account_id: string
      instance_id: string
      username: string
      protocol: string
      grants: { database: string; privilege: string; privileges: string[] }[]
    }>()
    mocks.getDBAccountPrivileges.mockImplementation(() => stale.promise)

    const wrapper = mountDialog()
    await flushPromises()

    mocks.getDBAccountPrivileges.mockImplementation(() => fresh.promise)
    await wrapper.setProps({ account: { id: 'account-2', username: 'dba' } })
    await flushPromises()

    fresh.resolve({
      account_id: 'account-2',
      instance_id: 'instance-1',
      username: 'dba',
      protocol: 'mysql',
      grants: [{ database: 'newest', privilege: 'read', privileges: ['SELECT'] }],
    })
    await flushPromises()
    assert.match(wrapper.get('[data-testid="privileges-table"]').text(), /newest/)

    stale.resolve({
      account_id: 'account-1',
      instance_id: 'instance-1',
      username: 'app',
      protocol: 'mysql',
      grants: [{ database: 'stale', privilege: 'read', privileges: ['SELECT'] }],
    })
    await flushPromises()
    assert.match(wrapper.get('[data-testid="privileges-table"]').text(), /newest/)
    assert.doesNotMatch(wrapper.get('[data-testid="privileges-table"]').text(), /stale/)
    wrapper.unmount()
  })
})
