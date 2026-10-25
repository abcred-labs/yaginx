const DAY_MS = 24 * 60 * 60 * 1000;

export type CertificateTone = "success" | "warning" | "danger";

/** 距过期的整天数（向下取整）；已过期时为负数。notAfter 为 Unix 秒。 */
export const certificateDaysLeft = (notAfter: number, now = Date.now()) => Math.floor((notAfter * 1000 - now) / DAY_MS);

/** 剩余 >=45 天绿色，>=15 天黄色，其余（含已过期）红色。 */
export const certificateTone = (daysLeft: number): CertificateTone => daysLeft >= 45 ? "success" : daysLeft >= 15 ? "warning" : "danger";
