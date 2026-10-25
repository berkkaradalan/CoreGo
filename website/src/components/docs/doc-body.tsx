import { CodeBlock, Pre } from "fumadocs-ui/components/codeblock";
import defaultMdxComponents from "fumadocs-ui/mdx";
import type { Root } from "hast";
import { toJsxRuntime, type Components } from "hast-util-to-jsx-runtime";
import type { ComponentProps } from "react";
import { Fragment, jsx, jsxs } from "react/jsx-runtime";

/**
 * The components markdown elements render with: Fumadocs UI headings
 * (with anchors), links, tables, callouts and code blocks (copy button,
 * tabs). Code blocks keep the background set by the Shiki theme.
 */
const components = {
  ...defaultMdxComponents,
  pre: (props: ComponentProps<"pre">) => (
    <CodeBlock {...props} keepBackground>
      <Pre>{props.children}</Pre>
    </CodeBlock>
  ),
} as unknown as Partial<Components>;

/** Renders a processed markdown tree (see src/lib/docs/markdown.ts). */
export function DocBody({ tree }: { tree: Root }) {
  return toJsxRuntime(tree, { Fragment, jsx, jsxs, components });
}
