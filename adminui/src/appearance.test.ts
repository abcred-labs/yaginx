import { describe, expect, it } from 'vitest';
import { buildTheme, chartPalette } from './appearance';

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

describe('buildTheme', () => {
  it('configures a legible light sidebar menu', () => {
    const menu = buildTheme('light').components?.Menu;
    expect(menu?.itemBg).toBe('#ffffff');
    expect(menu?.itemColor).toBe('#172033');
    expect(menu?.itemSelectedBg).toBe('#e6efff');
  });

  it('configures the dark sidebar menu with distinct active states', () => {
    const menu = buildTheme('dark').components?.Menu;
    expect(menu?.darkItemBg).toBe('#0b1220');
    expect(menu?.darkItemHoverBg).toBe('#142842');
    expect(menu?.darkItemSelectedBg).toBe('#173b68');
  });
});
