import type { Int64 } from "@/api/website";

export const websiteViewPath = (id: Int64): string => `/website/view/${encodeURIComponent(id)}`;
export const websiteEditPath = (id: Int64): string => `/website/${encodeURIComponent(id)}`;
