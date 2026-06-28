<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    :title="title"
    :width="width"
    :close-on-click-modal="false"
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

const emit = defineEmits<{
  'update:visible': [value: boolean]
  submit: []
}>()
</script>

<style scoped>
.form-dialog-body {
  max-height: 60vh;
  overflow-y: auto;
  padding-right: 4px;
}
</style>
