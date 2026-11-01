import type { EmailBlockLink } from "@/types/email-blocks";
import type { Http } from "./http";
// The block list, reached only through a block link: every call sends the
// link's token in the body, never in the URL.
export const emailBlockRoutes = {
  check: () => "/api/email-blocks/check",
  store: () => "/api/email-blocks",
  destroy: () => "/api/email-blocks",
};
// api.emailBlocks(): the block link's page. A dead link is an ApiError with
// field "token"; blocking an address an account uses is a 409.
export function emailBlocks(http: Http) {
  return {
    check: (token: string) =>
      http.send<EmailBlockLink>("POST", emailBlockRoutes.check(), { token }),
    // Blocks the link's address (already blocked is fine too).
    store: (token: string) =>
      http.send<void>("POST", emailBlockRoutes.store(), { token }),
    // Unblocks it (not blocked is fine too).
    destroy: (token: string) =>
      http.send<void>("DELETE", emailBlockRoutes.destroy(), { token }),
  };
}
