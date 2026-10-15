import { describe, expect, it } from "vitest";
import { buildEmbedSnippet, widgetAppUrl } from "./snippet";

describe("embed snippet", () => {
  it("builds a loader snippet with the public widget ID and API base", () => {
    const snippet = buildEmbedSnippet({ widgetHost: "https://widget.example.com/", widgetId: "wdg_abc", apiBase: "https://api.example.com/" });
    expect(snippet).toBe(
      '<script src="https://widget.example.com/loader.js" data-widget-id="wdg_abc" data-api-base="https://api.example.com" defer></script>',
    );
  });

  it("strips attribute-breaking quotes from the widget ID", () => {
    const snippet = buildEmbedSnippet({ widgetHost: "https://w.example.com", widgetId: 'a"b', apiBase: "https://api.example.com" });
    expect(snippet).not.toContain('"b');
    expect(snippet).toContain("data-widget-id=");
    expect(snippet).toContain("data-api-base=");
  });

  it("builds the iframe URL with encoded parameters", () => {
    const url = widgetAppUrl("https://w.example.com/", "wdg_1", "http://localhost:8080");
    expect(url).toContain("widgetId=wdg_1");
    expect(url).toContain("apiBase=http%3A%2F%2Flocalhost%3A8080");
  });
});
