import { brotliCompressSync } from "node:zlib";
import { gzipSync, zlibSync } from "fflate";
import { describe, expect, it } from "vitest";
import {
  canDecompressResponseBody,
  decompressResponseBody,
  getResponseContentEncodings
} from "./responseBodyDecoder";

const base64 = (value: Uint8Array) => Buffer.from(value).toString("base64");
const source = new TextEncoder().encode('{"message":"compressed response"}');

describe("response body decoder", () => {
  it("finds Content-Encoding values without regard to header casing", () => {
    const encodings = getResponseContentEncodings({ "content-ENCODING": [" gzip, BR "] });

    expect(encodings).toEqual(["gzip", "br"]);
    expect(canDecompressResponseBody(encodings)).toBe(true);
  });

  it.each([
    ["gzip", gzipSync(source)],
    ["deflate", zlibSync(source)],
    ["br", new Uint8Array(brotliCompressSync(source))]
  ])("decompresses %s response bodies", async (encoding, compressed) => {
    await expect(decompressResponseBody(base64(compressed), [encoding])).resolves.toEqual({
      ok: true,
      text: '{"message":"compressed response"}'
    });
  });

  it("decompresses multiple encodings in reverse order", async () => {
    const compressed = new Uint8Array(brotliCompressSync(gzipSync(source)));

    await expect(decompressResponseBody(base64(compressed), ["gzip", "br"])).resolves.toEqual({
      ok: true,
      text: '{"message":"compressed response"}'
    });
  });

  it("rejects unsupported encodings and invalid or truncated payloads", async () => {
    expect(canDecompressResponseBody(["zstd"])).toBe(false);
    await expect(decompressResponseBody(base64(gzipSync(source)), ["zstd"])).resolves.toMatchObject({ ok: false });
    await expect(decompressResponseBody("not-base64", ["gzip"])).resolves.toMatchObject({ ok: false });
    await expect(decompressResponseBody(base64(gzipSync(source).slice(0, -1)), ["gzip"])).resolves.toMatchObject({ ok: false });
  });
});
