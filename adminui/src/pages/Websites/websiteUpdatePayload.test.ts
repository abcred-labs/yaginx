import { describe, expect, it } from 'vitest';
import { websiteUpdatePayload } from './websiteUpdatePayload';

describe('websiteUpdatePayload', () => {
  it('migrates legacy default upstream into a fallback rule before stripping old fields', () => {
    expect(websiteUpdatePayload('101', { name: 'site', defaultDestination: 'http://first.example', defaultDestinationHost: 'origin.example' })).toEqual({
      id: '101',
      name: 'site',
      upstreamPools: [{ id: 'legacy-default', name: '兼容默认上游', strategy: 'weightedRoundRobin', endpoints: [{ address: 'http://first.example', host: 'origin.example', weight: 1, enabled: true }] }],
      proxyRules: [{ isEnabled: true, isFallback: true, upstreamPoolId: 'legacy-default' }],
    });
  });

  it('does not duplicate an existing legacy pool or fallback', () => {
    const payload = websiteUpdatePayload('101', {
      name: 'site',
      defaultDestination: 'http://first.example',
      upstreamPools: [{ id: 'legacy-default', name: 'custom', strategy: 'random', endpoints: [] }],
      proxyRules: [{ isEnabled: true, isFallback: true, upstreamPoolId: 'custom' }],
    });
    expect(payload.upstreamPools).toHaveLength(1);
    expect(payload.proxyRules).toHaveLength(1);
  });

  it('serializes the selected rule target without conflicting destinations', () => {
    const payload = websiteUpdatePayload('101', {
      name: 'site',
      proxyRules: [
        { isEnabled: true, targetType: 'direct', destination: 'http://direct.example', upstreamPoolId: 'stale-pool' },
        { isEnabled: true, targetType: 'pool', destination: 'http://stale.example', upstreamPoolId: 'api-prod' },
      ],
    });
    expect(payload.proxyRules).toEqual([
      { isEnabled: true, destination: 'http://direct.example', upstreamPoolId: undefined },
      { isEnabled: true, destination: undefined, upstreamPoolId: 'api-prod' },
    ]);
  });

  it('uses the route ID instead of a stale form ID', () => {
    expect(websiteUpdatePayload('101', { id: '202', name: 'site' })).toEqual({
      id: '101',
      name: 'site',
    });
  });

  it('preserves the maintenance-mode setting selected in the workspace', () => {
    expect(websiteUpdatePayload('101', { name: 'site', isMaintenanceMode: true })).toEqual({
      id: '101',
      name: 'site',
      isMaintenanceMode: true,
    });
  });

  it('drops a stale sampling rate when the mode is not all', () => {
    expect(websiteUpdatePayload('101', {
      name: 'site',
      requestHistory: { mode: 'errorsOnly', successSamplingRate: 0.25 },
    })).toEqual({
      id: '101',
      name: 'site',
      requestHistory: { mode: 'errorsOnly' },
    });
  });

  it('keeps the sampling rate for all mode', () => {
    expect(websiteUpdatePayload('101', {
      name: 'site',
      requestHistory: { mode: 'all', successSamplingRate: 0.25 },
    })).toEqual({
      id: '101',
      name: 'site',
      requestHistory: { mode: 'all', successSamplingRate: 0.25 },
    });
  });
});
