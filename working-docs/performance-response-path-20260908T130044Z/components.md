# Component distributions for the same slow reads

Each cell contains 16 reads at or above its own frozen nearest-rank dispatch p99. Values are median / maximum milliseconds; p95 and p99 of these 16 both select the maximum. Full distributions for all reads and the >=p95 and >=p99 cohorts are in `correlation.json`. Same-process components use monotonic timestamps. Percentages divide sums of individual component durations by sums of the same reads' total durations; they are not sums or differences of percentiles.

## 1000-even-diagnostic

| Component | Median / max, ms | Share of cohort time |
|---|---:|---:|
| dispatch -> request-write-start | 0.004080 / 1.671547 | 15.23% |
| request-write-start -> handler-entry | 0.039591 / 1.085742 | 14.72% |
| handler-entry -> query-ready | 0.083751 / 0.261573 | 11.34% |
| query-ready -> rows-encoded | 0.001820 / 0.003310 | 0.24% |
| rows-encoded -> envelope-start | 0.001370 / 0.005080 | 0.18% |
| envelope-start -> envelope-ready | 0.002420 / 0.008750 | 0.33% |
| envelope-ready -> enqueue | 0.000330 / 0.003840 | 0.07% |
| enqueue -> dequeue | 0.003390 / 0.060040 | 1.12% |
| dequeue -> socket-write-start | 0.001340 / 0.006651 | 0.22% |
| socket-write-start -> frame-received | 0.228473 / 1.545716 | 43.50% |
| frame-received -> outer-decoded | 0.002570 / 0.005430 | 0.32% |
| outer-decoded -> route-entry | 0.000220 / 0.001740 | 0.04% |
| route-entry -> pending-enqueue | 0.000190 / 0.011540 | 0.11% |
| pending-enqueue -> pending-dequeue | 0.015560 / 0.400394 | 7.01% |
| pending-dequeue -> benchmark-receipt | 0.000170 / 0.000430 | 0.02% |
| benchmark-receipt -> handoff | 0.002220 / 0.063400 | 1.16% |
| handoff -> decode-start | 0.000430 / 0.044971 | 0.53% |
| decode-start -> decode-done | 0.006830 / 0.285003 | 3.03% |
| decode-done -> validation-done | 0.007420 / 0.011730 | 0.84% |

Overlapping intervals (excluded from the disjoint shares):

- server-socket-write: median 0.009920 ms; range 0.003670–0.042530 ms.
- client-request-write: median 0.014400 ms; range 0.007500–0.049881 ms.
- write-done-to-client-receipt-signed: median 0.220013 ms; range 0.007710–1.537676 ms.

## 4000-burst-diagnostic

| Component | Median / max, ms | Share of cohort time |
|---|---:|---:|
| dispatch -> request-write-start | 0.002110 / 2.368336 | 6.59% |
| request-write-start -> handler-entry | 0.039230 / 1.473825 | 15.29% |
| handler-entry -> query-ready | 0.080180 / 0.238672 | 2.99% |
| query-ready -> rows-encoded | 0.001140 / 0.003130 | 0.04% |
| rows-encoded -> envelope-start | 0.000770 / 0.001400 | 0.02% |
| envelope-start -> envelope-ready | 0.001220 / 0.003240 | 0.05% |
| envelope-ready -> enqueue | 0.000250 / 0.000450 | 0.01% |
| enqueue -> dequeue | 0.002890 / 0.013660 | 0.11% |
| dequeue -> socket-write-start | 0.000890 / 0.001860 | 0.03% |
| socket-write-start -> frame-received | 0.503145 / 6.388868 | 37.13% |
| frame-received -> outer-decoded | 0.004130 / 3.562278 | 14.31% |
| outer-decoded -> route-entry | 0.000200 / 0.019181 | 0.04% |
| route-entry -> pending-enqueue | 0.000150 / 0.000280 | 0.01% |
| pending-enqueue -> pending-dequeue | 0.010480 / 1.383135 | 9.79% |
| pending-dequeue -> benchmark-receipt | 0.000100 / 0.014811 | 0.03% |
| benchmark-receipt -> handoff | 0.002100 / 0.175382 | 0.67% |
| handoff -> decode-start | 0.000170 / 0.013810 | 0.04% |
| decode-start -> decode-done | 0.007550 / 2.357555 | 12.68% |
| decode-done -> validation-done | 0.004210 / 0.011410 | 0.15% |

Overlapping intervals (excluded from the disjoint shares):

- server-socket-write: median 0.007060 ms; range 0.002490–0.019190 ms.
- client-request-write: median 0.009401 ms; range 0.004140–1.364724 ms.
- write-done-to-client-receipt-signed: median 0.483965 ms; range 0.003280–6.386388 ms.

## 4000-even-diagnostic

| Component | Median / max, ms | Share of cohort time |
|---|---:|---:|
| dispatch -> request-write-start | 0.001840 / 0.005430 | 0.13% |
| request-write-start -> handler-entry | 0.065200 / 1.121752 | 15.12% |
| handler-entry -> query-ready | 0.159932 / 0.951370 | 14.14% |
| query-ready -> rows-encoded | 0.001820 / 0.003540 | 0.11% |
| rows-encoded -> envelope-start | 0.001350 / 0.002260 | 0.08% |
| envelope-start -> envelope-ready | 0.002020 / 0.003930 | 0.13% |
| envelope-ready -> enqueue | 0.000410 / 0.000990 | 0.03% |
| enqueue -> dequeue | 0.004130 / 0.528905 | 3.57% |
| dequeue -> socket-write-start | 0.001351 / 0.003290 | 0.10% |
| socket-write-start -> frame-received | 0.472795 / 1.706927 | 46.65% |
| frame-received -> outer-decoded | 0.003500 / 0.717708 | 8.43% |
| outer-decoded -> route-entry | 0.000170 / 0.015930 | 0.08% |
| route-entry -> pending-enqueue | 0.000170 / 0.000490 | 0.01% |
| pending-enqueue -> pending-dequeue | 0.010850 / 0.454885 | 4.62% |
| pending-dequeue -> benchmark-receipt | 0.000110 / 0.000690 | 0.01% |
| benchmark-receipt -> handoff | 0.003950 / 0.038640 | 0.54% |
| handoff -> decode-start | 0.000530 / 0.463535 | 1.87% |
| decode-start -> decode-done | 0.006380 / 0.526556 | 4.01% |
| decode-done -> validation-done | 0.006300 / 0.010190 | 0.37% |

Overlapping intervals (excluded from the disjoint shares):

- server-socket-write: median 0.008400 ms; range 0.003030–0.021481 ms.
- client-request-write: median 0.007760 ms; range 0.005480–0.029780 ms.
- write-done-to-client-receipt-signed: median 0.469765 ms; range 0.015060–1.698767 ms.

## 1000-burst-diagnostic

| Component | Median / max, ms | Share of cohort time |
|---|---:|---:|
| dispatch -> request-write-start | 0.002040 / 0.035981 | 0.24% |
| request-write-start -> handler-entry | 0.308764 / 1.342735 | 15.24% |
| handler-entry -> query-ready | 0.141102 / 1.299414 | 7.89% |
| query-ready -> rows-encoded | 0.002530 / 0.011071 | 0.12% |
| rows-encoded -> envelope-start | 0.001390 / 0.004170 | 0.06% |
| envelope-start -> envelope-ready | 0.002430 / 0.004620 | 0.08% |
| envelope-ready -> enqueue | 0.000360 / 0.003680 | 0.02% |
| enqueue -> dequeue | 0.003960 / 1.125351 | 4.64% |
| dequeue -> socket-write-start | 0.001340 / 0.221703 | 0.55% |
| socket-write-start -> frame-received | 2.784949 / 2.974282 | 70.04% |
| frame-received -> outer-decoded | 0.005270 / 0.014810 | 0.18% |
| outer-decoded -> route-entry | 0.000220 / 0.014030 | 0.04% |
| route-entry -> pending-enqueue | 0.000220 / 0.000750 | 0.01% |
| pending-enqueue -> pending-dequeue | 0.004040 / 0.016141 | 0.19% |
| pending-dequeue -> benchmark-receipt | 0.000230 / 0.000320 | 0.01% |
| benchmark-receipt -> handoff | 0.002261 / 0.006640 | 0.10% |
| handoff -> decode-start | 0.000170 / 0.000960 | 0.01% |
| decode-start -> decode-done | 0.006340 / 0.013801 | 0.25% |
| decode-done -> validation-done | 0.008360 / 0.013610 | 0.30% |

Overlapping intervals (excluded from the disjoint shares):

- server-socket-write: median 0.013510 ms; range 0.007520–0.035951 ms.
- client-request-write: median 0.012940 ms; range 0.008960–0.107601 ms.
- write-done-to-client-receipt-signed: median 2.767609 ms; range 0.004951–2.948711 ms.

