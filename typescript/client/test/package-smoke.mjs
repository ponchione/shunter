import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { cpSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const repoRoot = dirname(dirname(packageRoot));
const smokeRoot = join(packageRoot, ".tmp_package_smoke");

const run = (command, args, cwd) => {
  execFileSync(command, args, { cwd, stdio: "inherit" });
};

const fileDependency = (fromDir, toPath) =>
  `file:${relative(fromDir, toPath).replaceAll("\\", "/")}`;

const writeFixtureApp = (appRoot, dependency) => {
  mkdirSync(appRoot, { recursive: true });
  writeFileSync(
    join(appRoot, "package.json"),
    JSON.stringify(
      {
        private: true,
        type: "module",
        dependencies: {
          "@shunter/client": dependency,
        },
        devDependencies: {
          typescript: "^5.9.0",
        },
      },
      null,
      2,
    ) + "\n",
  );

  mkdirSync(join(appRoot, "generated"), { recursive: true });
  cpSync(
    join(repoRoot, "codegen", "testdata", "v1_module_contract.ts"),
    join(appRoot, "generated", "v1_module_contract.ts"),
  );

  mkdirSync(join(appRoot, "src"), { recursive: true });
  writeFileSync(
    join(appRoot, "src", "index.ts"),
    `import { SHUNTER_SUBPROTOCOL_V1, shunterProtocol as runtimeProtocol } from "@shunter/client";
import {
  reducers,
  shunterContract,
  shunterProtocol as generatedProtocol,
  type ReducerCaller,
} from "../generated/v1_module_contract";

const caller: ReducerCaller = async (_name, args) => args;
const protocol: typeof SHUNTER_SUBPROTOCOL_V1 = generatedProtocol.defaultSubprotocol;
const contractFormat: string = shunterContract.contractFormat;
const moduleName: string | undefined = shunterContract.moduleName;

await caller(reducers.createMessage, new Uint8Array());

console.log(runtimeProtocol.defaultSubprotocol, protocol, contractFormat, moduleName);
`,
  );

  writeFileSync(
    join(appRoot, "tsconfig.json"),
    JSON.stringify(
      {
        compilerOptions: {
          target: "ES2022",
          module: "ESNext",
          moduleResolution: "Bundler",
          lib: ["ES2022", "DOM"],
          strict: true,
          skipLibCheck: true,
          verbatimModuleSyntax: true,
        },
        include: ["src/**/*.ts", "generated/**/*.ts"],
      },
      null,
      2,
    ) + "\n",
  );
};

const verifyFixtureApp = (appRoot) => {
  run("npm", ["install", "--ignore-scripts", "--no-audit", "--no-fund"], appRoot);
  run("npm", ["exec", "--", "tsc", "-p", "tsconfig.json", "--noEmit"], appRoot);
  run(
    "node",
    [
      "--input-type=module",
      "--eval",
      "import('@shunter/client').then((sdk) => { if (sdk.shunterProtocol.defaultSubprotocol !== 'v1.bsatn.shunter') process.exit(1); })",
    ],
    appRoot,
  );
};

const writeWorkspaceClientPackage = (clientRoot) => {
  mkdirSync(clientRoot, { recursive: true });
  cpSync(join(packageRoot, "package.json"), join(clientRoot, "package.json"));
  cpSync(join(packageRoot, "README.md"), join(clientRoot, "README.md"));
  cpSync(join(packageRoot, "dist"), join(clientRoot, "dist"), { recursive: true });
};

rmSync(smokeRoot, { force: true, recursive: true });
mkdirSync(smokeRoot, { recursive: true });

run("npm", ["run", "build"], packageRoot);

const packOutput = execFileSync("npm", ["pack", "--pack-destination", smokeRoot], {
  cwd: packageRoot,
  encoding: "utf8",
});
const tarball = packOutput.trim().split(/\r?\n/).at(-1);
assert.ok(tarball, "npm pack should print the tarball name");

const tarballPath = join(smokeRoot, tarball);

const tarballAppRoot = join(smokeRoot, "tarball-app");
writeFixtureApp(tarballAppRoot, fileDependency(tarballAppRoot, tarballPath));
verifyFixtureApp(tarballAppRoot);

const fileAppRoot = join(smokeRoot, "file-app");
writeFixtureApp(fileAppRoot, fileDependency(fileAppRoot, packageRoot));
verifyFixtureApp(fileAppRoot);

const workspaceRoot = join(smokeRoot, "workspace");
const workspaceAppRoot = join(workspaceRoot, "app");
writeWorkspaceClientPackage(join(workspaceRoot, "client"));
writeFixtureApp(workspaceAppRoot, "1.0.0");
writeFileSync(
  join(workspaceRoot, "package.json"),
  JSON.stringify(
    {
      private: true,
      workspaces: ["client", "app"],
      devDependencies: {
        typescript: "^5.9.0",
      },
    },
    null,
    2,
  ) + "\n",
);
run("npm", ["install", "--ignore-scripts", "--no-audit", "--no-fund"], workspaceRoot);
run("npm", ["exec", "--", "tsc", "-p", "tsconfig.json", "--noEmit"], workspaceAppRoot);
run(
  "node",
  [
    "--input-type=module",
    "--eval",
    "import('@shunter/client').then((sdk) => { if (sdk.shunterProtocol.defaultSubprotocol !== 'v1.bsatn.shunter') process.exit(1); })",
  ],
  workspaceAppRoot,
);
