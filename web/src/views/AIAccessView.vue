<template>
  <div class="ai-access-page">
    <el-alert
      type="warning"
      :closable="false"
      show-icon
      title="AI ??????????????????????????????"
    />

    <el-card shadow="never" class="ai-doc-card">
      <template #header>
        <div class="card-heading">
          <strong>AI ????</strong>
          <el-tag type="success" effect="light">Markdown</el-tag>
        </div>
      </template>
      <p class="muted">???????? AI?????? API??????????????</p>
      <div class="copy-row">
        <el-input :model-value="docsURL" readonly>
          <template #prepend>????</template>
        </el-input>
        <el-button type="primary" @click="copy(docsURL)">????</el-button>
        <el-button @click="openDocs">????</el-button>
      </div>
    </el-card>

    <div class="ai-grid">
      <el-card shadow="never" class="ai-create-card">
        <template #header><strong>?? AI ????</strong></template>
        <el-form label-position="top" @submit.prevent="createToken">
          <el-form-item label="?????">
            <el-input v-model="form.name" placeholder="??????? Agent" />
          </el-form-item>
          <div class="form-pair">
            <el-form-item label="???????">
              <el-select v-model="form.accessTTL" style="width: 100%">
                <el-option label="1 ??" :value="3600" />
                <el-option label="8 ??" :value="28800" />
                <el-option label="24 ??" :value="86400" />
              </el-select>
            </el-form-item>
            <el-form-item label="???????">
              <el-select v-model="form.refreshTTL" style="width: 100%">
                <el-option label="7 ?" :value="604800" />
                <el-option label="30 ?" :value="2592000" />
                <el-option label="90 ?" :value="7776000" />
              </el-select>
            </el-form-item>
          </div>
          <el-button type="primary" :loading="creating" @click="createToken"
            >????</el-button
          >
        </el-form>
      </el-card>

      <el-card shadow="never" class="ai-token-card">
        <template #header>
          <div class="card-heading">
            <strong>?????</strong
            ><el-button
              text
              :icon="Refresh"
              :loading="loading"
              @click="loadTokens"
              >??</el-button
            >
          </div>
        </template>
        <el-table v-loading="loading" :data="tokens" size="small">
          <el-table-column prop="name" label="??" min-width="140" />
          <el-table-column label="??????" min-width="170">
            <template #default="scope">{{
              formatDate(scope.row.access_expires_at)
            }}</template>
          </el-table-column>
          <el-table-column label="??????" min-width="170">
            <template #default="scope">{{
              formatDate(scope.row.refresh_expires_at)
            }}</template>
          </el-table-column>
          <el-table-column label="??" width="90">
            <template #default="scope">
              <el-tag :type="scope.row.revoked_at ? 'danger' : 'success'">{{
                scope.row.revoked_at ? "???" : "??"
              }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="??" width="90" fixed="right">
            <template #default="scope">
              <el-button
                v-if="!scope.row.revoked_at"
                link
                type="danger"
                @click="revoke(scope.row.id)"
                >??</el-button
              >
            </template>
          </el-table-column>
        </el-table>
        <el-empty
          v-if="!loading && tokens.length === 0"
          description="?? AI ??"
        />
      </el-card>
    </div>

    <el-card v-if="issued" shadow="never" class="issued-card">
      <template #header
        ><div class="card-heading">
          <strong>???????</strong><el-tag type="warning">????</el-tag>
        </div></template
      >
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="?????? access_token ? refresh_token??????????"
      />
      <div class="secret-row">
        <span>access_token</span
        ><el-input
          :model-value="issued.access_token"
          readonly
          show-password
        /><el-button @click="copy(issued.access_token)">??</el-button>
      </div>
      <div class="secret-row">
        <span>refresh_token</span
        ><el-input
          :model-value="issued.refresh_token"
          readonly
          show-password
        /><el-button @click="copy(issued.refresh_token)">??</el-button>
      </div>
    </el-card>
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
    ElMessage.error(error.message || "?? AI ????");
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
    ElMessage.success("AI ?????");
  } catch (error: any) {
    ElMessage.error(error.message || "?? AI ????");
  } finally {
    creating.value = false;
  }
}

async function revoke(id: string) {
  try {
    await ElMessageBox.confirm("???? AI ????????????????", "?? AI ??", {
      type: "warning",
    });
    await apiClient.revokeAIToken(id);
    ElMessage.success("?????");
    await loadTokens();
  } catch (error: any) {
    if (error !== "cancel" && error !== "close")
      ElMessage.error(error.message || "????");
  }
}

async function copy(value: string) {
  try {
    await writeClipboardText(value);
    ElMessage.success("???");
  } catch {
    ElMessage.error("??????????");
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
