import { describe, expect, it } from "vitest";
import { domainViewPath } from "./domainViewRoute";
import { websiteViewPath } from "./websiteViewRoute";

describe("domain resource-view routes", () => {
  it("encodes canonical Host paths", () => {
    expect(domainViewPath("api.example.test:8443")).toBe("/resource/hosts/view/api.example.test%3A8443");
  });

  it("preserves large website IDs as URL text", () => {
    expect(websiteViewPath("2066836992053874700")).toBe("/website/view/2066836992053874700");
  });
});
