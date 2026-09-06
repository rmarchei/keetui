package keepass

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

func TestEntryCountMatchesStatsAndSkipsRecycleSubtree(t *testing.T) {
	db, err := Create(filepath.Join(t.TempDir(), "count.kdbx"), "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	root := db.RootGroup().UUID
	group, err := db.AddGroup(root, "nested")
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []gokeepasslib.UUID{root, group} {
		if _, err := db.AddEntry(parent, Entry{Password: "abc"}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want int) {
		t.Helper()
		if got := db.EntryCount(); got != want || got != db.Stats().TotalEntries {
			t.Fatalf("count=%d, want %d", got, want)
		}
	}
	check(2)
	db.Database.Content.Meta.RecycleBinEnabled.Bool = true
	if err := db.DeleteGroup(group); err != nil {
		t.Fatal(err)
	}
	check(1)
	if err := db.RestoreGroup(group, root); err != nil {
		t.Fatal(err)
	}
	check(2)
	db.Database.Content.Root.Groups = nil
	check(0)
}

func TestHealthSnapshotIndependentDuringConcurrentMutations(t *testing.T) {
	db, err := Create(filepath.Join(t.TempDir(), "snapshot.kdbx"), "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	uuid, err := db.AddEntry(db.RootGroup().UUID, Entry{Title: "original", Password: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := db.HealthSnapshot()
	done := make(chan error, 1)
	go func() {
		for range 20 {
			stats, err := snapshot.Stats(context.Background())
			if err != nil {
				done <- err
				return
			}
			audit, err := snapshot.Audit(context.Background())
			if err != nil {
				done <- err
				return
			}
			if stats.TotalEntries != 1 || stats.WeakPasswords != 1 || len(audit.WeakEntries) != 1 || audit.WeakEntries[0].Entry.Title != "original" {
				done <- fmt.Errorf("snapshot changed during live mutations")
				return
			}
		}
		done <- nil
	}()
	for i := range 20 {
		if err := db.UpdateEntry(uuid, Entry{Title: fmt.Sprintf("changed-%d", i), Password: "Xk9#mQ2$"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if db.Stats().WeakPasswords != 0 || len(db.Audit().WeakEntries) != 0 {
		t.Fatal("Good password flagged as weak")
	}
}

func TestHealthScanHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := HealthSnapshot{root: Group{Entries: []Entry{{Password: "abc"}}}}
	if result, err := s.Stats(ctx); !errors.Is(err, context.Canceled) || result != (DBStats{}) {
		t.Fatal("canceled stats returned partial data")
	}
	if result, err := s.Audit(ctx); !errors.Is(err, context.Canceled) || result.TotalIssues() != 0 {
		t.Fatal("canceled audit returned partial data")
	}
}

func BenchmarkEntryCount(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db := &DB{Database: gokeepasslib.NewDatabase()}
			group := gokeepasslib.NewGroup()
			group.Entries = make([]gokeepasslib.Entry, count)
			db.Database.Content.Root.Groups = []gokeepasslib.Group{group}
			for b.Loop() {
				if db.EntryCount() != count {
					b.Fatal("incorrect count")
				}
			}
		})
	}
}
