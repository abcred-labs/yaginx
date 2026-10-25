import { describe, expect, it } from "vitest";
import { certificateDaysLeft, certificateTone } from "./hostCertificate";

const NOW = Date.UTC(2026, 9, 25);
const inDays = (days: number) => (NOW + days * 86_400_000) / 1000;

describe("certificate remaining days", () => {
  it("floors to whole days and goes negative once expired", () => {
    expect(certificateDaysLeft(inDays(45), NOW)).toBe(45);
    expect(certificateDaysLeft(inDays(44.9), NOW)).toBe(44);
    expect(certificateDaysLeft(inDays(-0.5), NOW)).toBe(-1);
  });

  it("maps days to tone at the 45 and 15 day boundaries", () => {
    expect(certificateTone(45)).toBe("success");
    expect(certificateTone(44)).toBe("warning");
    expect(certificateTone(15)).toBe("warning");
    expect(certificateTone(14)).toBe("danger");
    expect(certificateTone(-3)).toBe("danger");
  });
});
