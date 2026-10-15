// Fails if frontend sources or built bundles contain privileged secrets or
// unsafe rendering primitives. Run after builds: `npm run security`.
// Only public configuration (VITE_API_BASE_URL, anon key names) may appear.
//
// Secret patterns apply to src/ and dist/ (nothing secret may ship).
// Sink patterns (innerHTML/eval) apply to src/ only: the bundled React DOM
// runtime legitimately contains innerHTML, while our code must never use it.
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { join, extname } from "node:path";

const srcRoots = ["src"].filter((d) => existsSync(d));
const allRoots = ["src", "dist"].filter((d) => existsSync(d));
const extensions = new Set([".ts", ".tsx", ".js", ".mjs", ".cjs", ".html"]);

const checks = [
  { roots: allRoots, name: "service-role key assignment", pattern: /SERVICE_ROLE_KEY\s*=\s*['"][^'"]+['"]/ },
  { roots: allRoots, name: "service_role reference", pattern: /service_role/i },
  { roots: allRoots, name: "embedded JWT", pattern: /eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{10,}/ },
  { roots: allRoots, name: "internal service token value", pattern: /INTERNAL_SERVICE_TOKEN['"]?\s*[:=]\s*['"][A-Za-z0-9_-]{8,}/ },
  { roots: allRoots, name: "internal runtime URL", pattern: /localhost:808[12]/ },
  { roots: allRoots, name: "llama.cpp reference", pattern: /llama\.cpp|LLAMA_CPP/i },
  { roots: srcRoots, name: "dangerous HTML sink", pattern: /dangerouslySetInnerHTML|\.innerHTML\s*=/ },
  { roots: srcRoots, name: "eval", pattern: /\beval\s*\(/ },
];

function* files(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "node_modules") continue;
      yield* files(full);
    } else if (extensions.has(extname(entry.name))) {
      yield full;
    }
  }
}

let failures = 0;
for (const { roots, name, pattern } of checks) {
  for (const root of roots) {
    for (const file of files(root)) {
      const text = readFileSync(file, "utf8");
      if (pattern.test(text)) {
        console.error(`FORBIDDEN [${name}]: ${file}`);
        failures += 1;
      }
    }
  }
}
if (failures > 0) {
  console.error(`${failures} forbidden pattern(s) found.`);
  process.exit(1);
}
console.log("security scan passed: no privileged secrets or unsafe sinks found.");
