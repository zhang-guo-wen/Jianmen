<template>
  <div class="ai-access-page">
    <el-alert type="warning" :closable="false" show-icon :title="C.warning" />
    <div class="top-grid">
      <el-card shadow="never" class="docs-card">
        <template #header
          ><div class="card-heading">
            <strong>{{ C.docs }}</strong
            ><el-tag type="success">Markdown</el-tag>
          </div></template
        >
        <p class="muted">{{ C.docs_hint }}</p>
        <div class="copy-row">
          <el-input :model-value="docsURL" readonly
            ><template #prepend>{{ C.doc_path }}</template></el-input
          ><el-button type="primary" @click="copy(docsURL)">{{
            C.copy_path
          }}</el-button>
        </div>
        <div class="docs-actions">
          <el-button :loading="docsLoading" @click="openDocsDialog">{{
            C.view_full
          }}</el-button
          ><el-button :disabled="!docsContent" @click="copy(docsContent)">{{
            C.copy_all
          }}</el-button>
        </div>
        <el-skeleton v-if="docsLoading" :rows="5" animated />
        <pre v-else class="docs-preview">{{
          docsContent || C.load_docs_error
        }}</pre>
      </el-card>
      <el-card shadow="never" class="create-card">
        <template #header
          ><strong>{{ C.create }}</strong></template
        >
        <el-form label-position="top" @submit.prevent="createToken"
          ><el-form-item :label="C.client"
            ><el-input v-model="form.name" :placeholder="C.client_ph"
          /></el-form-item>
          <div class="form-pair">
            <el-form-item :label="C.access"
              ><el-select v-model="form.accessTTL" style="width: 100%"
                ><el-option :label="`1 ${C.hour}`" :value="3600" /><el-option
                  :label="`8 ${C.hour}`"
                  :value="28800" /><el-option
                  :label="`24 ${C.hour}`"
                  :value="86400" /></el-select></el-form-item
            ><el-form-item :label="C.refresh"
              ><el-select v-model="form.refreshTTL" style="width: 100%"
                ><el-option :label="`7 ${C.day}`" :value="604800" /><el-option
                  :label="`30 ${C.day}`"
                  :value="2592000" /><el-option
                  :label="`90 ${C.day}`"
                  :value="7776000" /></el-select
            ></el-form-item>
          </div>
          <el-button type="primary" :loading="creating" @click="createToken">{{
            C.create
          }}</el-button></el-form
        >
      </el-card>
    </div>
    <el-card shadow="never" class="token-card">
      <template #header
        ><div class="card-heading">
          <strong>{{ C.issued }}</strong
          ><el-button
            text
            :icon="Refresh"
            :loading="loading"
            @click="loadTokens"
            >{{ C.reload }}</el-button
          >
        </div></template
      >
      <el-table v-loading="loading" :data="tokens" size="small"
        ><el-table-column
          prop="name"
          :label="C.name"
          min-width="150"
        /><el-table-column :label="C.access_exp" min-width="175"
          ><template #default="scope">{{
            formatDate(scope.row.access_expires_at)
          }}</template></el-table-column
        ><el-table-column :label="C.refresh_exp" min-width="175"
          ><template #default="scope">{{
            formatDate(scope.row.refresh_expires_at)
          }}</template></el-table-column
        ><el-table-column :label="C.last_used" min-width="160"
          ><template #default="scope">{{
            formatDate(scope.row.last_used_at)
          }}</template></el-table-column
        ><el-table-column :label="C.status" width="90"
          ><template #default="scope"
            ><el-tag :type="scope.row.revoked_at ? 'danger' : 'success'">{{
              scope.row.revoked_at ? C.revoked : C.valid
            }}</el-tag></template
          ></el-table-column
        ><el-table-column :label="C.actions" width="150" fixed="right"
          ><template #default="scope"
            ><el-button link type="primary" @click="openToken(scope.row.id)">{{
              C.view
            }}</el-button
            ><el-button
              v-if="!scope.row.revoked_at"
              link
              type="danger"
              @click="revoke(scope.row.id)"
              >{{ C.revoke }}</el-button
            ></template
          ></el-table-column
        ></el-table
      ><el-empty
        v-if="!loading && tokens.length === 0"
        :description="C.empty"
      />
    </el-card>
    <el-dialog
      v-model="tokenDialogVisible"
      :title="issuedToken ? C.secret_title : C.detail_title"
      width="min(760px, 92vw)"
      destroy-on-close
      @closed="clearTokenDialog"
      ><template v-if="issuedToken"
        ><el-alert
          type="warning"
          :closable="false"
          :title="C.one_time_secret"
        /><div class="secret-row">
          <span>{{ C.access_token }}</span
          ><el-input
            :model-value="issuedToken.access_token"
            readonly
            show-password
          /><el-button @click="copy(issuedToken.access_token)">{{
            C.copy
          }}</el-button>
        </div>
        <div class="secret-row">
          <span>{{ C.refresh_token }}</span
          ><el-input
            :model-value="issuedToken.refresh_token"
            readonly
            show-password
          /><el-button @click="copy(issuedToken.refresh_token)">{{
            C.copy
          }}</el-button>
        </div>
        <el-button
          class="config-button"
          type="primary"
          plain
          @click="copyConfig"
          >{{ C.copy_config }}</el-button
        ></template><template v-else-if="tokenDetail"
        ><el-alert
          type="info"
          :closable="false"
          :title="C.detail_hint"
        /><el-descriptions :column="1" border class="token-details">
          <el-descriptions-item :label="C.name">{{
            tokenDetail.name
          }}</el-descriptions-item>
          <el-descriptions-item :label="C.access_exp">{{
            formatDate(tokenDetail.access_expires_at)
          }}</el-descriptions-item>
          <el-descriptions-item :label="C.refresh_exp">{{
            formatDate(tokenDetail.refresh_expires_at)
          }}</el-descriptions-item>
          <el-descriptions-item :label="C.last_used">{{
            formatDate(tokenDetail.last_used_at)
          }}</el-descriptions-item>
        </el-descriptions></template
      ><template #footer
        ><el-button
          v-if="tokenDetail && !tokenDetail.revoked_at"
          type="primary"
          @click="reissueToken"
          >{{ C.reissue }}</el-button
        ><el-button
          v-if="tokenDetail && !tokenDetail.revoked_at"
          type="danger"
          @click="revoke(tokenDetail.id)"
          >{{ C.revoke }}</el-button
        ><el-button @click="tokenDialogVisible = false">{{
          C.close
        }}</el-button></template
      ></el-dialog
    >
    <el-dialog v-model="docsVisible" :title="C.docs" width="min(960px, 94vw)">
      <pre class="docs-full">{{ docsContent }}</pre>
      <template #footer
        ><el-button @click="copy(docsContent)">{{ C.copy_all }}</el-button
        ><el-button type="primary" @click="docsVisible = false">{{
          C.close
        }}</el-button></template
      ></el-dialog
    >
  </div>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { Refresh } from "@element-plus/icons-vue";
import {
  apiClient,
  type AIAccessTokenRecord,
  type IssuedAIAccessToken,
} from "@/api/client";
import { writeClipboardText } from "@/utils/clipboard";
const C = {
  detail_title: "令牌详情",
  detail_hint: "令牌秘密仅在创建或刷新后显示一次，无法再次查看。",
  one_time_secret: "请立即保存这些令牌秘密；关闭此窗口后将无法再次查看。",
  reissue: "重新签发",
  reissued: "已重新签发令牌，旧令牌已撤销",
  warning: "AI 只会获得当前用户已授权的资源。令牌和临时密码都不要写入日志。",
  docs: "AI 连接文档",
  docs_hint: "这里显示完整Markdown文档。可以直接复制全文或在弹窗中阅读。",
  doc_path: "文档路径",
  copy_path: "复制路径",
  view_full: "查看完整文档",
  copy_all: "复制全文",
  load_docs_error: "加载AI文档失败",
  create: "创建AI访问令牌",
  client: "客户端名称",
  client_ph: "例如：生产运维Agent",
  access: "访问令牌有效期",
  refresh: "刷新令牌有效期",
  hour: "小时",
  day: "天",
  issued: "已签发令牌",
  reload: "刷新",
  name: "名称",
  access_exp: "访问令牌到期",
  refresh_exp: "刷新令牌到期",
  last_used: "最后使用",
  status: "状态",
  revoked: "已撤销",
  valid: "有效",
  actions: "操作",
  view: "查看",
  revoke: "撤销",
  empty: "暂无AI令牌",
  secret_title: "AI令牌",
  secret_hint: "令牌已加密保存。这里可以反复查看和复制。",
  access_token: "访问令牌",
  refresh_token: "刷新令牌",
  copy: "复制",
  copy_config: "复制AI配置",
  close: "关闭",
  only_once: "该历史令牌创建于加密保存功能之前，无法恢复。请重新创建新令牌。",
  created: "AI令牌已创建",
  revoked_ok: "令牌已撤销",
  confirm_revoke: "确认撤销这个AI令牌吗？",
  revoke_title: "撤销AI令牌",
  copied: "已复制",
  copy_error: "复制失败，请手动复制",
  load_error: "加载AI令牌失败",
  create_error: "创建AI令牌失败",
};
const tokens = ref<AIAccessTokenRecord[]>([]),
  loading = ref(false),
  creating = ref(false),
  docsLoading = ref(false),
  docsContent = ref(""),
  docsVisible = ref(false),
  tokenDialogVisible = ref(false),
  tokenDetail = ref<AIAccessTokenRecord | null>(null),
  issuedToken = ref<IssuedAIAccessToken | null>(null);
const form = reactive({ name: "", accessTTL: 3600, refreshTTL: 2592000 });
const docsURL = `${window.location.origin}/api/ai/docs`;
async function loadTokens() {
  loading.value = true;
  try {
    tokens.value = await apiClient.getAITokens();
  } catch (e: any) {
    ElMessage.error(e.message || C.load_error);
  } finally {
    loading.value = false;
  }
}
async function loadDocs() {
  docsLoading.value = true;
  try {
    docsContent.value = await apiClient.getAIDocs();
  } catch (e: any) {
    ElMessage.error(e.message || C.load_docs_error);
  } finally {
    docsLoading.value = false;
  }
}
async function createToken() {
  creating.value = true;
  try {
    const created = await apiClient.createAIToken({
      name: form.name.trim() || "AI client",
      access_ttl_seconds: form.accessTTL,
      refresh_ttl_seconds: form.refreshTTL,
    });
    form.name = "";
    issuedToken.value = created;
    tokenDetail.value = null;
    tokenDialogVisible.value = true;
    await loadTokens();
    ElMessage.success(C.created);
  } catch (e: any) {
    ElMessage.error(e.message || C.create_error);
  } finally {
    creating.value = false;
  }
}
async function openToken(id: string) {
  try {
    issuedToken.value = null;
    tokenDetail.value = await apiClient.getAIToken(id);
    tokenDialogVisible.value = true;
  } catch (e: any) {
    ElMessage.error(e.message || C.load_error);
  }
}
async function reissueToken() {
  if (!tokenDetail.value) return;
  creating.value = true;
  try {
    const reissued = await apiClient.reissueAIToken(tokenDetail.value.id);
    issuedToken.value = reissued;
    tokenDetail.value = null;
    await loadTokens();
    ElMessage.success(C.reissued);
  } catch (e: any) {
    ElMessage.error(e.message || C.create_error);
  } finally {
    creating.value = false;
  }
}
function clearTokenDialog() {
  issuedToken.value = null;
  tokenDetail.value = null;
}
async function revoke(id: string) {
  try {
    await ElMessageBox.confirm(C.confirm_revoke, C.revoke_title, {
      type: "warning",
    });
    await apiClient.revokeAIToken(id);
    if (tokenDetail.value?.id === id) {
      tokenDialogVisible.value = false;
      tokenDetail.value = null;
    }
    ElMessage.success(C.revoked_ok);
    await loadTokens();
  } catch (e: any) {
    if (e !== "cancel" && e !== "close")
      ElMessage.error(e.message || C.load_error);
  }
}
async function copy(value: string) {
  if (!value) return;
  try {
    await writeClipboardText(value);
    ElMessage.success(C.copied);
  } catch {
    ElMessage.error(C.copy_error);
  }
}
async function copyConfig() {
  if (!issuedToken.value) return;
  await copy(
    JSON.stringify(
      {
        access_token: issuedToken.value.access_token,
        refresh_token: issuedToken.value.refresh_token,
      },
      null,
      2,
    ),
  );
}
function openDocsDialog() {
  docsVisible.value = true;
  if (!docsContent.value) void loadDocs();
}
function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString("zh-CN") : "-";
}
onMounted(() => {
  void loadTokens();
  void loadDocs();
});
</script>
<style scoped>
.ai-access-page {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 0;
  overflow: auto;
  padding-bottom: 18px;
}
.top-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.7fr) minmax(280px, 0.8fr);
  gap: 14px;
}
.docs-card,
.create-card,
.token-card {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}
.card-heading,
.copy-row,
.docs-actions,
.secret-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.card-heading {
  justify-content: space-between;
}
.muted {
  margin: 0 0 12px;
  color: var(--color-text-secondary);
  font-size: 13px;
}
.docs-actions {
  justify-content: flex-end;
  margin: 12px 0;
}
.docs-preview,
.docs-full {
  margin: 0;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font:
    12px/1.65 ui-monospace,
    SFMono-Regular,
    Consolas,
    monospace;
  color: var(--color-text);
  background: var(--color-surface-muted);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  padding: 14px;
}
.docs-preview {
  max-height: 320px;
}
.docs-full {
  height: calc(70vh - 120px);
  min-height: 360px;
}
.secret-row {
  margin-top: 14px;
}
.secret-row > span {
  width: 125px;
  flex-shrink: 0;
  font:
    12px ui-monospace,
    SFMono-Regular,
    Consolas,
    monospace;
}
.secret-row .el-input {
  flex: 1;
}
.secret-alert {
  margin-top: 14px;
}
.config-button {
  margin-top: 16px;
}
.token-details {
  margin-top: 14px;
}
.top-grid :deep(.el-card__body),
.token-card :deep(.el-card__body) {
  min-width: 0;
}
@media (max-width: 980px) {
  .top-grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .copy-row,
  .secret-row {
    align-items: stretch;
    flex-direction: column;
  }
  .secret-row > span {
    width: auto;
  }
  .docs-actions {
    justify-content: stretch;
    flex-wrap: wrap;
  }
}
</style>
