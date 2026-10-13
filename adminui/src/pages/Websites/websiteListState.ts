import type { Int64, IWebsiteBatchDeleteResult, IWebsiteSearchRequest } from "@/api/website";

export const websiteListOperationPath = (operation: "analytics" | "edit" | "childRules", id: Int64): string => {
	const routeID = encodeURIComponent(id);
  switch (operation) {
    case "analytics":
      return `../${routeID}/analytics`;
    case "edit":
      return `../${routeID}`;
    case "childRules":
      return `../${routeID}/routes`;
  }
};

export const buildWebsiteSearchRequest = (values: IWebsiteSearchRequest): IWebsiteSearchRequest => {
  const domain = values.domain?.trim();
  const upstream = values.upstream?.trim();
  return {
    ...(domain ? { domain } : {}),
    ...(upstream ? { upstream } : {}),
    ...(typeof values.hasCertificate === "boolean" ? { hasCertificate: values.hasCertificate } : {}),
  };
};

export const websiteBatchDeleteResultText = (result: IWebsiteBatchDeleteResult): string => {
  const deleted = result.deletedIds?.length ?? 0;
  const notFound = result.notFoundIds?.length ?? 0;
  return notFound > 0 ? `已删除 ${deleted} 个站点，${notFound} 个站点不存在。` : `已删除 ${deleted} 个站点。`;
};
