<template>
  <div class="page-card data-table-card">
    <div class="page-card__toolbar" v-if="showSearch || $slots['toolbar-extra']">
      <div class="page-card__search" v-if="showSearch">
        <el-input
          v-model="searchText"
          :placeholder="searchPlaceholder"
          clearable
          @keyup.enter="emit('search', searchText)"
          @clear="emit('search', '')"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>
      <div class="page-card__spacer" v-if="showSearch"></div>
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
        highlight-current-row
        @row-click="(row: any) => emit('row-click', row)"
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
        layout="total, sizes, prev, pager, next"
        size="small"
        background
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
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
  }>(),
  {
    loading: false,
    pageSizes: () => [20, 50, 100],
    showSearch: true,
    searchPlaceholder: '搜索...',
    rowKey: 'id',
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
</script>
