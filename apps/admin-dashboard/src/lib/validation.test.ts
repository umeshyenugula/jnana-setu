import { describe, expect, it } from "vitest";
import { validateOriginEntry, validateTextInput, validateUploadFile, validateWidgetForm } from "./validation";

function fakeFile(name: string, size: number): File {
  return { name, size } as File;
}

describe("upload validation", () => {
  it("accepts supported types within limits", () => {
    for (const name of ["a.txt", "b.MD", "c.pdf", "d.docx"]) {
      expect(validateUploadFile(fakeFile(name, 100))).toBeNull();
    }
  });

  it("rejects unsupported, empty, and oversized files", () => {
    expect(validateUploadFile(fakeFile("evil.exe", 100))).toContain("Unsupported");
    expect(validateUploadFile(fakeFile("a.txt", 0))).toContain("empty");
    expect(validateUploadFile(fakeFile("a.pdf", 11 * 1024 * 1024))).toContain("10 MB");
  });

  it("validates text input bounds", () => {
    expect(validateTextInput("", "x")).toContain("title");
    expect(validateTextInput("t", "   ")).toContain("Content");
    expect(validateTextInput("t", "ok")).toBeNull();
  });
});

describe("widget origin validation", () => {
  it("accepts bare http(s) origins", () => {
    expect(validateOriginEntry("https://example.com")).toBeNull();
    expect(validateOriginEntry("http://localhost:5500")).toBeNull();
  });

  it("rejects paths, queries, fragments, and schemes", () => {
    expect(validateOriginEntry("https://example.com/app")).toContain("bare origin");
    expect(validateOriginEntry("https://example.com?q=1")).toContain("bare origin");
    expect(validateOriginEntry("ftp://example.com")).toContain("http or https");
    expect(validateOriginEntry("not a url")).toContain("valid URL");
  });

  it("validates the whole widget form", () => {
    expect(validateWidgetForm("", ["https://a.com"])).toContain("display name");
    expect(validateWidgetForm("Chat", [])).toContain("allowed origin");
    expect(validateWidgetForm("Chat", ["https://a.com", "https://a.com"])).toContain("Duplicate");
    expect(validateWidgetForm("Chat", ["https://a.com"])).toBeNull();
  });
});
