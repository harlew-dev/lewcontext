import { Button as BaseButton, type ButtonProps } from "baseui/button";
export function Button({ children, ...props }: ButtonProps) {
  return (
    <BaseButton
      size="compact"
      {...props}
      disabled={props.disabled || props.isLoading}
      aria-busy={props.isLoading || undefined}
      overrides={{
        BaseButton: {
          style: {
            fontSize: "13px",
            fontWeight: 600,
            paddingTop: "10px",
            paddingBottom: "10px",
            paddingLeft: "14px",
            paddingRight: "14px",
            gap: "7px",
            lineHeight: "18px",
          },
        },
        ...props.overrides,
      }}
    >
      {children}
    </BaseButton>
  );
}
