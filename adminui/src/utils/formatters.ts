const maximumFractionDigits = 4;

const decimalFormatter = new Intl.NumberFormat("zh-CN", {
  maximumFractionDigits
});

const integerFormatter = new Intl.NumberFormat("zh-CN", {
  maximumFractionDigits: 0
});

export const toNumber = (value: number | string | undefined | null): number => {
  if (value === undefined || value === null || value === "") {
    return 0;
  }

  const parsedValue = Number(value);
  return Number.isFinite(parsedValue) ? parsedValue : 0;
};

export const formatNumber = (value: number | string | undefined | null): string =>
  decimalFormatter.format(toNumber(value));

export const formatPercent = (value: number | string | undefined | null): string =>
  `${formatNumber(toNumber(value) * 100)}%`;

export const formatBytes = (value: number | string | undefined | null): string => {
  const numericValue = toNumber(value);
  if (numericValue <= 0) {
    return "0 B";
  }

  const units = ["B", "KB", "MB", "GB", "TB"];
  let currentValue = numericValue;
  let unitIndex = 0;
  while (currentValue >= 1024 && unitIndex < units.length - 1) {
    currentValue /= 1024;
    unitIndex += 1;
  }

  if (unitIndex === 0) {
    return `${integerFormatter.format(currentValue)} ${units[unitIndex]}`;
  }
  return `${formatNumber(currentValue)} ${units[unitIndex]}`;
};

export const formatAxisNumber = (value: number): string => {
  if (Math.abs(value) >= 1000000) {
    return `${formatNumber(value / 1000000)}M`;
  }
  if (Math.abs(value) >= 1000) {
    return `${formatNumber(value / 1000)}K`;
  }
  return integerFormatter.format(value);
};
