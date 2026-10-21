import { describe, expect, it } from 'vitest';
import { columnFiltersToRequest } from './ServerDataTable';

describe('columnFiltersToRequest', () => {
  it('flattens header filters by column id', () => {
    expect(columnFiltersToRequest([{ id: 'name', value: 'a.com' }, { id: 'status', value: ['running'] }]))
      .toEqual({ name: 'a.com', status: ['running'] });
  });

  it('returns an empty request without filters', () => {
    expect(columnFiltersToRequest([])).toEqual({});
  });
});
