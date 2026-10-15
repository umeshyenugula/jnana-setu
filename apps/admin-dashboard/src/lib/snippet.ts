// Embed-snippet builder for the widget settings page. The public widget ID
// is not a secret; the snippet only points the loader at the configured
// widget host origin.
export interface SnippetOptions {
  widgetHost: string;
  widgetId: string;
}

export function buildEmbedSnippet(options: SnippetOptions): string {
  const host = options.widgetHost.replace(/\/+$/, "");
  const id = options.widgetId.replace(/"/g, "");
  return `<script src="${host}/loader.js" data-widget-id="${id}" defer></script>`;
}

export function widgetAppUrl(widgetHost: string, widgetId: string, apiBase: string): string {
  const params = new URLSearchParams({
    widgetId,
    apiBase,
  });
  return `${widgetHost.replace(/\/+$/, "")}/?${params.toString()}`;
}
