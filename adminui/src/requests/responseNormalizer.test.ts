import { describe, expect, it } from "vitest";
import { isFirstPartyApiUrl, normalizeApiResponse } from "./responseNormalizer";

describe("API response normalization", () => {
  it("preserves the v2 website response used by the edit form", () => {
    expect(normalizeApiResponse({
      code: 200,
      data: {
        id: "1783552257170739200",
        defaultDestination: "http://198.51.100.22:10030",
        redirectHttpToHttps: true,
        proxyRules: [{
          pathPattern: "/api",
          isEnabled: true,
          extraHeaders: [{ key: "X-Proxy-Target", value: "v2" }]
        }],
        simpleResponses: [{
          path: "/healthz",
          statusCode: 200,
          extraHeaders: [{ key: "Content-Type", value: "text/plain" }]
        }]
      }
    })).toEqual({
      code: 200,
      data: {
        id: "1783552257170739200",
        defaultDestination: "http://198.51.100.22:10030",
        redirectHttpToHttps: true,
        proxyRules: [{ pathPattern: "/api", isEnabled: true, extraHeaders: [{ key: "X-Proxy-Target", value: "v2" }] }],
        simpleResponses: [{ path: "/healthz", statusCode: 200, extraHeaders: [{ key: "Content-Type", value: "text/plain" }] }],
      },
    });
  });

  it("preserves domain, SMTP, profile, and monitoring payload fields", () => {
    expect(normalizeApiResponse({
      data: {
        certSpec: { hasCertificate: false, notAfter: null },
        autoRequestCertificate: true,
        userName: "admin",
        fromEmail: "admin@yaginx.com",
        requestAt: 1785600000,
        durationMs: 18,
      }
    })).toEqual({
      data: {
        certSpec: { hasCertificate: false, notAfter: null },
        autoRequestCertificate: true,
        userName: "admin",
        fromEmail: "admin@yaginx.com",
        requestAt: 1785600000,
        durationMs: 18
      }
    });
  });

  it("preserves canonical fields, nulls, and header map keys", () => {
    expect(normalizeApiResponse({
      id: 2,
      requestHeaders: {
        "Content-Type": ["application/json"],
        Server: ["v2"]
      },
      responseHeaders: {
        "X-Request-Id": ["abc"]
      },
      nullValue: null
    })).toEqual({
      id: 2,
      requestHeaders: {
        "Content-Type": ["application/json"],
        Server: ["v2"]
      },
      responseHeaders: {
        "X-Request-Id": ["abc"]
      },
      nullValue: null
    });
  });

  it("only enables first-party API URLs", () => {
    expect(isFirstPartyApiUrl("/api/website/get")).toBe(true);
    expect(isFirstPartyApiUrl("/api/account/profile")).toBe(true);
    expect(isFirstPartyApiUrl("/webapi/central/api/v1/model_schemas/query")).toBe(false);
  });
});
