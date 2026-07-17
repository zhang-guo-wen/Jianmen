<template>
  <div class="containers-page">
    <div class="containers-toolbar">
      <div>
        <span class="eyebrow">RUNTIME WORKSPACE</span>
        <h2>容器工作台</h2>
      </div>
      <div class="toolbar-actions">
        <el-button :loading="loading" @click="loadEndpoints">
          <el-icon><Refresh /></el-icon>刷新
        </el-button>
        <el-button v-if="permission.canDo('container:create')" type="primary" @click="openCreate">
          <el-icon><Plus /></el-icon>新增连接
        </el-button>
      </div>
    </div>

    <div class="workspace-card">
      <aside class="endpoint-panel">
        <div class="panel-heading">
          <div>
            <strong>连接与容器</strong>
            <span>{{ endpoints.length }} 个运行时</span>
          </div>
          <el-button link :loading="loading" @click="loadEndpoints"><el-icon><Refresh /></el-icon></el-button>
        </div>
        <el-scrollbar class="endpoint-scroll">
          <div v-if="!loading && endpoints.length === 0" class="empty-tree">
            <el-icon><Box /></el-icon>
            <span>还没有容器连接</span>
            <el-button link type="primary" @click="openCreate">先添加一个运行时</el-button>
          </div>
          <div v-for="endpoint in endpoints" :key="endpoint.id" class="tree-endpoint">
            <button class="tree-row endpoint-row" :class="{ active: selectedEndpoint?.id === endpoint.id }" @click="selectEndpoint(endpoint)">
              <el-icon class="tree-chevron" :class="{ expanded: expandedEndpoints.has(endpoint.id || '') }"><ArrowRight /></el-icon>
              <span class="runtime-dot" :class="endpoint.runtime"></span>
              <span class="tree-label">{{ endpoint.name }}</span>
              <el-tag size="small" :type="endpoint.status === 'active' ? 'success' : 'info'" effect="plain">{{ endpoint.runtime }}</el-tag>
            </button>
            <div v-if="expandedEndpoints.has(endpoint.id || '')" class="container-children">
              <button
                v-for="container in containersByEndpoint[endpoint.id || ''] || []"
                :key="container.id"
                class="tree-row container-row"
                :class="{ active: selectedContainer?.id === container.id && selectedEndpoint?.id === endpoint.id }"
                @click="selectContainer(endpoint, container)"
              >
                <span class="container-state" :class="container.state"></span>
                <span class="tree-label">{{ container.name || container.id.slice(0, 12) }}</span>
                <span class="tree-meta">{{ container.state || 'unknown' }}</span>
              </button>
              <div v-if="loadingContainers[endpoint.id || '']" class="tree-loading">加载容器中...</div>
              <div v-else-if="(containersByEndpoint[endpoint.id || ''] || []).length === 0" class="tree-loading">暂无容器</div>
            </div>
          </div>
        </el-scrollbar>
        <div v-if="selectedEndpoint" class="endpoint-footer">
          <el-button link type="primary" @click="openEdit(selectedEndpoint)">编辑连接</el-button>
          <el-button link type="danger" @click="removeEndpoint(selectedEndpoint)">删除</el-button>
        </div>
      </aside>

      <main class="runtime-panel">
        <div v-if="selectedContainer" class="detail-header">
          <div>
            <div class="detail-kicker">{{ selectedEndpoint?.name }} / CONTAINER</div>
            <h3>{{ selectedContainer.name || selectedContainer.id }}</h3>
            <p>{{ selectedContainer.image || '未返回镜像信息' }} · {{ selectedContainer.id }}</p>
          </div>
          <div class="detail-actions">
            <el-button size="small" @click="refreshLogs"><el-icon><Refresh /></el-icon>刷新日志</el-button>
            <el-button size="small" @click="copyLogs"><el-icon><CopyDocument /></el-icon>复制日志</el-button>
          </div>
        </div>
        <div v-else-if="selectedEndpoint" class="detail-header endpoint-summary">
          <div>
            <div class="detail-kicker">RUNTIME ENDPOINT</div>
            <h3>{{ selectedEndpoint.name }}</h3>
            <p>{{ endpointDescription(selectedEndpoint) }}</p>
          </div>
          <el-button type="primary" plain @click="refreshEndpointContainers">读取容器</el-button>
        </div>
        <el-empty v-else description="从左侧选择一个运行时连接" class="workspace-empty">
          <el-button type="primary" @click="openCreate">新增容器连接</el-button>
        </el-empty>

        <div v-if="selectedContainer" class="log-workspace">
          <div class="log-tabs">
            <span class="active">日志</span>
            <span>详情</span>
            <el-tag size="small" effect="plain">tail 200</el-tag>
          </div>
          <pre v-loading="logsLoading" class="log-viewer">{{ logs || '暂无日志输出' }}</pre>
        </div>
        <div v-else-if="selectedEndpoint" class="endpoint-info-grid">
          <div class="info-tile"><span>运行时</span><strong>{{ selectedEndpoint.runtime }}</strong></div>
          <div class="info-tile"><span>连接方式</span><strong>{{ connectionModeLabel(selectedEndpoint.connection_mode) }}</strong></div>
          <div class="info-tile"><span>地址</span><strong>{{ selectedEndpoint.address }}</strong></div>
          <div class="info-tile"><span>SSH 账号</span><strong>{{ selectedEndpoint.host_account_name || '未使用 SSH' }}</strong></div>
        </div>
      </main>
    </div>

    <FormDialog
      v-model:visible="dialogVisible"
      :title="editingId ? '编辑容器连接' : '新增容器连接'"
      width="620px"
      :loading="submitting"
      @submit="submitEndpoint"
    >
      <el-form :model="form" label-width="104px">
        <el-form-item label="运行时" required>
          <el-radio-group v-model="form.runtime" @change="onRuntimeChange">
            <el-radio-button label="docker">Docker</el-radio-button>
            <el-radio-button label="containerd">containerd（K8s 推荐）</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="连接方式" required>
          <el-select v-model="form.connection_mode" style="width: 100%">
            <el-option v-if="form.runtime === 'docker'" label="Docker Engine API" value="docker_api" />
            <el-option v-if="form.runtime === 'docker'" label="SSH 执行 Docker 命令" value="ssh" />
            <el-option v-if="form.runtime === 'containerd'" label="SSH + CRI（crictl）" value="containerd" />
          </el-select>
          <div class="field-hint">Docker API 可连接 HTTP/TCP 或 Unix Socket；containerd 通过 SSH 调用 crictl 读取 Kubernetes 容器。</div>
        </el-form-item>
        <el-form-item label="连接地址" required>
          <el-input v-model="form.address" :placeholder="addressPlaceholder">
            <template #prepend v-if="form.connection_mode === 'docker_api'">API</template>
          </el-input>
        </el-form-item>
        <el-form-item v-if="form.connection_mode === 'docker_api'" label="端口">
          <el-input-number v-model="form.port" :min="0" :max="65535" controls-position="right" style="width: 100%" />
        </el-form-item>
        <template v-if="form.connection_mode !== 'docker_api'">
          <el-form-item label="主机">
            <el-select v-model="form.host_id" filterable clearable style="width: 100%" placeholder="选择 SSH 主机" @change="onHostChange">
              <el-option v-for="host in hosts" :key="host.id" :label="`${host.name} (${host.address}:${host.port})`" :value="host.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="主机账号" required>
            <div class="field-with-action">
              <el-select v-model="form.host_account_id" filterable clearable style="flex: 1" placeholder="选择对应 SSH 账号">
                <el-option v-for="account in hostAccounts" :key="account.id" :label="`${account.name || account.username} (${account.username})`" :value="account.id" />
              </el-select>
              <el-button @click="quickAccountVisible = true">快速新增</el-button>
            </div>
          </el-form-item>
        </template>
        <el-collapse v-model="moreSections">
          <el-collapse-item title="更多设置" name="more">
            <el-form-item label="名称"><el-input v-model="form.name" placeholder="默认使用连接地址" /></el-form-item>
            <el-form-item label="分组">
              <el-select v-model="form.group" allow-create filterable clearable style="width: 100%">
                <el-option v-for="group in groups" :key="group" :label="group" :value="group" />
              </el-select>
            </el-form-item>
            <el-form-item label="备注"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item>
          </el-collapse-item>
        </el-collapse>
        <div class="test-connection-row">
          <el-button plain :loading="testing" @click="testConnection">测试连接</el-button>
          <span v-if="testResult" :class="testResult.ok ? 'test-ok' : 'test-failed'">{{ testResult.message }}</span>
        </div>
      </el-form>
    </FormDialog>

    <el-dialog v-model="quickAccountVisible" title="快速新增主机和账号" width="520px" append-to-body destroy-on-close>
      <el-form :model="quickAccount" label-width="90px">
        <el-form-item label="主机名称"><el-input v-model="quickAccount.host_name" placeholder="默认使用主机地址:端口" /></el-form-item>
        <el-form-item label="主机地址" required><el-input v-model="quickAccount.address" placeholder="例如 10.0.0.8" /></el-form-item>
        <el-form-item label="SSH 端口"><el-input-number v-model="quickAccount.port" :min="1" :max="65535" controls-position="right" style="width: 100%" /></el-form-item>
        <el-form-item label="登录账号" required><el-input v-model="quickAccount.username" placeholder="root" /></el-form-item>
        <el-form-item label="登录密码" required><el-input v-model="quickAccount.password" type="password" show-password /></el-form-item>
      </el-form>
      <template #footer><el-button @click="quickAccountVisible = false">取消</el-button><el-button type="primary" :loading="quickSaving" @click="createQuickAccount">创建并选择</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowRight, Box, CopyDocument, Plus, Refresh } from '@element-plus/icons-vue'
import FormDialog from '@/components/FormDialog.vue'
import { apiClient, type ContainerEndpointPayload, type ContainerEndpointView, type ContainerRecord, type HostPayload, type HostView, type TargetPayload, type TargetRecord } from '@/api/client'
import { usePermissionStore } from '@/stores/permission'
import { writeClipboardText } from '@/utils/clipboard'

const permission = usePermissionStore()
const endpoints = ref<ContainerEndpointView[]>([])
const hosts = ref<HostView[]>([])
const hostAccounts = ref<TargetRecord[]>([])
const groups = ref<string[]>([])
const containersByEndpoint = reactive<Record<string, ContainerRecord[]>>({})
const loadingContainers = reactive<Record<string, boolean>>({})
const expandedEndpoints = reactive(new Set<string>())
const selectedEndpoint = ref<ContainerEndpointView | null>(null)
const selectedContainer = ref<ContainerRecord | null>(null)
const logs = ref('')
const logsLoading = ref(false)
const loading = ref(false)
const dialogVisible = ref(false)
const quickAccountVisible = ref(false)
const editingId = ref('')
const submitting = ref(false)
const testing = ref(false)
const quickSaving = ref(false)
const moreSections = ref<string[]>([])
const testResult = ref<{ ok: boolean; message: string } | null>(null)

const emptyForm = () => ({ name: '', group: '', runtime: 'docker', connection_mode: 'docker_api', address: 'unix:///var/run/docker.sock', port: 0, host_id: '', host_account_id: '', remark: '' })
const form = reactive(emptyForm())
const quickAccount = reactive({ host_name: '', address: '', port: 22, username: '', password: '' })

const addressPlaceholder = computed(() => {
  if (form.connection_mode === 'docker_api') return 'unix:///var/run/docker.sock 或 http://127.0.0.1:2375'
  return form.runtime === 'containerd' ? '/run/containerd/containerd.sock' : '/var/run/docker.sock'
})

async function loadEndpoints() {
  loading.value = true
  try {
    const [endpointPage, hostPage, resourceGroups] = await Promise.all([
      apiClient.getContainerEndpoints({ page: 1, page_size: 200 }),
      apiClient.getHosts({ page: 1, page_size: 200 }),
      apiClient.getResourceGroups({ group_type: 'resource', page: 1, page_size: 200 }),
    ])
    endpoints.value = endpointPage.items
    hosts.value = hostPage.items
    groups.value = (resourceGroups.items || []).map(item => item.name).filter(Boolean)
    if (selectedEndpoint.value && !endpoints.value.some(item => item.id === selectedEndpoint.value?.id)) {
      selectedEndpoint.value = null
      selectedContainer.value = null
    }
  } catch (error: any) {
    ElMessage.error(error.message || '加载容器连接失败')
  } finally {
    loading.value = false
  }
}

async function selectEndpoint(endpoint: ContainerEndpointView) {
  selectedEndpoint.value = endpoint
  selectedContainer.value = null
  logs.value = ''
  const id = endpoint.id || ''
  if (expandedEndpoints.has(id)) {
    expandedEndpoints.delete(id)
    return
  }
  expandedEndpoints.add(id)
  await refreshEndpointContainers()
}

async function refreshEndpointContainers() {
  if (!selectedEndpoint.value?.id) return
  const id = selectedEndpoint.value.id
  loadingContainers[id] = true
  try {
    const result = await apiClient.listContainers(id)
    containersByEndpoint[id] = result.items || []
  } catch (error: any) {
    ElMessage.error(error.message || '读取容器列表失败')
  } finally {
    loadingContainers[id] = false
  }
}

function selectContainer(endpoint: ContainerEndpointView, container: ContainerRecord) {
  selectedEndpoint.value = endpoint
  selectedContainer.value = container
  void refreshLogs()
}

async function refreshLogs() {
  if (!selectedEndpoint.value?.id || !selectedContainer.value?.id) return
  logsLoading.value = true
  try {
    const result = await apiClient.getContainerLogs(selectedEndpoint.value.id, selectedContainer.value.id)
    logs.value = result.logs || ''
  } catch (error: any) {
    logs.value = error.message || '读取日志失败'
  } finally {
    logsLoading.value = false
  }
}

async function copyLogs() {
  try {
    await writeClipboardText(logs.value)
    ElMessage.success('日志已复制')
  } catch {
    ElMessage.error('复制日志失败')
  }
}

function openCreate() {
  Object.assign(form, emptyForm())
  editingId.value = ''
  testResult.value = null
  moreSections.value = []
  dialogVisible.value = true
  void loadHosts()
}

function openEdit(endpoint: ContainerEndpointView) {
  Object.assign(form, { ...emptyForm(), ...endpoint })
  editingId.value = endpoint.id || ''
  testResult.value = null
  moreSections.value = []
  dialogVisible.value = true
  void loadHosts()
  if (endpoint.host_id) void loadHostAccounts(endpoint.host_id)
}

function onRuntimeChange() {
  form.connection_mode = form.runtime === 'docker' ? 'docker_api' : 'containerd'
  form.address = form.runtime === 'docker' ? 'unix:///var/run/docker.sock' : '/run/containerd/containerd.sock'
  form.port = 0
}

async function loadHosts() {
  const result = await apiClient.getHosts({ page: 1, page_size: 200 })
  hosts.value = result.items
}

async function onHostChange(hostID: string) {
  form.host_account_id = ''
  await loadHostAccounts(hostID)
}

async function loadHostAccounts(hostID: string) {
  if (!hostID) {
    hostAccounts.value = []
    return
  }
  const result = await apiClient.getHostAccounts(hostID, { page: 1, page_size: 200 })
  hostAccounts.value = result.items
}

function buildPayload(): ContainerEndpointPayload {
  return {
    name: form.name.trim() || undefined,
    group: form.group.trim() || undefined,
    runtime: form.runtime,
    connection_mode: form.connection_mode,
    address: form.address.trim(),
    port: form.port || undefined,
    host_id: form.host_id || undefined,
    host_account_id: form.host_account_id || undefined,
    remark: form.remark.trim() || undefined,
  }
}

async function testConnection() {
  testing.value = true
  testResult.value = null
  try {
    const result = await apiClient.testContainerConnection(buildPayload())
    testResult.value = { ok: result.ok, message: result.ok ? `连接成功 · ${result.latency_ms} ms` : (result.message || '连接失败') }
  } catch (error: any) {
    testResult.value = { ok: false, message: error.message || '测试连接失败' }
  } finally {
    testing.value = false
  }
}

async function submitEndpoint() {
  if (!form.address.trim()) return ElMessage.warning('请填写连接地址')
  if (form.connection_mode !== 'docker_api' && !form.host_account_id) return ElMessage.warning('请选择 SSH 主机账号')
  submitting.value = true
  try {
    const payload = buildPayload()
    if (editingId.value) await apiClient.updateContainerEndpoint(editingId.value, payload)
    else await apiClient.createContainerEndpoint(payload)
    ElMessage.success(editingId.value ? '容器连接已更新' : '容器连接已创建')
    dialogVisible.value = false
    await loadEndpoints()
  } catch (error: any) {
    ElMessage.error(error.message || '保存容器连接失败')
  } finally {
    submitting.value = false
  }
}

async function removeEndpoint(endpoint: ContainerEndpointView) {
  try {
    await ElMessageBox.confirm(`确定删除容器连接“${endpoint.name}”？`, '删除确认', { type: 'warning' })
    await apiClient.deleteContainerEndpoint(endpoint.id!)
    ElMessage.success('容器连接已删除')
    selectedEndpoint.value = null
    selectedContainer.value = null
    await loadEndpoints()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || '删除失败')
  }
}

async function createQuickAccount() {
  if (!quickAccount.address.trim() || !quickAccount.username.trim() || !quickAccount.password) return ElMessage.warning('请填写主机地址、登录账号和密码')
  quickSaving.value = true
  try {
    const hostPayload: HostPayload = { name: quickAccount.host_name.trim(), address: quickAccount.address.trim(), port: quickAccount.port, group: form.group.trim(), remark: '' }
    const host = await apiClient.createHost(hostPayload)
    const targetPayload: TargetPayload = {
      id: `container-${Date.now()}`,
      host_id: host.id,
      name: quickAccount.username.trim(),
      group: form.group.trim(),
      remark: '容器连接快速创建',
      host: host.address,
      port: host.port,
      username: quickAccount.username.trim(),
      password: quickAccount.password,
      private_key_path: '', private_key_pem: '', passphrase: '',
      insecure_ignore_host_key: true, host_key_fingerprint: '', known_hosts_path: '',
    }
    const account = await apiClient.createTarget(targetPayload)
    form.host_id = String(host.id || '')
    form.host_account_id = String(account.id || '')
    hostAccounts.value = [account]
    quickAccountVisible.value = false
    ElMessage.success('主机和账号已创建并选中')
  } catch (error: any) {
    ElMessage.error(error.message || '快速新增失败')
  } finally {
    quickSaving.value = false
  }
}

function endpointDescription(endpoint: ContainerEndpointView) {
  return `${connectionModeLabel(endpoint.connection_mode)} · ${endpoint.address}${endpoint.port ? `:${endpoint.port}` : ''}`
}

function connectionModeLabel(mode: string) {
  if (mode === 'docker_api') return 'Docker Engine API'
  if (mode === 'containerd') return 'SSH + CRI'
  return 'SSH 命令'
}

onMounted(() => void loadEndpoints())
</script>

<style scoped>
.containers-page { height: 100%; min-height: 0; display: flex; flex-direction: column; gap: 16px; }
.containers-toolbar { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; }
.eyebrow { color: #8c6a3c; font-size: 10px; letter-spacing: .18em; font-weight: 800; }
.containers-toolbar h2 { margin: 4px 0 0; color: #1f2b27; font: 700 24px/1.1 Georgia, serif; }
.toolbar-actions { display: flex; gap: 8px; }
.workspace-card { min-height: 560px; flex: 1; display: grid; grid-template-columns: 330px minmax(0, 1fr); overflow: hidden; border: 1px solid #dce5df; border-radius: 18px; background: #fbfdfb; box-shadow: 0 18px 50px rgba(37, 62, 48, .08); }
.endpoint-panel { display: flex; min-height: 0; flex-direction: column; border-right: 1px solid #e1e9e4; background: #f3f7f4; }
.panel-heading { display: flex; align-items: center; justify-content: space-between; padding: 18px 18px 14px; border-bottom: 1px solid #e1e9e4; }
.panel-heading strong, .panel-heading span { display: block; }
.panel-heading strong { color: #29443a; font-size: 14px; }
.panel-heading span { margin-top: 4px; color: #82938a; font-size: 12px; }
.endpoint-scroll { flex: 1; min-height: 0; padding: 10px; }
.tree-row { display: flex; align-items: center; width: 100%; border: 0; border-radius: 9px; background: transparent; color: #456056; cursor: pointer; text-align: left; }
.tree-row:hover { background: #e8f0eb; }
.tree-row.active { background: #dbece1; color: #204d37; }
.endpoint-row { min-height: 40px; gap: 7px; padding: 7px 8px; }
.container-row { gap: 8px; min-height: 34px; padding: 5px 8px 5px 33px; font-size: 12px; }
.tree-chevron { color: #82938a; transition: transform .2s ease; }
.tree-chevron.expanded { transform: rotate(90deg); }
.runtime-dot, .container-state { flex: 0 0 auto; width: 8px; height: 8px; border-radius: 50%; background: #9caea4; }
.runtime-dot.docker { background: #4b8bd8; }
.runtime-dot.containerd { background: #db8c55; }
.container-state.running, .container-state.ready { background: #42a56d; }
.container-state.exited, .container-state.stopped { background: #b6c1bb; }
.tree-label { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-meta { color: #92a098; font-size: 10px; }
.container-children { margin-left: 11px; border-left: 1px solid #d6e1da; }
.tree-loading { padding: 7px 10px 7px 33px; color: #9aaa9f; font-size: 11px; }
.empty-tree { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 80px 20px; color: #9aaa9f; font-size: 12px; }
.empty-tree .el-icon { font-size: 30px; color: #bfd0c4; }
.endpoint-footer { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 15px; border-top: 1px solid #e1e9e4; }
.runtime-panel { min-width: 0; min-height: 0; display: flex; flex-direction: column; background: #fff; }
.detail-header { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 28px 30px 22px; border-bottom: 1px solid #edf2ee; }
.detail-kicker { color: #9aab9f; font-size: 10px; letter-spacing: .15em; font-weight: 800; }
.detail-header h3 { margin: 7px 0 5px; color: #20342b; font-size: 21px; }
.detail-header p { margin: 0; color: #83948a; font-size: 12px; }
.detail-actions { display: flex; gap: 8px; }
.log-workspace { display: flex; min-height: 0; flex: 1; flex-direction: column; }
.log-tabs { display: flex; align-items: center; gap: 22px; padding: 13px 30px; border-bottom: 1px solid #edf2ee; color: #a2aea7; font-size: 12px; }
.log-tabs .active { color: #2e7350; font-weight: 700; }
.log-tabs .el-tag { margin-left: auto; }
.log-viewer { min-height: 0; flex: 1; margin: 0; overflow: auto; padding: 24px 30px; background: #17221d; color: #d0e5d6; font: 12px/1.7 Consolas, 'SFMono-Regular', monospace; white-space: pre-wrap; }
.workspace-empty { margin: auto; }
.endpoint-info-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding: 30px; }
.info-tile { display: flex; flex-direction: column; gap: 8px; padding: 18px; border: 1px solid #e6eee8; border-radius: 12px; background: #fbfdfb; }
.info-tile span { color: #96a49b; font-size: 11px; }
.info-tile strong { overflow: hidden; color: #345444; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.field-hint { margin-top: 4px; color: #93a198; font-size: 12px; line-height: 1.5; }
.field-with-action { display: flex; width: 100%; gap: 8px; }
.test-connection-row { display: flex; align-items: center; gap: 12px; margin-top: 16px; padding-top: 15px; border-top: 1px solid #edf2ee; }
.test-ok { color: #2e8a57; font-size: 12px; }
.test-failed { color: #c65845; font-size: 12px; }
@media (max-width: 900px) { .workspace-card { grid-template-columns: 260px minmax(0, 1fr); } .detail-header { align-items: flex-start; flex-direction: column; } }
@media (max-width: 680px) { .containers-toolbar { align-items: flex-start; flex-direction: column; } .workspace-card { display: flex; flex-direction: column; } .endpoint-panel { min-height: 270px; max-height: 40vh; border-right: 0; border-bottom: 1px solid #e1e9e4; } .runtime-panel { min-height: 420px; } }
</style>
