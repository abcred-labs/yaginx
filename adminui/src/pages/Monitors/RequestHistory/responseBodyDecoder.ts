import brotliPromise from "brotli-wasm";
import { gunzipSync, unzlibSync } from "fflate";

const supportedContentEncodings = new Set(["gzip", "br", "deflate"]);

export type SupportedContentEncoding = "gzip" | "br" | "deflate";

export type ResponseBodyDecodeResult =
  | { ok: true; text: string }
  | { ok: false; error: string };

export const getResponseContentEncodings = (headers: Record<string, string[]> | null | undefined): string[] => {
  const values = Object.entries(headers ?? {})
    .filter(([name]) => name.toLowerCase() === "content-encoding")
    .flatMap(([, headerValues]) => headerValues ?? []);

  return values
    .flatMap((value) => value.split(","))
    .map((value) => value.trim().toLowerCase())
    .filter((value) => value.length > 0 && value !== "identity");
};

export const canDecompressResponseBody = (encodings: string[]): encodings is SupportedContentEncoding[] => {
  return encodings.length > 0 && encodings.every((encoding) => supportedContentEncodings.has(encoding));
};

const decodeBase64 = (value: string): Uint8Array => {
  const normalized = value.replace(/\s/g, "");
  if (
    normalized.length === 0 ||
    normalized.length % 4 !== 0 ||
    !/^[A-Za-z0-9+/]*={0,2}$/.test(normalized)
  ) {
    throw new Error("invalid base64");
  }

  const binary = atob(normalized);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
};

export const decompressResponseBody = async (
  value: string,
  encodings: string[]
): Promise<ResponseBodyDecodeResult> => {
  if (!canDecompressResponseBody(encodings)) {
    return { ok: false, error: "Response Body 使用了不支持的 Content-Encoding，无法解压查看。" };
  }

  try {
    let bytes = decodeBase64(value);
    for (const encoding of [...encodings].reverse()) {
      switch (encoding) {
        case "gzip":
          bytes = gunzipSync(bytes);
          break;
        case "deflate":
          bytes = unzlibSync(bytes);
          break;
        case "br": {
          const brotli = await brotliPromise;
          bytes = brotli.decompress(bytes);
          break;
        }
      }
    }
    return { ok: true, text: new TextDecoder().decode(bytes) };
  } catch {
    return { ok: false, error: "无法解压 Response Body：压缩数据可能已截断、损坏或与 Content-Encoding 不匹配。" };
  }
};
