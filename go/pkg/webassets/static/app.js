// Striatum human-operator dashboard logic.
//
// This file is served from /static/app.js (script-src 'self'), so it runs under
// the existing Content-Security-Policy. The dashboard logic used to live in an
// inline <script> in page.html, which CSP `script-src 'self'` (no nonce, no
// unsafe-inline) blocked outright — the served app.js was a 53-byte stub and the
// dashboard JS never executed (#358). Moving the logic here, reading the
// template-injected run id from a data- attribute, and wiring event handlers with
// addEventListener (instead of inline onclick/onchange, which are also CSP-blocked)
// makes the dashboard run WITHOUT weakening the CSP.

document.documentElement.dataset.striatumWeb = "go";

(function () {
  "use strict";

  // The selected run id is injected by the page template into a CSP-safe data-
  // attribute on <body> (a data attribute is inert markup, not executable script).
  const selectedRunID = document.body ? (document.body.dataset.selectedRunId || "") : "";
  let activeSSE = null;

  document.addEventListener("DOMContentLoaded", () => {
    loadSidebarRuns();

    const searchInput = document.getElementById("run-search");
    if (searchInput) {
      searchInput.addEventListener("input", (e) => {
        filterSidebarRuns(e.target.value);
      });
    }

    // Wire the former inline event handlers (CSP-blocked) via addEventListener.
    const selector = document.getElementById("console-stream-selector");
    if (selector) {
      selector.addEventListener("change", (e) => onConsoleStreamSelectorChange(e.target.value));
    }
    const clearButton = document.getElementById("console-clear-button");
    if (clearButton) {
      clearButton.addEventListener("click", clearConsole);
    }
    const rawToggle = document.getElementById("raw-json-toggle");
    if (rawToggle) {
      rawToggle.addEventListener("click", () => {
        const raw = document.getElementById("raw-json");
        if (raw) {
          raw.style.display = raw.style.display === "none" ? "block" : "none";
        }
      });
    }
    document.querySelectorAll(".js-tail-pty").forEach((button) => {
      button.addEventListener("click", () => {
        selectConsoleStream("pty", button.dataset.sessionId || "", button.dataset.ptyLabel || "");
      });
    });

    // If a run is selected, set up the initial live dialogue stream.
    if (selectedRunID) {
      selectConsoleStream("dialogue");
      loadRunConversations(selectedRunID);
    }
  });

  async function loadSidebarRuns() {
    const sidebarList = document.getElementById("runs-sidebar-list");
    if (!sidebarList) return;
    try {
      const response = await fetch("/v1/runs");
      if (!response.ok) throw new Error("Failed to load runs");
      const json = await response.json();
      const runs = json.data.runs || [];

      if (runs.length === 0) {
        sidebarList.innerHTML = `<div class="muted" style="text-align: center; padding: 12px; font-size: 0.85rem;">No runs found.</div>`;
        return;
      }

      // Save raw list on container for filtering
      sidebarList.dataset.runsJson = JSON.stringify(runs);
      renderSidebarItems(runs);
    } catch (err) {
      sidebarList.innerHTML = `<div class="muted" style="text-align: center; padding: 12px; font-size: 0.85rem; color: var(--error-color);">Error: ${err.message}</div>`;
    }
  }

  function renderSidebarItems(runs) {
    const sidebarList = document.getElementById("runs-sidebar-list");
    if (!sidebarList) return;
    sidebarList.innerHTML = "";

    runs.forEach((run) => {
      const isActive = run.run_id === selectedRunID;
      const item = document.createElement("div");
      item.className = `run-item ${isActive ? "active-run" : ""}`;
      item.onclick = () => {
        window.location.href = `/run?id=${run.run_id}`;
      };

      const workflowLine = run.workflow_name
        ? `<span class="run-workflow-label">${escapeHTML(run.workflow_name)}</span>`
        : "";
      item.innerHTML = `
        <div class="run-item-header">
          <span class="run-id-label">${run.run_id.substring(0, 12)}...</span>
          <span class="pill ${run.state === "active" ? "pill-active" : run.state === "completed" ? "pill-completed" : run.state === "paused" ? "pill-paused" : "pill-error"}">
            ${run.state === "active" ? '<span class="dot-pulsing"></span>' : ""}${run.state}
          </span>
        </div>
        ${workflowLine}
        <span class="run-branch-label">${run.branch_name || "no branch"}</span>
      `;
      sidebarList.appendChild(item);
    });
  }

  function filterSidebarRuns(query) {
    const sidebarList = document.getElementById("runs-sidebar-list");
    if (!sidebarList || !sidebarList.dataset.runsJson) return;

    const runs = JSON.parse(sidebarList.dataset.runsJson);
    const filtered = runs.filter(
      (run) =>
        run.run_id.toLowerCase().includes(query.toLowerCase()) ||
        (run.branch_name && run.branch_name.toLowerCase().includes(query.toLowerCase())) ||
        (run.workflow_name && run.workflow_name.toLowerCase().includes(query.toLowerCase()))
    );
    renderSidebarItems(filtered);
  }

  // Load active Conversations from PostgreSQL for the selected run
  async function loadRunConversations(runId) {
    const container = document.getElementById("conversations-table-container");
    if (!container) return;
    try {
      const response = await fetch(`/v1/runs/${runId}/conversations`);
      if (!response.ok) throw new Error("Failed to load conversations");
      const json = await response.json();
      const items = json.data.items || [];

      if (items.length === 0) {
        container.innerHTML = `<div class="console-empty" style="padding: 24px;">No conversation threads found for this run.</div>`;
        return;
      }

      let html = `
        <table style="margin-top: 0;">
          <thead>
            <tr>
              <th>Conversation ID</th>
              <th>Topic</th>
              <th>State</th>
              <th>Rounds</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
      `;

      items.forEach((c) => {
        html += `
          <tr>
            <td style="font-family: var(--font-mono); font-size: 0.85rem;">${c.conversation_id}</td>
            <td style="font-weight: 500;">${c.topic || "N/A"}</td>
            <td>
              <span class="pill ${c.state === "open" ? "pill-active" : "pill-completed"}">
                ${c.state === "open" ? '<span class="dot-pulsing"></span>' : ""}${c.state}
              </span>
            </td>
            <td>${c.round_count} / ${c.max_rounds}</td>
            <td>
              <a href="/v1/runs/${runId}/conversations/${c.conversation_id}?view=chat" target="_blank"
                 style="font-family: var(--font-mono); font-size: 0.8rem; background: var(--bg-tertiary); border: 1px solid var(--border-color); padding: 4px 8px; border-radius: 4px; font-weight: 600;">
                Open chat thread
              </a>
            </td>
          </tr>
        `;
      });

      html += `</tbody></table>`;
      container.innerHTML = html;
    } catch (err) {
      container.innerHTML = `<div class="console-empty" style="padding: 24px; color: var(--error-color);">Error loading conversations: ${err.message}</div>`;
    }
  }

  // Ephemeral diagnostic streaming (RFC 0092)
  function clearConsole() {
    const box = document.getElementById("diagnostic-console-box");
    if (box) box.innerHTML = `<div class="console-empty">Console cleared. Waiting for logs...</div>`;
  }

  function selectConsoleStream(type, id = "", label = "") {
    const selector = document.getElementById("console-stream-selector");
    const titleLabel = document.getElementById("console-stream-label");

    if (type === "dialogue") {
      if (selector) selector.value = "dialogue";
      if (titleLabel) titleLabel.textContent = "Live agent dialogue feed";
      setupDialogueSSE(selectedRunID);
    } else if (type === "pty") {
      if (selector) selector.value = `pty:${id}`;
      if (titleLabel) titleLabel.textContent = `PTY Terminal: ${label}`;
      setupPtySSE(id);
    }
  }

  function onConsoleStreamSelectorChange(value) {
    if (value === "dialogue") {
      selectConsoleStream("dialogue");
    } else if (value.startsWith("pty:")) {
      const sessionID = value.split(":")[1];
      const option = document.querySelector(`#console-stream-selector option[value="${value}"]`);
      selectConsoleStream("pty", sessionID, option ? option.textContent : sessionID);
    }
  }

  function setupDialogueSSE(runId) {
    if (activeSSE) {
      activeSSE.close();
    }

    const box = document.getElementById("diagnostic-console-box");
    if (!box) return;
    box.innerHTML = `<div class="console-empty">Connecting to live agent dialogue stream...</div>`;

    // Connect to the Live Dialogue Event Source
    activeSSE = new EventSource(`/v1/runs/${runId}/live-dialogue`);

    activeSSE.onopen = () => {
      box.innerHTML = `<div class="console-empty">Connection established. Streaming enqueued dialogue...</div>`;
    };

    activeSSE.onmessage = (event) => {
      const emptyState = document.getElementById("console-empty-state");
      if (emptyState) emptyState.remove();

      try {
        const payload = JSON.parse(event.data);

        // Render enqueued queue message
        const msgDiv = document.createElement("div");
        msgDiv.className = "console-message";
        msgDiv.innerHTML = `
          <div class="console-message-header">[${payload.author_session_id || "system"}] &gt; ${payload.topic || "message"}</div>
          <div class="console-message-body">${escapeHTML(payload.body)}</div>
        `;
        box.appendChild(msgDiv);
        box.scrollTop = box.scrollHeight;
      } catch (e) {
        // Log raw text if not JSON
        const rawDiv = document.createElement("div");
        rawDiv.className = "console-message";
        rawDiv.textContent = event.data;
        box.appendChild(rawDiv);
        box.scrollTop = box.scrollHeight;
      }
    };

    activeSSE.onerror = (err) => {
      console.error("Dialogue SSE connection error:", err);
    };
  }

  function setupPtySSE(sessionId) {
    if (activeSSE) {
      activeSSE.close();
    }

    const box = document.getElementById("diagnostic-console-box");
    if (!box) return;
    box.innerHTML = `<div class="console-empty">Connecting to live supervisor PTY stream...</div>`;

    // Connect to the Live PTY Event Source (RFC 0092)
    activeSSE = new EventSource(`/v1/sessions/${sessionId}/live-pty`);

    activeSSE.onopen = () => {
      box.innerHTML = `<div class="console-empty">Connected to PTY log tail. Reading active terminal buffer...</div>`;
    };

    activeSSE.onmessage = (event) => {
      const emptyState = document.getElementById("console-empty-state");
      if (emptyState) emptyState.remove();

      const rawDiv = document.createElement("div");
      rawDiv.style.fontFamily = "var(--font-mono)";
      rawDiv.style.color = "#ffb000"; // Sleek Amber terminal color for PTY stdout
      rawDiv.textContent = event.data;
      box.appendChild(rawDiv);
      box.scrollTop = box.scrollHeight;
    };

    activeSSE.onerror = (err) => {
      console.error("PTY SSE connection error:", err);
    };
  }

  function escapeHTML(str) {
    if (!str) return "";
    return str.replace(/[&<>'"]/g, (tag) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[tag] || tag));
  }
})();
