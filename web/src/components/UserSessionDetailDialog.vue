<!-- UserSession 授权详情弹窗 -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useI18n, type TranslationKey } from '@/i18n';
import { apiClient, type UserSessionDetail } from '@/api/client';

const { t } = useI18n();

const props = defineProps<{
  modelValue: boolean;
  sessionId: string;
}>();

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void;
}>();

const loading = ref(false);
const detail = ref<UserSessionDetail | null>(null);
const error = ref('');

const authTypeMap: Record<string, TranslationKey> = {
  normal: 'audit.authTypeNormal',
  temporary: 'audit.authTypeTemporary',
  ai: 'audit.authTypeAI',
  unknown: 'audit.authTypeUnknown',
};

const authTypeTagType: Record<string, string> = {
  normal: 'primary',
  temporary: 'warning',
  ai: 'success',
  unknown: 'info',
};

const statusTagType: Record<string, string> = {
  active: 'success',
  expired: 'info',
  disabled: 'danger',
};

const statusLabelMap: Record<string, TranslationKey> = {
  active: 'audit.statusActive',
  expired: 'audit.statusExpired',
  disabled: 'audit.statusDisabled',
};

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
});

watch(
  () => props.modelValue && props.sessionId,
  async (shouldLoad) => {
    if (!shouldLoad) return;
    loading.value = true;
    error.value = '';
    detail.value = null;
    try {
      detail.value = await apiClient.getUserSessionBySessionID(props.sessionId);
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : t('audit.error.loadDetail');
    } finally {
      loading.value = false;
    }
  },
);
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="t('audit.userSessionDetail')"
    width="520px"
    destroy-on-close
    @closed="detail = null; error = ''"
  >
    <div v-if="loading" v-loading="loading" style="min-height: 200px" />

    <div v-else-if="error" style="text-align: center; padding: 40px 0; color: var(--el-color-danger)">
      {{ error }}
    </div>

    <el-descriptions v-else-if="detail" :column="1" border>
      <el-descriptions-item :label="t('audit.sessionId')">
        {{ detail.session_id }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.authType')">
        <el-tag :type="authTypeTagType[detail.authorization_type] || 'info'" size="small">
          {{ t(authTypeMap[detail.authorization_type] || 'audit.authTypeUnknown') }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.authorizedUser')">
        {{ detail.username }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.authorizedBy')">
        {{ detail.authorized_by || '-' }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.startTime')">
        {{ detail.starts_at }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.validity')">
        <template v-if="detail.session_type === 'permanent' && !detail.expires_at">
          {{ t('audit.permanent') }}
        </template>
        <template v-else-if="detail.expires_at">
          {{ detail.expires_at }}
        </template>
        <template v-else>-</template>
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.remark')">
        {{ detail.remark || '-' }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.status')">
        <el-tag :type="statusTagType[detail.effective_status] || 'info'" size="small">
          {{ t(statusLabelMap[detail.effective_status] || 'audit.statusActive') }}
        </el-tag>
      </el-descriptions-item>
    </el-descriptions>

    <div v-else style="text-align: center; padding: 40px 0; color: var(--el-text-color-secondary)">
      {{ t('audit.noData') }}
    </div>
  </el-dialog>
</template>
