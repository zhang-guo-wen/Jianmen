<template>
  <div class="ai-access-page">
    <el-alert type="warning" :closable="false" show-icon :title="C.warning" />
    <el-card shadow="never" class="ai-doc-card">
      <template #header
        ><div class="card-heading">
          <strong>AI {{ C.docs }}</strong
          ><el-tag type="success" effect="light">Markdown</el-tag>
        </div></template
      >
      <p class="muted">{{ C.hint }}</p>
      <div class="copy-row">
        <el-input :model-value="docsURL" readonly
          ><template #prepend>{{ C.path }}</template></el-input
        ><el-button type="primary" @click="copy(docsURL)">{{
          C.copy_path
        }}</el-button
        ><el-button @click="openDocs">{{ C.open }}</el-button>
      </div>
    </el-card>
    <div class="ai-grid">
      <el-card shadow="never" class="ai-create-card">
        <template #header
          ><strong>{{ C.create }}</strong></template
        >
        <el-form label-position="top" @submit.prevent="createToken">
          <el-form-item :label="C.client"
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
          }}</el-button>
        </el-form>
      </el-card>
      <el-card shadow="never" class="ai-token-card">
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
            min-width="140"
          /><el-table-column :label="C.access_exp" min-width="170"
            ><template #default="scope">{{
              formatDate(scope.row.access_expires_at)
            }}</template></el-table-column
          ><el-table-column :label="C.refresh_exp" min-width="170"
            ><template #default="scope">{{
              formatDate(scope.row.refresh_expires_at)
            }}</template></el-table-column
          ><el-table-column :label="C.status" width="90"
            ><template #default="scope"
              ><el-tag :type="scope.row.revoked_at ? 'danger' : 'success'">{{
                scope.row.revoked_at ? C.revoked : C.valid
              }}</el-tag></template
            ></el-table-column
          ><el-table-column :label="C.actions" width="90" fixed="right"
            ><template #default="scope"
              ><el-button
                v-if="!scope.row.revoked_at"
                link
                type="danger"
                @click="revoke(scope.row.id)"
                >{{ C.revoke }}</el-button
              ></template
            ></el-table-column
          ></el-table
        >
        <el-empty
          v-if="!loading && tokens.length === 0"
          :description="C.empty"
        />
      </el-card>
    </div>
    <el-card v-if="issued" shadow="never" class="issued-card"
      ><template #header
        ><div class="card-heading">
          <strong>{{ C.once }}</strong
          ><el-tag type="warning">{{ C.save }}</el-tag>
        </div></template
      ><el-alert type="warning" :closable="false" show-icon :title="C.rotate" />
      <div class="secret-row">
        <span>access_token</span
        ><el-input
          :model-value="issued.access_token"
          readonly
          show-password
        /><el-button @click="copy(issued.access_token)">{{ C.copy }}</el-button>
      </div>
      <div class="secret-row">
        <span>refresh_token</span
        ><el-input
          :model-value="issued.refresh_token"
          readonly
          show-password
        /><el-button @click="copy(issued.refresh_token)">{{
          C.copy
        }}</el-button>
      </div></el-card
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
  warning: "AI 只会获得当前用户已授权的资源。令牌和临时密码都不要写入日志。",
  docs: "连接文档",
  hint: "把这个地址提供给AI。文档只描述API、不包含任何账号密码或密钥。",
  path: "文档路径",
  copy_path: "复制路径",
  open: "打开文档",
  create: "创建AI访问令牌",
  client: "客户端名称",
  client_ph: "例如生产运维Agent",
  access: "访问令牌有效期",
  refresh: "刷新令牌有效期",
  hour: "小时",
  day: "天",
  issued: "已签发令牌",
  reload: "刷新",
  name: "名称",
  access_exp: "访问令牌到期",
  refresh_exp: "刷新令牌到期",
  status: "状态",
  revoked: "已撤销",
  valid: "有效",
  actions: "操作",
  revoke: "撤销",
  empty: "暂无AI令牌",
  once: "令牌仅显示一次",
  save: "立即保存",
  rotate: "刷新时会轮换access_token和refresh_token。旧令牌会立即失效。",
  copy: "复制",
  copied: "复制",
  load_error: "加载AI令牌失败",
  created: "AI令牌已创建",
  create_error: "创建AI令牌失败",
  confirm: "确认撤销？撤销后该AI客户端将不能继续访问。",
  revoke_title: "撤销AI令牌",
  revoked_ok: "令牌已撤销",
  revoke_error: "撤销失败",
  copy_error: "复制失败，请手动复制",
};
const tokens = ref<AIAccessTokenRecord[]>([]);
const issued = ref<IssuedAIAccessToken | null>(null);
const loading = ref(false);
const creating = ref(false);
const form = reactive({ name: "", accessTTL: 3600, refreshTTL: 2592000 });
const docsURL = `${window.location.origin}/api/ai/docs`;
async function loadTokens() {
  loading.value = true;
  try {
    tokens.value = await apiClient.getAITokens();
  } catch (error: any) {
    ElMessage.error(error.message || C.load_error);
  } finally {
    loading.value = false;
  }
}
async function createToken() {
  creating.value = true;
  try {
    issued.value = await apiClient.createAIToken({
      name: form.name.trim() || "AI client",
      access_ttl_seconds: form.accessTTL,
      refresh_ttl_seconds: form.refreshTTL,
    });
    form.name = "";
    await loadTokens();
    ElMessage.success(C.created);
  } catch (error: any) {
    ElMessage.error(error.message || C.create_error);
  } finally {
    creating.value = false;
  }
}
async function revoke(id: string) {
  try {
    await ElMessageBox.confirm(C.confirm, C.revoke_title, { type: "warning" });
    await apiClient.revokeAIToken(id);
    ElMessage.success(C.revoked_ok);
    await loadTokens();
  } catch (error: any) {
    if (error !== "cancel" && error !== "close")
      ElMessage.error(error.message || C.revoke_error);
  }
}
async function copy(value: string) {
  try {
    await writeClipboardText(value);
    ElMessage.success(C.copied);
  } catch {
    ElMessage.error(C.copy_error);
  }
}
function openDocs() {
  window.open(docsURL, "_blank", "noopener,noreferrer");
}
function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString("zh-CN") : "-";
}
onMounted(loadTokens);
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
.ai-doc-card,
.ai-create-card,
.ai-token-card,
.issued-card {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}
.ai-grid {
  display: grid;
  grid-template-columns: minmax(280px, 0.8fr) minmax(520px, 1.6fr);
  gap: 14px;
}
.card-heading,
.copy-row,
.secret-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.card-heading {
  justify-content: space-between;
}
.copy-row .el-input,
.secret-row .el-input {
  flex: 1;
}
.muted {
  margin: 0 0 12px;
  color: var(--color-text-secondary);
  font-size: 13px;
}
.secret-row {
  margin-top: 12px;
}
.secret-row > span {
  width: 110px;
  flex-shrink: 0;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
}
@media (max-width: 980px) {
  .ai-grid {
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
}
</style>
