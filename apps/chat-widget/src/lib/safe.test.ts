import { describe, expect, it } from "vitest";
import { escapeHtml, isSafeHttpUrl, sourceLabel, sourceLink } from "./safe";

describe("safe rendering", () => {
  it("escapes HTML special characters", () => {
    expect(escapeHtml('<script>alert("x")</script>')).toBe("&lt;script&gt;alert(&quot;x&quot;)&lt;/script&gt;");
    expect(escapeHtml("a'b&c")).toBe("a&#39;b&amp;c");
  });

  it("allows only http(s) URLs", () => {
    expect(isSafeHttpUrl("https://example.com/docs")).toBe(true);
    expect(isSafeHttpUrl("http://example.com")).toBe(true);
    expect(isSafeHttpUrl("javascript:alert(1)")).toBe(false);
    expect(isSafeHttpUrl("data:text/html,<h1>x</h1>")).toBe(false);
    expect(isSafeHttpUrl("ftp://example.com")).toBe(false);
    expect(isSafeHttpUrl("not a url")).toBe(false);
    expect(isSafeHttpUrl(undefined)).toBe(false);
    expect(isSafeHttpUrl(42)).toBe(false);
  });

  it("resolves source links only from verified metadata", () => {
    expect(sourceLink({ url: "https://example.com/help" })).toBe("https://example.com/help");
    expect(sourceLink({ link: "http://example.com/a" })).toBe("http://example.com/a");
    expect(sourceLink({ url: "javascript:alert(1)" })).toBeNull();
    expect(sourceLink({})).toBeNull();
    expect(sourceLink(undefined)).toBeNull();
  });

  it("labels sources safely with length bounds", () => {
    expect(sourceLabel({ title: "  Help Center  " }, "fallback")).toBe("Help Center");
    expect(sourceLabel({ section: "Billing" }, "fallback")).toBe("Billing");
    expect(sourceLabel({}, "fallback")).toBe("fallback");
    expect(sourceLabel({ title: "x".repeat(200) }, "fallback")).toHaveLength(80);
    expect(sourceLabel({ title: 42 }, "fallback")).toBe("fallback");
  });
});
