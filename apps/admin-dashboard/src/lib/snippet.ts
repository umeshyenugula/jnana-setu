// Embed-snippet builder for the widget settings page. The public widget ID
// is not a secret; the snippet only points the loader at the configured
// widget host origin and API origin. data-api-base is required: without it
// the iframe would call the static widget host instead of the API.
export interface SnippetOptions {
  widgetHost: string;
  widgetId: string;
  apiBase: string;
}

export function buildEmbedSnippet(options: SnippetOptions): string {
  const host = options.widgetHost.replace(/\/+$/, "");
  const id = options.widgetId.replace(/"/g, "");
  const api = options.apiBase.replace(/\/+$/, "").replace(/"/g, "");
  return `<script src="${host}/loader.js" data-widget-id="${id}" data-api-base="${api}" defer></script>`;
}

export function widgetAppUrl(widgetHost: string, widgetId: string, apiBase: string): string {
  const params = new URLSearchParams({
    widgetId,
    apiBase,
  });
  return `${widgetHost.replace(/\/+$/, "")}/?${params.toString()}`;
}
