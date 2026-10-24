//go:build darwin

package pasteboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// Every test uses a private named pasteboard; the general pasteboard (the
// user's clipboard) is never read or written.
func private(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("keetui-test-%d-%s", os.Getpid(), t.Name())
	t.Cleanup(UsePrivate(name))
	if _, err := ChangeCount(); errors.Is(err, ErrUnavailable) {
		t.Skipf("pasteboard server unavailable: %v", err)
	}
	return name
}

func types(t *testing.T) []string {
	t.Helper()
	var out []string
	selTypes := objc.RegisterName("types")
	selCount := objc.RegisterName("count")
	selAt := objc.RegisterName("objectAtIndex:")
	selUTF8 := objc.RegisterName("UTF8String")
	if err := call(func() error {
		arr := board.Send(selTypes)
		if arr == 0 {
			return nil
		}
		for i := range objc.Send[uint](arr, selCount) {
			p := objc.Send[unsafe.Pointer](arr.Send(selAt, i), selUTF8)
			n := 0
			for *(*byte)(unsafe.Add(p, n)) != 0 {
				n++
			}
			out = append(out, string(unsafe.Slice((*byte)(p), n)))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// excludedFromHistory asks the pasteboard server whether it will keep the
// current contents out of clipboard history. The getter is private, so the
// check is skipped where it does not exist.
func excludedFromHistory(t *testing.T) bool {
	t.Helper()
	sel := objc.RegisterName("_shouldExcludeFromHistory")
	var responds, excluded bool
	if err := call(func() error {
		responds = objc.Send[bool](board, objc.RegisterName("respondsToSelector:"), sel)
		if responds {
			excluded = objc.Send[bool](board, sel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !responds {
		t.Skip("_shouldExcludeFromHistory not available on this macOS")
	}
	return excluded
}

func TestCopyMarksText(t *testing.T) {
	private(t)
	secret := []byte("pässwörd-🔑")
	count, err := Copy(secret, true)
	if err != nil {
		t.Fatal(err)
	}
	got := types(t)
	for _, want := range []string{typeConcealed, typeTransient, typeText} {
		if !slices.Contains(got, want) {
			t.Fatalf("types %q missing %s", got, want)
		}
	}
	if now, err := ChangeCount(); err != nil || now != count {
		t.Fatalf("changeCount = %d, %v; want %d", now, err, count)
	}
	if text, err := Text(); err != nil || string(text) != string(secret) {
		t.Fatalf("text = %q, %v", text, err)
	}
	if !excludedFromHistory(t) {
		t.Fatal("pasteboard server would record the marked copy in history")
	}
}

func TestNoTransientWithoutScheduledClear(t *testing.T) {
	private(t)
	if _, err := Copy([]byte("x"), false); err != nil {
		t.Fatal(err)
	}
	got := types(t)
	if slices.Contains(got, typeTransient) || !slices.Contains(got, typeConcealed) {
		t.Fatalf("types = %q", got)
	}
}

func TestClearEmptiesAndChangesCount(t *testing.T) {
	private(t)
	count, err := Copy([]byte("secret"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	if got := types(t); len(got) != 0 {
		t.Fatalf("types after clear = %q", got)
	}
	if now, _ := ChangeCount(); now == count {
		t.Fatal("clear did not change changeCount")
	}
	if _, err := Text(); err == nil {
		t.Fatal("text still readable after clear")
	}
}

func TestOtherProcessWriteChangesCount(t *testing.T) {
	name := private(t)
	count, err := Copy([]byte("mine"), false)
	if err != nil {
		t.Fatal(err)
	}
	js := `ObjC.import("AppKit"); function run(a) { const p = $.NSPasteboard.pasteboardWithName(a[0]); p.clearContents; p.setStringForType($("theirs"), "public.utf8-plain-text") }`
	if out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", js, name).CombinedOutput(); err != nil {
		t.Skipf("osascript: %v %s", err, out)
	}
	if now, err := ChangeCount(); err != nil || now == count {
		t.Fatalf("changeCount = %d, %v; another process's write went unnoticed", now, err)
	}
	if text, err := Text(); err != nil || string(text) != "theirs" {
		t.Fatalf("text = %q, %v", text, err)
	}
}

func TestConcurrentCallsAreSerialized(t *testing.T) {
	private(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				if _, err := Copy([]byte("c"), false); err != nil {
					t.Error(err)
					return
				}
				_, _ = ChangeCount()
				_ = Clear()
			}
		})
	}
	wg.Wait()
}
