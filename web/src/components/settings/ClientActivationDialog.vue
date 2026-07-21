<script setup lang="ts">
const props = defineProps<{
  modelValue: boolean;
  title: string;
  command: string;
  loading?: boolean;
}>();

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void;
  (event: 'copy'): void;
  (event: 'confirm'): void;
}>();
</script>

<template>
  <el-dialog
    :model-value="props.modelValue"
    :title="props.title"
    width="680px"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <p class="activation-copy">请执行协议注册命令，激活本地客户端</p>
    <el-input
      data-testid="client-activation-command"
      type="textarea"
      :model-value="props.command"
      readonly
      :rows="4"
      class="activation-command"
    />
    <template #footer>
      <el-button data-testid="client-activation-copy" @click="emit('copy')">复制命令</el-button>
      <el-button
        data-testid="client-activation-confirm"
        type="primary"
        :loading="props.loading"
        :disabled="!props.command"
        @click="emit('confirm')"
      >
        已激活
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.activation-copy {
  margin: 0 0 12px;
  color: var(--color-text-secondary);
  font-size: 13px;
}

.activation-command :deep(.el-textarea__inner) {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
}
</style>
