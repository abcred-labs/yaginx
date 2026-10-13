import { describe, expect, it } from "vitest";
import { explainRequestStatus } from "./requestStatusExplanation";

describe("request status explanations", () => {
  it("explains a successful upstream response from the user's perspective", () => {
    const explanation = explainRequestStatus({
      statusCode: 200,
      upstreamStatusCode: 200,
      statusSource: "Upstream"
    }, "final");

    expect(explanation.title).toBe("最终状态 200：成功");
    expect(explanation.conclusion).toContain("由上游服务返回");
    expect(explanation.sourceLabel).toBe("上游返回");
  });

  it("explains redirects and upstream server errors without treating them as gateway failures", () => {
    const redirect = explainRequestStatus({ statusCode: 302, statusSource: "YaginxPipeline" }, "final");
    const serverError = explainRequestStatus({
      statusCode: 502,
      upstreamStatusCode: 502,
      statusSource: "Upstream",
      errorCategory: "UpstreamReturnedError"
    }, "final");

    expect(redirect.meaning).toContain("暂时位于其他地址");
    expect(serverError.impact).toContain("上游");
    expect(serverError.categoryLabel).toBe("上游原始错误");
  });

  it("identifies an intentional maintenance response without blaming the upstream", () => {
    const explanation = explainRequestStatus({
      statusCode: 503,
      statusSource: "YaginxPipeline",
      errorCategory: "SiteMaintenance"
    }, "final");

    expect(explanation.title).toBe("最终状态 503：站点维护中");
    expect(explanation.conclusion).toContain("维护模式");
    expect(explanation.impact).toContain("未进入静态响应");
    expect(explanation.categoryLabel).toBe("站点维护中");
  });

  it("explains every Yaginx reserved final status as a diagnostic rather than standard HTTP", () => {
    const expectedLabels: Record<number, string> = {
      601: "客户端已取消",
      602: "上游超时",
      603: "无可用上游",
      604: "上游请求失败",
      605: "上游响应头失败",
      606: "上游响应体失败",
      607: "上游请求体失败",
      608: "向客户端写入失败",
      609: "代理管线异常",
      610: "未分类代理异常"
    };

    for (const [status, label] of Object.entries(expectedLabels)) {
      const explanation = explainRequestStatus({
        statusCode: Number(status),
        statusSource: Number(status) === 601 ? "ClientCanceled" : "YaginxProxy"
      }, "final");

      expect(explanation.title).toContain(label);
      expect(explanation.notice).toContain("保留状态");
      expect(explanation.actions.length).toBeGreaterThan(0);
    }
  });

  it("does not confuse an upstream reserved number with a Yaginx diagnostic status", () => {
    const explanation = explainRequestStatus({
      statusCode: 502,
      upstreamStatusCode: 604,
      statusSource: "Upstream"
    }, "upstream");

    expect(explanation.title).toContain("上游自定义状态");
    expect(explanation.notice).toContain("最终状态");
    expect(explanation.meaning).toContain("标准 HTTP 未定义");
  });

  it("explains a missing upstream status using the recorded proxy source", () => {
    const explanation = explainRequestStatus({
      statusCode: 604,
      upstreamStatusCode: null,
      statusSource: "YaginxProxy",
      errorCategory: "UpstreamRequestFailure"
    }, "upstream");

    expect(explanation.status).toBeNull();
    expect(explanation.conclusion).toContain("取得上游响应前");
    expect(explanation.categoryLabel).toBe("上游请求失败");
  });

  it("uses a safe explanation for unknown custom status codes", () => {
    const explanation = explainRequestStatus({ statusCode: 777, statusSource: "Unknown" }, "final");

    expect(explanation.title).toContain("非标准状态");
    expect(explanation.meaning).toContain("不会猜测");
  });
});
