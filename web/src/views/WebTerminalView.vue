<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-select v-model="target" :placeholder="t('webTerminal.targetPlaceholder')" style="max-width: 260px">
        <el-option :label="t('webTerminal.pendingTarget')" value="pending" />
      </el-select>
      <el-button type="primary">{{ t('webTerminal.connect') }}</el-button>
    </div>

    <section class="terminal-frame">
      <div class="terminal-bar">
        <span class="terminal-dot" />
        <span class="terminal-dot" />
        <span class="terminal-dot" />
      </div>
      <pre class="terminal-content">{{ t('webTerminal.terminalText') }}</pre>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';

import { useI18n } from '@/i18n';

const { t } = useI18n();
const target = ref('pending');
</script>
