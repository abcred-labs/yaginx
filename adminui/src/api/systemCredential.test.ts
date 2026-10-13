import { beforeEach, describe, expect, it, vi } from "vitest";
import { FetchJson } from "@/requests/FetchJson";
import { createAPICredential, listAPICredentials, resetAPICredential, revokeAPICredential } from "./systemCredential";

vi.mock("@/requests/FetchJson", () => ({ FetchJson: vi.fn() }));

describe("system credential API", () => {
  beforeEach(() => vi.mocked(FetchJson).mockReset());

  it("uses the generic credential lifecycle endpoints", async () => {
    vi.mocked(FetchJson).mockResolvedValue({ code: 200 } as never);
    await listAPICredentials();
    await createAPICredential({ name: "prod", purpose: "mcp" });
    await resetAPICredential("42");
    await revokeAPICredential("42");
    expect(FetchJson).toHaveBeenNthCalledWith(1, { url: "/api/system_credentials", method: "GET" });
    expect(FetchJson).toHaveBeenNthCalledWith(2, { url: "/api/system_credentials", method: "POST", data: { name: "prod", purpose: "mcp" } });
    expect(FetchJson).toHaveBeenNthCalledWith(3, { url: "/api/system_credentials/42/reset", method: "POST" });
    expect(FetchJson).toHaveBeenNthCalledWith(4, { url: "/api/system_credentials/42/revoke", method: "POST" });
  });
});
