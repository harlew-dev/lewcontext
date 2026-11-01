// The CAPTCHA widgets ChallengeDialog can show, one adapter per provider the
// API may name in a 428 (internal/captcha). Turnstile is the only one; no
// npm dependency, just its script, loaded the first time a challenge opens
// (the page CSP allows its origin only while the server has a provider).

export type WidgetOptions = {
  siteKey: string;
  action: string;
  theme: "light" | "dark" | "auto";
  onToken(token: string): void;
  onError(message: string): void;
  onExpire(): void;
};
export type CaptchaWidget = {
  // The provider's script, loaded once.
  script: string;
  // Renders the widget into `el` (the script loaded) and returns what
  // removes it.
  render(el: HTMLElement, options: WidgetOptions): () => void;
};

type Turnstile = {
  render(el: HTMLElement, options: Record<string, unknown>): string;
  remove(id: string): void;
};
declare global {
  interface Window {
    turnstile?: Turnstile;
  }
}

export const providers: Record<string, CaptchaWidget> = {
  turnstile: {
    script:
      "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit",
    render(el, options) {
      const turnstile = window.turnstile;
      if (!turnstile) throw new Error("Turnstile didn't load.");
      // Usually invisible: it shows itself only when Cloudflare wants an
      // interaction.
      const id = turnstile.render(el, {
        sitekey: options.siteKey,
        action: options.action,
        theme: options.theme,
        appearance: "interaction-only",
        callback: options.onToken,
        "error-callback": () =>
          options.onError("The check couldn't be completed."),
        "expired-callback": options.onExpire,
      });
      return () => turnstile.remove(id);
    },
  },
};

// Scripts loaded or loading, by URL: a page loads each one once. Not user
// data, only whether a third party's code is already on the page.
const loaded = new Map<string, Promise<void>>();

// Loads a provider's script once; a failure (offline, a blocker) can be
// tried again.
export function loadScript(src: string): Promise<void> {
  const known = loaded.get(src);
  if (known) return known;
  const loading = new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = src;
    script.async = true;
    script.onload = () => resolve();
    script.onerror = () => {
      script.remove();
      loaded.delete(src);
      reject(new Error("The check's script didn't load."));
    };
    document.head.appendChild(script);
  });
  loaded.set(src, loading);
  return loading;
}
