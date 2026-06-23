# 早期实施分解（历史参考）

> 当前进展以 [current-progress.md](current-progress.md) 为准；后续开发计划以 [phase2-roadmap.md](phase2-roadmap.md) 为准。本文保留为早期阶段拆分、并行开发策略和技术清单参考，不再作为状态或路线图的权威来源。

This project is moving from a prototype bastion service to a managed bastion
platform. The implementation order is intentionally conservative: stabilize the
SSH/SFTP protocol path first, then split the web UI, then move metadata into a
database, then enforce RBAC.

## Parallel Development Strategy

Current repository state:

- `jianmen/` is still an untracked directory inside the old Teleport source
  tree.
- The parent repository has unrelated local changes.
- Because there is no clean committed baseline for `jianmen/`, real
  `git worktree` branches should not be used yet.

Current parallel mode:

- Use sub-agents for independent code slices with disjoint write sets.
- Main thread owns cross-cutting integration and tests.
- Workers only edit their assigned package.

Recommended worktree setup after a baseline commit exists:

```powershell
git -C jianmen init
git -C jianmen add .
git -C jianmen commit -m "baseline Jianmen"
git -C jianmen worktree add ..\jianmen-ssh feature/ssh-hardening
git -C jianmen worktree add ..\jianmen-web feature/vue-admin
git -C jianmen worktree add ..\jianmen-storage feature/storage-rbac
```

Do not create these worktrees until the project baseline is committed and the
generated runtime files under `bin/`, `data/`, and `logs/` are excluded.

## Phase 1: SSH/SFTP Foundation

Goal: common SSH clients can connect through the bastion reliably.

Client to bastion:

- Password authentication.
- Public key authentication from local client keys.
- `authorized_keys` style public key configuration.
- Keyboard-interactive password compatibility.
- Ed25519 and RSA host keys.

Bastion to target:

- Target password authentication.
- Target private key path.
- Target private key PEM.
- Target private key passphrase.
- Keyboard-interactive target password compatibility.

Protocol behavior:

- `shell`
- `exec`
- `sftp`
- `pty-req`
- `window-change`
- `env`
- `signal`
- stdout and stderr forwarding
- `exit-status`, `exit-signal`, and `eow` forwarding

Status: mostly implemented.

Implemented in this pass:

- Client public key authentication.
- User `authorized_keys` path support.
- Keyboard-interactive login to bastion.
- Keyboard-interactive password fallback to target hosts.
- RSA host key fallback beside the existing Ed25519 key.
- Target channel request forwarding for exec compatibility.
- Tests for public key auth, host key fallback, and exec status forwarding.

Remaining:

- Manual compatibility testing with OpenSSH, Xshell, Xftp, WinSCP, and IDEA.
- Idle timeout and keepalive policy.
- Strict target host key verification mode.
- Better audit metadata for auth method and client key fingerprint.

## Phase 2: Vue Admin Console

Goal: replace the embedded HTML management page with a separated frontend.

Backend:

- Keep Go as the API server.
- Move handlers under an API module.
- Return JSON only for management APIs.
- Serve the built frontend `dist` in single-binary mode as an optional
  deployment mode.

Frontend:

- Vue 3.
- Vite.
- Element Plus.
- Pinia or simple composables for state.
- Vue Router.

First pages:

- Login.
- Dashboard.
- Hosts.
- Host accounts.
- Users.
- Roles.
- Permissions.
- Online sessions.
- Historical sessions.
- Terminal replay.
- File audit.
- Database proxy audit.
- System settings.

## Phase 3: Storage Layer

Goal: support SQLite, MySQL, and PostgreSQL.

Recommended implementation:

- Add a storage package with repository interfaces.
- Use SQLite as the default development and single-node database.
- Add MySQL and PostgreSQL drivers behind the same interface.
- Keep large recording files on disk first; store metadata and indexes in DB.

Initial tables:

- users
- user_public_keys
- roles
- permissions
- role_permissions
- user_roles
- resources
- resource_groups
- resource_group_members
- hosts
- host_accounts
- credentials
- sessions
- session_commands
- session_file_events
- recordings
- temporary_accounts
- temporary_credentials
- temporary_account_grants
- audit_logs

## Phase 4: RBAC

Goal: enforce action permissions and resource permissions everywhere.

Action permissions:

- `host:create`
- `host:update`
- `host:delete`
- `host:view`
- `host_account:create`
- `host_account:update`
- `host_account:delete`
- `host_account:view`
- `session:connect`
- `session:disconnect`
- `session:view`
- `session:replay`
- `sftp:read`
- `sftp:write`
- `temporary_account:create`
- `temporary_account:update`
- `temporary_account:revoke`
- `temporary_credential:create`
- `temporary_credential:revoke`
- `audit:view`
- `recording:view`
- `recording:download`
- `user:create`
- `user:update`
- `user:delete`
- `role:create`
- `role:update`
- `permission:grant`

Resource permissions:

- host
- host group
- host account
- database proxy
- database account
- session
- recording

Connection authorization must check:

- user identity
- action permission, for example `session:connect`
- target host resource grant
- target account resource grant
- protocol permission, for example SSH shell or SFTP read/write
- time, source IP, and temporary-account constraints when present

## Phase 5: Platform Temporary Accounts

Temporary accounts are platform identities, not Linux users and not target
machine keys. They do not write anything to target hosts.

Connection flow:

```text
SSH client
  -> bastion temporary account authentication
  -> RBAC and temporary grant check
  -> bastion connects to target with stored host account credential
  -> target host remains unchanged
```

First version:

- Create temporary platform account.
- Set expiration time.
- Bind allowed host or host group.
- Bind allowed host account.
- Generate password or bind public key.
- Enforce session recording.
- Revoke account immediately.
- Reject login after expiration.

