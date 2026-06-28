<template>
  <el-switch
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
    :loading="loading"
    size="small"
  />
</template>

<script setup lang="ts">
defineProps<{
  modelValue: boolean
  loading?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()
</script>
