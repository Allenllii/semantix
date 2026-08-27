// Workspace shell interactions (issue #404). The shell renders standalone with
// zero backend calls; this script only toggles the context panel and keeps its
// accessible state in sync.
(function () {
  "use strict";
  var btn = document.querySelector("[data-ws-collapse]");
  if (!btn) return;
  btn.addEventListener("click", function () {
    var collapsed = document.body.classList.toggle("ws-right-collapsed");
    btn.setAttribute("aria-expanded", String(!collapsed));
  });
})();
