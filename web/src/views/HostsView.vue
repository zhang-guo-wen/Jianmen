<template>
  <div class="view-stack">
    <div class="toolbar">
      <el-input v-model="keyword" clearable :placeholder="t('hosts.placeholder.search')" style="max-width: 320px" />
      <div class="toolbar-actions">
        <el-button :loading="loading" @click="loadTargets">{{ t('common.refresh') }}</el-button>
        <el-button type="primary" @click="openCreateDialog">{{ t('hosts.action.new') }}</el-button>
      </div>
    </div>

    <el-card class="placeholder-panel" shadow="never">
      <el-alert v-if="error" :title="error" type="error" show-icon />
      <el-table v-else v-loading="loading" :data="filteredTargets" height="460" row-key="id">
        <el-table-column prop="id" :label="t('hosts.column.id')" min-width="140" />
        <el-table-column prop="name" :label="t('hosts.column.name')" min-width="160" />
        <el-table-column :label="t('hosts.address')" min-width="180">
          <template #default="{ row }">
            {{ targetAddress(row) }}
          </template>
        </el-table-column>
        <el-table-column prop="username" :label="t('hosts.column.username')" min-width="140" />
        <el-table-column :label="t('hosts.column.auth')" min-width="160">
          <template #default="{ row }">
            <el-tag>{{ authMethodText(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('hosts.column.hostCheck')" min-width="160">
          <template #default="{ row }">
            <el-tag :type="hostKeyTagType(row)">
              {{ hostKeyModeLabel(hostKeyModeForTarget(row)) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('hosts.column.actions')" fixed="right" width="170">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEditDialog(row)">{{ t('hosts.action.edit') }}</el-button>
            <el-tooltip :disabled="!isStaticTarget(row)" :content="t('hosts.message.staticDeleteBlocked')">
              <span>
                <el-button
                  :disabled="isStaticTarget(row)"
                  :loading="deletingId === targetId(row)"
                  link
                  type="danger"
                  @click="confirmDelete(row)"
                >
                  {{ t('hosts.action.delete') }}
                </el-button>
              </span>
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !filteredTargets.length && !error" :description="t('hosts.empty')" />
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :close-on-click-modal="!submitting"
      :title="dialogTitle"
      destroy-on-close
      width="760px"
    >
      <el-form
        ref="formRef"
        v-loading="detailsLoading"
        :model="form"
        :rules="rules"
        label-position="top"
        @submit.prevent="submitForm"
      >
        <div class="host-form-grid">
          <el-form-item :label="t('hosts.field.id')" prop="id">
            <el-input v-model="form.id" :disabled="isEditing" :placeholder="t('hosts.placeholder.id')" />
          </el-form-item>
          <el-form-item :label="t('hosts.field.name')" prop="name">
            <el-input v-model="form.name" :placeholder="t('hosts.placeholder.name')" />
          </el-form-item>
          <el-form-item :label="t('hosts.field.host')" prop="host">
            <el-input v-model="form.host" :placeholder="t('hosts.placeholder.host')" />
          </el-form-item>
          <el-form-item :label="t('hosts.field.port')" prop="port">
            <el-input-number v-model="form.port" :max="65535" :min="1" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('hosts.field.username')" prop="username">
            <el-input v-model="form.username" :placeholder="t('hosts.placeholder.username')" />
          </el-form-item>

          <el-form-item class="host-form-full" :label="t('hosts.auth.method')" prop="auth_method">
            <el-radio-group v-model="form.auth_method" @change="handleAuthMethodChange">
              <el-radio-button label="password">{{ t('hosts.auth.password') }}</el-radio-button>
              <el-radio-button label="private_key_path">{{ t('hosts.auth.privateKeyPath') }}</el-radio-button>
              <el-radio-button label="private_key_pem">{{ t('hosts.auth.privateKeyPem') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>

          <el-form-item v-if="form.auth_method === 'password'" :label="t('hosts.field.password')" prop="password">
            <el-input
              v-model="form.password"
              :placeholder="credentialPlaceholder"
              show-password
              type="password"
            />
          </el-form-item>

          <el-form-item
            v-if="form.auth_method === 'private_key_path'"
            :label="t('hosts.field.privateKeyPath')"
            prop="private_key_path"
          >
            <el-input v-model="form.private_key_path" :placeholder="credentialPlaceholder" />
          </el-form-item>

          <el-form-item
            v-if="isKeyAuthMethod(form.auth_method)"
            :label="t('hosts.field.passphrase')"
            prop="passphrase"
          >
            <el-input
              v-model="form.passphrase"
              :placeholder="secretPlaceholder"
              show-password
              type="password"
            />
          </el-form-item>

          <el-form-item
            v-if="form.auth_method === 'private_key_pem'"
            class="host-form-full"
            :label="t('hosts.field.privateKeyPem')"
            prop="private_key_pem"
          >
            <el-input
              v-model="form.private_key_pem"
              :autosize="{ minRows: 4, maxRows: 8 }"
              :placeholder="credentialPlaceholder"
              type="textarea"
            />
          </el-form-item>

          <el-form-item class="host-form-full" :label="t('hosts.hostKey.method')" prop="host_key_mode">
            <el-radio-group v-model="form.host_key_mode" @change="handleHostKeyModeChange">
              <el-radio-button label="ignore">{{ t('hosts.hostKey.ignore') }}</el-radio-button>
              <el-radio-button label="fingerprint">{{ t('hosts.hostKey.fingerprint') }}</el-radio-button>
              <el-radio-button label="known_hosts">{{ t('hosts.hostKey.knownHosts') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>

          <el-form-item
            v-if="form.host_key_mode === 'fingerprint'"
            :label="t('hosts.field.hostKeyFingerprint')"
            prop="host_key_fingerprint"
          >
            <el-input v-model="form.host_key_fingerprint" placeholder="SHA256:..." />
          </el-form-item>

          <el-form-item
            v-if="form.host_key_mode === 'known_hosts'"
            :label="t('hosts.field.knownHostsPath')"
            prop="known_hosts_path"
          >
            <el-input v-model="form.known_hosts_path" :placeholder="t('hosts.placeholder.knownHostsPath')" />
          </el-form-item>
        </div>
      </el-form>

      <template #footer>
        <el-button :disabled="submitting" @click="dialogVisible = false">{{ t('hosts.cancel') }}</el-button>
        <el-button :loading="submitting" type="primary" @click="submitForm">{{ t('hosts.action.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus';

import { apiClient, type ApiEnvelope, type TargetPayload, type TargetRecord } from '@/api/client';
import { useI18n } from '@/i18n';

type AuthMethod = 'password' | 'private_key_path' | 'private_key_pem';
type HostKeyMode = 'ignore' | 'fingerprint' | 'known_hosts';
type HostForm = TargetPayload & {
  auth_method: AuthMethod;
  host_key_mode: HostKeyMode;
};

const keyword = ref('');
const loading = ref(false);
const error = ref('');
const targets = ref<TargetRecord[]>([]);
const dialogVisible = ref(false);
const detailsLoading = ref(false);
const submitting = ref(false);
const deletingId = ref('');
const editingId = ref<string | null>(null);
const formRef = ref<FormInstance>();
const { t } = useI18n();

const form = reactive<HostForm>(emptyForm());

const isEditing = computed(() => editingId.value !== null);
const dialogTitle = computed(() => (isEditing.value ? t('hosts.dialog.editTitle') : t('hosts.createTitle')));
const secretPlaceholder = computed(() =>
  isEditing.value ? t('hosts.placeholder.keepSecret') : t('hosts.placeholder.optional')
);
const credentialPlaceholder = computed(() =>
  isEditing.value ? t('hosts.placeholder.keepSecret') : t('hosts.placeholder.required')
);
const filteredTargets = computed(() => {
  const query = keyword.value.trim().toLowerCase();

  if (!query) {
    return targets.value;
  }

  return targets.value.filter((target) =>
    [
      target.id,
      target.name,
      target.host,
      target.port,
      target.username,
      targetAddress(target),
      authMethodText(target),
      hostKeyModeLabel(hostKeyModeForTarget(target)),
      target.status,
      target.source
    ].some((value) => String(value ?? '').toLowerCase().includes(query))
  );
});

const rules: FormRules<HostForm> = {
  id: [
    { required: true, message: () => t('hosts.required.id'), trigger: 'blur' },
    {
      pattern: /^[^/\\.]+$/,
      message: () => t('hosts.required.idPattern'),
      trigger: 'blur'
    }
  ],
  name: [{ required: true, message: () => t('hosts.required.name'), trigger: 'blur' }],
  host: [{ required: true, message: () => t('hosts.required.host'), trigger: 'blur' }],
  port: [
    { required: true, message: () => t('hosts.required.port'), trigger: 'change' },
    { type: 'number', min: 1, max: 65535, message: () => t('hosts.required.port'), trigger: 'change' }
  ],
  username: [{ required: true, message: () => t('hosts.required.username'), trigger: 'blur' }],
  auth_method: [{ required: true, message: () => t('hosts.required.authMethod'), trigger: 'change' }],
  password: [{ validator: validatePassword, trigger: 'blur' }],
  private_key_path: [{ validator: validatePrivateKeyPath, trigger: 'blur' }],
  private_key_pem: [{ validator: validatePrivateKeyPEM, trigger: 'blur' }],
  host_key_mode: [{ required: true, message: () => t('hosts.required.hostKeyMode'), trigger: 'change' }],
  host_key_fingerprint: [{ validator: validateHostKeyFingerprint, trigger: 'blur' }],
  known_hosts_path: [{ validator: validateKnownHostsPath, trigger: 'blur' }]
};

function unwrapTargets(payload: ApiEnvelope<TargetRecord[]> | TargetRecord[]): TargetRecord[] {
  return Array.isArray(payload) ? payload : payload.data ?? [];
}

function unwrapTarget(payload: ApiEnvelope<TargetRecord> | TargetRecord): TargetRecord {
  return (payload as ApiEnvelope<TargetRecord>).data ?? (payload as TargetRecord);
}

function emptyForm(): HostForm {
  return {
    id: '',
    name: '',
    host: '',
    port: 22,
    username: '',
    auth_method: 'password',
    password: '',
    private_key_path: '',
    private_key_pem: '',
    passphrase: '',
    host_key_mode: 'ignore',
    insecure_ignore_host_key: true,
    host_key_fingerprint: '',
    known_hosts_path: ''
  };
}

function targetId(target: TargetRecord): string {
  return String(target.id ?? '');
}

function isStaticTarget(target: TargetRecord): boolean {
  const source = String(target.source ?? target.origin ?? target.kind ?? '').toLowerCase();

  return (
    target.static === true ||
    target.readonly === true ||
    target.deletable === false ||
    source === 'static' ||
    source === 'config' ||
    source === 'builtin'
  );
}

function numberFrom(value: unknown, fallback: number): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }

  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function stringFrom(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : '';
}

function hasValue(value: unknown): boolean {
	return String(value ?? '').trim().length > 0;
}

function formatText(template: string, values: Record<string, string>): string {
	return Object.entries(values).reduce(
		(text, [key, value]) => text.split(`{${key}}`).join(value),
		template
	);
}

function isAuthMethod(value: unknown): value is AuthMethod {
	return value === 'password' || value === 'private_key_path' || value === 'private_key_pem';
}

function isKeyAuthMethod(method: AuthMethod): boolean {
  return method === 'private_key_path' || method === 'private_key_pem';
}

function targetAuthMethods(target: TargetRecord): AuthMethod[] {
  const rawMethods = Array.isArray(target.auth_methods) ? target.auth_methods : [];
  const methods = rawMethods.filter(isAuthMethod);

  if (methods.length) {
    return methods;
  }

  const authType = target.auth_type;
  if (isAuthMethod(authType)) {
    return [authType];
  }

  return [
    target.password ? 'password' : null,
    target.private_key_path ? 'private_key_path' : null,
    target.private_key_pem ? 'private_key_pem' : null
  ].filter(isAuthMethod);
}

function inferAuthMethod(target: TargetRecord): AuthMethod {
  return targetAuthMethods(target)[0] ?? 'password';
}

function authMethodLabel(method: AuthMethod): string {
	switch (method) {
		case 'password':
			return t('hosts.auth.password');
		case 'private_key_path':
			return t('hosts.auth.privateKeyPath');
		case 'private_key_pem':
			return t('hosts.auth.privateKeyPem');
	}
}

function authMethodText(target: TargetRecord): string {
  const methods = targetAuthMethods(target);

	return methods.length ? methods.map(authMethodLabel).join(', ') : t('hosts.auth.none');
}

function targetAddress(target: TargetRecord): string {
  const host = stringFrom(target.host) || stringFrom(target.address);
  const port = numberFrom(target.port, 22);

  return host ? `${host}:${port}` : '';
}

function hostKeyModeForTarget(target: TargetRecord): HostKeyMode {
  if (target.insecure_ignore_host_key === false) {
    if (hasValue(target.host_key_fingerprint)) {
      return 'fingerprint';
    }

    if (hasValue(target.known_hosts_path)) {
      return 'known_hosts';
    }
  }

  return 'ignore';
}

function hostKeyModeLabel(mode: HostKeyMode): string {
	switch (mode) {
		case 'fingerprint':
			return t('hosts.hostKey.fingerprint');
		case 'known_hosts':
			return t('hosts.hostKey.knownHosts');
		case 'ignore':
			return t('hosts.hostKey.ignore');
	}
}

function hostKeyTagType(target: TargetRecord): 'success' | 'info' | 'warning' {
  const mode = hostKeyModeForTarget(target);

  if (mode === 'fingerprint') {
    return 'success';
  }

  return mode === 'known_hosts' ? 'info' : 'warning';
}

function validatePassword(_rule: unknown, value: unknown, callback: (error?: Error) => void) {
	if (!isEditing.value && form.auth_method === 'password' && !hasValue(value)) {
		callback(new Error(t('hosts.required.password')));
		return;
	}

  callback();
}

function validatePrivateKeyPath(_rule: unknown, value: unknown, callback: (error?: Error) => void) {
	if (!isEditing.value && form.auth_method === 'private_key_path' && !hasValue(value)) {
		callback(new Error(t('hosts.required.privateKeyPath')));
		return;
	}

  callback();
}

function validatePrivateKeyPEM(_rule: unknown, value: unknown, callback: (error?: Error) => void) {
	if (!isEditing.value && form.auth_method === 'private_key_pem' && !hasValue(value)) {
		callback(new Error(t('hosts.required.privateKeyPem')));
		return;
	}

  callback();
}

function validateHostKeyFingerprint(_rule: unknown, value: unknown, callback: (error?: Error) => void) {
	if (form.host_key_mode === 'fingerprint' && !hasValue(value)) {
		callback(new Error(t('hosts.required.hostKeyFingerprint')));
		return;
	}

  callback();
}

function validateKnownHostsPath(_rule: unknown, value: unknown, callback: (error?: Error) => void) {
	if (form.host_key_mode === 'known_hosts' && !hasValue(value)) {
		callback(new Error(t('hosts.required.knownHostsPath')));
		return;
	}

  callback();
}

function recordToForm(target: TargetRecord, fallbackId = ''): HostForm {
  const hostKeyMode = hostKeyModeForTarget(target);

  return {
    id: stringFrom(target.id) || fallbackId,
    name: stringFrom(target.name),
    host: stringFrom(target.host),
    port: numberFrom(target.port, 22),
    username: stringFrom(target.username),
    auth_method: inferAuthMethod(target),
    password: '',
    private_key_path: '',
    private_key_pem: '',
    passphrase: '',
    host_key_mode: hostKeyMode,
    insecure_ignore_host_key:
      typeof target.insecure_ignore_host_key === 'boolean' ? target.insecure_ignore_host_key : true,
    host_key_fingerprint: stringFrom(target.host_key_fingerprint),
    known_hosts_path: stringFrom(target.known_hosts_path)
  };
}

function resetForm(values: HostForm = emptyForm()) {
  Object.assign(form, values);
}

function buildPayload(): TargetPayload {
  const payload: TargetPayload = {
    id: form.id.trim(),
    name: form.name.trim(),
    host: form.host.trim(),
    port: Number(form.port),
    username: form.username.trim(),
    password: '',
    private_key_path: '',
    private_key_pem: '',
    passphrase: '',
    insecure_ignore_host_key: true,
    host_key_fingerprint: '',
    known_hosts_path: ''
  };

  if (form.auth_method === 'password') {
    payload.password = form.password;
  } else if (form.auth_method === 'private_key_path') {
    payload.private_key_path = form.private_key_path.trim();
    payload.passphrase = form.passphrase;
  } else {
    payload.private_key_pem = form.private_key_pem;
    payload.passphrase = form.passphrase;
  }

  if (form.host_key_mode === 'fingerprint') {
    payload.insecure_ignore_host_key = false;
    payload.host_key_fingerprint = form.host_key_fingerprint.trim();
  } else if (form.host_key_mode === 'known_hosts') {
    payload.insecure_ignore_host_key = false;
    payload.known_hosts_path = form.known_hosts_path.trim();
  }

  return payload;
}

function selectedCredentialValue(): string {
  if (form.auth_method === 'password') {
    return form.password;
  }

  return form.auth_method === 'private_key_path' ? form.private_key_path : form.private_key_pem;
}

function handleAuthMethodChange() {
  formRef.value?.clearValidate(['password', 'private_key_path', 'private_key_pem', 'passphrase']);
}

function handleHostKeyModeChange() {
  formRef.value?.clearValidate(['host_key_fingerprint', 'known_hosts_path']);
}

async function loadTargets() {
	loading.value = true;
	error.value = '';

	try {
		targets.value = unwrapTargets(await apiClient.getTargets());
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('hosts.error.loadList');
	} finally {
		loading.value = false;
	}
}

async function openCreateDialog() {
  editingId.value = null;
  resetForm();
  dialogVisible.value = true;
  await nextTick();
  formRef.value?.clearValidate();
}

async function openEditDialog(target: TargetRecord) {
  const id = targetId(target);

	if (!id) {
		ElMessage.error(t('hosts.error.missingId'));
		return;
	}

  editingId.value = id;
  resetForm(recordToForm(target, id));
  dialogVisible.value = true;
  detailsLoading.value = true;
  await nextTick();
  formRef.value?.clearValidate();

	try {
		resetForm(recordToForm(unwrapTarget(await apiClient.getTarget(id)), id));
	} catch (err) {
		ElMessage.error(err instanceof Error ? err.message : t('hosts.error.loadDetail'));
	} finally {
		detailsLoading.value = false;
	}
}

async function submitForm() {
  const valid = await formRef.value?.validate().catch(() => false);

  if (!valid) {
    return;
  }

  const payload = buildPayload();

	if (!isEditing.value && !hasValue(selectedCredentialValue())) {
		ElMessage.warning(formatText(t('hosts.warning.credentialRequired'), { method: authMethodLabel(form.auth_method) }));
		return;
	}

  if (
    isEditing.value &&
    isKeyAuthMethod(form.auth_method) &&
    hasValue(form.passphrase) &&
    !hasValue(selectedCredentialValue())
	) {
		ElMessage.warning(t('hosts.warning.passphraseNeedsKey'));
		return;
	}

  submitting.value = true;

  try {
    const editId = editingId.value;

	if (editId !== null) {
		await apiClient.updateTarget(editId, payload);
		ElMessage.success(t('hosts.message.updated'));
	} else {
		await apiClient.createTarget(payload);
		ElMessage.success(t('hosts.message.created'));
	}

    dialogVisible.value = false;
    await loadTargets();
	} catch (err) {
		ElMessage.error(err instanceof Error ? err.message : t('hosts.error.save'));
	} finally {
		submitting.value = false;
	}
}

async function confirmDelete(target: TargetRecord) {
  const id = targetId(target);

	if (!id) {
		ElMessage.error(t('hosts.error.missingId'));
		return;
	}

	if (isStaticTarget(target)) {
		ElMessage.warning(t('hosts.message.staticDeleteBlocked'));
		return;
	}

	try {
		await ElMessageBox.confirm(
			formatText(t('hosts.deleteConfirm'), { name: String(target.name ?? id) }),
			t('hosts.deleteTitle'),
			{
			cancelButtonText: t('hosts.cancel'),
			confirmButtonText: t('hosts.action.delete'),
			type: 'warning'
			}
		);
	} catch {
		return;
	}

  deletingId.value = id;

	try {
		await apiClient.deleteTarget(id);
		ElMessage.success(t('hosts.message.deleted'));
		await loadTargets();
	} catch (err) {
		ElMessage.error(err instanceof Error ? err.message : t('hosts.error.delete'));
	} finally {
		deletingId.value = '';
	}
}

onMounted(loadTargets);
</script>

<style scoped>
.toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.host-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}

.host-form-grid .el-input-number {
  width: 100%;
}

.host-form-full {
  grid-column: 1 / -1;
}

@media (max-width: 720px) {
  .host-form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
