// Checks the WCAG contrast ratios of the site colors in
// src/styles/colors.css (the light variant on :root, the dark variant on
// .dark) and exits with status 1 when a pair is below its threshold.
// Run it with `npm run check:contrast`.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const cssPath = path.join(here, "..", "src", "styles", "colors.css");
const css = fs.readFileSync(cssPath, "utf8");

/** [foreground token, background token, minimum ratio] */
const PAIRS = [
  ["foreground", "background", 4.5],
  ["foreground", "card", 4.5],
  ["foreground", "muted", 4.5],
  ["prose", "background", 4.5],
  ["prose", "card", 4.5],
  ["muted-foreground", "background", 4.5],
  ["muted-foreground", "card", 4.5],
  ["link", "background", 4.5],
  ["link", "card", 4.5],
  ["link", "code-background", 4.5],
  ["primary", "background", 4.5],
  ["primary-foreground", "primary", 4.5],
  ["foreground", "selection", 4.5],
  ["code-foreground", "code-background", 4.5],
  ["syntax-keyword", "code-background", 4.5],
  ["syntax-string", "code-background", 4.5],
  ["syntax-comment", "code-background", 4.5],
  ["syntax-function", "code-background", 4.5],
  ["syntax-constant", "code-background", 4.5],
  ["syntax-type", "code-background", 4.5],
  ["syntax-punctuation", "code-background", 4.5],
  ["info", "background", 4.5],
  ["success", "background", 4.5],
  ["warning", "background", 4.5],
  ["destructive", "background", 4.5],
  ["border", "background", 1.15],
];

/** Tokens that must not be pure black or pure white. */
const NOT_EXTREME = ["foreground", "prose"];

function parseHex(hex) {
  const m = /^#([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) throw new Error(`unsupported color ${hex} (use #rrggbb)`);
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function luminance([r, g, b]) {
  const lin = (c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(a, b) {
  const la = luminance(parseHex(a));
  const lb = luminance(parseHex(b));
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** Returns the body of the top-level rule with exactly this selector. */
function ruleBody(selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = [...css.matchAll(new RegExp(`^${escaped}\\s*\\{([^}]*)\\}`, "gm"))];
  if (matches.length !== 1) {
    console.error(`expected one "${selector}" rule in ${cssPath}, found ${matches.length}`);
    process.exit(1);
  }
  return matches[0][1];
}

const variants = [
  { mode: "light", dark: false, body: ruleBody(":root") },
  { mode: "dark", dark: true, body: ruleBody(".dark") },
];

let failures = 0;
const summary = [];
for (const { mode, dark, body } of variants) {
  const tokens = Object.fromEntries(
    [...body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)].map(([, k, v]) => [k, v.trim()]),
  );
  // Dark variants sit on pure black; light variants on off-white.
  const bg = tokens.background?.toLowerCase();
  if (dark && bg !== "#000000") {
    console.error(`FAIL ${mode}: --background must be #000000, got ${bg}`);
    failures++;
  }
  if (!dark && bg === "#ffffff") {
    console.error(`FAIL ${mode}: --background must not be pure white`);
    failures++;
  }
  for (const token of NOT_EXTREME) {
    const v = tokens[token]?.toLowerCase();
    if (v === "#000000" || v === "#ffffff") {
      console.error(`FAIL ${mode}: --${token} is pure ${v}`);
      failures++;
    }
  }
  const row = { mode };
  for (const [fg, bg, min] of PAIRS) {
    if (!tokens[fg] || !tokens[bg]) {
      console.error(`FAIL ${mode}: missing --${tokens[fg] ? bg : fg}`);
      failures++;
      continue;
    }
    const ratio = contrast(tokens[fg], tokens[bg]);
    if (ratio < min) {
      console.error(`FAIL ${mode}: --${fg} on --${bg} is ${ratio.toFixed(2)}:1 (needs ${min}:1)`);
      failures++;
    }
    if (bg === "background" && ["prose", "link", "muted-foreground"].includes(fg)) {
      row[fg] = ratio.toFixed(2);
    }
    if (fg === "syntax-comment") row["comment/code"] = ratio.toFixed(2);
  }
  summary.push(row);
}

console.table(summary);
if (failures > 0) {
  console.error(`${failures} contrast check(s) failed.`);
  process.exit(1);
}
console.log(`Light and dark variants pass (${PAIRS.length} pairs each).`);
