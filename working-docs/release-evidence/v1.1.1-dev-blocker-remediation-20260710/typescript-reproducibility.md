# TypeScript client reproducibility

- Result: pass
- Package: `@shunter/client` version `1.1.1-dev`
- Compiler: TypeScript `5.9.3`, selected from the package-local locked dependency
- Public publishing: not attempted and remains out of scope

The package manifest pins `typescript: 5.9.3`, the npm lockfile records the
same exact package and integrity, and build/test scripts invoke the installed
`tsc` rather than resolving an unconstrained current package.

Two consecutive `rtk npm --prefix typescript/client run build` commands
passed. After each run, `rtk git status --short -- typescript/client/dist`
was empty and `rtk git diff --exit-code -- typescript/client/dist` exited
zero.

Hashes before and after both builds were identical:

```text
60e049047c1921c2e893436287481c3d79593f1ef7f2b5603f308ed41478f204  index.js
b42a6ecd99e8802f0b2f8e013d88521d8da4fe5430e4ad27591ea29805d67437  index.js.map
8509522421d29e5153b978597b0458ab2cb84201559c039d23a2b1bd0f308a24  index.d.ts
a6d7800c1e85e45f1b11d958968128b927ea8587a884d0cc2e89c9862688b6fb  index.d.ts.map
```

Package test, dry-run pack, and packed-install smoke gates also passed. See the
adjacent TypeScript command logs.
