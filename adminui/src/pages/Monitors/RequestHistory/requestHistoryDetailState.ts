export type RequestHistoryDetailViewState = "idle" | "loading" | "missing" | "detail";

export const requestHistoryDetailViewState = (loading: boolean, missing: boolean, hasDetail: boolean): RequestHistoryDetailViewState => {
  if (loading) {
    return "loading";
  }
  if (missing) {
    return "missing";
  }
  return hasDetail ? "detail" : "idle";
};
