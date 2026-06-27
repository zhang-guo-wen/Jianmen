<template>
  <div class="pagination-bar" v-if="total > 0">
    <el-pagination
      v-model:current-page="currentPage"
      v-model:page-size="pageSizeModel"
      :page-sizes="pageSizes"
      :total="total"
      layout="total, sizes, prev, pager, next"
      size="small"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    currentPage: number;
    pageSize: number;
    total: number;
    pageSizes?: number[];
  }>(),
  { pageSizes: () => [20, 30, 50, 100] },
);

const emit = defineEmits<{
  'update:currentPage': [value: number];
  'update:pageSize': [value: number];
}>();

const currentPage = computed({
  get: () => props.currentPage,
  set: (v) => emit('update:currentPage', v),
});
const pageSizeModel = computed({
  get: () => props.pageSize,
  set: (v) => emit('update:pageSize', v),
});
</script>

<style scoped>
.pagination-bar {
  display: flex;
  justify-content: flex-end;
  margin-top: 8px;
  flex-shrink: 0;
}
</style>
