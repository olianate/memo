import { describe, expect, it, vi } from "vitest";
import { reportError, setErrorSink } from "./errorLog";
import type { ErrorReport } from "../types";

describe("errorLog", () => {
  it("buffers reports until a sink is available and hides tokens", () => {
    setErrorSink(null);
    reportError("preview failed token=ntn_1234567890abcdef", { action: "preview" }, "unit");

    const reports: ErrorReport[] = [];
    const sink = vi.fn(async (report: ErrorReport) => {
      reports.push(report);
    });
    setErrorSink(sink);

    expect(reports).toHaveLength(1);
    expect(reports[0].source).toBe("unit");
    expect(reports[0].message).not.toContain("ntn_");
    expect(reports[0].message).toContain("[已隐藏]");
    expect(reports[0].context).toContain("preview");

    setErrorSink(null);
  });

  it("deduplicates the same error in a short window", () => {
    const sink = vi.fn(async () => {});
    setErrorSink(sink);

    reportError("duplicate boom", { action: "dedupe" }, "unit-dedupe");
    reportError("duplicate boom", { action: "dedupe" }, "unit-dedupe");

    expect(sink).toHaveBeenCalledTimes(1);
    setErrorSink(null);
  });
});
