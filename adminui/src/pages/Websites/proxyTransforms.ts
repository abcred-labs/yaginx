import type { KeyValueString } from "@/api/website";

// Keys the gateway data plane understands.
export const proxyTransformKeys = [
  'PathPrefix', 'PathRemovePrefix', 'PathSet', 'QueryValueParameter',
  'RequestHeader', 'RequestHeadersCopy', 'ResponseHeader', 'ResponseHeadersCopy',
  'ResponseLocationHost', 'ResponseTrailer', 'ResponseTrailersCopy',
  'Forwarded', 'X-Forwarded', 'Set', 'Append', 'Remove', 'When', 'Action',
];

// The redirect-host rewrite is a primary key (host to match) plus the shared Set (replacement).
export const locationRewritePreset: KeyValueString[] = [
  { key: 'ResponseLocationHost', value: '' },
  { key: 'Set', value: '' },
];

export const normalizeProxyTransforms = (items: Array<Partial<KeyValueString>> | null | undefined): KeyValueString[] | undefined => {
  if (!items) return undefined;
  return items
    .map((item) => ({ key: (item?.key ?? '').trim(), value: (item?.value ?? '').trim() }))
    .filter((item) => item.key !== '');
};

export const hasDuplicateTransformKey = (items: Array<Partial<KeyValueString>> | null | undefined, key: string, index: number) => {
  const wanted = key.trim().toLowerCase();
  return !!wanted && (items ?? []).some((item, itemIndex) => itemIndex !== index && (item?.key ?? '').trim().toLowerCase() === wanted);
};
