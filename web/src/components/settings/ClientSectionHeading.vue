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
    <div class="section-heading__toolbar">
      <el-tag :type="statusType" effect="light">{{ statusLabel }}</el-tag>
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 20px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--color-border);
}

.section-heading h2 {
  margin: 0;
  font-size: 18px;
  line-height: 1.3;
}

.section-heading p {
  margin: 6px 0 0;
  color: var(--color-text-secondary);
  font-size: 13px;
}

.section-heading__toolbar {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
}

@media (max-width: 760px) {
  .section-heading {
    align-items: flex-start;
    flex-direction: column;
    gap: 10px;
  }

  .section-heading__toolbar {
    width: 100%;
    flex-wrap: wrap;
  }
}
</style>