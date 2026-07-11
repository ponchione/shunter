# Clean-ref qualification preflight

- Qualification start (UTC): 2026-07-10T14:08:23Z
- Shunter candidate: `bfef461409e9158b53ad4dc96dc956ca1598fe6a`
- opsboard-canary candidate: `fedcbb6de9eabb539e561e814e9687f27ddb4fe6`
- Initial decision: pass; both candidate refs and worktrees meet the required clean initial state

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk whoami`
- Exit code: 0

```text
gernsback
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk uname -a`
- Exit code: 0

```text
Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk uname -m`
- Exit code: 0

```text
x86_64
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk lscpu`
- Exit code: 0

```text
Architecture:                            x86_64
CPU op-mode(s):                          32-bit, 64-bit
Address sizes:                           48 bits physical, 48 bits virtual
Byte Order:                              Little Endian
CPU(s):                                  24
On-line CPU(s) list:                     0-23
Vendor ID:                               AuthenticAMD
Model name:                              AMD Ryzen 9 9900X 12-Core Processor
CPU family:                              26
Model:                                   68
Thread(s) per core:                      2
Core(s) per socket:                      12
Socket(s):                               1
Stepping:                                0
Frequency boost:                         enabled
CPU(s) scaling MHz:                      61%
CPU max MHz:                             5662.0161
CPU min MHz:                             613.9540
BogoMIPS:                                8799.98
Flags:                                   fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm constant_tsc rep_good amd_lbr_v2 nopl xtopology nonstop_tsc cpuid extd_apicid aperfmperf rapl pni pclmulqdq monitor ssse3 fma cx16 sse4_1 sse4_2 movbe popcnt aes xsave avx f16c rdrand lahf_lm cmp_legacy svm extapic cr8_legacy abm sse4a misalignsse 3dnowprefetch osvw ibs skinit wdt tce topoext perfctr_core perfctr_nb bpext perfctr_llc mwaitx cpuid_fault cpb cat_l3 cdp_l3 hw_pstate ssbd mba perfmon_v2 ibrs ibpb stibp ibrs_enhanced vmmcall fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid cqm rdt_a avx512f avx512dq rdseed adx smap avx512ifma clflushopt clwb avx512cd sha_ni avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves cqm_llc cqm_occup_llc cqm_mbm_total cqm_mbm_local user_shstk avx_vnni avx512_bf16 clzero irperf xsaveerptr rdpru wbnoinvd cppc arat npt lbrv svm_lock nrip_save tsc_scale vmcb_clean flushbyasid decodeassists pausefilter pfthreshold avic v_vmsave_vmload vgif x2avic v_spec_ctrl vnmi avx512vbmi umip pku ospke avx512_vbmi2 gfni vaes vpclmulqdq avx512_vnni avx512_bitalg avx512_vpopcntdq rdpid bus_lock_detect movdiri movdir64b overflow_recov succor smca fsrm avx512_vp2intersect flush_l1d amd_lbr_pmc_freeze
Virtualization:                          AMD-V
L1d cache:                               576 KiB (12 instances)
L1i cache:                               384 KiB (12 instances)
L2 cache:                                12 MiB (12 instances)
L3 cache:                                64 MiB (2 instances)
NUMA node(s):                            1
NUMA node0 CPU(s):                       0-23
Vulnerability Gather data sampling:      Not affected
Vulnerability Ghostwrite:                Not affected
Vulnerability Indirect target selection: Not affected
Vulnerability Itlb multihit:             Not affected
Vulnerability L1tf:                      Not affected
Vulnerability Mds:                       Not affected
Vulnerability Meltdown:                  Not affected
Vulnerability Mmio stale data:           Not affected
Vulnerability Old microcode:             Not affected
Vulnerability Reg file data sampling:    Not affected
Vulnerability Retbleed:                  Not affected
Vulnerability Spec rstack overflow:      Mitigation; IBPB on VMEXIT only
Vulnerability Spec store bypass:         Mitigation; Speculative Store Bypass disabled via prctl
Vulnerability Spectre v1:                Mitigation; usercopy/swapgs barriers and __user pointer sanitization
Vulnerability Spectre v2:                Mitigation; Enhanced / Automatic IBRS; IBPB conditional; STIBP always-on; PBRSB-eIBRS Not affected; BHI Not affected
Vulnerability Srbds:                     Not affected
Vulnerability Tsa:                       Not affected
Vulnerability Tsx async abort:           Not affected
Vulnerability Vmscape:                   Mitigation; IBPB on VMEXIT
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk go version`
- Exit code: 0

```text
go version go1.26.3 linux/amd64
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk node --version`
- Exit code: 0

```text
v24.4.1
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk npm --version`
- Exit code: 0

```text
11.17.0
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git status --short --branch`
- Exit code: 0

```text
## main...origin/main [ahead 3]

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git rev-parse HEAD`
- Exit code: 0

```text
bfef461409e9158b53ad4dc96dc956ca1598fe6a
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git diff --stat`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git diff --name-status`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git diff`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git diff --cached`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git ls-files --others --exclude-standard`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git log --oneline --decorate -8`
- Exit code: 0

```text
bfef461 (HEAD -> main) Remediate v1.1.1-dev qualification blockers
2515db0 Record failed v1.1.1-dev qualification
d7e3856 Document continued development recommendations
9fddcf0 (origin/main) Reduce one-off query helper duplication
b03d4b3 Simplify multi-join split-or fixtures
ad81c2c code reduction
8527a22 doc updates
d5c3bb3 test: factor repeated runtime test setup
```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git status --short --branch`
- Exit code: 0

```text
## master

```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git rev-parse HEAD`
- Exit code: 0

```text
fedcbb6de9eabb539e561e814e9687f27ddb4fe6
```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git diff --stat`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git diff --name-status`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git diff`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git diff --cached`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git ls-files --others --exclude-standard`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git log --oneline --decorate -8`
- Exit code: 0

```text
fedcbb6 (HEAD -> master) Refresh canary for current Shunter contracts
e69bce7 Refresh canary for current Shunter protocol
a45ec6f Cover app reducer validation recovery canary
86a4a22 Cover malformed reducer args recovery canary
2f0420d Cover missing reducer recovery canary
fbc85a8 Cover reducer permission recovery canary
471a70e Cover denied declared query recovery canary
bf4f3f1 Cover audit admission multi recovery canary
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk read VERSION`
- Exit code: 0

```text
v1.1.1-dev
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk node -e "const p=require('./typescript/client/package.json'); console.log(JSON.stringify({version:p.version,private:p.private,typescript:(p.devDependencies||{}).typescript||(p.dependencies||{}).typescript},null,2))"`
- Exit code: 0

```text
{
  "version": "1.1.1-dev",
  "private": true,
  "typescript": "5.9.3"
}
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk node -e "const p=require('./typescript/client/package-lock.json'); console.log(JSON.stringify({lockVersion:p.version,rootSpecifier:p.packages[''].devDependencies.typescript,resolvedVersion:p.packages['node_modules/typescript'].version},null,2))"`
- Exit code: 0

```text
{
  "lockVersion": "1.1.1-dev",
  "rootSpecifier": "5.9.3",
  "resolvedVersion": "5.9.3"
}
```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk find . -name '*.test'`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git status --short -- typescript/client/dist`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git diff --exit-code -- typescript/client/dist`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk node -e "const b=require('fs').readFileSync('examples/hosted-chat/frontend/src/generated/hosted_chat.ts'); console.log(JSON.stringify({size:b.length,lastBytes:[...b.subarray(-4)],canonicalTwoNewlineFooter:b.length>=2&&b[b.length-1]===10&&b[b.length-2]===10&&(b.length<3||b[b.length-3]!==10)},null,2))"`
- Exit code: 0

```text
{
  "size": 21664,
  "lastBytes": [
    10,
    125,
    10,
    10
  ],
  "canonicalTwoNewlineFooter": true
}
```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk find . -name '*.test'`
- Exit code: 0

```text

```

## Command

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk go list -m -json github.com/ponchione/shunter`
- Exit code: 0

```text
{
	"Path": "github.com/ponchione/shunter",
	"Version": "v0.0.0-00010101000000-000000000000",
	"Replace": {
		"Path": "../shunter",
		"Dir": "/home/gernsback/source/shunter",
		"GoMod": "/home/gernsback/source/shunter/go.mod",
		"GoVersion": "1.25.5"
	},
	"Dir": "/home/gernsback/source/shunter",
	"GoMod": "/home/gernsback/source/shunter/go.mod",
	"GoVersion": "1.25.5"
}
```


