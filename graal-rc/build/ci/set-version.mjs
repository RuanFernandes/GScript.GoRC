import fs from "node:fs";
import path from "node:path";

const version = (process.argv[2] ?? "").replace(/^v/, "");
if (!/^\d+\.\d+\.\d+$/.test(version)) {
  throw new Error(`Version "${version}" must contain exactly three numeric components.`);
}

const repositoryRoot = process.env.GRAAL_RC_ROOT
  ? path.resolve(process.env.GRAAL_RC_ROOT)
  : process.cwd();

function read(filePath) {
  return fs.readFileSync(path.join(repositoryRoot, filePath), "utf8");
}

function write(filePath, content, original = content) {
  const eol = original.includes("\r\n") ? "\r\n" : "\n";
  const normalized = content.replace(/\r?\n/g, eol);
  fs.writeFileSync(path.join(repositoryRoot, filePath), normalized.endsWith(eol) ? normalized : `${normalized}${eol}`);
}

function replaceOnce(filePath, pattern, replacement) {
  const original = read(filePath);
  if (!pattern.test(original)) {
    throw new Error(`Could not update version in ${filePath}`);
  }
  const updated = original.replace(pattern, replacement);
  if (updated !== original) {
    write(filePath, updated, original);
  }
}

write("VERSION", `${version}\n`, read("VERSION"));
replaceOnce(
  "build/config.yml",
  /(^  version:\s*")[^"]+("\s+# The application version$)/m,
  `$1${version}$2`,
);
replaceOnce(
  "build/linux/nfpm/nfpm.yaml",
  /(^version:\s*")[^"]+(")/m,
  `$1${version}$2`,
);
replaceOnce(
  "build/windows/wails.exe.manifest",
  /(name="com\.rauanf\.graalrc"\s+version=")[^"]+(")/,
  `$1${version}$2`,
);
replaceOnce(
  "frontend/src/lib/appVersion.ts",
  /(APP_VERSION\s*=\s*")[^"]+(")/,
  `$1${version}$2`,
);
replaceOnce(
  "internal/graalscript/server.go",
  /(ServerInfo:\s*ServerInfo\{Name:\s*"graalscript-lsp",\s*Version:\s*")[^"]+(")/,
  `$1${version}$2`,
);

const windowsInfoPath = "build/windows/info.json";
const windowsInfoSource = read(windowsInfoPath);
const windowsInfo = JSON.parse(windowsInfoSource);
windowsInfo.fixed.file_version = version;
windowsInfo.info["0000"].ProductVersion = version;
write(windowsInfoPath, JSON.stringify(windowsInfo, null, "\t") + "\n", windowsInfoSource);

for (const plist of ["build/darwin/Info.plist", "build/darwin/Info.dev.plist", "build/ios/Info.plist"]) {
  replaceOnce(
    plist,
    /(<key>CFBundleShortVersionString<\/key>\s*<string>)[^<]+(<\/string>)/,
    `$1${version}$2`,
  );
  replaceOnce(
    plist,
    /(<key>CFBundleVersion<\/key>\s*<string>)[^<]+(<\/string>)/,
    `$1${version}$2`,
  );
}

replaceOnce(
  "build/ios/Info.dev.plist",
  /(<key>CFBundleShortVersionString<\/key>\s*<string>)[^<]+(<\/string>)/,
  `$1${version}-dev$2`,
);

for (const msixManifest of ["build/windows/msix/template.xml", "build/windows/msix/app_manifest.xml"]) {
  replaceOnce(msixManifest, /(Version=")[^".]+\.[^".]+\.[^".]+\.[^".]+(")/, `$1${version}.0$2`);
}

console.log(`Updated Graal RC build metadata to ${version}`);
