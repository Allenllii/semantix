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
    notice: document.querySelector("[data-ws-notice]")
  };

  var EFFORT_LABELS = { low: "低", medium: "中", high: "高", max: "max" };
  var EFFORT_LEVELS = ["low", "medium", "high"];
  var openMenu = null; // currently open dropdown element or null

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
  function loadProject() {
    setState(el.project, "loading");
    return getJSON("/status").then(function (status) {
      var cwd = String(status.cwd || "");
      var name = cwd.split(/[\\/]/).filter(Boolean).pop() || "semantix";
      setValue(el.projectName, name);
      setState(el.project, "ok");
    }).catch(function () {
      setValue(el.projectName, "未知项目");
      setState(el.project, "error");
    });
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
    loadProject();
    loadBranches();
    loadModels(); // effort arrives piggybacked on GET /models
  }

  initSelectors();

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
