const isIPv4 = (value: string) => {
  const octets = value.split(".");
  return octets.length === 4 && octets.every((octet) => /^\d+$/.test(octet) && Number(octet) <= 255);
};

// The Gateway validates before persisting. This only prevents malformed
// upstream profile data from exposing a destructive UI action.
export const actionableProfileNetworkCIDR = (value?: string | null): string | null => {
  const cidr = value?.trim();
  if (!cidr) {
    return null;
  }
  const [address, prefix, ...extra] = cidr.split("/");
  if (!address || !prefix || extra.length > 0 || !/^\d+$/.test(prefix)) {
    return null;
  }
  const prefixLength = Number(prefix);
  if (isIPv4(address)) {
    return prefixLength <= 32 ? cidr : null;
  }
  return address.includes(":") && /^[0-9a-fA-F:.]+$/.test(address) && prefixLength <= 128 ? cidr : null;
};
