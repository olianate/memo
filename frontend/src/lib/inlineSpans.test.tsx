import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { InlineSpans, safeLinkUrl } from "./markdown";

describe("links", () => {
  it("accepts web, mail and bare-domain links", () => {
    expect(safeLinkUrl("https://example.com/a?b=1")).toBe("https://example.com/a?b=1");
    expect(safeLinkUrl("mailto:hi@example.com")).toBe("mailto:hi@example.com");
    expect(safeLinkUrl("example.com/docs")).toBe("https://example.com/docs");
    expect(safeLinkUrl("javascript:alert(1)")).toBeNull();
  });

  it("calls the open handler when a rendered link is clicked", () => {
    const onOpenLink = vi.fn();
    render(
      <InlineSpans
        spans={[{ text: "example", link: "https://example.com" }]}
        onOpenLink={onOpenLink}
      />,
    );
    fireEvent.click(screen.getByText("example"));
    expect(onOpenLink).toHaveBeenCalledWith("https://example.com");
  });
});
