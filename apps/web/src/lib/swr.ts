import { SWRConfig, type SWRConfiguration } from "swr";
import { ApiError } from "./api";
// SWR's settings for the whole app (root.tsx). A failed read is retried
// twice, unless the API refused it (4xx): a 403 or 404 won't change, and
// retrying a 429 would spend more of the rate limit. The error and its Retry
// button stay on screen instead.
export const swrConfig: SWRConfiguration = {
  errorRetryCount: 2,
  revalidateOnFocus: true,
  onErrorRetry(error, key, config, revalidate, options) {
    if (error instanceof ApiError && error.status >= 400 && error.status < 500)
      return;
    SWRConfig.defaultValue.onErrorRetry(
      error,
      key,
      config,
      revalidate,
      options,
    );
  },
};
