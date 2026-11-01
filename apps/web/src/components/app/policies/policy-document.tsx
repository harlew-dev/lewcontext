import { Fragment, type ReactNode } from "react";
import { AuthLayout } from "@/components/layout/auth-layout";
import { Divider } from "@/components/ui/divider";
import { Notice } from "@/components/ui/feedback";
import { Heading, HeadingGroup, Subheading } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Strong, Text, TextLink } from "@/components/ui/text";
import { formatDate } from "@/lib/format";
import { routes } from "@/lib/routes";
import type { Block, Inline, Policy, PolicyName } from "@/types/policies";
import { PolicyLinks } from "./policy-links";
// A policy page (/policy/terms-of-service, /policy/privacy-policy): the
// document its loader read at build time, in the sign-in pages' layout with
// a wider column, then links to both documents. No session: it reads the
// same to everyone.
export function PolicyPage({
  name,
  policy,
}: {
  name: PolicyName;
  policy: Policy;
}) {
  return (
    <AuthLayout>
      <article className="grid w-full max-w-2xl grid-cols-1 gap-8">
        <Logo href={routes.home} />
        <HeadingGroup>
          <Heading>{policy.title}</Heading>
          <Text>Last updated {formatDate(policy.updated, "long")}</Text>
        </HeadingGroup>
        {/* Production refuses to publish a template (POLICIES=on), so this
            only ever shows while trying it out. */}
        {policy.boilerplate && (
          <Notice variant="warning">
            Template text: edit <Code>apps/web/content/policy/{name}.md</Code>{" "}
            and remove its <Code>boilerplate</Code> line before publishing.
          </Notice>
        )}
        <PolicyDocument blocks={policy.blocks} />
        <PolicyLinks />
      </article>
    </AuthLayout>
  );
}
// The document's blocks, drawn with our components. Every string is text,
// which React escapes: nothing here is ever inserted as HTML.
export function PolicyDocument({ blocks }: { blocks: Block[] }) {
  return (
    <div className="space-y-4">
      {blocks.map((block, i) => (
        <BlockView key={i} block={block} />
      ))}
    </div>
  );
}
const listClasses =
  "space-y-2 pl-6 text-base/6 text-zinc-500 marker:text-zinc-400 sm:text-sm/6 dark:text-zinc-400";
function BlockView({ block }: { block: Block }) {
  switch (block.type) {
    case "heading":
      return (
        <Subheading
          level={block.level}
          id={block.id}
          className={block.level === 2 ? "pt-4" : "pt-2"}
        >
          <Inlines items={block.children} />
        </Subheading>
      );
    case "paragraph":
      return (
        <Text>
          <Inlines items={block.children} />
        </Text>
      );
    case "list": {
      const List = block.ordered ? "ol" : "ul";
      return (
        <List
          className={`${listClasses} ${block.ordered ? "list-decimal" : "list-disc"}`}
        >
          {block.items.map((item, i) => (
            <li key={i} className="space-y-2">
              {item.map((child, j) => (
                <ItemBlock key={j} block={child} />
              ))}
            </li>
          ))}
        </List>
      );
    }
    case "table":
      return (
        <Table dense>
          <TableHead>
            <TableRow>
              {block.header.map((cell, i) => (
                <TableHeader key={i}>
                  <Inlines items={cell} />
                </TableHeader>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {block.rows.map((row, i) => (
              <TableRow key={i}>
                {row.map((cell, j) => (
                  <TableCell key={j}>
                    {/* Long cells wrap, unlike the app's lists. */}
                    <div className="min-w-32 whitespace-normal">
                      <Inlines items={cell} />
                    </div>
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      );
    case "rule":
      return <Divider />;
  }
}
// A list item's text is inline, not a paragraph of its own.
function ItemBlock({ block }: { block: Block }) {
  if (block.type === "paragraph") return <Inlines items={block.children} />;
  return <BlockView block={block} />;
}
function Inlines({ items }: { items: Inline[] }) {
  return (
    <>
      {items.map((item, i) => (
        <Fragment key={i}>{inline(item)}</Fragment>
      ))}
    </>
  );
}
function inline(item: Inline): ReactNode {
  switch (item.type) {
    case "text":
      return item.text;
    case "strong":
      return (
        <Strong>
          <Inlines items={item.children} />
        </Strong>
      );
    case "em":
      return (
        <em>
          <Inlines items={item.children} />
        </em>
      );
    case "code":
      return <Code>{item.text}</Code>;
    case "link":
      return (
        <TextLink href={item.href}>
          <Inlines items={item.children} />
        </TextLink>
      );
    case "break":
      return <br />;
  }
}
