import type { ThemeRegistrationRaw } from "shiki";

/**
 * The Shiki theme of the site. Its colors are CSS variables (--sh-*,
 * mapped from the color tokens in src/styles/tokens.css), so a single
 * highlighting pass renders correctly in light and dark mode.
 */
export const codeTheme: ThemeRegistrationRaw = {
  name: "corego",
  type: "dark",
  colors: {
    "editor.foreground": "var(--sh-foreground)",
    "editor.background": "var(--sh-background)",
  },
  // TextMate token rules (called "tokenColors" in VS Code themes).
  settings: [
    { settings: { foreground: "var(--sh-foreground)" } },
    {
      scope: ["comment", "punctuation.definition.comment", "string.quoted.docstring.multi"],
      settings: { foreground: "var(--sh-comment)", fontStyle: "italic" },
    },
    {
      scope: [
        "keyword",
        "storage.type",
        "storage.modifier",
        "keyword.control",
        "keyword.function",
        "keyword.type",
        "keyword.package",
        "keyword.import",
      ],
      settings: { foreground: "var(--sh-keyword)" },
    },
    {
      scope: ["keyword.operator", "punctuation", "meta.brace", "keyword.operator.assignment"],
      settings: { foreground: "var(--sh-punctuation)" },
    },
    {
      scope: ["string", "string.quoted", "string.template", "string.interpolated", "markup.inline.raw"],
      settings: { foreground: "var(--sh-string)" },
    },
    {
      scope: [
        "constant.numeric",
        "constant.language",
        "constant.character",
        "constant.other",
        "variable.other.constant",
        "support.constant",
        "constant.character.escape",
      ],
      settings: { foreground: "var(--sh-constant)" },
    },
    {
      scope: [
        "entity.name.function",
        "support.function",
        "meta.function-call entity.name.function",
        "variable.function",
      ],
      settings: { foreground: "var(--sh-function)" },
    },
    {
      scope: [
        "entity.name.type",
        "entity.name.class",
        "support.type",
        "storage.type.numeric.go",
        "storage.type.string.go",
        "storage.type.boolean.go",
        "storage.type.error.go",
        "storage.type.byte.go",
        "storage.type.rune.go",
        "entity.name.package",
        "entity.name.import",
        "support.type.property-name",
        "variable.other.env",
        "variable.other.dotenv",
        "entity.other.attribute-name",
        "entity.name.tag",
      ],
      settings: { foreground: "var(--sh-type)" },
    },
    { scope: ["variable.parameter", "variable.other"], settings: { foreground: "var(--sh-foreground)" } },
    {
      scope: ["markup.underline.link", "string.other.link"],
      settings: { foreground: "var(--sh-link)", fontStyle: "underline" },
    },
    { scope: ["markup.bold"], settings: { fontStyle: "bold" } },
    { scope: ["markup.italic"], settings: { fontStyle: "italic" } },
  ],
};
