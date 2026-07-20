<script setup lang="ts">
import { Close, VideoPlay } from '@element-plus/icons-vue';

import { useI18n } from '@/i18n';

defineProps<{
  executing: boolean;
  disabled: boolean;
}>();

const emit = defineEmits<{
  execute: [];
  cancel: [];
}>();

const sql = defineModel<string>({ required: true });
const { t } = useI18n();

function handleKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
    event.preventDefault();
    emit('execute');
  }
}
</script>

<template>
  <section class="editor-panel" aria-labelledby="sql-console-editor-title">
    <header class="editor-header">
      <div>
        <strong id="sql-console-editor-title">{{ t('sqlConsole.editorLabel') }}</strong>
        <span>{{ t('sqlConsole.keyboardHint') }}</span>
      </div>
      <div class="editor-actions">
        <el-button
          v-if="executing"
          :icon="Close"
          @click="emit('cancel')"
        >
          {{ t('sqlConsole.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :icon="VideoPlay"
          :loading="executing"
          :disabled="disabled"
          @click="emit('execute')"
        >
          {{ t('sqlConsole.execute') }}
        </el-button>
      </div>
    </header>

    <el-input
      v-model="sql"
      class="sql-input"
      type="textarea"
      resize="none"
      :disabled="executing"
      :autosize="false"
      spellcheck="false"
      aria-label="SQL"
      @keydown="handleKeydown"
    />
  </section>
</template>

<style scoped>
.editor-panel {
  display: flex;
  flex: 0 0 34%;
  min-height: 220px;
  flex-direction: column;
  overflow: hidden;
  background: #0b1220;
  border: 1px solid #1e293b;
  border-radius: var(--radius-lg);
  box-shadow: 0 18px 42px rgb(15 23 42 / 14%);
}

.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: 0 0 auto;
  gap: 16px;
  padding: 12px 14px 12px 18px;
  color: #e2e8f0;
  background: #111827;
  border-bottom: 1px solid #263244;
}

.editor-header strong,
.editor-header span {
  display: block;
}

.editor-header strong {
  font-size: 13px;
}

.editor-header span {
  margin-top: 2px;
  color: #7f8da3;
  font-family: var(--font-mono);
  font-size: 11px;
}

.editor-actions {
  display: flex;
  gap: 8px;
}

.editor-actions :deep(.el-button) {
  margin: 0;
}

.sql-input {
  flex: 1;
  min-height: 0;
}

.sql-input :deep(.el-textarea),
.sql-input :deep(.el-textarea__inner) {
  height: 100%;
}

.sql-input :deep(.el-textarea__inner) {
  padding: 18px 20px;
  color: #dbeafe;
  font-family: var(--font-mono);
  font-size: 14px;
  line-height: 1.7;
  background:
    linear-gradient(90deg, rgb(30 41 59 / 42%) 1px, transparent 1px) 0 0 / 3.25rem 100%,
    #0b1220;
  border: 0;
  border-radius: 0;
  box-shadow: none;
  caret-color: #60a5fa;
}
</style>
