import { Text, TextLink } from "@/components/ui/text";
import { cx } from "@/lib/cx";
import { routes } from "@/lib/routes";
// "Terms of Service · Privacy Policy": on the policy pages, and below the
// homepage's, login's and register's card while the policies are published
// (usePolicies). `newTab` opens them in a new tab, so a form's typed fields
// survive reading them.
export function PolicyLinks({
  className,
  newTab = false,
}: {
  className?: string;
  newTab?: boolean;
}) {
  const target = newTab ? "_blank" : undefined;
  const rel = newTab ? "noopener" : undefined;
  return (
    <Text className={cx(className, "flex flex-wrap gap-x-2")}>
      <TextLink href={routes.termsOfService} target={target} rel={rel}>
        Terms of Service
      </TextLink>
      <span aria-hidden="true">·</span>
      <TextLink href={routes.privacyPolicy} target={target} rel={rel}>
        Privacy Policy
      </TextLink>
    </Text>
  );
}
