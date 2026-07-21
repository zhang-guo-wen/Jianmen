<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  title: string;
  desc?: string;
  configured?: boolean;
  registered?: boolean;
}>();

const statusLabel = computed(() => {
  if (!props.configured) return '未配置';
  return props.registered ? '已就绪' : '待注册协议';
});

const statusType = computed(() => {
  if (!props.configured) return 'info';
  return props.registered ? 'success' : 'warning';
});
</script>

<template>
  <div class="section-heading">
    <div>
      <h2>{{ title }}</h2>
      <p v-if="desc">{{ desc }}</p>
    </div>
    <div class="section-heading__actions">
      <el-tag :type="statusType" effect="light">{{ statusLabel }}</el-tag>
      <slot name="actions" />
    </div>
  </div>
</template>
