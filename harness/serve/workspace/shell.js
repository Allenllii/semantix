// Workspace shell interactions.
// - #404: context-panel collapse and the narrow-viewport side drawer.
// - #405: topbar project/branch/model/effort chips hydrated from live backend
//   state; model & effort switch through the same `/model` `/effort` submit
//   commands the TUI uses — no new CLI semantics. Every load re-reads server
//   state, which is what makes the workspace survive refresh.
(function () {
  "use strict";
  var nativeFetch = window.fetch.bind(window);

  // ── #404 chrome: collapse + side drawer ──
  var collapseBtn = document.querySelector("[data-ws-collapse]");
  if (collapseBtn) {
    collapseBtn.addEventListener("click", function () {
      var collapsed = document.body.classList.toggle("ws-right-collapsed");
      collapseBtn.setAttribute("aria-expanded", String(!collapsed));
    });
  }

  // ── auth: mirrors the fragment-token bootstrap in serve/index.html ──
  function bootstrapFragmentToken() {
    var fragment = new URLSearchParams(window.location.hash.slice(1));
    var token = fragment.get("token");
    if (!token) return Promise.resolve();
    fragment.delete("token");
    var cleanHash = fragment.toString();
    window.history.replaceState(null, "", window.location.pathname + window.location.search + (cleanHash ? "#" + cleanHash : ""));
    return nativeFetch("/auth/token", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ token: token })
    }).then(function (r) {
      if (!r.ok) throw new Error("auth failed (HTTP " + r.status + ")");
    });
  }
  var authReady = bootstrapFragmentToken();

  function getJSON(url) {
    return authReady.then(function () {
      return nativeFetch(url, { headers: { accept: "application/json" } });
    }).then(function (r) {
      if (!r.ok) throw new Error(url + " -> HTTP " + r.status);
      return r.json();
    });
  }

  function postCommand(input) {
    return authReady.then(function () {
      return nativeFetch("/submit", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ input: input })
      });
    }).then(function (r) {
      if (r.status === 204) return;
      return r.text().then(function (text) {
        throw new Error(text || "HTTP " + r.status);
      });
    });
  }

  function postJSON(url, body) {
    return authReady.then(function () {
      return nativeFetch(url, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body || {})
      });
    }).then(function (r) {
      if (!r.ok) {
        return r.text().then(function (text) {
          throw new Error(text || "HTTP " + r.status);
        });
      }
    });
  }

  // ── #405 selector elements ──
  var el = {
    project: document.querySelector("[data-ws-project]"),
    projectName: document.querySelector("[data-ws-project-name]"),
    branch: document.querySelector("[data-ws-branch]"),
    branchName: document.querySelector("[data-ws-branch-name]"),
    branchMenu: document.querySelector("[data-ws-branch-menu]"),
    model: document.querySelector("[data-ws-model]"),
    modelName: document.querySelector("[data-ws-model-name]"),
    modelMenu: document.querySelector("[data-ws-model-menu]"),
    effort: document.querySelector("[data-ws-effort]"),
    effortValue: document.querySelector("[data-ws-effort-value]"),
    effortMenu: document.querySelector("[data-ws-effort-menu]"),
    notice: document.querySelector("[data-ws-notice]"),
    taskList: document.querySelector("[data-ws-task-list]"),
    newTask: document.querySelector("[data-ws-new-task]"),
    sideProjectName: document.querySelector("[data-ws-side-project-name]"),
    timeline: document.querySelector("[data-ws-timeline]"),
    demo: document.querySelector("[data-ws-demo]")
  };

  // Sidebar/project shared state (GUI-3): whether the CURRENT session is
  // running drives the 运行中 pill for the highlighted task row.
  var sessionRunning = false;
  var EFFORT_LABELS = { low: "低", medium: "中", high: "高", max: "max" };
  var EFFORT_LEVELS = ["low", "medium", "high"];
  var TASK_PILLS = {
    running: ["运行中", "ws-state-running"],
    done: ["完成", "ws-state-done"],
    empty: ["空会话", "ws-state-empty"]
  };
  var openMenu = null; // currently open dropdown element or null

  // GUI-4 (#407): consume the versioned workspace stream as a transport
  // contract. The shell does not synthesize chat/tool/cache cards here; it
  // only validates ordering and lets the existing refresh paths react to a
  // gap. Unknown event names and malformed payloads are deliberately ignored
  // so a newer server cannot crash an older workspace page.
  var workspaceEvents = null;
  var lastEventSeq = 0;
  var eventTaskID = "";
  var canonicalEventTypes = {
    user_message: true,
    assistant_message: true,
    plan: true,
    tool_start: true,
    tool_result: true,
    diff: true,
    permission_request: true,
    task_status: true,
    cache_status: true,
    error: true,
    unknown: true
  };

  // GUI-5 (#408): the timeline is a projection of the versioned stream. The
  // stream sequence is the only ordering authority; tool IDs are the only
  // merge key. This keeps deltas idempotent without creating a second history
  // store or guessing at events the server did not publish.
  var MAX_INLINE_CHARS = 1200;
  var MAX_RENDER_CHARS = 200000;
  var workflow = {
    assistant: null,
    plan: null,
    tools: Object.create(null),
    active: false
  };

  function clearNode(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
  }

  function activateWorkflow() {
    if (workflow.active) return;
    workflow.active = true;
    if (el.demo) el.demo.classList.add("is-hidden");
  }

  function resetWorkflow() {
    workflow.assistant = null;
    workflow.plan = null;
    workflow.tools = Object.create(null);
    workflow.active = false;
    clearNode(el.timeline);
    if (el.demo) el.demo.classList.remove("is-hidden");
  }

  function makeEvent(kind, label, icon) {
    if (!el.timeline) return null;
    activateWorkflow();
    var article = document.createElement("article");
    article.className = "ws-event ws-event--" + kind;
    article.setAttribute("data-ws-event-kind", kind);
    var head = document.createElement("header");
    head.className = "ws-event__head";
    var iconEl = document.createElement("span");
    iconEl.className = "ws-event__icon";
    iconEl.setAttribute("aria-hidden", "true");
    iconEl.textContent = icon || "•";
    var labelEl = document.createElement("strong");
    labelEl.className = "ws-event__label";
    labelEl.textContent = label || "事件";
    var statusEl = document.createElement("span");
    statusEl.className = "ws-event__status";
    var body = document.createElement("div");
    body.className = "ws-event__body";
    head.appendChild(iconEl);
    head.appendChild(labelEl);
    head.appendChild(statusEl);
    article.appendChild(head);
    article.appendChild(body);
    el.timeline.appendChild(article);
    return { article: article, head: head, label: labelEl, status: statusEl, body: body };
  }

  function setStatus(card, text, state) {
    if (!card || !card.status) return;
    card.status.textContent = text || "";
    card.status.className = "ws-event__status" + (state ? " is-" + state : "");
    if (state === "failed") card.article.classList.add("ws-event--error");
  }

  function appendExpandable(parent, value, label, className) {
    if (!parent || value === undefined || value === null) return;
    var text = String(value);
    if (!text) return;
    var bounded = text.length > MAX_RENDER_CHARS ? text.slice(0, MAX_RENDER_CHARS) + "\n…（输出已限制为 200000 字符）" : text;
    if (bounded.length <= MAX_INLINE_CHARS) {
      var inline = document.createElement("pre");
      inline.className = className || "ws-event__detail";
      inline.textContent = bounded;
      parent.appendChild(inline);
      return;
    }
    var details = document.createElement("details");
    details.className = "ws-event__detail";
    var summary = document.createElement("summary");
    summary.className = "ws-event__toggle";
    summary.textContent = (label || "展开内容") + "（" + bounded.length + " 字符）";
    var pre = document.createElement("pre");
    pre.className = className || "ws-event__detail";
    pre.textContent = bounded.slice(0, MAX_INLINE_CHARS) + "\n…";
    details.addEventListener("toggle", function () {
      if (details.open && pre.textContent.indexOf("\n…") !== -1) pre.textContent = bounded;
    });
    details.appendChild(summary);
    details.appendChild(pre);
    parent.appendChild(details);
  }

  function toolLabel(name) {
    var labels = {
      read_file: "读取文件", read_files: "读取文件", glob: "查找文件",
      grep: "搜索内容", bash: "执行命令", shell: "执行命令",
      write_file: "写入文件", edit_file: "编辑文件", apply_patch: "应用补丁",
      todo_write: "更新计划", complete_step: "完成计划项"
    };
    return labels[name] || name || "工具";
  }

  function toolKey(tool, seq) {
    return tool && tool.id ? String(tool.id) : "anonymous-tool-" + String(seq || lastEventSeq);
  }

  function renderTool(card, record) {
    if (!card || !record) return;
    clearNode(card.body);
    if (record.args) appendExpandable(card.body, record.args, "展开参数", "ws-tool-preview");
    if (record.output) appendExpandable(card.body, record.output, "展开输出", "ws-tool-preview");
    if (record.err) appendExpandable(card.body, record.err, "展开错误", "ws-tool-preview");
    if (record.truncated) {
      var note = document.createElement("div");
      note.className = "ws-timeline__notice";
      note.textContent = "输出过长，服务端已截断显示。";
      card.body.appendChild(note);
    }
  }

  function parsePlan(value) {
    if (!value) return null;
    var raw = value;
    if (typeof value === "string") {
      try { raw = JSON.parse(value); } catch (_) {
        return value.split(/\r?\n/).map(function (line) {
          return line.replace(/^\s*(?:[-*]|\d+[.)])\s*/, "").trim();
        }).filter(Boolean).map(function (content) { return { content: content, status: "pending" }; });
      }
    }
    var items = Array.isArray(raw) ? raw : raw.todos || raw.items || raw.plan;
    if (!Array.isArray(items)) return null;
    return items.map(function (item) {
      if (typeof item === "string") return { content: item, status: "pending" };
      return { content: String(item.content || item.title || item.label || ""), status: String(item.status || "pending") };
    }).filter(function (item) { return item.content; });
  }

  function renderPlan(items) {
    if (!el.timeline || !items) return;
    if (!workflow.plan) workflow.plan = makeEvent("plan", "计划", "✓");
    if (!workflow.plan) return;
    clearNode(workflow.plan.body);
    var list = document.createElement("ol");
    list.className = "ws-plan";
    items.forEach(function (item) {
      var li = document.createElement("li");
      li.textContent = item.content;
      if (item.status === "completed" || item.status === "done") li.className = "is-done";
      list.appendChild(li);
    });
    workflow.plan.body.appendChild(list);
    setStatus(workflow.plan, items.length + " 项", "done");
  }

  function renderAssistant(data) {
    var kind = data.kind || "text";
    if (!workflow.assistant) workflow.assistant = makeEvent("assistant", "semantix", "✦");
    if (!workflow.assistant) return;
    if (kind === "message") {
      workflow.assistant.text = String(data.text || "");
      workflow.assistant.reasoning = String(data.reasoning || "");
    } else if (kind === "reasoning") {
      workflow.assistant.reasoning = (workflow.assistant.reasoning || "") + String(data.reasoning || data.text || "");
    } else {
      workflow.assistant.text = (workflow.assistant.text || "") + String(data.text || "");
    }
    clearNode(workflow.assistant.body);
    if (workflow.assistant.text) {
      var text = document.createElement("div");
      text.className = "ws-event__text";
      text.textContent = workflow.assistant.text;
      workflow.assistant.body.appendChild(text);
    }
    if (workflow.assistant.reasoning) appendExpandable(workflow.assistant.body, workflow.assistant.reasoning, "展开思考过程");
    setStatus(workflow.assistant, kind === "message" ? "已完成" : "生成中", kind === "message" ? "done" : "running");
  }

  function renderToolEvent(kind, tool, seq) {
    if (!tool) return;
    var key = toolKey(tool, seq);
    var record = workflow.tools[key];
    if (!record) {
      record = { args: "", output: "", err: "", truncated: false, progress: "" };
      record.card = makeEvent("tool", toolLabel(tool.name), "⚙");
      workflow.tools[key] = record;
    }
    if (tool.name) record.card.label.textContent = toolLabel(tool.name) + " · " + tool.name;
    if (tool.args) record.args = String(tool.args);
    if (kind === "tool_start") {
      record.state = "running";
      setStatus(record.card, "进行中", "running");
    } else if (kind === "tool_result") {
      record.output = String(tool.output || "");
      record.err = String(tool.err || "");
      record.truncated = !!tool.truncated;
      record.state = record.err ? "failed" : "done";
      setStatus(record.card, record.err ? "失败" : "已完成", record.err ? "failed" : "done");
      if (tool.durationMs) record.card.status.textContent += " · " + tool.durationMs + " ms";
    } else {
      record.progress += String(tool.output || "");
      record.output = record.progress;
      record.state = "running";
      setStatus(record.card, "进行中", "running");
    }
    renderTool(record.card, record);
    if (tool.name === "todo_write" && tool.args) renderPlan(parsePlan(tool.args));
  }

  function renderStatus(kind, data) {
    var card;
    if (kind === "error") {
      card = makeEvent("error", "执行失败", "!");
      if (card) { appendExpandable(card.body, data.err || data.text || data.detail, "查看错误"); setStatus(card, "失败", "failed"); }
      return;
    }
    if (kind === "cache_status") {
      card = makeEvent("cache", "缓存状态", "◌");
      if (card) {
        var u = data.usage || {};
        var hit = Number(u.cacheHitTokens || 0), miss = Number(u.cacheMissTokens || 0);
        card.body.textContent = "命中 " + hit + " · 未命中 " + miss;
        setStatus(card, "已更新", "done");
      }
      return;
    }
    if (kind === "plan") { renderPlan(parsePlan(data.plan || data.text || data.detail)); return; }
    card = makeEvent(kind === "retry" ? "retry" : "status", kind === "retry" ? "重试" : "任务状态", kind === "retry" ? "↻" : "•");
    if (!card) return;
    var text = data.text || data.detail || data.phase || data.outcome || "";
    if (kind === "retry") text = "第 " + (data.retryAttempt || "?") + " / " + (data.retryMax || "?") + " 次重试" + (data.retryScope ? " · " + data.retryScope : "");
    if (text) card.body.textContent = String(text);
    setStatus(card, kind === "retry" ? "等待中" : "已记录", kind === "retry" ? "running" : "done");
  }

  // ── tiny view helpers ──
  function setState(chip, state) {
    chip.setAttribute("data-ws-state", state);
    chip.setAttribute("aria-busy", state === "loading" ? "true" : "false");
  }

  function setValue(span, text) {
    span.textContent = text;
  }

  function showNotice(message, kind) {
    if (!el.notice) return;
    el.notice.textContent = message;
    el.notice.className = "ws-notice ws-notice--" + (kind || "error");
    el.notice.hidden = false;
    clearTimeout(showNotice.timer);
    showNotice.timer = setTimeout(function () { el.notice.hidden = true; }, 6000);
  }

  function handleWorkspaceEvent(message) {
    var payload;
    try {
      payload = JSON.parse(message.data || "");
    } catch (_) {
      return;
    }
    if (!payload || payload.v !== 1 || !Number.isSafeInteger(payload.seq) || payload.seq < 1) return;
    if (eventTaskID && payload.task_id !== eventTaskID) return;
    if (!eventTaskID && typeof payload.task_id === "string") eventTaskID = payload.task_id;
    if (payload.seq <= lastEventSeq) return;
    if (lastEventSeq && payload.seq > lastEventSeq + 1) {
      // A dropped frame or expired replay window is a signal to refresh
      // derived state, never a reason to terminate the EventSource.
      refreshTasks();
      showNotice("事件流存在缺口，已刷新任务状态。", "warn");
    }
    lastEventSeq = payload.seq;
    if (!canonicalEventTypes[payload.type]) return;
    var data;
    try { data = typeof payload.data === "string" ? JSON.parse(payload.data || "{}") : payload.data; } catch (_) { return; }
    if (!data || typeof data !== "object") return;
    // The inner eventwire frame remains the source of truth. The renderer only
    // projects it into cards; it never treats text as markup or invents data.
    if (data.kind === "turn_started") workflow.assistant = null;
    switch (payload.type) {
      case "user_message":
        var user = makeEvent("user", "用户", "›");
        if (user) { user.body.textContent = String(data.text || ""); setStatus(user, "已发送", "done"); }
        break;
      case "assistant_message":
        renderAssistant(data);
        break;
      case "plan":
        renderStatus("plan", data);
        break;
      case "tool_start":
      case "tool_result":
        renderToolEvent(data.kind === "tool_progress" ? "tool_progress" : payload.type, data.tool, payload.seq);
        break;
      case "error":
        renderStatus("error", data);
        break;
      case "cache_status":
        renderStatus("cache_status", data);
        break;
      case "task_status":
        renderStatus(data.kind === "retrying" ? "retry" : "task_status", data);
        break;
      case "unknown":
        // Forward-compatible frames remain visible as a neutral status card;
        // malformed/unknown inner data still cannot break the page.
        renderStatus("task_status", { text: data.text || data.detail || "收到未识别事件" });
        break;
      case "permission_request":
        var approval = data.approval || data.ask || {};
        var permission = makeEvent("status", "需要确认", "?");
        if (permission) {
          permission.body.textContent = String(approval.subject || approval.reason || approval.tool || "等待用户确认");
          setStatus(permission, "等待中", "running");
        }
        break;
    }
  }

  function connectWorkspaceEvents() {
    if (!window.EventSource) return;
    if (workspaceEvents) workspaceEvents.close();
    resetWorkflow();
    eventTaskID = "";
    lastEventSeq = 0;
    workspaceEvents = new EventSource("/workspace/events");
    Object.keys(canonicalEventTypes).forEach(function (type) {
      workspaceEvents.addEventListener(type, handleWorkspaceEvent);
    });
  }

  function closeMenus() {
    if (!openMenu) return;
    openMenu.hidden = true;
    var btn = openMenu.parentElement && openMenu.parentElement.querySelector("button");
    if (btn) btn.setAttribute("aria-expanded", "false");
    openMenu = null;
  }

  function toggleMenu(menu, button) {
    var opening = menu.hidden;
    closeMenus();
    if (opening) {
      menu.hidden = false;
      button.setAttribute("aria-expanded", "true");
      openMenu = menu;
    }
  }

  function fillList(menu, items, onPick) {
    while (menu.firstChild) menu.removeChild(menu.firstChild);
    items.forEach(function (item) {
      var li = document.createElement("li");
      li.setAttribute("role", "option");
      li.className = "ws-pop__row";
      if (item.disabled) li.setAttribute("aria-disabled", "true");
      li.textContent = item.label;
      if (item.hint) {
        var hint = document.createElement("span");
        hint.className = "ws-pop__hint";
        hint.textContent = item.hint;
        li.appendChild(hint);
      }
      if (item.active) li.classList.add("is-active");
      if (!item.disabled && onPick) {
        li.addEventListener("click", function () { closeMenus(); onPick(item); });
      }
      menu.appendChild(li);
    });
  }

  // ── loaders: one per chip family ──
  // refreshTasks reads /status + /sessions together: the project chip name,
  // the sidebar project row, and the 运行中 pills all derive from the same
  // real backend state (#405 #406).
  function refreshTasks() {
    setState(el.project, "loading");
    return getJSON("/status").then(function (status) {
      var cwd = String(status.cwd || "");
      var name = cwd.split(/[\\/]/).filter(Boolean).pop() || "semantix";
      setValue(el.projectName, name);
      if (el.sideProjectName) el.sideProjectName.textContent = name;
      sessionRunning = !!status.running;
      setState(el.project, "ok");
      return getJSON("/sessions").then(renderTasks);
    }).catch(function () {
      setValue(el.projectName, "未知项目");
      setState(el.project, "error");
      renderTasks(null);
    });
  }

  function deriveTaskState(s) {
    if ((s.current && sessionRunning) || s.in_flight) return "running";
    if ((s.turns || 0) > 0) return "done";
    return "empty";
  }

  function renderTasks(sessions) {
    if (!el.taskList) return;
    while (el.taskList.firstChild) el.taskList.removeChild(el.taskList.firstChild);
    if (!Array.isArray(sessions)) {
      var err = document.createElement("li");
      err.className = "ws-tasks-note";
      err.textContent = "任务列表不可用。";
      el.taskList.appendChild(err);
      return;
    }
    if (!sessions.length) {
      var empty = document.createElement("li");
      empty.className = "ws-tasks-note";
      empty.textContent = "还没有任务：点击上方「新建任务」开始第一个会话。";
      el.taskList.appendChild(empty);
      return;
    }
    sessions.forEach(function (s) {
      var li = document.createElement("li");
      var row = document.createElement("button");
      row.type = "button";
      row.className = "ws-task-row" + (s.current ? " is-current" : "");

      var dot = document.createElement("span");
      dot.className = "ws-dot" + (s.current ? " is-on" : "");

      var title = document.createElement("span");
      title.className = "ws-task-title";
      title.textContent = s.title || s.name;

      var meta = document.createElement("span");
      meta.className = "ws-task-meta";
      meta.textContent = s.turns ? s.turns + " 轮" : "";

      var stateKey = deriveTaskState(s);
      var pill = document.createElement("span");
      pill.className = "ws-state-pill " + TASK_PILLS[stateKey][1];
      pill.textContent = TASK_PILLS[stateKey][0];

      row.appendChild(dot);
      row.appendChild(title);
      row.appendChild(meta);
      row.appendChild(pill);
      li.appendChild(row);

      if (s.current) {
        row.setAttribute("aria-current", "true");
        row.title = "当前任务";
      } else {
        row.title = "切换到该任务（会话内容保留）";
        row.addEventListener("click", function () { switchTask(s); });
      }
      el.taskList.appendChild(li);
    });
  }

  function switchTask(s) {
    postJSON("/resume", { path: s.path }).then(refreshTasks).catch(function (err) {
      showNotice("切换任务失败：" + err.message, "error");
    }).then(connectWorkspaceEvents);
  }

  function loadBranches() {
    setState(el.branch, "loading");
    return getJSON("/branches").then(function (data) {
      var branches = Array.isArray(data.branches) ? data.branches : [];
      // Session branches are informational for the shell: read-only display
      // keeps fork/resume flows in the sessions picker where they belong.
      fillList(el.branchMenu, branches.map(function (b, i) {
        return {
          label: b.name || b.id || "(未命名分支)",
          hint: i === 0 ? "当前" : "",
          disabled: true,
          active: i === 0
        };
      }));
      setState(el.branch, "ok");
      if (!branches.length) {
        setValue(el.branchName, "无分支");
        el.branch.title = "当前会话没有分支记录（只读展示）";
        return;
      }
      var current = branches[0];
      var label = current.name || current.id;
      setValue(el.branchName, label);
      el.branch.title = "当前会话分支：" + label + "（只读展示）";
    }).catch(function () {
      setValue(el.branchName, "不可用");
      setState(el.branch, "error");
    });
  }

  function renderEffort(level, hasModel) {
    var label = level ? (EFFORT_LABELS[level] || level) : hasModel ? "默认" : "—";
    setValue(el.effortValue, label);
    setState(el.effort, "ok");
    el.effort.title = "推理强度：" + label;
    var rows = EFFORT_LEVELS.map(function (lv) {
      return { label: EFFORT_LABELS[lv], level: lv, active: lv === level };
    });
    if (level && !EFFORT_LABELS[level]) {
      rows.push({ label: level, level: level, active: true });
    } else if (!level) {
      rows.push({ label: "未设置（跟随模型默认）", disabled: !hasModel, active: false });
    }
    fillList(el.effortMenu, rows, pickEffort);
  }

  function loadModels() {
    setState(el.model, "loading");
    return getJSON("/models").then(function (data) {
      renderEffort(String(data.effort || "").toLowerCase(), !!data.current);
      var models = Array.isArray(data.models) ? data.models : [];
      if (!models.length) {
        // #405 acceptance: an explicit, visible unavailable-model signal.
        setValue(el.modelName, "模型不可用");
        setState(el.model, "error");
        el.model.title = "没有任何已配置的可用模型；请在 provider 设置中添加后刷新";
        fillList(el.modelMenu, [{ label: "模型不可用：未配置任何模型", disabled: true }]);
        showNotice("模型不可用：未发现已配置的聊天模型，请检查 provider 配置。", "warn");
        return;
      }
      var activeRef = null;
      fillList(el.modelMenu, models.map(function (m) {
        var ref = m.ref || (m.provider + "/" + m.model);
        var label = m.model && m.provider ? m.model + " · " + m.provider : ref;
        if (m.active) activeRef = ref;
        return { label: label, ref: ref, active: !!m.active };
      }), pickModel);
      setValue(el.modelName, data.label || (activeRef || "").split("/").pop());
      setState(el.model, "ok");
      el.model.title = "当前模型：" + (activeRef || data.label || "");
    }).catch(function () {
      setValue(el.modelName, "不可用");
      setState(el.model, "error");
      el.model.title = "无法读取模型列表";
    });
  }

  // ── actions: reuse the CLI command surface (`POST /submit` intercepting
  // /model and /effort), so switching never forks argument semantics. ──
  function pickModel(item) {
    setState(el.model, "loading");
    postCommand("/model " + item.ref).then(loadModels).catch(function (err) {
      setState(el.model, "error");
      showNotice("切换模型失败：" + err.message, "error");
    });
  }

  function pickEffort(item) {
    setState(el.effort, "loading");
    postCommand("/effort " + item.level).then(loadModels).catch(function (err) {
      setState(el.effort, "error");
      showNotice("切换推理强度失败：" + err.message, "error");
    });
  }

  function initSelectors() {
    if (!el.project || !el.model) return;
    el.model.addEventListener("click", function () { toggleMenu(el.modelMenu, el.model); });
    el.effort.addEventListener("click", function () { toggleMenu(el.effortMenu, el.effort); });
    el.branch.addEventListener("click", function () { toggleMenu(el.branchMenu, el.branch); });
    document.addEventListener("keydown", function (e) { if (e.key === "Escape") closeMenus(); });
    document.addEventListener("click", function (e) {
      if (openMenu && openMenu.parentElement && !openMenu.parentElement.contains(e.target)) closeMenus();
    });
    if (el.newTask) {
      // 创建任务后自动进入新会话：/new 在服务端完成会话切换，这里只负责刷新侧栏 (#406).
      el.newTask.addEventListener("click", function () {
        setState(el.newTask, "loading");
        postJSON("/new").then(refreshTasks).catch(function (err) {
          showNotice("创建任务失败：" + err.message, "error");
        }).finally(function () {
          setState(el.newTask, "ok");
          connectWorkspaceEvents();
        });
      });
    }
    refreshTasks();
    loadBranches();
    loadModels(); // effort arrives piggybacked on GET /models
  }

  initSelectors();
  connectWorkspaceEvents();

  // ── #404 side drawer (narrow viewports only) ──
  var sideToggle = document.querySelector("[data-ws-side-toggle]");
  var side = document.getElementById("ws-side");
  var sideScrim = document.querySelector("[data-ws-side-close]");
  if (!sideToggle || !side) return;

  function isNarrow() {
    return window.matchMedia("(max-width: 860px)").matches;
  }

  function setSideOpen(open) {
    var active = Boolean(open && isNarrow());
    document.body.classList.toggle("ws-side-open", active);
    sideToggle.setAttribute("aria-expanded", String(active));
    side.setAttribute("aria-hidden", String(isNarrow() && !active));
  }

  sideToggle.addEventListener("click", function () {
    setSideOpen(!document.body.classList.contains("ws-side-open"));
  });
  if (sideScrim) sideScrim.addEventListener("click", function () { setSideOpen(false); });
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") setSideOpen(false);
  });
  window.addEventListener("resize", function () { setSideOpen(document.body.classList.contains("ws-side-open")); });
  setSideOpen(false);
})();
