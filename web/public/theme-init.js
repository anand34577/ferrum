// Runs before first paint so returning dark-mode users never see a light
// flash. Kept as an external file so the CSP (script-src 'self') stays
// free of inline-script allowances.
(function () {
  try {
    var stored = localStorage.getItem("ferrum-theme");
    var dark = stored === "dark" || (stored !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    if (dark) document.documentElement.classList.add("dark");
    var look = localStorage.getItem("ferrum-look");
    var knownLooks = ["proxmox", "terminal", "glassFlightDeck", "midnight", "paper"];
    if (knownLooks.indexOf(look) !== -1) document.documentElement.dataset.look = look;
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute("content", dark ? "#121417" : "#ffffff");
  } catch {
    /* no storage / no matchMedia — default light theme applies */
  }
})();
