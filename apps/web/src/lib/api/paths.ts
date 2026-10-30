// A path to an API GET that also carries the type of its response, so SWR keys
// and route loaders know what they load. The field exists only for the type
// checker; at runtime an ApiPath is a plain string.
export type ApiPath<T> = string & { readonly response?: T };
// Brands a path with its response type. Only resource files (lib/api/*) call
// this, next to the endpoint the type describes.
export function path<T>(value: string): ApiPath<T> {
  return value as ApiPath<T>;
}
