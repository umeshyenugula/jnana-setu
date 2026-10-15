import { ChatWidget } from "./components/ChatWidget";

function param(name: string): string | null {
  return new URLSearchParams(window.location.search).get(name);
}

export default function App() {
  const widgetId = (param("widgetId") || "").trim();
  const apiBase = (param("apiBase") || "").trim() || window.location.origin;

  if (!widgetId || widgetId.length > 128) {
    return (
      <main className="jnana-root">
        <p role="alert" className="jnana-error">
          This chat widget is missing its configuration. Please check the embed snippet.
        </p>
      </main>
    );
  }

  // The iframe is same-origin with the loader only when served from the
  // widget host; the loader passes its own origin explicitly. document.referrer
  // identifies the embedding page for message validation.
  const hostOrigin = document.referrer ? new URL(document.referrer).origin : window.location.origin;

  return (
    <main className="jnana-root">
      <ChatWidget widgetId={widgetId} apiBase={apiBase} hostOrigin={hostOrigin} />
    </main>
  );
}
