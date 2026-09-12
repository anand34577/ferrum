// Shared chrome behavior: theme toggle (persisted, mirrors the app's own
// light/dark preference model) and the mobile nav disclosure.
(function () {
  const root = document.documentElement;
  const KEY = "ferrum-site-theme";

  function apply(theme) {
    if (theme === "light") root.setAttribute("data-theme", "light");
    else root.removeAttribute("data-theme");
  }

  let stored = null;
  try { stored = localStorage.getItem(KEY); } catch (_) {}
  if (stored) apply(stored);
  else if (window.matchMedia("(prefers-color-scheme: light)").matches) apply("light");

  document.addEventListener("DOMContentLoaded", () => {
    const toggle = document.querySelector("[data-theme-toggle]");
    if (toggle) {
      toggle.addEventListener("click", () => {
        const isLight = root.getAttribute("data-theme") === "light";
        const next = isLight ? "dark" : "light";
        apply(next);
        try { localStorage.setItem(KEY, next); } catch (_) {}
      });
    }

    const navToggle = document.querySelector("[data-nav-toggle]");
    const navLinks = document.querySelector(".navlinks");
    if (navToggle && navLinks) {
      navToggle.addEventListener("click", () => {
        const open = navLinks.classList.toggle("is-open");
        navToggle.setAttribute("aria-expanded", String(open));
      });
    }
  });
})();
