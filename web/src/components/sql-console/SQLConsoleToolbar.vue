<script setup lang="ts">
import { Connection, Loading } from '@element-plus/icons-vue';

import type { DBAccountRecord } from '@/api/client';
import { useI18n } from '@/i18n';

defineProps<{
  accounts: readonly DBAccountRecord[];
  databases: readonly string[];
  loading: boolean;
  connecting: boolean;
  connected: boolean;
  executing: boolean;
}>();

const emit = defineEmits<{
  accountChange: [];
  refresh: [];
}>();

const accountId = defineModel<string>('accountId', { required: true });
const database = defineModel<string>('database', { required: true });
const { t } = useI18n();

function accountLabel(account: DBAccountRecord): string {
  const target = account.instance_name || account.instance_address || '数据库';
  return `${target} / ${account.username || account.unique_name || account.id}`;
}
</script>

<template>
  <section class="resource-bar" aria-labelledby="sql-console-session-title">
    <div class="session-mark">
      <span class="session-mark__icon" aria-hidden="true">
        <el-icon><Connection /></el-icon>
      </span>
      <div>
        <strong id="sql-console-session-title">{{ t('sqlConsole.sessionControlled') }}</strong>
        <span v-if="connecting">{{ t('sqlConsole.connecting') }}</span>
        <span v-else-if="connected">{{ t('sqlConsole.connectedHint') }}</span>
        <span v-else>{{ t('sqlConsole.sessionHint') }}</span>
      </div>
    </div>

    <div class="resource-fields">
      <label class="resource-field">
        <span>{{ t('sqlConsole.account') }}</span>
        <el-select
          v-model="accountId"
          filterable
          :loading="loading"
          :disabled="executing || connecting"
          :placeholder="t('sqlConsole.account')"
          @change="emit('accountChange')"
        >
          <el-option
            v-for="account in accounts"
            :key="String(account.id)"
            :label="accountLabel(account)"
            :value="String(account.id)"
          >
            <div class="account-option">
              <span>{{ accountLabel(account) }}</span>
              <small>{{ account.instance_protocol?.toUpperCase() }}</small>
            </div>
          </el-option>
        </el-select>
      </label>

      <label class="resource-field resource-field--database">
        <span>{{ t('sqlConsole.database') }}</span>
        <el-select
          v-model="database"
          filterable
          :loading="connecting"
          :disabled="executing || connecting || !connected"
          :placeholder="connecting ? t('sqlConsole.connecting') : t('sqlConsole.databasePlaceholder')"
        >
          <el-option
            v-for="item in databases"
            :key="item"
            :label="item"
            :value="item"
          />
        </el-select>
      </label>

      <el-button
        class="refresh-button"
        :icon="loading ? Loading : undefined"
        :loading="loading"
        :disabled="executing || connecting"
        @click="emit('refresh')"
      >
        {{ t('common.refresh') }}
      </el-button>
    </div>
  </section>
</template>

<style scoped>
.resource-bar {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 18px;
  padding: 16px 18px;
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.session-mark {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 250px;
}

.session-mark__icon {
  display: grid;
  flex: 0 0 auto;
  width: 38px;
  height: 38px;
  color: #047857;
  background: #ecfdf5;
  border: 1px solid #a7f3d0;
  border-radius: 12px;
  place-items: center;
}

.session-mark strong,
.session-mark span {
  display: block;
}

.session-mark strong {
  font-size: 14px;
}

.session-mark span {
  margin-top: 3px;
  color: var(--color-text-secondary);
  font-size: 12px;
}

.resource-fields {
  display: flex;
  align-items: flex-end;
  justify-content: flex-end;
  flex: 1;
  gap: 10px;
  min-width: 0;
}

.resource-field {
  display: grid;
  width: min(340px, 42%);
  gap: 6px;
}

.resource-field--database {
  width: min(280px, 34%);
}

.resource-field > span {
  color: #475569;
  font-size: 12px;
  font-weight: 700;
}

.account-option {
  display: flex;
  justify-content: space-between;
  gap: 16px;
}

.account-option small {
  color: var(--color-text-secondary);
}

.refresh-button {
  margin-bottom: 1px;
}

@media (max-width: 980px) {
  .resource-bar,
  .resource-fields {
    align-items: stretch;
    flex-direction: column;
  }

  .resource-field,
  .resource-field--database {
    width: 100%;
  }
}
</style>
