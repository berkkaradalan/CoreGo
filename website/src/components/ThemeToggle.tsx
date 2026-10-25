"use client";

/**
 * Toggles between the dark (default) and light theme. The initial theme
 * is applied before hydration by the inline script in the root layout,
 * so this button only flips the attribute and remembers the choice.
 */
export function ThemeToggle() {
  function toggle() {
    const root = document.documentElement;
    const next = root.dataset.theme === "light" ? "dark" : "light";
    root.dataset.theme = next;
    try {
      localStorage.setItem("corego-theme", next);
    } catch {
      // Storage can be unavailable (private mode); the toggle still works.
    }
  }

  return (
    <button type="button" className="icon-button" onClick={toggle} aria-label="Toggle color theme" title="Toggle color theme">
      <svg className="icon-sun" viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
      <svg className="icon-moon" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" />
      </svg>
    </button>
  );
}

/**
 * The inline script that applies the theme before first paint: a
 * ?theme=light|dark query parameter (handy for sharing and screenshots),
 * otherwise the stored choice.
 */
export const themeScript = `(function(){try{var q=/[?&]theme=(light|dark)/.exec(location.search);var t=q?q[1]:localStorage.getItem("corego-theme");if(t==="light"||t==="dark"){document.documentElement.dataset.theme=t;}}catch(e){}})();`;
