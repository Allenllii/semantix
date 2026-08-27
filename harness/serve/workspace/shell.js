// Workspace shell interactions (issue #404). The shell renders standalone with
// zero backend calls; this script only toggles the context panel and keeps its
// accessible state in sync.
(function () {
  "use strict";
  var collapseBtn = document.querySelector("[data-ws-collapse]");
  var sideToggle = document.querySelector("[data-ws-side-toggle]");
  var side = document.getElementById("ws-side");
  var sideScrim = document.querySelector("[data-ws-side-close]");

  if (collapseBtn) {
    collapseBtn.addEventListener("click", function () {
      var collapsed = document.body.classList.toggle("ws-right-collapsed");
      collapseBtn.setAttribute("aria-expanded", String(!collapsed));
    });
  }

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
