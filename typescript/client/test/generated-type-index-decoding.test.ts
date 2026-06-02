import {
  decodeRowList,
  encodeBsatnProduct,
} from "../src/index.js";
import type {
  BsatnColumn,
} from "../src/index.js";
import {
  decodeFlatValuesRow,
  tableRowDecoders,
} from "./fixtures/flat_type_index_canary.js";
import type {
  FlatValuesRow,
} from "./fixtures/flat_type_index_canary.js";

const flatValuesColumns = [
  { name: "id", kind: "uint64" },
  { name: "label", kind: "string" },
  { name: "bucket", kind: "string" },
  { name: "seq", kind: "uint64" },
  { name: "flag", kind: "bool" },
  { name: "i8", kind: "int8" },
  { name: "u8", kind: "uint8" },
  { name: "i16", kind: "int16" },
  { name: "u16", kind: "uint16" },
  { name: "i32", kind: "int32" },
  { name: "u32", kind: "uint32" },
  { name: "i64", kind: "int64" },
  { name: "u64", kind: "uint64" },
  { name: "i128", kind: "int128" },
  { name: "u128", kind: "uint128" },
  { name: "i256", kind: "int256" },
  { name: "u256", kind: "uint256" },
  { name: "f32", kind: "float32" },
  { name: "f64", kind: "float64" },
  { name: "created_at", kind: "timestamp" },
  { name: "ttl", kind: "duration" },
  { name: "uuid", kind: "uuid" },
  { name: "blob", kind: "bytes" },
  { name: "metadata", kind: "json" },
  { name: "tags", kind: "arrayString" },
  { name: "optional_note", kind: "string", nullable: true },
] as const satisfies readonly BsatnColumn[];

interface CanaryInput {
  readonly id: bigint;
  readonly label: string;
  readonly bucket: string;
  readonly seq: bigint;
  readonly note: string | null;
}

const inputs: readonly CanaryInput[] = [
  { id: 1n, label: "alpha", bucket: "active", seq: 10n, note: "alpha note" },
  { id: 3n, label: "gamma", bucket: "active", seq: 30n, note: null },
];

const encodedRows = inputs.map((input) => encodeBsatnProduct(canaryRowValues(input), flatValuesColumns));
const protocolRows = decodeRowList(rowListFrameFor(encodedRows));
assertEqual(protocolRows.rows.length, inputs.length, "protocol row count");
assertByteRows(protocolRows.rows, encodedRows, "protocol row bytes");

const decodedRows = protocolRows.rows.map(decodeFlatValuesRow);
const expectedRows = inputs.map(expectedCanaryRow);
assertDecodedRows(decodedRows, expectedRows, "direct generated decoder");

const decoderFromGeneratedMap = tableRowDecoders["flat_values"];
assertDecodedRow(decoderFromGeneratedMap(protocolRows.rows[0]), expectedRows[0], "generated decoder map");

function canaryRowValues(input: CanaryInput): readonly unknown[] {
  const idNumber = Number(input.id);
  const seqNumber = Number(input.seq);
  return [
    input.id,
    input.label,
    input.bucket,
    input.seq,
    input.id % 2n === 1n,
    -idNumber,
    idNumber + 10,
    -(idNumber * 2),
    idNumber * 2 + 10,
    -(idNumber * 3),
    idNumber * 3 + 10,
    -(input.id * 4n),
    input.id * 4n + 10n,
    wide128(input.id, input.seq + 100n),
    wide128(input.id + 200n, input.seq + 200n),
    wide256(input.id, input.seq + 300n, input.id + 300n, input.seq + 301n),
    wide256(input.id + 400n, input.seq + 400n, input.id + 401n, input.seq + 401n),
    idNumber + 0.25,
    idNumber + 0.5,
    1_700_000_000_000_000n + input.seq,
    input.seq * 1_000n,
    canaryUUID(input.id),
    new Uint8Array([idNumber, seqNumber, 0xa5]),
    { bucket: input.bucket, id: idNumber, label: input.label },
    [input.bucket, input.label, `seq-${input.seq}`],
    input.note,
  ];
}

function expectedCanaryRow(input: CanaryInput): FlatValuesRow {
  const idNumber = Number(input.id);
  const seqNumber = Number(input.seq);
  return {
    id: input.id,
    label: input.label,
    bucket: input.bucket,
    seq: input.seq,
    flag: input.id % 2n === 1n,
    i8: -idNumber,
    u8: idNumber + 10,
    i16: -(idNumber * 2),
    u16: idNumber * 2 + 10,
    i32: -(idNumber * 3),
    u32: idNumber * 3 + 10,
    i64: -(input.id * 4n),
    u64: input.id * 4n + 10n,
    i128: wide128(input.id, input.seq + 100n),
    u128: wide128(input.id + 200n, input.seq + 200n),
    i256: wide256(input.id, input.seq + 300n, input.id + 300n, input.seq + 301n),
    u256: wide256(input.id + 400n, input.seq + 400n, input.id + 401n, input.seq + 401n),
    f32: idNumber + 0.25,
    f64: idNumber + 0.5,
    createdAt: 1_700_000_000_000_000n + input.seq,
    ttl: input.seq * 1_000n,
    uuid: canaryUUID(input.id),
    blob: new Uint8Array([idNumber, seqNumber, 0xa5]),
    metadata: { bucket: input.bucket, id: idNumber, label: input.label },
    tags: [input.bucket, input.label, `seq-${input.seq}`],
    optionalNote: input.note,
  };
}

function wide128(hi: bigint, lo: bigint): bigint {
  return (hi << 64n) + lo;
}

function wide256(w0: bigint, w1: bigint, w2: bigint, w3: bigint): bigint {
  return (w0 << 192n) + (w1 << 128n) + (w2 << 64n) + w3;
}

function canaryUUID(id: bigint): string {
  const bytes = new Uint8Array(16);
  bytes.set([0x10, 0x20, 0x30, 0x40]);
  bytes[8] = Number((id >> 24n) & 0xffn);
  bytes[9] = Number((id >> 16n) & 0xffn);
  bytes[10] = Number((id >> 8n) & 0xffn);
  bytes[11] = Number(id & 0xffn);
  bytes[15] = Number((id + 0x40n) & 0xffn);
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function rowListFrameFor(rows: readonly Uint8Array[]): Uint8Array {
  const rowBytesLength = rows.reduce((sum, row) => sum + 4 + row.length, 0);
  const frame = new Uint8Array(4 + rowBytesLength);
  let offset = writeUint32LE(frame, 0, rows.length);
  for (const row of rows) {
    offset = writeUint32LE(frame, offset, row.length);
    frame.set(row, offset);
    offset += row.length;
  }
  return frame;
}

function writeUint32LE(frame: Uint8Array, offset: number, value: number): number {
  new DataView(frame.buffer, frame.byteOffset, frame.byteLength).setUint32(offset, value, true);
  return offset + 4;
}

function assertDecodedRows(actual: readonly FlatValuesRow[], expected: readonly FlatValuesRow[], label: string): void {
  assertEqual(actual.length, expected.length, `${label} length`);
  for (let i = 0; i < expected.length; i += 1) {
    assertDecodedRow(actual[i], expected[i], `${label} row ${i}`);
  }
}

function assertDecodedRow(actual: FlatValuesRow, expected: FlatValuesRow, label: string): void {
  assertEqual(actual.id, expected.id, `${label} id`);
  assertEqual(actual.label, expected.label, `${label} label`);
  assertEqual(actual.bucket, expected.bucket, `${label} bucket`);
  assertEqual(actual.seq, expected.seq, `${label} seq`);
  assertEqual(actual.flag, expected.flag, `${label} flag`);
  assertEqual(actual.i8, expected.i8, `${label} i8`);
  assertEqual(actual.u8, expected.u8, `${label} u8`);
  assertEqual(actual.i16, expected.i16, `${label} i16`);
  assertEqual(actual.u16, expected.u16, `${label} u16`);
  assertEqual(actual.i32, expected.i32, `${label} i32`);
  assertEqual(actual.u32, expected.u32, `${label} u32`);
  assertEqual(actual.i64, expected.i64, `${label} i64`);
  assertEqual(actual.u64, expected.u64, `${label} u64`);
  assertEqual(actual.i128, expected.i128, `${label} i128`);
  assertEqual(actual.u128, expected.u128, `${label} u128`);
  assertEqual(actual.i256, expected.i256, `${label} i256`);
  assertEqual(actual.u256, expected.u256, `${label} u256`);
  assertEqual(actual.f32, expected.f32, `${label} f32`);
  assertEqual(actual.f64, expected.f64, `${label} f64`);
  assertEqual(actual.createdAt, expected.createdAt, `${label} createdAt`);
  assertEqual(actual.ttl, expected.ttl, `${label} ttl`);
  assertEqual(actual.uuid, expected.uuid, `${label} uuid`);
  assertBytes(actual.blob, expected.blob, `${label} blob`);
  assertMetadata(actual.metadata, expected.metadata, `${label} metadata`);
  assertStringArray(actual.tags, expected.tags, `${label} tags`);
  assertEqual(actual.optionalNote, expected.optionalNote, `${label} optionalNote`);
}

function assertByteRows(actual: readonly Uint8Array[], expected: readonly Uint8Array[], label: string): void {
  assertEqual(actual.length, expected.length, `${label} length`);
  for (let i = 0; i < expected.length; i += 1) {
    assertBytes(actual[i], expected[i], `${label} row ${i}`);
  }
}

function assertBytes(actual: Uint8Array, expected: Uint8Array, label: string): void {
  assertEqual(actual.length, expected.length, `${label} length`);
  for (let i = 0; i < expected.length; i += 1) {
    assertEqual(actual[i], expected[i], `${label}[${i}]`);
  }
}

function assertStringArray(actual: readonly string[], expected: readonly string[], label: string): void {
  assertEqual(actual.length, expected.length, `${label} length`);
  for (let i = 0; i < expected.length; i += 1) {
    assertEqual(actual[i], expected[i], `${label}[${i}]`);
  }
}

function assertMetadata(actual: unknown, expected: unknown, label: string): void {
  if (!isMetadata(actual) || !isMetadata(expected)) {
    throw new Error(`${label}: metadata is not the expected canary object`);
  }
  assertEqual(actual.bucket, expected.bucket, `${label}.bucket`);
  assertEqual(actual.id, expected.id, `${label}.id`);
  assertEqual(actual.label, expected.label, `${label}.label`);
}

function isMetadata(value: unknown): value is { readonly bucket: string; readonly id: number; readonly label: string } {
  return typeof value === "object" &&
    value !== null &&
    typeof (value as { readonly bucket?: unknown }).bucket === "string" &&
    typeof (value as { readonly id?: unknown }).id === "number" &&
    typeof (value as { readonly label?: unknown }).label === "string";
}

function assertEqual<T>(actual: T, expected: T, label: string): void {
  if (!Object.is(actual, expected)) {
    throw new Error(`${label}: got ${formatValue(actual)} want ${formatValue(expected)}`);
  }
}

function formatValue(value: unknown): string {
  if (typeof value === "bigint") {
    return `${value}n`;
  }
  if (value instanceof Uint8Array) {
    return `Uint8Array(${Array.from(value).join(",")})`;
  }
  return String(value);
}
