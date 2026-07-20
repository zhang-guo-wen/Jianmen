<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    :title="title"
    :close-on-click-modal="false"
    class="form-dialog crud-form-dialog"
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
withDefaults(
  defineProps<{
    visible: boolean
    title: string
    loading?: boolean
    submitText?: string
  }>(),
  {
    loading: false,
    submitText: '保存',
  },
)

const emit = defineEmits<{
  'update:visible': [value: boolean]
  submit: []
}>()
</script>

<style scoped>
.form-dialog-body {
  min-width: 0;
}

:deep(.form-dialog .el-dialog__body) {
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
