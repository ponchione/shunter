package types

import (
	"strings"
	"testing"
)

func TestApproxMemoryBytesIncludesDynamicPayloads(t *testing.T) {
	shortString := NewString("x").ApproxMemoryBytes()
	longString := NewString(strings.Repeat("x", 64)).ApproxMemoryBytes()
	if longString <= shortString {
		t.Fatalf("long string memory = %d, want > short string %d", longString, shortString)
	}

	shortBytes := NewBytes([]byte{1}).ApproxMemoryBytes()
	longBytes := NewBytes(make([]byte, 64)).ApproxMemoryBytes()
	if longBytes <= shortBytes {
		t.Fatalf("long bytes memory = %d, want > short bytes %d", longBytes, shortBytes)
	}

	shortArray := NewArrayString([]string{"x"}).ApproxMemoryBytes()
	longArray := NewArrayString([]string{"x", strings.Repeat("y", 64)}).ApproxMemoryBytes()
	if longArray <= shortArray {
		t.Fatalf("long array memory = %d, want > short array %d", longArray, shortArray)
	}
}

func TestProductValueApproxMemoryBytesIncludesValues(t *testing.T) {
	row := ProductValue{NewUint64(1), NewString("alice")}
	if got := row.ApproxMemoryBytes(); got <= NewUint64(1).ApproxMemoryBytes() {
		t.Fatalf("row memory = %d, want larger than one scalar value", got)
	}
}
