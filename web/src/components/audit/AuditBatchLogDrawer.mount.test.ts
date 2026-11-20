// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import { expect, it, vi } from 'vitest';
import AuditBatchLogDrawer from './AuditBatchLogDrawer.vue';

const getLogs = vi.hoisted(() => vi.fn());
vi.mock('@/api/client', () => ({ apiClient: { getAuditBatchLogs: getLogs } }));
vi.mock('@/i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }));

const TableDriver = defineComponent({
  props: ['data', 'total', 'page'],
  emits: ['search', 'update:page'],
  setup(props, { emit }) {
    return () => h('div', [
      h('p', { class: 'items' }, `${props.total}: ${props.data.map((x: { text: string }) => x.text).join(', ')}`),
      h('button', { class: 'next', onClick: () => emit('update:page', 2) }, '下页'),
      h('button', { class: 'search', onClick: () => emit('search', 'older') }, '搜索'),
      h('button', { class: 'clear', onClick: () => emit('search', '') }, '清除'),
    ]);
  },
});

it('submits batch log search and resets the page', async () => {
  getLogs.mockImplementation(async (_scope, _id, params) => ({
    items: [{ kind: 'query', id: 'q', text: params.q === 'older' ? '较早匹配结果' : '普通批次日志' }],
    total: params.q === 'older' ? 1 : 100,
  }));
  const wrapper = mount(AuditBatchLogDrawer, {
    props: { modelValue: true, scope: 'db', batchId: 'batch-1' },
    global: { stubs: { ElDrawer: { template: '<div><slot /></div>' }, DataTableCard: TableDriver, ElAlert: true, ElTableColumn: true, ElTag: true, ElButton: true } },
  });
  await flushPromises();
  expect(wrapper.get('.items').text()).toBe('100: 普通批次日志');
  await wrapper.get('.next').trigger('click');
  await flushPromises();
  expect(getLogs.mock.lastCall?.[2]).toMatchObject({ page: 2 });
  await wrapper.get('.search').trigger('click');
  await flushPromises();
  expect(getLogs.mock.lastCall?.slice(0, 3)).toEqual(['db', 'batch-1', { page: 1, page_size: 50, q: 'older' }]);
  expect(wrapper.get('.items').text()).toBe('1: 较早匹配结果');
  await wrapper.get('.clear').trigger('click');
  await flushPromises();
  expect(wrapper.get('.items').text()).toBe('100: 普通批次日志');
  wrapper.unmount();
});
