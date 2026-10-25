import Image from "next/image";

/**
 * The CoreGo mark: a gunmetal gopher head with sage eyes on a black
 * rounded tile. On the black background of the dark theme the tile
 * blends in; on light backgrounds it reads as an app icon.
 */
export function LogoMark({ size = 28, className }: { size?: number; className?: string }) {
  return <Image src="/logo-mark.png" width={size} height={size} alt="" aria-hidden="true" className={className} priority />;
}

/** The mark followed by the word mark. */
export function Logo({ label = "CoreGo", size = 28 }: { label?: string; size?: number }) {
  return (
    <span className="inline-flex items-center gap-2">
      <LogoMark size={size} />
      <span className="text-[0.95rem] font-semibold tracking-tight text-foreground">{label}</span>
    </span>
  );
}
