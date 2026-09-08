# Representative request timelines

Frozen selection: first, median and maximum dispatch latency within each run's >=p99 cohort. All times below are wall-clock milliseconds relative to the same request's dispatch; negative intended-arrival offsets expose scheduling lateness. Raw wall and in-process monotonic nanoseconds are in `correlation.json` and each diagnostic's `timelines.json`. Socket-write completion can overlap receipt; it is not a delivery acknowledgment. Maximum observed timestamp-pair mapping spread was 44.760 microseconds.

## 1000-even-diagnostic

- first: client 25, ordinal 97, request 197, connection `724f23b1292f816c686f42dd9f35a7bb`; dispatch 0.454375 ms, intended-arrival 1.189483 ms.
- median: client 1, ordinal 20, request 120, connection `5a966a75ec06a6ab36693dbe4af7572f`; dispatch 0.726088 ms, intended-arrival 0.735843 ms.
- maximum: client 14, ordinal 34, request 134, connection `98ff2729e897d363985c7a1974b59195`; dispatch 1.820019 ms, intended-arrival 1.872685 ms.

| Timestamp | First | Median | Maximum |
|---|---:|---:|---:|
| intended | -0.735108 | -0.009755 | -0.052666 |
| dispatch | 0.000000 | 0.000000 | 0.000000 |
| request-write-start | 0.007260 | 0.002541 | 1.671547 |
| request-write-done | 0.057131 | 0.014791 | 1.684618 |
| request-write-return | 0.057441 | 0.014881 | 1.684768 |
| handler-entry | 0.081281 | 0.418815 | 1.702858 |
| query-ready | 0.178842 | 0.660027 | 1.767158 |
| rows-encoded | 0.181992 | 0.661997 | 1.768838 |
| envelope-start | 0.183552 | 0.663578 | 1.770208 |
| envelope-ready | 0.186042 | 0.666598 | 1.772628 |
| enqueue | 0.186472 | 0.666928 | 1.772918 |
| dequeue | 0.189712 | 0.670008 | 1.774808 |
| socket-write-start | 0.190992 | 0.671908 | 1.776058 |
| socket-write-done | 0.199452 | 0.687938 | 1.792229 |
| frame-received | 0.419465 | 0.707498 | 1.799939 |
| outer-decoded | 0.422025 | 0.709238 | 1.801929 |
| route-entry | 0.422675 | 0.709418 | 1.802119 |
| pending-enqueue | 0.422935 | 0.709608 | 1.802309 |
| pending-dequeue | 0.439745 | 0.713948 | 1.804569 |
| benchmark-receipt | 0.439835 | 0.714068 | 1.804709 |
| handoff | 0.441815 | 0.716308 | 1.805719 |
| decode-start | 0.442145 | 0.716658 | 1.806049 |
| decode-done | 0.446265 | 0.720988 | 1.811659 |
| validation-done | 0.454375 | 0.726088 | 1.820019 |

## 4000-burst-diagnostic

- first: client 31, ordinal 36, request 136, connection `b06d4193d309c608e753a14b71a2f101`; dispatch 1.250904 ms, intended-arrival 16.562263 ms.
- median: client 2, ordinal 10, request 110, connection `47639a8a7b88dc1d689c31a9564b1b9b`; dispatch 2.897431 ms, intended-arrival 14.714332 ms.
- maximum: client 12, ordinal 11, request 111, connection `3168a322599f809719fa5836bbb4b563`; dispatch 9.269839 ms, intended-arrival 13.191431 ms.

| Timestamp | First | Median | Maximum |
|---|---:|---:|---:|
| intended | -15.311359 | -11.816901 | -3.921592 |
| dispatch | 0.000000 | 0.000000 | 0.000000 |
| request-write-start | 0.001520 | 0.002690 | 0.001011 |
| request-write-done | 0.009531 | 0.012091 | 0.008241 |
| request-write-return | 0.009851 | 0.012241 | 0.008301 |
| handler-entry | 0.029301 | 0.024511 | 0.023251 |
| query-ready | 0.267983 | 0.260663 | 0.139212 |
| rows-encoded | 0.269993 | 0.262593 | 0.140052 |
| envelope-start | 0.270973 | 0.263993 | 0.140632 |
| envelope-ready | 0.273053 | 0.267133 | 0.141522 |
| enqueue | 0.273363 | 0.267573 | 0.141762 |
| dequeue | 0.276253 | 0.271603 | 0.155422 |
| socket-write-start | 0.277993 | 0.273323 | 0.155822 |
| socket-write-done | 0.290563 | 0.281813 | 0.158302 |
| frame-received | 1.236193 | 0.510386 | 6.544690 |
| outer-decoded | 1.237084 | 0.511806 | 7.312618 |
| route-entry | 1.237224 | 0.511936 | 7.312918 |
| pending-enqueue | 1.237424 | 0.512116 | 7.313098 |
| pending-dequeue | 1.240824 | 0.515326 | 8.684352 |
| benchmark-receipt | 1.240934 | 0.515506 | 8.684482 |
| handoff | 1.242324 | 0.521356 | 8.791044 |
| decode-start | 1.242494 | 0.535166 | 8.791254 |
| decode-done | 1.247074 | 2.892721 | 9.265629 |
| validation-done | 1.250904 | 2.897431 | 9.269839 |

## 4000-even-diagnostic

- first: client 19, ordinal 78, request 178, connection `a2f7e06435b5d2265ce56fb59fe97a73`; dispatch 1.174562 ms, intended-arrival 32.645078 ms.
- median: client 27, ordinal 80, request 180, connection `e5fcd5417d33ca6c76330274c7c57e30`; dispatch 1.501986 ms, intended-arrival 16.363876 ms.
- maximum: client 1, ordinal 81, request 181, connection `58adfe08b2d75b5481806ded665db984`; dispatch 2.787929 ms, intended-arrival 14.883356 ms.

| Timestamp | First | Median | Maximum |
|---|---:|---:|---:|
| intended | -31.470516 | -14.861890 | -12.095427 |
| dispatch | 0.000000 | 0.000000 | 0.000000 |
| request-write-start | 0.001670 | 0.001580 | 0.001240 |
| request-write-done | 0.008720 | 0.007731 | 0.006790 |
| request-write-return | 0.008860 | 0.007861 | 0.006850 |
| handler-entry | 0.276523 | 0.018041 | 1.122992 |
| query-ready | 0.323503 | 0.330764 | 1.318084 |
| rows-encoded | 0.324373 | 0.334304 | 1.319924 |
| envelope-start | 0.324933 | 0.335814 | 1.321024 |
| envelope-ready | 0.325883 | 0.339704 | 1.322764 |
| enqueue | 0.326183 | 0.340104 | 1.323164 |
| dequeue | 0.662697 | 0.344254 | 1.326224 |
| socket-write-start | 0.663167 | 0.347174 | 1.328064 |
| socket-write-done | 0.666197 | 0.367404 | 1.339364 |
| frame-received | 1.135962 | 1.452826 | 2.713259 |
| outer-decoded | 1.137202 | 1.469606 | 2.715929 |
| route-entry | 1.137322 | 1.470516 | 2.731859 |
| pending-enqueue | 1.137422 | 1.470856 | 2.732079 |
| pending-dequeue | 1.161752 | 1.479516 | 2.768979 |
| benchmark-receipt | 1.161822 | 1.479786 | 2.769089 |
| handoff | 1.163292 | 1.483736 | 2.773019 |
| decode-start | 1.163482 | 1.485016 | 2.773649 |
| decode-done | 1.167842 | 1.495246 | 2.779329 |
| validation-done | 1.174562 | 1.501986 | 2.787929 |

## 1000-burst-diagnostic

- first: client 5, ordinal 77, request 177, connection `4e1efc3594c2d261b317bbd0615d1055`; dispatch 1.542077 ms, intended-arrival 1.753213 ms.
- median: client 11, ordinal 46, request 146, connection `a67a7178632a76c8874b7e3434a12fd9`; dispatch 3.257985 ms, intended-arrival 4.321647 ms.
- maximum: client 1, ordinal 46, request 146, connection `b3eedb804e1ea0873d4d3041ed8697cb`; dispatch 3.629698 ms, intended-arrival 4.361447 ms.

| Timestamp | First | Median | Maximum |
|---|---:|---:|---:|
| intended | -0.211136 | -1.063662 | -0.731749 |
| dispatch | 0.000000 | 0.000000 | 0.000000 |
| request-write-start | 0.010230 | 0.001450 | 0.002320 |
| request-write-done | 0.068351 | 0.013570 | 0.015600 |
| request-write-return | 0.068451 | 0.013640 | 0.015680 |
| handler-entry | 1.352965 | 0.271613 | 0.541125 |
| query-ready | 1.383625 | 0.372384 | 0.694617 |
| rows-encoded | 1.393505 | 0.375604 | 0.696367 |
| envelope-start | 1.394295 | 0.377864 | 0.697757 |
| envelope-ready | 1.395025 | 0.380764 | 0.699737 |
| enqueue | 1.395275 | 0.381124 | 0.699997 |
| dequeue | 1.397895 | 0.385224 | 0.703717 |
| socket-write-start | 1.398635 | 0.390024 | 0.704787 |
| socket-write-done | 1.412135 | 0.424595 | 0.716387 |
| frame-received | 1.525756 | 3.219844 | 3.600548 |
| outer-decoded | 1.526376 | 3.225974 | 3.601338 |
| route-entry | 1.526606 | 3.226074 | 3.615378 |
| pending-enqueue | 1.526786 | 3.226254 | 3.615538 |
| pending-dequeue | 1.529646 | 3.242395 | 3.619018 |
| benchmark-receipt | 1.529926 | 3.242655 | 3.619138 |
| handoff | 1.532187 | 3.245065 | 3.621238 |
| decode-start | 1.532327 | 3.245215 | 3.621398 |
| decode-done | 1.536097 | 3.251815 | 3.623688 |
| validation-done | 1.542077 | 3.257985 | 3.629698 |

