# TypeScript client reproducibility

- Result: pass
- Package: `@shunter/client` version `1.1.1-dev`
- Compiler: package-local TypeScript `5.9.3`
- Dependency install: `rtk npm --prefix typescript/client ci` passed from the checked-in lockfile
- Public publishing: not attempted and remains out of scope

The manifest pins TypeScript 5.9.3, the lockfile resolves exactly 5.9.3, and
`typescript/client/node_modules/.bin/tsc --version` reported `Version 5.9.3`.

The four hashes before and after the required build were identical:

```text
60e049047c1921c2e893436287481c3d79593f1ef7f2b5603f308ed41478f204  typescript/client/dist/index.js
b42a6ecd99e8802f0b2f8e013d88521d8da4fe5430e4ad27591ea29805d67437  typescript/client/dist/index.js.map
8509522421d29e5153b978597b0458ab2cb84201559c039d23a2b1bd0f308a24  typescript/client/dist/index.d.ts
a6d7800c1e85e45f1b11d958968128b927ea8587a884d0cc2e89c9862688b6fb  typescript/client/dist/index.d.ts.map
```

After the build, `rtk git status --short -- typescript/client/dist` was
empty and `rtk git diff --exit-code -- typescript/client/dist` exited zero.
No source-map or other `dist` drift occurred. Package test, dry-run pack, and
packed-install smoke gates also passed.

