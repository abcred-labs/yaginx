import { describe, expect, it } from "vitest";
import { actionableProfileNetworkCIDR } from "./ipProfileNetwork";

describe("actionableProfileNetworkCIDR", () => {
  it("accepts IPv4 and IPv6 CIDRs returned by the profile API", () => {
    expect(actionableProfileNetworkCIDR("74.248.0.0/15")).toBe("74.248.0.0/15");
    expect(actionableProfileNetworkCIDR("2001:db8::/32")).toBe("2001:db8::/32");
  });

  it("hides the network action for empty or malformed profile data", () => {
    expect(actionableProfileNetworkCIDR()).toBeNull();
    expect(actionableProfileNetworkCIDR("74.248.35.25")).toBeNull();
    expect(actionableProfileNetworkCIDR("74.248.0.0/33")).toBeNull();
    expect(actionableProfileNetworkCIDR("not-a-network/24")).toBeNull();
  });
});
