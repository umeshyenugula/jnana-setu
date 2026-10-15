import { describe, expect, it } from "vitest";
import { clearSessionId, loadSessionId, saveSessionId, sessionKey } from "./session";

function memoryStorage(): Storage {
  const map = new Map<string, string>();
  return {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
    clear: () => map.clear(),
    key: (i: number) => [...map.keys()][i] ?? null,
    get length() {
      return map.size;
    },
  };
}

describe("session storage", () => {
  it("scopes sessions per widget and validates shape", () => {
    const storage = memoryStorage();
    expect(loadSessionId("wdg_1", storage)).toBeNull();
    saveSessionId("wdg_1", "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", storage);
    expect(loadSessionId("wdg_1", storage)).toBe("ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    expect(loadSessionId("wdg_2", storage)).toBeNull();
    expect(sessionKey("wdg_1")).not.toBe(sessionKey("wdg_2"));
  });

  it("rejects malformed stored values", () => {
    const storage = memoryStorage();
    storage.setItem(sessionKey("wdg_1"), "attacker-controlled");
    expect(loadSessionId("wdg_1", storage)).toBeNull();
  });

  it("clears sessions", () => {
    const storage = memoryStorage();
    saveSessionId("wdg_1", "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", storage);
    clearSessionId("wdg_1", storage);
    expect(loadSessionId("wdg_1", storage)).toBeNull();
  });
});
