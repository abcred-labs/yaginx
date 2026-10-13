import { describe, expect, it } from "vitest";
import { buildMetricBarOption } from "./analyticsChartOptions";

describe("buildMetricBarOption", () => {
  it("renders metric bars from largest to smallest, starting at the top", () => {
    const option = buildMetricBarOption("Requests", [
      { type: "small", value: 3 },
      { type: "largest", value: 12 },
      { type: "middle", value: 7 }
    ], "#f59e0b");

    expect(option.yAxis).toMatchObject({
      type: "category",
      inverse: true,
      data: ["largest", "middle", "small"]
    });
    expect(option.series).toMatchObject([{
      name: "Requests",
      data: [12, 7, 3]
    }]);
  });
});
