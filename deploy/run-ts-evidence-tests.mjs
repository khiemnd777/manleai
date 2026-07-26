import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, isAbsolute, join, relative, resolve, sep } from "node:path";

const appRoot = realpathSync(process.cwd());
const testFiles = process.argv.slice(2).map(resolveTestFile);

if (testFiles.length === 0) {
  fail("At least one explicit TypeScript evidence test file is required.");
}

const compiler = join(
  appRoot,
  "node_modules",
  ".bin",
  process.platform === "win32" ? "tsc.cmd" : "tsc"
);
if (!existsSync(compiler)) {
  fail("The app-local TypeScript compiler is missing. Run npm ci first.");
}

const outputDirectory = mkdtempSync(join(tmpdir(), `manleai-${basename(appRoot)}-evidence-`));
try {
  run(compiler, [
    "--pretty", "false",
    "--target", "ES2020",
    "--module", "commonjs",
    "--moduleResolution", "node",
    "--esModuleInterop",
    "--strict",
    "--skipLibCheck",
    "--noEmitOnError",
    "--rootDir", appRoot,
    "--outDir", outputDirectory,
    ...testFiles
  ]);

  const emittedTests = testFiles.map((testFile) => {
    const emittedRelativePath = relative(appRoot, testFile).replace(/\.ts$/, ".js");
    const emittedPath = resolve(outputDirectory, emittedRelativePath);
    if (!existsSync(emittedPath)) {
      fail(`TypeScript did not emit the expected evidence test: ${emittedRelativePath}`);
    }
    return emittedPath;
  });
  run(process.execPath, ["--test", ...emittedTests]);
} finally {
  rmSync(outputDirectory, { recursive: true, force: false });
}

function resolveTestFile(candidate) {
  if (!candidate || isAbsolute(candidate)) {
    fail("Evidence test paths must be non-empty and relative to the app root.");
  }
  const resolved = resolve(appRoot, candidate);
  const relativePath = relative(appRoot, resolved);
  if (
    relativePath === "" ||
    relativePath === ".." ||
    relativePath.startsWith(`..${sep}`) ||
    !relativePath.endsWith(".test.ts")
  ) {
    fail(`Evidence test path is outside the app root or is not a .test.ts file: ${candidate}`);
  }
  if (!existsSync(resolved)) {
    fail(`Evidence test file does not exist: ${relativePath}`);
  }
  return resolved;
}

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: appRoot,
    encoding: "utf8",
    stdio: "inherit"
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    const signal = result.signal ? ` (signal ${result.signal})` : "";
    fail(`${basename(command)} failed${signal}.`);
  }
}

function fail(message) {
  throw new Error(message);
}
