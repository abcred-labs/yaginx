import type { Int64, WebsiteUpsertInput } from "@/api/website";
import { normalizeProxyTransforms } from "./proxyTransforms";

export type WebsiteUpdateValues = WebsiteUpsertInput;

// The route identifies the website being edited; form state must not choose it.
export const websiteUpdatePayload = (id: Int64, values: WebsiteUpdateValues): WebsiteUpdateValues => {
  const { defaultHost, defaultDestination, defaultDestinationHost, ...current } = values;
  void defaultHost;
  const upstreamPools = [...(values.upstreamPools ?? [])];
  const proxyRules = (values.proxyRules ?? []).map((rule) => {
    const { targetType, ...storedRule } = rule;
    if (targetType === 'direct') {
      return { ...storedRule, upstreamPoolId: undefined };
    }
    if (targetType === 'pool') {
      return { ...storedRule, destination: undefined, destinationHost: undefined };
    }
    return storedRule;
  });
  const legacyDestination = typeof defaultDestination === 'string' ? defaultDestination.trim() : '';
  if (legacyDestination) {
    if (!upstreamPools.some((pool) => pool.id === 'legacy-default')) {
      upstreamPools.push({
        id: 'legacy-default',
        name: '兼容默认上游',
        strategy: 'weightedRoundRobin',
        endpoints: [{
          address: defaultDestination as string,
          host: defaultDestinationHost ?? undefined,
          weight: 1,
          enabled: true,
        }],
      });
    }
    if (!proxyRules.some((rule) => rule.isEnabled && rule.isFallback)) {
      proxyRules.push({ isEnabled: true, isFallback: true, upstreamPoolId: 'legacy-default' });
    }
  }
  const ipAccessCIDRs = values.ipAccessCIDRs?.map((value) => value.trim()).filter(Boolean);
  return {
    ...current,
    id,
    ...(values.expectedUpdatedAt ? { expectedUpdatedAt: values.expectedUpdatedAt } : {}),
    ...(values.ipAccessCIDRs !== undefined ? { ipAccessCIDRs } : {}),
    responseHeaderTimeout: values.responseHeaderTimeout?.trim() || undefined,
    ...(values.proxyTransforms !== undefined && values.proxyTransforms !== null ? { proxyTransforms: normalizeProxyTransforms(values.proxyTransforms) } : {}),
    ...(values.upstreamPools !== undefined || legacyDestination ? { upstreamPools } : {}),
    ...(values.proxyRules !== undefined || legacyDestination ? { proxyRules } : {}),
    ...(values.requestHistory ? { requestHistory: values.requestHistory.mode === 'all'
      ? values.requestHistory
      : { mode: values.requestHistory.mode } } : {}),
  };
};
