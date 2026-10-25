/**
 * The CoreGo mark: a minimal, geometric gopher head in Go blue, built
 * from a handful of shapes so it stays legible from favicon size up.
 */
export function LogoMark({ size = 28, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" className={className ? `logo-mark ${className}` : "logo-mark"}>
      <circle cx="13" cy="17" r="7" fill="#00ADD8" />
      <circle cx="51" cy="17" r="7" fill="#00ADD8" />
      <circle cx="13" cy="17" r="3" fill="#007D9C" />
      <circle cx="51" cy="17" r="3" fill="#007D9C" />
      <rect x="8" y="10" width="48" height="50" rx="22" fill="#00ADD8" />
      <circle cx="22.5" cy="28" r="8.5" fill="#fff" />
      <circle cx="41.5" cy="28" r="8.5" fill="#fff" />
      <circle cx="24.5" cy="29" r="3.6" fill="#0c0e12" />
      <circle cx="43.5" cy="29" r="3.6" fill="#0c0e12" />
      <ellipse cx="32" cy="42.5" rx="7.5" ry="5.5" fill="#7FD9EE" />
      <ellipse cx="32" cy="39.6" rx="3.4" ry="2.3" fill="#0c0e12" />
      <rect x="28.6" y="45" width="6.8" height="6.5" rx="1.4" fill="#fff" />
      <path d="M32 45v6.5" stroke="#7FD9EE" strokeWidth="1" />
    </svg>
  );
}

/** The mark followed by the word mark. */
export function Logo({ label = "CoreGo", size = 28 }: { label?: string; size?: number }) {
  return (
    <span className="logo">
      <LogoMark size={size} />
      <span className="logo-text">{label}</span>
    </span>
  );
}
