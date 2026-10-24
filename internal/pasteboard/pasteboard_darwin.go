//go:build darwin

package pasteboard

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	typeText      = "public.utf8-plain-text" // NSPasteboardTypeString
	typeConcealed = "org.nspasteboard.ConcealedType"
	typeTransient = "org.nspasteboard.TransientType"

	contentsCurrentHostOnly uint = 1 << 0 // NSPasteboardContentsCurrentHostOnly
)

// ErrUnavailable means AppKit or the pasteboard server could not be reached.
var ErrUnavailable = errors.New("macOS pasteboard unavailable")

var (
	// mu serializes every AppKit call: concurrent use of one NSPasteboard
	// from two OS threads crashes in objc_msgSend.
	mu       sync.Mutex
	initOnce sync.Once
	initErr  error

	testName string // a private named pasteboard; tests only
	board    objc.ID

	poolPush func() uintptr
	poolPop  func(uintptr)

	clsPasteboard, clsData, clsString objc.Class

	selGeneral       = objc.RegisterName("generalPasteboard")
	selWithName      = objc.RegisterName("pasteboardWithName:")
	selRelease       = objc.RegisterName("releaseGlobally")
	selChangeCount   = objc.RegisterName("changeCount")
	selClearContents = objc.RegisterName("clearContents")
	selPrepare       = objc.RegisterName("prepareForNewContentsWithOptions:")
	selSetData       = objc.RegisterName("setData:forType:")
	selDataForType   = objc.RegisterName("dataForType:")
	selData          = objc.RegisterName("data")
	selDataWithBytes = objc.RegisterName("dataWithBytes:length:")
	selBytes         = objc.RegisterName("bytes")
	selLength        = objc.RegisterName("length")
	selStringUTF8    = objc.RegisterName("stringWithUTF8String:")
)

// load opens AppKit on first use, so processes that never copy do not pay
// for it.
func load() error {
	initOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit",
			purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			initErr = fmt.Errorf("%w: %v", ErrUnavailable, err)
			return
		}
		libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = fmt.Errorf("%w: %v", ErrUnavailable, err)
			return
		}
		purego.RegisterLibFunc(&poolPush, libobjc, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&poolPop, libobjc, "objc_autoreleasePoolPop")
		clsPasteboard = objc.GetClass("NSPasteboard")
		clsData = objc.GetClass("NSData")
		clsString = objc.GetClass("NSString")
		if clsPasteboard == 0 || clsData == 0 || clsString == 0 {
			initErr = ErrUnavailable
		}
	})
	return initErr
}

// call runs fn serialized, on a locked OS thread, inside an autorelease pool.
func call(fn func() error) error {
	mu.Lock()
	defer mu.Unlock()
	if err := load(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := poolPush()
	defer poolPop(pool)
	if board == 0 {
		if testName != "" {
			board = objc.ID(clsPasteboard).Send(selWithName, nsString(testName))
		} else {
			board = objc.ID(clsPasteboard).Send(selGeneral)
		}
		if board == 0 {
			return ErrUnavailable
		}
	}
	return fn()
}

func nsString(s string) objc.ID { return objc.ID(clsString).Send(selStringUTF8, s) }

// Copy replaces the pasteboard contents with text and returns the
// changeCount that identifies this write. Set transient only when a clear
// is scheduled.
func Copy(text []byte, transient bool) (int, error) {
	var count int
	err := call(func() error {
		// Clears the pasteboard and keeps this generation on this Mac. Do not
		// call clearContents afterwards: that resets the options.
		objc.Send[int](board, selPrepare, contentsCurrentHostOnly)
		empty := objc.ID(clsData).Send(selData)
		markers := []string{typeConcealed}
		if transient {
			markers = append(markers, typeTransient)
		}
		for _, t := range markers {
			if !objc.Send[bool](board, selSetData, empty, nsString(t)) {
				return fmt.Errorf("pasteboard: set %s failed", t)
			}
		}
		var ptr unsafe.Pointer
		if len(text) > 0 {
			ptr = unsafe.Pointer(&text[0])
		}
		// Raw UTF-8 bytes: unlike pbcopy, no dependence on LANG.
		data := objc.ID(clsData).Send(selDataWithBytes, ptr, uint(len(text)))
		if !objc.Send[bool](board, selSetData, data, nsString(typeText)) {
			return errors.New("pasteboard: set text failed")
		}
		count = objc.Send[int](board, selChangeCount)
		return nil
	})
	return count, err
}

// ChangeCount returns the pasteboard's current changeCount. Any write or
// clear, by any process, changes it.
func ChangeCount() (int, error) {
	var count int
	err := call(func() error {
		count = objc.Send[int](board, selChangeCount)
		return nil
	})
	return count, err
}

// Text returns the pasteboard's plain-text contents.
func Text() ([]byte, error) {
	var text []byte
	err := call(func() error {
		data := board.Send(selDataForType, nsString(typeText))
		if data == 0 {
			return errors.New("pasteboard: no text")
		}
		n := objc.Send[uint](data, selLength)
		if n == 0 {
			text = []byte{}
			return nil
		}
		p := objc.Send[unsafe.Pointer](data, selBytes)
		text = append([]byte(nil), unsafe.Slice((*byte)(p), n)...)
		return nil
	})
	return text, err
}

// Clear empties the pasteboard.
func Clear() error {
	return call(func() error {
		objc.Send[int](board, selClearContents)
		return nil
	})
}

// UsePrivate switches to a private named pasteboard, so tests never touch
// the general one (the user's clipboard). The returned cleanup releases it.
func UsePrivate(name string) (cleanup func()) {
	mu.Lock()
	testName, board = name, 0
	mu.Unlock()
	return func() {
		_ = call(func() error { board.Send(selRelease); return nil })
		mu.Lock()
		testName, board = "", 0
		mu.Unlock()
	}
}
