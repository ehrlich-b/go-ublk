// Progressive enhancement only: every page works without this file.
(function () {
  "use strict";
  var root = document.documentElement;

  // Theme toggle. The stored choice is applied by an inline script in <head>.
  var toggle = document.querySelector(".theme-toggle");
  if (toggle) {
    toggle.hidden = false;
    toggle.addEventListener("click", function () {
      var current = root.getAttribute("data-theme");
      if (!current) {
        current = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
      }
      var next = current === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("theme", next); } catch (e) { /* storage unavailable */ }
    });
  }

  // Copy buttons on highlighted code blocks.
  document.querySelectorAll(".highlight").forEach(function (block) {
    var code = block.querySelector("pre code") || block.querySelector("pre");
    if (!code || !navigator.clipboard) return;
    var button = document.createElement("button");
    button.type = "button";
    button.className = "copy-button";
    button.textContent = "Copy";
    button.setAttribute("aria-label", "Copy code to clipboard");
    button.addEventListener("click", function () {
      navigator.clipboard.writeText(code.innerText.replace(/\n$/, "")).then(function () {
        button.textContent = "Copied";
        button.classList.add("copied");
        setTimeout(function () { button.textContent = "Copy"; button.classList.remove("copied"); }, 1500);
      });
    });
    block.appendChild(button);
  });

  // UAPI reference: text search on top of the CSS-only filters, and open the
  // details of a row reached by its #anchor.
  var uapi = document.querySelector(".uapi");
  if (uapi) {
    var search = uapi.querySelector(".uapi-search");
    var input = uapi.querySelector("#uapi-q");
    var rows = uapi.querySelectorAll("tbody tr");
    var empty = uapi.querySelector(".uapi-empty");
    var refresh = function () {
      var q = input.value.trim().toLowerCase();
      rows.forEach(function (row) {
        row.classList.toggle("q-miss", q !== "" && row.textContent.toLowerCase().indexOf(q) === -1);
      });
      var visible = Array.prototype.some.call(rows, function (row) { return row.offsetParent !== null; });
      empty.hidden = visible;
    };
    if (search && input) {
      search.hidden = false;
      input.addEventListener("input", refresh);
      uapi.addEventListener("change", refresh);
    }
    var openTarget = function () {
      var id = decodeURIComponent(location.hash.slice(1));
      var row = id && document.getElementById(id);
      if (row && uapi.contains(row)) {
        var d = row.querySelector("details");
        if (d) d.open = true;
      }
    };
    openTarget();
    window.addEventListener("hashchange", openTarget);
  }

  // Highlight the current section in the right-hand table of contents.
  var tocLinks = document.querySelectorAll(".toc a[href^='#']");
  if (tocLinks.length && "IntersectionObserver" in window) {
    var byId = {};
    tocLinks.forEach(function (a) { byId[decodeURIComponent(a.getAttribute("href").slice(1))] = a; });
    var headings = Array.prototype.filter.call(document.querySelectorAll(".prose h2[id], .prose h3[id]"), function (h) { return byId[h.id]; });
    var setActive = function (id) {
      tocLinks.forEach(function (a) { a.classList.remove("active"); });
      if (byId[id]) byId[id].classList.add("active");
    };
    var observer = new IntersectionObserver(function () {
      var current = null;
      headings.forEach(function (h) { if (h.getBoundingClientRect().top < 120) current = h.id; });
      setActive(current || (headings[0] && headings[0].id));
    }, { rootMargin: "0px 0px -60% 0px", threshold: [0, 1] });
    headings.forEach(function (h) { observer.observe(h); });
  }
})();
