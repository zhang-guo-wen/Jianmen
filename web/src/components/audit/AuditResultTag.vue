<script setup lang="ts">
defineProps<{
  label: string;
  type: 'success' | 'warning' | 'danger' | 'info' | 'primary';
  detail?: string;
}>();
</script>

<template>
  <el-tooltip :disabled="!detail" :content="detail || ''" placement="top">
    <el-tag
      :type="type"
      :tabindex="detail ? 0 : -1"
      :aria-label="detail ? `${label}：${detail}` : label"
      size="small"
      effect="plain"
    >
      {{ label }}
    </el-tag>
  </el-tooltip>
</template>
