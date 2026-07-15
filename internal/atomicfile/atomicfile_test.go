package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMkdirAllDurableSynchronizesEachNewDirectoryEntry(t *testing.T) {
	existing := t.TempDir()
	path := filepath.Join(existing, "new-a", "new-b", "data")
	var synced []string

	if err := MkdirAllDurable(path, 0o750, func(path string) error {
		synced = append(synced, path)
		return nil
	}); err != nil {
		t.Fatalf("MkdirAllDurable: %v", err)
	}

	wantSynced := []string{
		filepath.Dir(existing),
		existing,
		filepath.Join(existing, "new-a"),
		filepath.Join(existing, "new-a", "new-b"),
	}
	if !reflect.DeepEqual(synced, wantSynced) {
		t.Fatalf("synced directories = %#v, want %#v", synced, wantSynced)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat durable path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("durable path mode = %v, want directory", info.Mode())
	}
}

func TestMkdirAllDurableReturnsSyncFailureBeforeExtendingPath(t *testing.T) {
	existing := t.TempDir()
	newA := filepath.Join(existing, "new-a")
	newB := filepath.Join(newA, "new-b")
	path := filepath.Join(newB, "data")
	syncErr := errors.New("injected ancestor sync failure")

	err := MkdirAllDurable(path, 0o750, func(path string) error {
		if path == newA {
			return syncErr
		}
		return nil
	})
	if !errors.Is(err, syncErr) {
		t.Fatalf("MkdirAllDurable error = %v, want injected sync failure", err)
	}
	if _, err := os.Stat(newB); err != nil {
		t.Fatalf("directory created before its publication sync should remain for repair: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path beyond failed sync stat error = %v, want not exist", err)
	}
}

func TestMkdirAllDurableRetryRepairsPriorFailedDirectoryPublication(t *testing.T) {
	existing := t.TempDir()
	newA := filepath.Join(existing, "new-a")
	newB := filepath.Join(newA, "new-b")
	path := filepath.Join(newB, "data")
	syncErr := errors.New("injected first sync failure")

	if err := MkdirAllDurable(path, 0o750, func(path string) error {
		if path == newA {
			return syncErr
		}
		return nil
	}); !errors.Is(err, syncErr) {
		t.Fatalf("first MkdirAllDurable error = %v, want injected failure", err)
	}

	var retrySynced []string
	if err := MkdirAllDurable(path, 0o750, func(path string) error {
		retrySynced = append(retrySynced, path)
		return nil
	}); err != nil {
		t.Fatalf("retry MkdirAllDurable: %v", err)
	}
	wantSynced := []string{newA, newB}
	if !reflect.DeepEqual(retrySynced, wantSynced) {
		t.Fatalf("retry synced directories = %#v, want repair then extension %#v", retrySynced, wantSynced)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat path after repaired retry: %v", err)
	}
}

func TestMkdirAllDurableRetryRepairsCompletedPathPublication(t *testing.T) {
	existing := t.TempDir()
	path := filepath.Join(existing, "data")
	syncErr := errors.New("injected final directory sync failure")

	if err := MkdirAllDurable(path, 0o750, func(path string) error {
		if path == existing {
			return syncErr
		}
		return nil
	}); !errors.Is(err, syncErr) {
		t.Fatalf("first MkdirAllDurable error = %v, want final publication failure", err)
	}

	var retrySynced []string
	if err := MkdirAllDurable(path, 0o750, func(path string) error {
		retrySynced = append(retrySynced, path)
		return nil
	}); err != nil {
		t.Fatalf("retry MkdirAllDurable: %v", err)
	}
	wantSynced := []string{existing}
	if !reflect.DeepEqual(retrySynced, wantSynced) {
		t.Fatalf("retry synced directories = %#v, want final publication repair %#v", retrySynced, wantSynced)
	}
}
