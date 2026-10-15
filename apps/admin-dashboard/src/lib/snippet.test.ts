import { describe, expect, it } from "vitest";
import { buildEmbedSnippet, widgetAppUrl } from "./snippet";

describe("embed snippet", () => {
  it("builds a loader snippet with the public widget ID", () => {
    const snippet = buildEmbedSnippet({ widgetHost: "https://widget.example.com/", widgetId: "wdg_abc" });
    expect(snippet).toBe(
      '<script src="https://widget.example.com/loader.js" data-widget-id="wdg_abc" defer></script>',
    );
  });

  it("strips attribute-breaking quotes from the widget ID", () => {
    const snippet = buildEmbedSnippet({ widgetHost: "https://w.example.com", widgetId: 'a"b' });
    expect(snippet).not.toContain('"b');
    expect(snippet).toContain("data-widget-id=");
  });

  it("builds the iframe URL with encoded parameters", () => {
    const url = widgetAppUrl("https://w.example.com/", "wdg_1", "http://localhost:8080");
    expect(url).toContain("widgetId=wdg_1");
    expect(url).toContain("apiBase=http%3A%2F%2Flocalhost%3A8080");
  });
});
