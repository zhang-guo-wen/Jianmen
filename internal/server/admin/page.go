package admin

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jianmen Admin</title>
  <style>
    :root {
      --bg: #f6f8fb;
      --panel: #ffffff;
      --line: #d9e0ea;
      --text: #172033;
      --muted: #65748b;
      --accent: #1f7a62;
      --danger: #b42318;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font: 14px/1.45 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 14px 24px;
      background: #111827;
      color: #fff;
    }
    h1 { margin: 0; font-size: 18px; font-weight: 650; }
    main { max-width: 1280px; margin: 0 auto; padding: 22px; }
    .toolbar, .actions, .form-grid { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
    input {
      height: 34px;
      min-width: 180px;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 0 10px;
      font: inherit;
    }
    input.small { min-width: 90px; width: 90px; }
    button {
      height: 34px;
      border: 1px solid #176b55;
      background: var(--accent);
      color: #fff;
      border-radius: 6px;
      padding: 0 12px;
      font: inherit;
      cursor: pointer;
    }
    button.secondary { background: #fff; color: var(--text); border-color: var(--line); }
    .grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 16px;
      margin-top: 16px;
    }
    .full { grid-column: 1 / -1; }
    section {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      overflow: hidden;
    }
    section h2 {
      margin: 0;
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
      font-size: 15px;
    }
    .body { padding: 12px 14px; }
    table { width: 100%; border-collapse: collapse; }
    th, td { padding: 8px 8px; border-bottom: 1px solid #edf1f6; text-align: left; vertical-align: top; }
    th { color: var(--muted); font-weight: 600; white-space: nowrap; }
    tr:last-child td { border-bottom: 0; }
    code, pre {
      font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
      font-size: 12px;
    }
    pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
      max-height: 360px;
      overflow: auto;
      background: #0f172a;
      color: #dbeafe;
      padding: 12px;
      border-radius: 6px;
    }
    .hint { margin-top: 10px; color: var(--muted); }
    .muted { color: var(--muted); }
    .error { color: var(--danger); }
    @media (max-width: 900px) {
      .grid { grid-template-columns: 1fr; }
      header { align-items: stretch; flex-direction: column; }
      input { min-width: 0; width: 100%; }
      input.small { width: 100%; }
    }
  </style>
</head>
<body>
  <header>
    <h1>Jianmen Admin</h1>
    <div class="toolbar">
      <input id="token" type="password" placeholder="Bearer token">
      <button id="saveToken">Save</button>
      <button class="secondary" id="refresh">Refresh</button>
    </div>
  </header>
  <main>
    <div id="status" class="muted">Loading</div>
    <div class="grid">
      <section>
        <h2>Users</h2>
        <div class="body" id="users"></div>
      </section>
      <section>
        <h2>Targets</h2>
        <div class="body">
          <div id="targets"></div>
          <div class="hint">SSH default target: <code>ssh -p 47102 admin@127.0.0.1</code></div>
          <div class="hint">SSH selected target: <code>ssh -p 47102 admin+TARGET_ID@127.0.0.1</code></div>
        </div>
      </section>
      <section class="full">
        <h2>Add Target</h2>
        <div class="body">
          <div class="form-grid">
            <input id="targetId" placeholder="target id, e.g. web01">
            <input id="targetHost" placeholder="host, e.g. 10.0.0.12">
            <input id="targetPort" class="small" placeholder="22" value="22">
            <input id="targetUser" placeholder="ssh username">
            <input id="targetPassword" type="password" placeholder="ssh password">
            <input id="targetFingerprint" placeholder="host key fingerprint, SHA256:...">
            <button id="addTarget">Add</button>
          </div>
          <div class="hint">Saved to <code>data/targets.json</code>. The target can be used immediately.</div>
        </div>
      </section>
      <section class="full">
        <h2>SSH Sessions</h2>
        <div class="body" id="sessions"></div>
      </section>
      <section class="full">
        <h2>Database Connections</h2>
        <div class="body" id="dbConnections"></div>
      </section>
      <section class="full">
        <h2>Detail</h2>
        <div class="body"><pre id="detail">Select a record to inspect audit details.</pre></div>
      </section>
    </div>
  </main>
  <script>
    const tokenInput = document.querySelector("#token");
    const statusEl = document.querySelector("#status");
    const detailEl = document.querySelector("#detail");
    tokenInput.value = localStorage.getItem("jianmen_token") || "";

    document.querySelector("#saveToken").onclick = () => {
      localStorage.setItem("jianmen_token", tokenInput.value);
      refresh();
    };
    document.querySelector("#refresh").onclick = refresh;
    document.querySelector("#addTarget").onclick = addTarget;

    function authHeaders(extra = {}) {
      return { "Authorization": "Bearer " + tokenInput.value, ...extra };
    }

    async function api(path, options = {}) {
      const res = await fetch(path, {
        ...options,
        headers: authHeaders(options.headers || {})
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || res.statusText);
      return data;
    }

    function escapeHtml(value) {
      return String(value ?? "").replace(/[&<>"']/g, ch => ({
        "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;"
      }[ch]));
    }

    function table(rows, columns) {
      if (!rows.length) return '<span class="muted">No data</span>';
      return '<table><thead><tr>' + columns.map(c => '<th>' + escapeHtml(c.label) + '</th>').join('') +
        '</tr></thead><tbody>' + rows.map(row => '<tr>' + columns.map(c => '<td>' +
        (c.render ? c.render(row) : escapeHtml(row[c.key])) + '</td>').join('') + '</tr>').join('') +
        '</tbody></table>';
    }

    function showDetail(value) {
      detailEl.textContent = JSON.stringify(value, null, 2);
    }

    async function addTarget() {
      try {
        const target = {
          id: document.querySelector("#targetId").value.trim(),
          name: document.querySelector("#targetId").value.trim(),
          host: document.querySelector("#targetHost").value.trim(),
          port: Number(document.querySelector("#targetPort").value || "22"),
          username: document.querySelector("#targetUser").value.trim(),
          password: document.querySelector("#targetPassword").value,
          insecure_ignore_host_key: false,
          host_key_fingerprint: document.querySelector("#targetFingerprint").value.trim()
        };
        if (!target.host_key_fingerprint) {
          throw new Error("host key fingerprint is required");
        }
        const created = await api("/api/targets", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(target)
        });
        showDetail(created);
        statusEl.textContent = "Target added: " + created.id;
        statusEl.className = "muted";
        await refresh();
      } catch (err) {
        statusEl.textContent = err.message;
        statusEl.className = "error";
      }
    }

    async function refresh() {
      statusEl.textContent = "Loading";
      statusEl.className = "muted";
      localStorage.setItem("jianmen_token", tokenInput.value);
      try {
        const [health, users, targets, sessions, dbConnections] = await Promise.all([
          api("/api/health"),
          api("/api/users"),
          api("/api/targets"),
          api("/api/sessions"),
          api("/api/db/connections")
        ]);
        statusEl.textContent = "OK " + health.time;
        document.querySelector("#users").innerHTML = table(users, [
          { key: "id", label: "ID" },
          { key: "username", label: "Username" }
        ]);
        document.querySelector("#targets").innerHTML = table(targets, [
          { key: "id", label: "ID" },
          { key: "name", label: "Name" },
          { key: "host", label: "Host" },
          { key: "port", label: "Port" },
          { key: "username", label: "Account" }
        ]);
        document.querySelector("#sessions").innerHTML = table(sessions, [
          { key: "started_at", label: "Started" },
          { key: "user", label: "User" },
          { key: "target", label: "Target" },
          { key: "client_ip", label: "Client" },
          { key: "id", label: "Actions", render: row => '<div class="actions">' +
            '<button class="secondary" onclick="loadSession(\'' + row.id + '\', \'meta\')">Meta</button>' +
            '<button class="secondary" onclick="loadSession(\'' + row.id + '\', \'commands\')">Commands</button>' +
            '<button class="secondary" onclick="loadSession(\'' + row.id + '\', \'files\')">Files</button>' +
            '<button class="secondary" onclick="loadSession(\'' + row.id + '\', \'file-summary\')">Summary</button>' +
            '</div>' }
        ]);
        document.querySelector("#dbConnections").innerHTML = table(dbConnections, [
          { key: "started_at", label: "Started" },
          { key: "name", label: "Name" },
          { key: "protocol", label: "Protocol" },
          { key: "client_addr", label: "Client" },
          { key: "upstream_addr", label: "Upstream" },
          { key: "id", label: "Actions", render: row => '<div class="actions">' +
            '<button class="secondary" onclick="loadDB(\'' + row.id + '\', \'meta\')">Meta</button>' +
            '<button class="secondary" onclick="loadDB(\'' + row.id + '\', \'queries\')">SQL</button>' +
            '</div>' }
        ]);
      } catch (err) {
        statusEl.textContent = err.message;
        statusEl.className = "error";
      }
    }

    async function loadSession(id, artifact) {
      showDetail(await api("/api/sessions/" + id + "/" + artifact));
    }
    async function loadDB(id, artifact) {
      showDetail(await api("/api/db/connections/" + id + "/" + artifact));
    }
    window.loadSession = loadSession;
    window.loadDB = loadDB;
    refresh();
  </script>
</body>
</html>`
