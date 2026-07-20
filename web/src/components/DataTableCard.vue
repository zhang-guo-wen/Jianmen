<template>
  <div ref="card" class="page-card data-table-card">
    <div
      v-if="showSearch || $slots['toolbar-filter'] || $slots['toolbar-extra']"
      class="page-card__toolbar"
    >
      <div class="page-card__search" v-if="showSearch">
        <el-input
          v-model="searchText"
          :placeholder="searchPlaceholder"
          :aria-label="searchPlaceholder"
          name="table_search"
          autocomplete="off"
          clearable
          @keyup.enter="emit('search', searchText)"
          @clear="emit('search', '')"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>
      <slot name="toolbar-filter"></slot>
      <div
        v-if="showSearch && !$slots['toolbar-filter']"
        class="page-card__spacer"
      ></div>
      <div class="page-card__actions">
        <slot name="toolbar-extra"></slot>
      </div>
    </div>
    <div class="page-card__body">
      <el-table
        :data="data"
        :row-key="rowKey"
        v-loading="loading"
        size="small"
        :highlight-current-row="rowClickable"
        @row-click="handleRowClick"
        style="width: 100%"
        height="100%"
      >
        <slot></slot>
      </el-table>
    </div>
    <div v-if="total > 0" class="page-card__footer">
      <el-pagination
        v-model:current-page="currentPageModel"
        v-model:page-size="currentPageSizeModel"
        :page-sizes="pageSizes"
        :total="total"
        :layout="paginationLayout"
        :pager-count="compactPagination ? 5 : 7"
        size="small"
        background
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, useTemplateRef } from 'vue'
import { Search } from '@element-plus/icons-vue'

const props = withDefaults(
  defineProps<{
    data: any[]
    loading?: boolean
    total: number
    page: number
    pageSize: number
    pageSizes?: number[]
    showSearch?: boolean
    searchPlaceholder?: string
    search?: string
    rowKey?: string
    rowClickable?: boolean
  }>(),
  {
    loading: false,
    pageSizes: () => [20, 50, 100],
    showSearch: true,
    searchPlaceholder: '搜索…',
    rowKey: 'id',
    rowClickable: false,
  },
)

const emit = defineEmits<{
  'update:page': [page: number]
  'update:pageSize': [size: number]
  'update:search': [keyword: string]
  search: [keyword: string]
  'row-click': [row: any]
}>()

const localSearchText = ref('')
const card = useTemplateRef<HTMLElement>('card')
const compactPagination = shallowRef(false)
let resizeObserver: ResizeObserver | undefined

const paginationLayout = computed(() =>
  compactPagination.value ? 'prev, pager, next' : 'total, sizes, prev, pager, next',
)
const searchText = computed({
  get: () => props.search ?? localSearchText.value,
  set: (value: string) => {
    if (props.search === undefined) localSearchText.value = value
    emit('update:search', value)
  },
})

const currentPageModel = computed({
  get: () => props.page,
  set: (v) => emit('update:page', v),
})
const currentPageSizeModel = computed({
  get: () => props.pageSize,
  set: (v) => emit('update:pageSize', v),
})

function handleRowClick(row: any) {
  if (props.rowClickable) emit('row-click', row)
}

function measurePagination() {
  compactPagination.value = Boolean(card.value && card.value.clientWidth < 560)
}

onMounted(() => {
  resizeObserver = new ResizeObserver(measurePagination)
  if (card.value) resizeObserver.observe(card.value)
  void nextTick(measurePagination)
})

onBeforeUnmount(() => resizeObserver?.disconnect())
</script>

<style scoped>
.data-table-card > .page-card__body {
  overflow: hidden;
}

.data-table-card > .page-card__footer {
  min-width: 0;
  overflow-x: auto;
  overscroll-behavior-inline: contain;
  scrollbar-width: thin;
}

.data-table-card > .page-card__footer :deep(.el-pagination) {
  flex: 0 0 auto;
}

@media (max-width: 560px) {
  .data-table-card > .page-card__footer {
    justify-content: center;
    padding-inline: 8px;
  }
}
</style>
