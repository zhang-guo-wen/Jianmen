<template>
  <div class="view-stack audit-view">
    <el-tabs v-model="auditScope" class="page-tabs">
      <el-tab-pane v-if="permission.canDo('audit:view')" :label="t('audit.scope.ssh')" name="ssh">
        <el-alert v-if="sessionError" :title="sessionError" type="error" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="sessions"
            :loading="sessionsLoading"
            :total="sessionTotal"
            v-model:page="sessionPage"
            v-model:page-size="sessionPageSize"
            v-model:search="sessionKeyword"
            search-placeholder="搜索会话…"
            @search="onSessionSearch"
          >
            <template #toolbar-extra>
              <el-button :loading="sessionsLoading" :icon="Refresh" @click="loadSessions">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.instance')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">
                {{ sessionInstance(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.account')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">
                {{ sessionAccount(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.operator')" min-width="150">
              <template #default="{ row }">
                {{ sessionUser(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.sessionId')" width="110">
              <template #default="{ row }">
                <el-link v-if="row.session_id" type="primary" @click.stop="showUserSessionDetail(row.session_id)">
                  {{ row.session_id }}
                </el-link>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.protocol')" width="90">
              <template #default="{ row }">
                <el-tag :type="sessionProtocolTag(row)" size="small" effect="plain">{{ sessionProtocol(row) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('sessions.column.started')" width="170" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">
                {{ formatTime(row.started_at ?? row.startedAt) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.duration')" width="90">
              <template #default="{ row }">
                {{ formatDurationSeconds(computeDuration(row.started_at, row.ended_at)) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.logCount')" width="90" align="center">
              <template #default="{ row }">{{ row.log_count ?? 0 }}</template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" fixed="right" width="180">
              <template #default="{ row }">
                <el-button :disabled="!hasReplay(row)" link type="success" @click="loadSessionArtifact(row, 'replay')">
                  {{ t('audit.action.replay') }}
                </el-button>
                <el-button link type="primary" @click="loadSessionLog(row)">
                  {{ t('audit.action.log') }}
                </el-button>
              </template>
            </el-table-column>
          </DataTableCard>
        </div>
      </el-tab-pane>
      <el-tab-pane v-if="canAccessRDPTab" :label="t('audit.scope.rdp')" name="rdp">
        <el-alert v-if="rdpError" :title="rdpError" type="error" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="rdpSessions"
            :loading="rdpLoading"
            :total="rdpTotal"
            v-model:page="rdpPage"
            v-model:page-size="rdpPageSize"
            v-model:search="rdpKeyword"
            search-placeholder="搜索 RDP 会话…"
            @search="onRDPSearch"
          >
            <template #toolbar-extra>
              <el-button :loading="rdpLoading" :icon="Refresh" @click="loadRDPSessions">
                {{ t('common.refresh') }}
              </el-button>
            </template>
            <el-table-column label="Windows 主机" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ rdpSessionTarget(row) }}</template>
            </el-table-column>
            <el-table-column label="主机账号" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ rdpSessionAccount(row) }}</template>
            </el-table-column>
            <el-table-column label="操作用户" min-width="130" show-overflow-tooltip>
              <template #default="{ row }">{{ row.username || row.user_id || '-' }}</template>
            </el-table-column>
            <el-table-column label="结果" width="100">
              <template #default="{ row }">
                <el-tooltip
                  :disabled="!row.failure_message"
                  :content="row.failure_message || ''"
                  placement="top"
                >
                  <el-tag
                    :type="rdpOutcomeTag(row.outcome)"
                    :tabindex="row.failure_message ? 0 : -1"
                    :aria-label="row.failure_message
                      ? `${rdpOutcomeLabel(row.outcome)}：${row.failure_message}`
                      : rdpOutcomeLabel(row.outcome)"
                    size="small"
                    effect="plain"
                  >
                    {{ rdpOutcomeLabel(row.outcome) }}
                  </el-tag>
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column label="开始时间" width="170" class-name="col-time">
              <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
            </el-table-column>
            <el-table-column label="时长" width="90">
              <template #default="{ row }">
                {{ formatDurationSeconds(computeDuration(row.started_at, row.ended_at)) }}
              </template>
            </el-table-column>
            <el-table-column label="录屏" width="100">
              <template #default="{ row }">
                <el-tag :type="rdpRecordingTag(row.recording_status)" size="small" effect="plain">
                  {{ rdpRecordingLabel(row.recording_status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" fixed="right" width="90">
              <template #default="{ row }">
                <el-button
                  :disabled="!row.has_replay"
                  link
                  type="success"
                  @click="openRDPReplay(row)"
                >
                  回放
                </el-button>
              </template>
            </el-table-column>
          </DataTableCard>
        </div>
      </el-tab-pane>
      <el-tab-pane v-if="permission.canDo('db:audit:view')" :label="t('audit.scope.db')" name="db">
        <el-alert v-if="dbError" :title="dbError" type="warning" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="dbConnections"
            :loading="dbLoading"
            :total="dbTotal"
            v-model:page="dbPage"
            v-model:page-size="dbPageSize"
            v-model:search="dbKeyword"
            search-placeholder="搜索数据库连接…"
            @search="onDBSearch"
          >
            <template #toolbar-extra>
              <el-button :loading="dbLoading" :icon="Refresh" @click="loadDBConnections">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.instance')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ row.target_name || row.upstream_addr || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.account')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">{{ row.account_name || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.operator')" min-width="150">
              <template #default="{ row }">
                {{ row.username || row.name || '-' }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.sessionId')" width="110">
              <template #default="{ row }">
                <el-link v-if="row.session_id" type="primary" @click.stop="showUserSessionDetail(row.session_id)">
                  {{ row.session_id }}
                </el-link>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.protocol')" width="110">
              <template #default="{ row }">
                <el-tag :type="databaseProtocolTag(row.protocol)" size="small" effect="plain">
                  {{ formatDatabaseProtocol(row.protocol) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('sessions.column.started')" min-width="170" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">
                {{ formatTime(row.started_at) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.duration')" width="100">
              <template #default="{ row }">
                {{ formatDuration(row.duration_ms ?? computeDurationMs(row.started_at, row.ended_at)) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.logCount')" width="90" align="center">
              <template #default="{ row }">{{ row.log_count ?? 0 }}</template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" fixed="right" width="90">
              <template #default="{ row }">
                <el-button link type="primary" @click="loadDBArtifact(row, 'queries')">
                  {{ t('audit.action.queries') }}
                </el-button>
              </template>
            </el-table-column>
          </DataTableCard>
        </div>
      </el-tab-pane>
      <el-tab-pane v-if="permission.canDo('session:view')" :label="t('audit.scope.online')" name="online">
        <el-alert v-if="onlineError" :title="onlineError" type="warning" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="onlineSessions"
            :loading="onlineLoading"
            :total="onlineTotal"
            v-model:page="onlinePage"
            v-model:page-size="onlinePageSize"
            v-model:search="onlineKeyword"
            :search-placeholder="t('audit.search.online')"
            @search="onOnlineSearch"
          >
            <template #toolbar-extra>
              <el-button :loading="onlineLoading" :icon="Refresh" @click="loadOnlineSessions">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.instance')" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ row.instance || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.protocol')" width="110">
              <template #default="{ row }">
                <el-tag :type="onlineProtocolTag(row)" size="small" effect="plain">
                  {{ onlineProtocol(row) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('audit.column.account')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ row.account || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.operator')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">{{ row.operator || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.sessionId')" width="110">
              <template #default="{ row }">
                <el-link v-if="row.session_id" type="primary" @click.stop="showUserSessionDetail(row.session_id)">
                  {{ row.session_id }}
                </el-link>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('sessions.column.started')" width="170" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" fixed="right" width="210">
              <template #default="{ row }">
                <el-button :disabled="!row.has_replay" link type="success" @click="loadOnlineReplay(row)">
                  {{ t('audit.action.replay') }}
                </el-button>
                <el-button link type="primary" @click="loadOnlineLog(row)">
                  {{ t('audit.action.log') }}
                </el-button>
                <el-button
                  v-if="permission.canDo('session:disconnect')"
                  :loading="disconnectingSessionID === row.id"
                  link
                  type="danger"
                  @click="disconnectOnlineSession(row)"
                >
                  {{ t('audit.action.disconnect') }}
                </el-button>
              </template>
            </el-table-column>
          </DataTableCard>
        </div>
      </el-tab-pane>
      <el-tab-pane v-if="permission.canDo('audit:view')" :label="t('audit.scope.logins')" name="logins">
        <el-alert v-if="loginAuditError" :title="loginAuditError" type="error" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="loginAuditLogs"
            :loading="loginAuditLoading"
            :total="loginAuditTotal"
            v-model:page="loginAuditPage"
            v-model:page-size="loginAuditPageSize"
            v-model:search="loginAuditKeyword"
            :search-placeholder="t('audit.search.logins')"
            @search="onLoginAuditSearch"
          >
            <template #toolbar-extra>
              <el-select v-model="loginAuditOutcome" size="small" style="width: 110px" @change="loadLoginAuditLogs">
                <el-option :label="t('audit.filter.all')" value="" />
                <el-option :label="t('audit.result.success')" value="success" />
                <el-option :label="t('audit.result.failure')" value="failure" />
                <el-option :label="t('audit.result.blocked')" value="blocked" />
              </el-select>
              <el-button :loading="loginAuditLoading" :icon="Refresh" @click="loadLoginAuditLogs">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.time')" width="175" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column prop="username" :label="t('audit.column.username')" min-width="140" show-overflow-tooltip />
            <el-table-column :label="t('audit.column.result')" width="100">
              <template #default="{ row }">
                <el-tag :type="loginOutcomeTag(row.outcome)" size="small" effect="plain">{{ loginOutcomeLabel(row.outcome) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="reason" :label="t('audit.column.reason')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ row.reason || '-' }}</template>
            </el-table-column>
            <el-table-column prop="client_ip" :label="t('audit.column.client')" width="140" show-overflow-tooltip />
            <el-table-column prop="user_agent" :label="t('audit.column.userAgent')" min-width="240" show-overflow-tooltip />
          </DataTableCard>
        </div>
      </el-tab-pane>
      <el-tab-pane v-if="permission.canDo('audit:view')" :label="t('audit.scope.operations')" name="operations">
        <el-alert v-if="operationAuditError" :title="operationAuditError" type="error" show-icon style="margin-bottom: 12px" />
        <div class="page-container">
          <DataTableCard
            :data="operationAuditLogs"
            :loading="operationAuditLoading"
            :total="operationAuditTotal"
            v-model:page="operationAuditPage"
            v-model:page-size="operationAuditPageSize"
            v-model:search="operationAuditKeyword"
            :search-placeholder="t('audit.search.operations')"
            @search="onOperationAuditSearch"
          >
            <template #toolbar-extra>
              <el-select v-model="operationAuditAction" size="small" style="width: 110px" @change="loadOperationAuditLogs">
                <el-option :label="t('audit.filter.all')" value="" />
                <el-option :label="t('audit.action.create')" value="create" />
                <el-option :label="t('audit.action.update')" value="update" />
                <el-option :label="t('audit.action.delete')" value="delete" />
                <el-option :label="t('audit.action.revoke')" value="revoke" />
                <el-option :label="t('audit.action.test')" value="test" />
              </el-select>
              <el-button :loading="operationAuditLoading" :icon="Refresh" @click="loadOperationAuditLogs">{{ t('common.refresh') }}</el-button>
            </template>
            <el-table-column :label="t('audit.column.time')" width="175" show-overflow-tooltip class-name="col-time">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column prop="actor_username" :label="t('audit.column.operator')" width="130" show-overflow-tooltip />
            <el-table-column :label="t('audit.column.action')" width="100">
              <template #default="{ row }">{{ operationActionLabel(row.action) }}</template>
            </el-table-column>
            <el-table-column prop="resource_type" :label="t('audit.column.resource')" min-width="150" show-overflow-tooltip />
            <el-table-column :label="t('audit.column.resourceId')" min-width="170" show-overflow-tooltip>
              <template #default="{ row }">{{ row.resource_id || row.resource_name || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('audit.column.result')" width="100">
              <template #default="{ row }">
                <el-tag :type="operationResultTag(row)" size="small" effect="plain">{{ operationResultLabel(row) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="client_ip" :label="t('audit.column.client')" width="140" show-overflow-tooltip />
          </DataTableCard>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-drawer
      v-model="drawerVisible"
      direction="rtl"
      size="65%"
      @close="closeDetail"
    >
      <template #title>
        <div class="toolbar">
          <span>{{ detailTitle || t('audit.title.detail') }}</span>
          <el-tag v-if="detailKind">{{ detailKind === 'queries' ? t('audit.action.queries') : detailKind }}</el-tag>
        </div>
      </template>
      <el-alert v-if="detailError" :title="detailError" type="error" show-icon />
      <div v-else v-loading="detailLoading" class="drawer-content">
        <el-descriptions v-if="isDBMeta" :column="2" border>
          <el-descriptions-item :label="t('common.id')">{{ dbMeta.id || t('common.none') }}</el-descriptions-item>
          <el-descriptions-item :label="t('common.name')">{{ dbMeta.name || t('common.none') }}</el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.protocol')">
            {{ dbMeta.protocol || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.authUser')">
            {{ dbMeta.username || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.database')">
            {{ dbMeta.database || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.application')">
            {{ dbMeta.application_name || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.client')">
            {{ dbMeta.client_addr || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.upstream')">
            {{ dbMeta.upstream_addr || t('common.none') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.allowedUsers')" :span="2">
            <el-tag :type="dbMeta.allowed_users_enforced ? 'success' : 'info'">
              {{ dbMeta.allowed_users_enforced ? t('common.enabled') : t('common.disabled') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('audit.column.observation')" :span="2">
            {{ dbMeta.auth_observation || t('common.none') }}
          </el-descriptions-item>
        </el-descriptions>

        <div v-else-if="isReplay" class="replay-panel">
          <div class="replay-controls">
            <div class="replay-meta">
              <span>{{ formatReplayDuration(replayCurrentTime) }}</span>
              <el-slider
                v-model="replaySeekPercent"
                :max="100"
                :show-tooltip="false"
                :disabled="!replayFrames.length"
                size="small"
                @change="seekReplay"
              />
              <span>{{ formatReplayDuration(replayDuration) }}</span>
            </div>
            <div class="replay-actions">
              <el-button :disabled="!replayOutputFrames.length" type="primary" size="small" @click="playReplay">
                {{ replayPlaying ? t('audit.action.restart') : t('audit.action.play') }}
              </el-button>
              <el-button :disabled="!replayPlaying" size="small" @click="stopReplay">
                {{ t('audit.action.stop') }}
              </el-button>
              <el-select
                v-model="playbackSpeed"
                size="small"
                :disabled="replayPlaying"
                style="width: 64px"
              >
                <el-option
                  v-for="s in speedOptions"
                  :key="s"
                  :label="`${s}x`"
                  :value="s"
                />
              </el-select>
            </div>
          </div>
          <div class="replay-meta-secondary">
            <span>{{ t('audit.replay.frames') }} {{ replayFrames.length }}</span>
            <span>{{ t('audit.replay.outputFrames') }} {{ replayOutputFrames.length }}</span>
            <span>{{ t('audit.replay.size') }} {{ formatBytes(replayRawBytes) }}</span>
          </div>
          <div class="replay-terminal-shell">
            <div ref="replayTerminalHostRef" class="replay-terminal" />
            <div v-if="replayTerminalMessage" class="replay-terminal-empty">
              {{ replayTerminalMessage }}
            </div>
          </div>
        </div>

        <DataTableCard
          v-else-if="isDBQueries"
          :key="`queries-${logSearchVersion}`"
          :data="mergedQueryEvents"
          :total="dbQueryTotal"
          :loading="detailLoading"
          row-key="seq"
          :search-placeholder="t('audit.search.sqlLog')"
          v-model:page="logPage"
          v-model:page-size="logPageSize"
          @search="onLogSearch"
        >
          <el-table-column :label="t('audit.column.time')" width="170" show-overflow-tooltip class-name="col-time">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.sql')" min-width="420">
            <template #default="{ row }">
              <div class="query-sql-cell">
                <span class="query-sql-cell__text" :title="row.sql">{{ row.sql }}</span>
                <el-tag v-if="row.sql_truncated" type="warning" size="small" effect="plain">
                  {{ t('audit.query.truncated') }} {{ formatBytes(row.sql_original_bytes) }}
                </el-tag>
                <el-button
                  v-if="row.sql"
                  class="query-sql-cell__copy"
                  link
                  size="small"
                  @click.stop="copyQuerySQL(row.sql)"
                >
                  {{ t('audit.query.copyPreview') }}
                </el-button>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.duration')" width="100">
            <template #default="{ row }">
              {{ formatDuration(row.duration_ms) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.result')" width="110">
            <template #default="{ row }">
              <el-tag :type="queryStatusType(row.status)" size="small" effect="plain">
                {{ queryStatusLabel(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
        </DataTableCard>

        <DataTableCard
          v-else-if="isCommands"
          :key="`commands-${logSearchVersion}`"
          :data="pagedCommandEvents"
          :total="filteredCommandEvents.length"
          :search-placeholder="t('audit.search.commandLog')"
          v-model:page="logPage"
          v-model:page-size="logPageSize"
          @search="onLogSearch"
        >
          <el-table-column :label="t('audit.column.time')" width="175" show-overflow-tooltip class-name="col-time">
            <template #default="{ row }">
              {{ formatTime(row.timestamp ?? row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column prop="command" :label="t('audit.column.command')" min-width="280" show-overflow-tooltip />
          <el-table-column prop="output" :label="t('audit.column.output')" min-width="280" show-overflow-tooltip />
        </DataTableCard>

        <DataTableCard
          v-else-if="isFiles"
          :data="pagedFileEvents"
          :total="fileEvents.length"
          :show-search="false"
          v-model:page="logPage"
          v-model:page-size="logPageSize"
        >
          <el-table-column :label="t('audit.column.time')" width="175" show-overflow-tooltip class-name="col-time">
            <template #default="{ row }">
              {{ formatTime(row.timestamp ?? row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.action')" width="80">
            <template #default="{ row }">
              {{ formatFileAction(row.action) }}
            </template>
          </el-table-column>
          <el-table-column prop="path" :label="t('audit.column.path')" min-width="420" show-overflow-tooltip />
          <el-table-column :label="t('audit.column.result')" width="75">
            <template #default="{ row }">
              <el-tag :type="row.result === 'success' ? 'success' : 'danger'" size="small">
                {{ row.result === 'success' ? t('audit.result.success') : t('audit.result.failure') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('audit.column.size')" width="75">
            <template #default="{ row }">
              <template v-if="row.size > 0">{{ formatBytes(row.size) }}</template>
            </template>
          </el-table-column>
        </DataTableCard>

        <el-empty v-else :description="t('audit.empty.detail')" />
      </div>
    </el-drawer>

    <el-dialog
      v-model="rdpReplayVisible"
      title="RDP 会话回放"
      width="min(1180px, calc(100vw - 32px))"
      destroy-on-close
      @closed="destroyRDPReplay"
    >
      <el-alert
        v-if="rdpReplayError"
        :title="rdpReplayError"
        type="error"
        show-icon
        style="margin-bottom: 12px"
      />
      <div v-loading="rdpReplayLoading" class="rdp-replay-panel">
        <div class="rdp-replay-controls">
          <div class="rdp-replay-actions">
            <el-button
              type="primary"
              :disabled="rdpReplayDuration <= 0"
              @click="toggleRDPReplay"
            >
              {{ rdpReplayPlaying ? '暂停' : '播放' }}
            </el-button>
            <el-button :disabled="rdpReplayDuration <= 0" @click="restartRDPReplay">
              重播
            </el-button>
          </div>
          <div class="rdp-replay-timeline">
            <span>{{ formatRDPReplayTime(rdpReplayPosition) }}</span>
            <el-slider
              v-model="rdpReplayPosition"
              :max="Math.max(rdpReplayDuration, 1)"
              :show-tooltip="false"
              :disabled="rdpReplayDuration <= 0"
              aria-label="RDP 回放进度"
              @change="seekRDPReplay"
            />
            <span>{{ formatRDPReplayTime(rdpReplayDuration) }}</span>
          </div>
        </div>
        <div ref="rdpReplayHostRef" class="rdp-replay-display">
          <el-empty v-if="!rdpReplayLoading && !rdpReplayDuration && !rdpReplayError" description="录屏中暂无可播放画面" />
        </div>
      </div>
    </el-dialog>

    <UserSessionDetailDialog v-model="sessionDetailVisible" :session-id="sessionDetailId" />
  </div>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import Guacamole from 'guacamole-common-js';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Refresh } from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { useRoute } from 'vue-router';

import DataTableCard from '@/components/DataTableCard.vue';
import UserSessionDetailDialog from '@/components/UserSessionDetailDialog.vue';
import {
  apiClient,
  type DBConnectionMetaRecord,
  type DBConnectionRecord,
  type DBQueryEventRecord,
  type OnlineSessionRecord,
  type SessionCommandRecord,
  type SessionFileEventRecord,
  type SessionRecord,
  type LoginAuditRecord,
  type OperationAuditRecord,
  type RDPAuditSessionRecord,
} from '@/api/client';
import { useI18n } from '@/i18n';
import { usePermissionStore } from '@/stores/permission';
import { installUnicodeGuacamoleParser } from '@/utils/guacamoleProtocol';
import {
  bindRDPReplayDisplay,
  type RDPReplayDisplayBinding,
  type RDPReplayScaleDisplay,
} from '@/utils/rdpReplayDisplay';

type AuditScope = 'logins' | 'operations' | 'ssh' | 'rdp' | 'db' | 'online';
type DetailKind = '' | 'meta' | 'commands' | 'files' | 'file-summary' | 'queries' | 'replay';
type ReplayFrame = {
  time: number;
  stream: string;
  data: string;
};
type ReplayData = {
  header: Record<string, unknown>;
  frames: ReplayFrame[];
  raw: string;
};

interface RDPReplayDisplay extends RDPReplayScaleDisplay {
  getElement(): HTMLDivElement;
}

interface RDPRecording {
  connect(data?: string): void;
  disconnect(): void;
  abort(): void;
  play(): void;
  pause(): void;
  seek(position: number, callback?: () => void): void;
  isPlaying(): boolean;
  getPosition(): number;
  getDuration(): number;
  getDisplay(): RDPReplayDisplay;
  onload: (() => void) | null;
  onerror: ((message: string) => void) | null;
  onprogress: ((duration: number, parsedSize: number) => void) | null;
  onplay: (() => void) | null;
  onpause: (() => void) | null;
  onseek: ((position: number) => void) | null;
}

interface GuacamoleReplayRuntime {
  StaticHTTPTunnel: new (
    url: string,
    crossDomain?: boolean,
    headers?: Record<string, string>,
  ) => unknown;
  SessionRecording: new (source: unknown) => RDPRecording;
}

const GuacamoleReplay = Guacamole as unknown as GuacamoleReplayRuntime;
installUnicodeGuacamoleParser(Guacamole);

const { t } = useI18n();
const permission = usePermissionStore();
const route = useRoute();

function routeQueryValue(value: unknown): string {
  if (Array.isArray(value)) return routeQueryValue(value[0]);
  return typeof value === 'string' ? value.trim() : '';
}

function permittedAuditScope(value: unknown): AuditScope {
  const requested = routeQueryValue(value);
  if (requested === 'logins' && permission.canDo('audit:view')) return 'logins';
  if (requested === 'operations' && permission.canDo('audit:view')) return 'operations';
  if (requested === 'online' && permission.canDo('session:view')) return 'online';
  if (requested === 'db' && permission.canDo('db:audit:view')) return 'db';
  if (requested === 'rdp' && permission.canDo('rdp:recording:view')) return 'rdp';
  if (requested === 'ssh' && permission.canDo('audit:view')) return 'ssh';
  if (permission.canDo('audit:view')) return 'ssh';
  if (permission.canDo('rdp:recording:view')) return 'rdp';
  if (permission.canDo('db:audit:view')) return 'db';
  return 'online';
}

const initialAuditScope = permittedAuditScope(route.query.scope);
const initialAuditKeyword = routeQueryValue(route.query.q);
const auditScope = ref<AuditScope>(initialAuditScope);
const initialOnlineResourceType = initialAuditScope === 'online' ? routeQueryValue(route.query.resource_type) : '';
const initialOnlineResourceID = initialAuditScope === 'online' ? routeQueryValue(route.query.resource_id) : '';

// ── SSH session list state ──
const sessions = ref<SessionRecord[]>([]);
const sessionTotal = ref(0);
const sessionPage = ref(1);
const sessionPageSize = ref(50);
const sessionKeyword = ref(initialAuditScope === 'ssh' ? initialAuditKeyword : '');
const sessionsLoading = ref(false);
const sessionError = ref('');

// ── RDP audit and replay state ──
const canViewRDPRecordings = computed(() => permission.canDo('rdp:recording:view'));
const canAccessRDPTab = canViewRDPRecordings;
const rdpSessions = ref<RDPAuditSessionRecord[]>([]);
const rdpTotal = ref(0);
const rdpPage = ref(1);
const rdpPageSize = ref(50);
const rdpKeyword = ref(initialAuditScope === 'rdp' ? initialAuditKeyword : '');
const rdpLoading = ref(false);
const rdpError = ref('');

const rdpReplayVisible = ref(false);
const rdpReplayLoading = ref(false);
const rdpReplayError = ref('');
const rdpReplayHostRef = ref<HTMLElement>();
const rdpReplayPlaying = ref(false);
const rdpReplayPosition = ref(0);
const rdpReplayDuration = ref(0);
let rdpRecording: RDPRecording | undefined;
let rdpReplayResizeObserver: ResizeObserver | undefined;
let rdpReplayDisplayBinding: RDPReplayDisplayBinding | undefined;

// Login and management operation audit state
const loginAuditLogs = ref<LoginAuditRecord[]>([]);
const loginAuditTotal = ref(0);
const loginAuditPage = ref(1);
const loginAuditPageSize = ref(50);
const loginAuditKeyword = ref(initialAuditScope === 'logins' ? initialAuditKeyword : '');
const loginAuditOutcome = ref('');
const loginAuditLoading = ref(false);
const loginAuditError = ref('');
const operationAuditLogs = ref<OperationAuditRecord[]>([]);
const operationAuditTotal = ref(0);
const operationAuditPage = ref(1);
const operationAuditPageSize = ref(50);
const operationAuditKeyword = ref(initialAuditScope === 'operations' ? initialAuditKeyword : '');
const operationAuditAction = ref('');
const operationAuditLoading = ref(false);
const operationAuditError = ref('');

// ── DB connection list state ──
const dbConnections = ref<DBConnectionRecord[]>([]);
const dbTotal = ref(0);
const dbPage = ref(1);
const dbPageSize = ref(50);
const dbKeyword = ref(initialAuditScope === 'db' ? initialAuditKeyword : '');
const dbLoading = ref(false);
const dbError = ref('');

// Online session list state
const onlineSessions = ref<OnlineSessionRecord[]>([]);
const onlineTotal = ref(0);
const onlinePage = ref(1);
const onlinePageSize = ref(50);
const onlineKeyword = ref(initialAuditScope === 'online' ? initialAuditKeyword : '');
const onlineResourceType = ref(initialOnlineResourceType);
const onlineResourceID = ref(initialOnlineResourceID);
const onlineLoading = ref(false);
const onlineError = ref('');
const disconnectingSessionID = ref('');
const sessionDetailVisible = ref(false);
const sessionDetailId = ref('');
let onlineRefreshTimer: number | undefined;

// ── Drawer state ──
const detailLoading = ref(false);
const detailError = ref('');
const detailTitle = ref('');
const detailKind = ref<DetailKind>('');
const detailData = ref<unknown>(null);
const drawerVisible = ref(false);
const logPage = ref(1);
const logPageSize = ref(50);
const logKeyword = ref('');
const logSearchVersion = ref(0);
const dbQueryConnectionID = ref('');
const dbQueryTotal = ref(0);
let detailRequestVersion = 0;

// ── Replay state ──
const playbackSpeed = ref(1);
const speedOptions = [1, 2, 4, 8];
const replayPlaying = ref(false);
const replayProgress = ref(0);
const replaySeekPercent = ref(0);
const replayCurrentTime = ref(0);
const replayRenderedOutput = ref(false);
const replayTerminalHostRef = ref<HTMLElement>();
let replayTerminal: Terminal | undefined;
let replayFitAddon: FitAddon | undefined;
let replayResizeObserver: ResizeObserver | undefined;
let replayTimer: number | undefined;
let replayStartedAt = 0;
let replayStartOffset = 0;
let replayFrameIndex = 0;

// ── Computed ──
const isDBMeta = computed(() => detailKind.value === 'meta' && isRecord(detailData.value) && 'protocol' in detailData.value);
function hasItems(data: unknown): boolean {
  if (Array.isArray(data)) return true;
  if (data && typeof data === 'object' && 'items' in data && Array.isArray((data as Record<string, unknown>).items)) return true;
  return false;
}
const isDBQueries = computed(() => detailKind.value === 'queries' && hasItems(detailData.value));
const isCommands = computed(() => detailKind.value === 'commands' && hasItems(detailData.value));
const isFiles = computed(() => detailKind.value === 'files' && hasItems(detailData.value));
const isReplay = computed(() => detailKind.value === 'replay' && isReplayData(detailData.value));
const dbMeta = computed(() => (isRecord(detailData.value) ? (detailData.value as DBConnectionMetaRecord) : {}));
const queryEvents = computed(() => extractItems<DBQueryEventRecord>(detailData.value));

// 合并 start/finish 事件为一行
interface MergedQueryEvent {
  seq: number;
  sql: string;
  sql_truncated: boolean;
  sql_original_bytes: number;
  comment: string;
  query_kind: string;
  status: string;
  duration_ms: number;
  started_at: number;
  error_code?: string;
  error_message?: string;
}
function splitSQLComment(sql: string): { comment: string; sql: string } {
  // 提取 MySQL /++ ... +/ 风格注释，作为来源信息
  const m = /^\/\*\s*(.+?)\s*\*\/\s*/.exec(sql);
  if (m) {
    return { comment: m[1], sql: sql.slice(m[0].length) };
  }
  return { comment: '', sql };
}
const mergedQueryEvents = computed<MergedQueryEvent[]>(() => {
  const map = new Map<number, MergedQueryEvent>();
  for (const ev of queryEvents.value) {
    const seq = ev.seq ?? 0;
    const cur = map.get(seq) ?? {
      seq,
      sql: '',
      sql_truncated: false,
      sql_original_bytes: 0,
      comment: '',
      query_kind: ev.query_kind ?? '',
      status: 'unknown',
      duration_ms: 0,
      started_at: ev.started_at ?? 0,
    };
    if (ev.type === 'query_started') {
      const parsed = splitSQLComment(ev.sql || cur.sql);
      cur.sql = parsed.sql || cur.sql;
      cur.comment = parsed.comment || cur.comment;
      cur.query_kind = ev.query_kind || cur.query_kind;
      cur.started_at = ev.started_at ?? cur.started_at;
      cur.sql_truncated = Boolean(ev.detail?.sql_truncated);
      cur.sql_original_bytes = Number(ev.detail?.sql_original_bytes ?? 0);
    } else {
      cur.status = ev.status ?? cur.status;
      cur.duration_ms = ev.duration_ms ?? cur.duration_ms;
      cur.error_code = ev.error_code;
      cur.error_message = ev.error_message;
      // 如果没有 duration_ms，用 started_at 和 completed_at 计算
      if (!cur.duration_ms && cur.started_at && ev.completed_at) {
        cur.duration_ms = ev.completed_at - cur.started_at;
      }
    }
    map.set(seq, cur);
  }
  return Array.from(map.values()).sort((a, b) => a.seq - b.seq);
});
function extractItems<T>(data: unknown): T[] {
  if (Array.isArray(data)) return data as T[];
  if (data && typeof data === 'object' && 'items' in data && Array.isArray((data as Record<string, unknown>).items)) {
    return (data as Record<string, unknown>).items as T[];
  }
  return [];
}
const commandEvents = computed(() => extractItems<SessionCommandRecord>(detailData.value));
const fileEvents = computed(() => extractItems<SessionFileEventRecord>(detailData.value));
const normalizedLogKeyword = computed(() => logKeyword.value.trim().toLowerCase());
const filteredCommandEvents = computed(() => {
  if (!normalizedLogKeyword.value) return commandEvents.value;
  return commandEvents.value.filter((event) => String(event.command ?? '').toLowerCase().includes(normalizedLogKeyword.value));
});

// Client-side pagination for drawer sub-tables
const pagedCommandEvents = computed(() => {
  const start = (logPage.value - 1) * logPageSize.value;
  return filteredCommandEvents.value.slice(start, start + logPageSize.value);
});
const pagedFileEvents = computed(() => {
  const start = (logPage.value - 1) * logPageSize.value;
  return fileEvents.value.slice(start, start + logPageSize.value);
});

const replayData = computed(() => (isReplayData(detailData.value) ? detailData.value : { header: {}, frames: [], raw: '' }));
const replayFrames = computed(() => replayData.value.frames);
const replayOutputFrames = computed(() => replayFrames.value.filter((frame) => frame.stream === 'o'));
const replayDuration = computed(() => replayFrames.value.at(-1)?.time ?? 0);
const replayRawBytes = computed(() => utf8ByteLength(replayData.value.raw));
const replayFirstOutputTime = computed(() => replayOutputFrames.value[0]?.time ?? 0);
const replayTerminalMessage = computed(() => {
  if (!isReplay.value) {
    return '';
  }
  if (!replayFrames.value.length) {
    return t('audit.empty.replay');
  }
  if (!replayOutputFrames.value.length) {
    return t('audit.empty.replayNoOutput');
  }
  if (replayPlaying.value && !replayRenderedOutput.value) {
    return t('audit.empty.replayWaiting');
  }
  return '';
});

function rdpSessionTarget(session: RDPAuditSessionRecord): string {
  return displayAuditIdentity(session.target_address || session.host_id, session.target_name);
}

function rdpSessionAccount(session: RDPAuditSessionRecord): string {
  return displayAuditIdentity(session.account_username || session.account_id, session.account_name);
}

function rdpOutcomeLabel(outcome: unknown): string {
  switch (String(outcome || '').toLowerCase()) {
    case 'succeeded': return '成功';
    case 'failed': return '失败';
    case 'denied': return '已拒绝';
    case 'terminated': return '已中止';
    case 'active': return '进行中';
    case 'connecting': return '连接中';
    default: return '未知';
  }
}

function rdpOutcomeTag(outcome: unknown): 'success' | 'warning' | 'danger' | 'info' {
  switch (String(outcome || '').toLowerCase()) {
    case 'succeeded': return 'success';
    case 'failed': return 'danger';
    case 'denied':
    case 'terminated': return 'warning';
    default: return 'info';
  }
}

function rdpRecordingLabel(status: unknown): string {
  switch (String(status || '').toLowerCase()) {
    case 'ready': return '可回放';
    case 'pending': return '录制中';
    case 'uploading': return '上传中';
    case 'failed': return '失败';
    case 'none': return '未录制';
    default: return '未知';
  }
}

function rdpRecordingTag(status: unknown): 'success' | 'warning' | 'danger' | 'info' {
  switch (String(status || '').toLowerCase()) {
    case 'ready': return 'success';
    case 'pending':
    case 'uploading': return 'warning';
    case 'failed': return 'danger';
    default: return 'info';
  }
}

async function loadRDPSessions() {
  if (!canViewRDPRecordings.value) return;
  rdpLoading.value = true;
  rdpError.value = '';
  try {
    const response = await apiClient.getRDPSessions({
      q: rdpKeyword.value.trim() || undefined,
      page: rdpPage.value,
      page_size: rdpPageSize.value,
    });
    rdpSessions.value = response.items ?? [];
    rdpTotal.value = response.total ?? 0;
  } catch (error) {
    rdpSessions.value = [];
    rdpError.value = error instanceof Error ? error.message : '加载 RDP 审计失败';
  } finally {
    rdpLoading.value = false;
  }
}

async function openRDPReplay(session: RDPAuditSessionRecord) {
  if (!session.id || !session.has_replay) return;
  destroyRDPReplay();
  rdpReplayVisible.value = true;
  rdpReplayLoading.value = true;
  rdpReplayError.value = '';
  await nextTick();

  const host = rdpReplayHostRef.value;
  if (!host) {
    rdpReplayLoading.value = false;
    rdpReplayError.value = '无法初始化回放画布';
    return;
  }

  const recordingURL = new URL(
    apiClient.getRDPRecordingURL(session.id),
    window.location.href,
  );
  const tunnel = new GuacamoleReplay.StaticHTTPTunnel(
    recordingURL.toString(),
    recordingURL.origin !== window.location.origin,
  );
  const recording = new GuacamoleReplay.SessionRecording(tunnel);
  rdpRecording = recording;

  const display = recording.getDisplay();
  const displayElement = display.getElement();
  displayElement.classList.add('rdp-recording-canvas');
  host.replaceChildren(displayElement);
  const displayBinding = bindRDPReplayDisplay(display, host);
  rdpReplayDisplayBinding = displayBinding;
  rdpReplayResizeObserver = new ResizeObserver(() => displayBinding.fit());
  rdpReplayResizeObserver.observe(host);

  recording.onprogress = duration => {
    rdpReplayDuration.value = duration;
    rdpReplayLoading.value = false;
    displayBinding.fit();
  };
  recording.onload = () => {
    rdpReplayDuration.value = recording.getDuration();
    rdpReplayLoading.value = false;
    displayBinding.fit();
  };
  recording.onerror = message => {
    rdpReplayLoading.value = false;
    rdpReplayError.value = message || '加载 RDP 录屏失败';
  };
  recording.onplay = () => {
    rdpReplayPlaying.value = true;
  };
  recording.onpause = () => {
    rdpReplayPlaying.value = false;
    rdpReplayPosition.value = recording.getPosition();
  };
  recording.onseek = position => {
    rdpReplayPosition.value = position;
    rdpReplayDuration.value = recording.getDuration();
  };
  recording.connect();
}

function toggleRDPReplay() {
  if (!rdpRecording) return;
  if (rdpRecording.isPlaying()) rdpRecording.pause();
  else rdpRecording.play();
}

function seekRDPReplay(position: number) {
  rdpRecording?.seek(position);
}

function restartRDPReplay() {
  if (!rdpRecording) return;
  rdpRecording.pause();
  rdpRecording.seek(0, () => rdpRecording?.play());
}

function formatRDPReplayTime(milliseconds: number): string {
  const seconds = Math.max(0, Math.round(milliseconds / 1000));
  const minutes = Math.floor(seconds / 60);
  const remainder = String(seconds % 60).padStart(2, '0');
  return `${minutes}:${remainder}`;
}

function destroyRDPReplay() {
  rdpReplayResizeObserver?.disconnect();
  rdpReplayResizeObserver = undefined;
  rdpReplayDisplayBinding?.detach();
  rdpReplayDisplayBinding = undefined;
  if (rdpRecording) {
    rdpRecording.onload = null;
    rdpRecording.onerror = null;
    rdpRecording.onprogress = null;
    rdpRecording.onplay = null;
    rdpRecording.onpause = null;
    rdpRecording.onseek = null;
    rdpRecording.pause();
    try {
      rdpRecording.abort();
    } catch {
      rdpRecording.disconnect();
    }
  }
  rdpRecording = undefined;
  rdpReplayPlaying.value = false;
  rdpReplayPosition.value = 0;
  rdpReplayDuration.value = 0;
  rdpReplayLoading.value = false;
  rdpReplayHostRef.value?.replaceChildren();
}

// ── Helpers ──

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isReplayData(value: unknown): value is ReplayData {
  return isRecord(value) && Array.isArray(value.frames) && typeof value.raw === 'string';
}

function sessionId(session: SessionRecord): string {
  return String(session.id ?? '');
}

function displayAuditIdentity(actualValue: unknown, displayNameValue: unknown): string {
  const actual = String(actualValue ?? '').trim();
  const displayName = String(displayNameValue ?? '').trim();
  if (!actual) return displayName || t('common.none');
  if (!displayName || displayName === actual) return actual;
  return `${actual}（${displayName}）`;
}

function sessionInstance(session: SessionRecord): string {
  return displayAuditIdentity(session.target_address ?? session.target_id, session.target_name);
}

function sessionAccount(session: SessionRecord): string {
  return displayAuditIdentity(session.account_username, session.account_name);
}

function sessionUser(session: SessionRecord): string {
  return String(session.username ?? session.user_username ?? session.user_id ?? t('common.none'));
}

function hasReplay(session: SessionRecord): boolean {
  return session.has_replay === true
    || (typeof session.replay_dir === 'string' && session.replay_dir.length > 0);
}

function formatTime(value: unknown): string {
  let d: Date | null = null
  if (typeof value === 'number' && Number.isFinite(value)) {
    d = new Date(value)
  } else if (typeof value === 'string' && value.trim()) {
    const parsed = Date.parse(value)
    if (!Number.isNaN(parsed)) d = new Date(parsed)
  }
  if (!d || Number.isNaN(d.getTime())) return t('common.none')
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).format(d)
}

function formatDuration(value: unknown): string {
  if (value === undefined || value === null) return t('common.none');
  const n = Number(value);
  if (!Number.isFinite(n)) return t('common.none');
  // n is milliseconds
  if (n < 1000) return `${Math.round(n)}ms`;
  const totalSeconds = n / 1000;
  if (totalSeconds < 60) return `${Math.round(totalSeconds * 10) / 10}s`;
  const mins = Math.floor(totalSeconds / 60);
  const secs = Math.round(totalSeconds % 60);
  return `${mins}m ${secs}s`;
}

function formatDurationSeconds(value: unknown): string {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return t('common.none');
  }
  if (value < 60) {
    return `${Math.round(value)}s`;
  }
  const mins = Math.floor(value / 60);
  const secs = Math.round(value % 60);
  return `${mins}m ${secs}s`;
}

function computeDuration(started_at: unknown, ended_at: unknown): number {
  const s = toTimestamp(started_at);
  const e = toTimestamp(ended_at);
  if (s && e && e > s) return (e - s) / 1000;
  return 0;
}

function computeDurationMs(started_at: unknown, ended_at: unknown): number {
  const s = toTimestamp(started_at);
  const e = toTimestamp(ended_at);
  if (s && e && e > s) return e - s;
  return 0;
}

function toTimestamp(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed)) return parsed;
  }
  return null;
}

function sessionProtocol(row: SessionRecord): string {
  const subtype = row.protocol_subtype || '';
  if (subtype === 'web-terminal') return 'Web';
  if (subtype === 'sftp') return 'SFTP';
  if (subtype === 'scp') return 'SCP';
  if (!subtype && !hasReplay(row)) return 'SFTP';
  return 'SSH';
}

function sessionProtocolTag(row: SessionRecord): 'success' | 'warning' | 'info' | 'danger' | '' {
  const subtype = row.protocol_subtype || '';
  if (subtype === 'web-terminal') return 'info';
  if (subtype === 'sftp') return 'warning';
  if (subtype === 'scp') return 'warning';
  if (!subtype && !hasReplay(row)) return 'warning';
  return 'success';
}

function formatDatabaseProtocol(protocol: unknown): string {
  switch (String(protocol ?? '').toLowerCase()) {
    case 'mysql':
      return 'MySQL';
    case 'postgres':
    case 'postgresql':
      return 'PostgreSQL';
    case 'redis':
      return 'Redis';
    default:
      return String(protocol || '-');
  }
}

function databaseProtocolTag(protocol: unknown): 'primary' | 'success' | 'warning' | 'info' | 'danger' | '' {
  switch (String(protocol ?? '').toLowerCase()) {
    case 'mysql':
      return 'warning';
    case 'postgres':
    case 'postgresql':
      return 'primary';
    case 'redis':
      return 'danger';
    default:
      return 'info';
  }
}

function formatReplayDuration(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0s';
  }
  if (value < 60) {
    return `${value.toFixed(1)}s`;
  }
  const minutes = Math.floor(value / 60);
  const seconds = Math.round(value % 60);
  return `${minutes}m ${seconds}s`;
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0 B';
  }
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

function queryStatusLabel(status: unknown): string {
  switch (String(status ?? '').toLowerCase()) {
    case 'success':
      return '成功';
    case 'error':
      return '失败';
    case 'policy_denied':
      return '拒绝';
    default:
      return '未知';
  }
}

function queryStatusType(status: unknown): 'success' | 'warning' | 'danger' | 'info' {
  switch (String(status ?? '').toLowerCase()) {
    case 'success':
      return 'success';
    case 'error':
    case 'policy_denied':
      return 'danger';
    case 'unknown':
      return 'warning';
    default:
      return 'info';
  }
}

function setDetail(title: string, kind: DetailKind, data: unknown) {
  stopReplay();
  detailTitle.value = title;
  detailKind.value = kind;
  detailData.value = data;
  drawerVisible.value = true;
  playbackSpeed.value = 1;
  logPage.value = 1;
  logKeyword.value = '';
  logSearchVersion.value++;
  replayProgress.value = 0;
  replaySeekPercent.value = 0;
  replayCurrentTime.value = 0;
  replayRenderedOutput.value = false;
  resetReplayTerminal();
}

function closeDetail() {
  stopReplay();
  detailRequestVersion++;
  dbQueryConnectionID.value = '';
  dbQueryTotal.value = 0;
  drawerVisible.value = false;
  playbackSpeed.value = 1;
}

async function copyQuerySQL(sql: string): Promise<void> {
  try {
    if (!navigator.clipboard?.writeText) {
      throw new Error('clipboard is unavailable');
    }
    await navigator.clipboard.writeText(sql);
    ElMessage.success(t('audit.query.copySuccess'));
  } catch {
    ElMessage.error(t('audit.query.copyFailed'));
  }
}

// ── Data fetching ──

async function loadOnlineSessions() {
  onlineLoading.value = true;
  onlineError.value = '';
  try {
    const res = await apiClient.getOnlineSessions({
      page: onlinePage.value,
      page_size: onlinePageSize.value,
      q: onlineKeyword.value || undefined,
      resource_type: onlineResourceType.value || undefined,
      resource_id: onlineResourceID.value || undefined,
    });
    onlineSessions.value = res.items ?? [];
    onlineTotal.value = res.total ?? 0;
  } catch (err) {
    onlineSessions.value = [];
    onlineError.value = err instanceof Error ? err.message : t('audit.error.loadOnline');
  } finally {
    onlineLoading.value = false;
  }
}

function loginOutcomeTag(outcome: unknown): 'success' | 'danger' | 'warning' | 'info' {
  switch (String(outcome ?? '').toLowerCase()) {
    case 'success': return 'success';
    case 'blocked': return 'warning';
    default: return 'danger';
  }
}

function loginOutcomeLabel(outcome: unknown): string {
  switch (String(outcome ?? '').toLowerCase()) {
    case 'success': return t('audit.result.success');
    case 'blocked': return t('audit.result.blocked');
    default: return t('audit.result.failure');
  }
}

function operationActionLabel(action: unknown): string {
  const key = String(action ?? '').toLowerCase();
  const labels: Record<string, string> = {
    create: t('audit.action.create'),
    update: t('audit.action.update'),
    delete: t('audit.action.delete'),
    revoke: t('audit.action.revoke'),
    test: t('audit.action.test'),
  };
  return labels[key] || String(action || '-');
}

function operationDetail(row: OperationAuditRecord): Record<string, unknown> {
  if (!row.detail) return {};
  try {
    const value = JSON.parse(row.detail) as unknown;
    return value && typeof value === 'object' ? value as Record<string, unknown> : {};
  } catch {
    return {};
  }
}

function operationResultLabel(row: OperationAuditRecord): string {
  const result = String(operationDetail(row).result || '');
  return result === 'success' ? t('audit.result.success') : t('audit.result.failure');
}

function operationResultTag(row: OperationAuditRecord): 'success' | 'danger' {
  return String(operationDetail(row).result || '') === 'success' ? 'success' : 'danger';
}

async function loadLoginAuditLogs() {
  loginAuditLoading.value = true;
  loginAuditError.value = '';
  try {
    const res = await apiClient.getLoginAuditLogs({
      page: loginAuditPage.value,
      page_size: loginAuditPageSize.value,
      q: loginAuditKeyword.value || undefined,
      outcome: loginAuditOutcome.value || undefined,
    });
    loginAuditLogs.value = res.items ?? [];
    loginAuditTotal.value = res.total ?? 0;
  } catch (err) {
    loginAuditLogs.value = [];
    loginAuditError.value = err instanceof Error ? err.message : t('audit.error.loadLogins');
  } finally {
    loginAuditLoading.value = false;
  }
}

async function loadOperationAuditLogs() {
  operationAuditLoading.value = true;
  operationAuditError.value = '';
  try {
    const res = await apiClient.getOperationAuditLogs({
      page: operationAuditPage.value,
      page_size: operationAuditPageSize.value,
      q: operationAuditKeyword.value || undefined,
      action: operationAuditAction.value || undefined,
    });
    operationAuditLogs.value = res.items ?? [];
    operationAuditTotal.value = res.total ?? 0;
  } catch (err) {
    operationAuditLogs.value = [];
    operationAuditError.value = err instanceof Error ? err.message : t('audit.error.loadOperations');
  } finally {
    operationAuditLoading.value = false;
  }
}

async function loadSessions() {
  sessionsLoading.value = true;
  sessionError.value = '';

  try {
    const res = await apiClient.getSessions({
      page: sessionPage.value,
      page_size: sessionPageSize.value,
      q: sessionKeyword.value || undefined,
    });
    sessions.value = res.items ?? [];
    sessionTotal.value = res.total ?? 0;
  } catch (err) {
    sessionError.value = err instanceof Error ? err.message : t('sessions.loadError');
  } finally {
    sessionsLoading.value = false;
  }
}

async function loadDBConnections() {
  dbLoading.value = true;
  dbError.value = '';

  try {
    const res = await apiClient.getDBConnections({
      page: dbPage.value,
      page_size: dbPageSize.value,
      q: dbKeyword.value || undefined,
    });
    dbConnections.value = res.items ?? [];
    dbTotal.value = res.total ?? 0;
  } catch (err) {
    dbConnections.value = [];
    dbError.value = err instanceof Error ? err.message : t('audit.error.loadDBConnections');
  } finally {
    dbLoading.value = false;
  }
}

function onLogSearch(q: string) {
  logKeyword.value = q;
  if (detailKind.value !== 'queries' || !dbQueryConnectionID.value) {
    logPage.value = 1;
    return;
  }
  if (logPage.value !== 1) {
    logPage.value = 1;
  } else {
    void loadDBQueryPage(dbQueryConnectionID.value, false);
  }
}

function onOnlineSearch(q: string) {
  onlineKeyword.value = q;
  onlinePage.value = 1;
  void loadOnlineSessions();
}

function onSessionSearch(q: string) {
  sessionKeyword.value = q;
  sessionPage.value = 1;
  loadSessions();
}

function onRDPSearch(q: string) {
  rdpKeyword.value = q;
  rdpPage.value = 1;
  void loadRDPSessions();
}

function onLoginAuditSearch(q: string) {
  loginAuditKeyword.value = q;
  loginAuditPage.value = 1;
  void loadLoginAuditLogs();
}

function onOperationAuditSearch(q: string) {
  operationAuditKeyword.value = q;
  operationAuditPage.value = 1;
  void loadOperationAuditLogs();
}

function onDBSearch(q: string) {
  dbKeyword.value = q;
  dbPage.value = 1;
  loadDBConnections();
}

// ── Session artifacts ──

async function loadSessionArtifact(session: SessionRecord, kind: Exclude<DetailKind, '' | 'queries'>) {
  const id = sessionId(session);

  if (!id) {
    ElMessage.error(t('audit.error.missingSession'));
    return;
  }

  const requestVersion = ++detailRequestVersion;
  dbQueryConnectionID.value = '';
  dbQueryTotal.value = 0;
  detailLoading.value = true;
  detailError.value = '';

  try {
    let title = '';
    let data: unknown;
    let startReplay = false;
    if (kind === 'meta') {
      title = `${t('audit.scope.ssh')} ${id}`;
      data = await apiClient.getSessionMeta(id);
    } else if (kind === 'replay') {
      title = `${t('audit.action.replay')} ${id}`;
      data = parseReplayCast(await apiClient.getSessionReplay(id));
      startReplay = true;
    } else if (kind === 'commands') {
      title = `${t('audit.action.commands')} ${id}`;
      data = await apiClient.getSessionCommands(id);
    } else if (kind === 'files') {
      title = `${t('audit.action.files')} ${id}`;
      data = await apiClient.getSessionFiles(id);
    } else {
      title = `${t('audit.action.summary')} ${id}`;
      data = await apiClient.getSessionFileSummary(id);
    }
    if (requestVersion !== detailRequestVersion) return;

    setDetail(title, kind, data);
    if (startReplay) {
      await nextTick();
      if (requestVersion !== detailRequestVersion) return;
      playReplay();
    }
  } catch (err) {
    if (requestVersion === detailRequestVersion) {
      detailError.value = err instanceof Error ? err.message : t('audit.error.loadArtifact');
    }
  } finally {
    if (requestVersion === detailRequestVersion) {
      detailLoading.value = false;
    }
  }
}

function formatFileAction(action: string): string {
  const map: Record<string, string> = {
    realpath: '解析路径',
    list: '列目录',
    open_read: '打开读取',
    open_write: '打开写入',
    read: '读取',
    write: '写入',
    close: '关闭',
    remove: '删除',
    rename: '重命名',
    mkdir: '创建目录',
    rmdir: '删除目录',
    stat: '查看属性',
    setstat: '设属性',
    fstat: '文件属性',
    fsetstat: '设文件属性',
    opendir: '打开目录',
    readdir: '读目录',
    readlink: '读链接',
    symlink: '创建链接',
  };
  return map[action] || action;
}

function isSFTP(row: SessionRecord): boolean {
  if (row.protocol_subtype === 'sftp') return true;
  if (!row.protocol_subtype && !hasReplay(row)) return true;
  return false;
}

function loadSessionLog(session: SessionRecord) {
  if (isSFTP(session)) {
    void loadSessionArtifact(session, 'files');
  } else {
    void loadSessionArtifact(session, 'commands');
  }
}

// ── Replay ──

function parseReplayCast(raw: string): ReplayData {
  const lines = raw.split(/\r?\n/).filter((line) => line.trim().length > 0);
  const header = parseReplayHeader(lines[0]);
  const frames: ReplayFrame[] = [];

  for (const line of lines.slice(1)) {
    try {
      const row = JSON.parse(line) as unknown[];
      const time = typeof row[0] === 'number' ? row[0] : Number(row[0]);
      const stream = typeof row[1] === 'string' ? row[1] : '';
      const data = typeof row[2] === 'string' ? row[2] : '';
      if (Number.isFinite(time) && data) {
        frames.push({ time: Math.max(0, time), stream, data });
      }
    } catch {
      // Skip malformed rows so one bad line does not break playback.
    }
  }

  frames.sort((a, b) => a.time - b.time);
  return { header, frames, raw };
}

function parseReplayHeader(line: string | undefined): Record<string, unknown> {
  if (!line) {
    return {};
  }
  try {
    const value = JSON.parse(line);
    return isRecord(value) ? value : {};
  } catch {
    return {};
  }
}

function playReplay() {
  const frames = replayFrames.value;
  stopReplay();
  const terminal = ensureReplayTerminal();
  terminal?.reset();
  replayProgress.value = 0;
  replaySeekPercent.value = 0;
  replayCurrentTime.value = 0;
  replayRenderedOutput.value = false;
  replayStartOffset = replayFirstOutputTime.value > 0 ? Math.max(0, replayFirstOutputTime.value - 0.2) : 0;
  replayFrameIndex = Math.max(
    0,
    frames.findIndex((frame) => frame.time >= replayStartOffset)
  );

  if (!frames.length || !replayOutputFrames.value.length) {
    return;
  }

  replayPlaying.value = true;
  replayStartedAt = performance.now();
  tickReplay();
}

function stopReplay() {
  if (replayTimer !== undefined) {
    window.clearTimeout(replayTimer);
    replayTimer = undefined;
  }
  cancelAutoScroll();
  replayPlaying.value = false;
}

let scrollRafId: number | undefined;

function autoScrollTerminal() {
  // Cancel any pending scroll — only the latest position matters
  if (scrollRafId !== undefined) {
    cancelAnimationFrame(scrollRafId);
  }
  scrollRafId = requestAnimationFrame(() => {
    const host = replayTerminalHostRef.value;
    if (!host) return;
    const viewport = host.querySelector('.xterm-viewport') as HTMLElement | null;
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
    // xterm may batch DOM updates — confirm on the next frame
    scrollRafId = requestAnimationFrame(() => {
      scrollRafId = undefined;
      viewport.scrollTop = viewport.scrollHeight;
    });
  });
}

function cancelAutoScroll() {
  if (scrollRafId !== undefined) {
    cancelAnimationFrame(scrollRafId);
    scrollRafId = undefined;
  }
}

function seekReplay(percent: number) {
  const frames = replayFrames.value;
  const duration = Math.max(replayDuration.value, 0.1);
  const targetTime = (percent / 100) * duration;

  // Find target frame index
  const targetIndex = frames.findIndex((f) => f.time >= targetTime);
  const idx = targetIndex >= 0 ? targetIndex : frames.length;

  const wasPlaying = replayPlaying.value;
  stopReplay();

  // Reset terminal and fast-forward
  const terminal = ensureReplayTerminal();
  terminal?.reset();
  replayRenderedOutput.value = false;

  for (let i = 0; i < idx; i++) {
    if (frames[i].stream === 'o') {
      terminal?.write(frames[i].data);
      replayRenderedOutput.value = true;
    }
  }

  autoScrollTerminal();

  // Update state
  replayFrameIndex = idx;
  replayProgress.value = percent;
  replaySeekPercent.value = percent;
  replayCurrentTime.value = targetTime;
  replayStartOffset = targetTime;

  if (wasPlaying) {
    replayPlaying.value = true;
    replayStartedAt = performance.now();
    tickReplay();
  }
}

function tickReplay() {
  if (!replayPlaying.value) {
    return;
  }

  const frames = replayFrames.value;
  const speed = playbackSpeed.value;
  const elapsed = ((performance.now() - replayStartedAt) / 1000) * speed + replayStartOffset;
  while (replayFrameIndex < frames.length && frames[replayFrameIndex].time <= elapsed) {
    appendReplayOutput(frames[replayFrameIndex]);
    replayFrameIndex++;
  }

  // Keep viewport at bottom so new output is always visible
  autoScrollTerminal();

  const duration = Math.max(replayDuration.value, 0.1);
  const pct =
    replayFrameIndex >= frames.length ? 100 : Math.min(99, Math.round((elapsed / duration) * 100));
  replayProgress.value = pct;
  replaySeekPercent.value = pct;
  replayCurrentTime.value = Math.min(elapsed, duration);

  if (replayFrameIndex >= frames.length) {
    replayPlaying.value = false;
    replayTimer = undefined;
    return;
  }

  replayTimer = window.setTimeout(tickReplay, 33);
}

function appendReplayOutput(frame: ReplayFrame) {
  if (frame.stream !== 'o') {
    return;
  }
  const terminal = ensureReplayTerminal();
  if (!terminal) {
    return;
  }
  replayRenderedOutput.value = true;
  terminal.write(frame.data);
}

function ensureReplayTerminal(): Terminal | undefined {
  const host = replayTerminalHostRef.value;
  if (!host) {
    return undefined;
  }

  if (!replayTerminal) {
    replayTerminal = new Terminal({
      convertEol: false,
      cursorBlink: false,
      disableStdin: true,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.2,
      scrollback: 5000,
      theme: {
        background: '#0b1220',
        foreground: '#d0d5dd',
        cursor: '#98a2b3',
        selectionBackground: '#344054'
      }
    });
    replayFitAddon = new FitAddon();
    replayTerminal.loadAddon(replayFitAddon);
    replayTerminal.open(host);

    // 自适应容器宽度，让终端内容不截断
    replayFitAddon.fit();

    // 容器大小变化时重新适配
    replayResizeObserver = new ResizeObserver(() => {
      if (replayFitAddon && replayTerminal) {
        replayFitAddon.fit();
      }
    });
    replayResizeObserver.observe(host);
  }

  return replayTerminal;
}

function resetReplayTerminal() {
  replayTerminal?.reset();
}

function destroyReplayTerminal() {
  replayResizeObserver?.disconnect();
  replayResizeObserver = undefined;
  replayFitAddon?.dispose();
  replayFitAddon = undefined;
  replayTerminal?.dispose();
  replayTerminal = undefined;
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

// ── DB artifacts ──

function onlineProtocol(row: OnlineSessionRecord): string {
  if (row.resource_type === 'database_instance') return formatDatabaseProtocol(row.protocol);
  if (String(row.protocol).toLowerCase() === 'rdp') return 'RDP';
  if (row.protocol_subtype === 'sftp') return 'SFTP';
  if (row.protocol_subtype === 'web-terminal') return 'Web';
  return 'SSH';
}

function onlineProtocolTag(row: OnlineSessionRecord): 'primary' | 'success' | 'warning' | 'info' | 'danger' | '' {
  if (row.resource_type === 'database_instance') return databaseProtocolTag(row.protocol);
  if (String(row.protocol).toLowerCase() === 'rdp') return 'primary';
  if (row.protocol_subtype === 'sftp') return 'warning';
  if (row.protocol_subtype === 'web-terminal') return 'success';
  return 'info';
}

function loadOnlineReplay(row: OnlineSessionRecord) {
  if (String(row.protocol).toLowerCase() === 'rdp') {
    void openRDPReplay({
      id: row.audit_session_id,
      protocol: 'rdp',
      target_name: row.instance,
      account_name: row.account,
      username: row.operator,
      started_at: row.started_at,
      has_replay: row.has_replay,
    });
    return;
  }
  void loadSessionArtifact({ id: row.audit_session_id, replay_dir: row.has_replay ? 'online' : '' }, 'replay');
}

function loadOnlineLog(row: OnlineSessionRecord) {
  if (row.resource_type === 'database_instance') {
    void loadDBArtifact({ id: row.audit_session_id }, 'queries');
    return;
  }
  if (String(row.protocol).toLowerCase() === 'rdp') {
    ElMessage.info('RDP 通道审计已记录元数据，不展示剪贴板或文件内容');
    return;
  }
  const kind = row.protocol_subtype === 'sftp' ? 'files' : 'commands';
  void loadSessionArtifact({
    id: row.audit_session_id,
    protocol: row.protocol,
    protocol_subtype: row.protocol_subtype,
    replay_dir: row.has_replay ? 'online' : '',
  }, kind);
}

async function disconnectOnlineSession(row: OnlineSessionRecord) {
  try {
    await ElMessageBox.confirm(
      t('audit.confirm.disconnectMessage'),
      t('audit.confirm.disconnectTitle'),
      { type: 'warning' },
    );
  } catch {
    return;
  }

  disconnectingSessionID.value = row.id;
  try {
    await apiClient.disconnectOnlineSession(row.id);
    ElMessage.success(t('audit.success.disconnected'));
    await loadOnlineSessions();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('audit.error.disconnect'));
  } finally {
    disconnectingSessionID.value = '';
  }
}

function showUserSessionDetail(sessionID: string) {
  sessionDetailId.value = sessionID;
  sessionDetailVisible.value = true;
}

async function loadDBArtifact(connection: DBConnectionRecord, kind: 'meta' | 'queries') {
  const id = String(connection.id ?? '');

  if (!id) {
    ElMessage.error(t('audit.error.missingConnection'));
    return;
  }

  if (kind === 'queries') {
    detailRequestVersion++;
    dbQueryConnectionID.value = '';
    logPage.value = 1;
    logKeyword.value = '';
    await loadDBQueryPage(id, true);
    return;
  }

  const requestVersion = ++detailRequestVersion;
  dbQueryConnectionID.value = '';
  dbQueryTotal.value = 0;
  detailLoading.value = true;
  detailError.value = '';

  try {
    const response = await apiClient.getDBConnectionMeta(id);
    if (requestVersion !== detailRequestVersion) return;
    setDetail(`${t('audit.scope.db')} ${id}`, kind, response);
  } catch (err) {
    if (requestVersion === detailRequestVersion) {
      detailError.value = err instanceof Error ? err.message : t('audit.error.loadArtifact');
    }
  } finally {
    if (requestVersion === detailRequestVersion) {
      detailLoading.value = false;
    }
  }
}

// ── Lifecycle & watchers ──

async function loadDBQueryPage(id: string, openDrawer: boolean) {
  const requestVersion = ++detailRequestVersion;
  detailLoading.value = true;
  detailError.value = '';

  try {
    const response = await apiClient.getDBConnectionQueries(id, {
      page: logPage.value,
      page_size: logPageSize.value,
      q: logKeyword.value.trim() || undefined,
    });
    if (requestVersion !== detailRequestVersion) return;

    dbQueryConnectionID.value = id;
    dbQueryTotal.value = response.total ?? 0;
    if (openDrawer) {
      setDetail(`${t('audit.action.queries')} ${id}`, 'queries', response);
    } else {
      detailData.value = response;
    }
  } catch (err) {
    if (requestVersion === detailRequestVersion) {
      detailError.value = err instanceof Error ? err.message : t('audit.error.loadArtifact');
    }
  } finally {
    if (requestVersion === detailRequestVersion) {
      detailLoading.value = false;
    }
  }
}

function applyRouteAuditFilter() {
  const scope = permittedAuditScope(route.query.scope);
  const keyword = routeQueryValue(route.query.q);
  auditScope.value = scope;
  if (scope === 'logins') {
    loginAuditKeyword.value = keyword;
    if (loginAuditPage.value === 1) void loadLoginAuditLogs();
    else loginAuditPage.value = 1;
    return;
  }
  if (scope === 'operations') {
    operationAuditKeyword.value = keyword;
    if (operationAuditPage.value === 1) void loadOperationAuditLogs();
    else operationAuditPage.value = 1;
    return;
  }
  if (scope === 'online') {
    onlineKeyword.value = keyword;
    onlineResourceType.value = routeQueryValue(route.query.resource_type);
    onlineResourceID.value = routeQueryValue(route.query.resource_id);
    if (onlinePage.value === 1) void loadOnlineSessions();
    else onlinePage.value = 1;
    return;
  }
  if (scope === 'rdp') {
    rdpKeyword.value = keyword;
    if (rdpPage.value === 1) void loadRDPSessions();
    else rdpPage.value = 1;
    return;
  }
  onlineKeyword.value = '';
  onlineResourceType.value = '';
  onlineResourceID.value = '';
  if (scope === 'ssh') {
    sessionKeyword.value = keyword;
    if (sessionPage.value === 1) void loadSessions();
    else sessionPage.value = 1;
  } else {
    dbKeyword.value = keyword;
    if (dbPage.value === 1) void loadDBConnections();
    else dbPage.value = 1;
  }
}

onMounted(() => {
  if (permission.canDo('audit:view')) {
    void loadSessions();
    void loadLoginAuditLogs();
    void loadOperationAuditLogs();
  }
  if (permission.canDo('db:audit:view')) void loadDBConnections();
  if (canViewRDPRecordings.value) void loadRDPSessions();
  if (permission.canDo('session:view')) {
    void loadOnlineSessions();
    onlineRefreshTimer = window.setInterval(() => {
      if (auditScope.value === 'online' && !onlineLoading.value) void loadOnlineSessions();
    }, 5000);
  }
});

watch(
  () => route.fullPath,
  () => {
    if (route.name === 'audit') applyRouteAuditFilter();
  },
);

watch(isReplay, async (value) => {
  if (value) {
    await nextTick();
    ensureReplayTerminal();
    resetReplayTerminal();
  }
});

// Watch pagination changes for main lists
watch([sessionPage, sessionPageSize], () => {
  if (auditScope.value === 'ssh') loadSessions();
});
watch([rdpPage, rdpPageSize], () => {
  if (auditScope.value === 'rdp') void loadRDPSessions();
});
watch([loginAuditPage, loginAuditPageSize], () => {
  if (auditScope.value === 'logins') loadLoginAuditLogs();
});
watch([operationAuditPage, operationAuditPageSize], () => {
  if (auditScope.value === 'operations') loadOperationAuditLogs();
});
watch([dbPage, dbPageSize], () => {
  if (auditScope.value === 'db') loadDBConnections();
});
watch([onlinePage, onlinePageSize], () => {
  if (auditScope.value === 'online') loadOnlineSessions();
});
watch([logPage, logPageSize], ([page, pageSize], [previousPage, previousPageSize]) => {
  if (detailKind.value !== 'queries' || !dbQueryConnectionID.value) return;
  if (pageSize !== previousPageSize && page !== 1) {
    logPage.value = 1;
    return;
  }
  if (page !== previousPage || pageSize !== previousPageSize) {
    void loadDBQueryPage(dbQueryConnectionID.value, false);
  }
});

onBeforeUnmount(() => {
  if (onlineRefreshTimer !== undefined) window.clearInterval(onlineRefreshTimer);
  stopReplay();
  destroyReplayTerminal();
  destroyRDPReplay();
});
</script>

<style scoped>
/* 保留 tab header 默认间距 (page-tabs 会清零) */
.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 15px;
  padding: 0;
}

.audit-view {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.placeholder-panel :deep(.el-segmented) {
  max-width: 100%;
}

:deep(.col-time) {
  white-space: nowrap;
}

/* Make drawer body a flex column so terminal can fill remaining space */
:deep(.el-drawer__body) {
  display: flex;
  flex-direction: column;
  padding: 12px 16px;
}

.drawer-content {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

.rdp-replay-actions,
.rdp-replay-timeline {
  display: flex;
  align-items: center;
  gap: 8px;
}

.rdp-replay-panel {
  display: flex;
  height: min(72dvh, 720px);
  min-height: 0;
  flex-direction: column;
  gap: 12px;
}

.rdp-replay-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
}

.rdp-replay-timeline {
  flex: 1 1 320px;
  min-width: 0;
}

.rdp-replay-timeline :deep(.el-slider) {
  flex: 1;
  min-width: 120px;
}

.rdp-replay-display {
  display: grid;
  flex: 1;
  min-height: clamp(220px, 52dvh, 620px);
  place-items: center;
  overflow: hidden;
  border-radius: 10px;
  background: #090f1d;
}

.rdp-replay-display :deep(.rdp-recording-canvas) {
  position: relative;
  transform-origin: center center;
}

.replay-panel {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex: 1;
  min-height: 0;
}

.replay-controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.replay-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  color: #667085;
  font-size: 12px;
}

.replay-meta :deep(.el-slider) {
  flex: 1;
  min-width: 80px;
}

.replay-meta-secondary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  color: #667085;
  font-size: 11px;
}

.replay-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.replay-terminal-shell {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 200px;
  overflow: auto;
  background: #0b1220;
  border-radius: 8px;
}

.replay-terminal {
  flex: 1;
  min-width: fit-content;
  /* 使用 flex 布局让终端自然填充高度，min-width:fit-content 允许 xterm 超宽时触发父级横向滚动 */
}

/* let xterm control its own dimensions based on cols/rows */

.replay-terminal :deep(.xterm-viewport) {
  overflow-y: auto !important;
  scrollbar-width: thin;
  scrollbar-color: #475467 transparent;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar) {
  width: 6px;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar-track) {
  background: transparent;
}

.replay-terminal :deep(.xterm-viewport::-webkit-scrollbar-thumb) {
  background: #475467;
  border-radius: 3px;
}

/* 不限制 xterm-screen 宽度，让它按 cols 自然渲染，终端壳层处理横向滚动 */

.replay-terminal-empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 16px;
  color: #98a2b3;
  font-size: 13px;
  text-align: center;
  pointer-events: none;
}

.query-sql-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.query-sql-cell__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.query-sql-cell :deep(.el-tag) {
  flex-shrink: 0;
}

.query-sql-cell__copy {
  flex-shrink: 0;
}

@media (max-width: 620px) {
  .rdp-replay-actions,
  .rdp-replay-timeline {
    width: 100%;
  }

  .rdp-replay-actions :deep(.el-button) {
    flex: 1;
    margin: 0;
  }

  .replay-controls {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
