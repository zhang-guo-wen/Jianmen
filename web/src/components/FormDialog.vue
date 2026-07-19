<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    :title="title"
    :width="dialogWidth"
    :close-on-click-modal="false"
    class="form-dialog"
    destroy-on-close
  >
    <div class="form-dialog-body">
      <slot></slot>
    </div>
    <template #footer>
      <el-button @click="emit('update:visible', false)">取消</el-button>
      <el-button type="primary" :loading="loading" @click="emit('submit')">
        {{ submitText }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    visible: boolean
    title: string
    width?: string
    loading?: boolean
    submitText?: string
  }>(),
  {
    width: '480px',
    loading: false,
    submitText: '保存',
  },
)

const dialogWidth = computed(() =>
  props.width.startsWith('min(') ? props.width : `min(${props.width}, calc(100vw - 24px))`,
)

const emit = defineEmits<{
  'update:visible': [value: boolean]
  submit: []
}>()
</script>

<style scoped>
.form-dialog-body {
  max-height: min(64dvh, 680px);
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

:deep(.form-dialog) {
  display: flex;
  max-height: calc(100dvh - 24px);
  flex-direction: column;
}

:deep(.form-dialog .el-dialog__body) {
  min-height: 0;
  padding-right: 16px;
}

:deep(.form-dialog .el-dialog__footer) {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

:deep(.form-dialog .el-dialog__footer .el-button) {
  margin: 0;
}

@media (max-width: 520px) {
  :deep(.form-dialog .el-dialog__body) {
    padding: 14px 12px;
  }

  :deep(.form-dialog .el-dialog__footer .el-button) {
    flex: 1 1 120px;
  }
}
</style>
