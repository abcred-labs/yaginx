import { describe, expect, it } from 'vitest';
import { buildModraTheme, chartPalette } from './appearance';

describe('chartPalette', () => {
  it('keeps flow, signal, and alert roles distinct in light mode', () => {
    const palette = chartPalette('light');
    expect(new Set([palette.flow, palette.signal, palette.alert]).size).toBe(3);
    expect(palette.grid).toBe('#dbe4ef');
  });

  it('uses a darker-surface palette with legible chart text', () => {
    const palette = chartPalette('dark');
    expect(palette.text).toBe('#d3ddec');
    expect(palette.grid).toBe('#2a3a53');
  });
});

describe('buildModraTheme', () => {
  it('uses the yaginx brand blue and a lighter variant in dark mode', () => {
    expect(buildModraTheme('light').color?.primary).toBe('#2575ff');
    expect(buildModraTheme('dark').color?.primary).toBe('#76a8ff');
  });

  it('keeps primary text legible on the primary surface', () => {
    expect(buildModraTheme('light').color?.primaryForeground).toBe('#ffffff');
    expect(buildModraTheme('dark').color?.primaryForeground).toBe('#0d1524');
  });
});
