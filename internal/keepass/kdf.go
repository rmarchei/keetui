package keepass

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/tobischo/gokeepasslib/v3"
)

// Names of key derivation functions, as in KDF.Name.
const (
	kdfAES      = "AES-KDF"
	kdfArgon2d  = "Argon2d"
	kdfArgon2id = "Argon2id"
)

// argon2Version is the Argon2 version KeePass clients write (1.3), the only
// one gokeepasslib computes.
const argon2Version = 0x13

// argon2idUUID identifies Argon2id in a KDBX 4 header. gokeepasslib knows
// only the Argon2d UUID (gokeepasslib.KdfArgon2).
var argon2idUUID = []byte{
	0x9E, 0x29, 0x8B, 0x19, 0x56, 0xDB, 0x47, 0x73,
	0xB2, 0x3D, 0xFC, 0x3E, 0xC6, 0xF0, 0xA1, 0xE6,
}

// KDF describes how a database derives its key from its credentials, which
// happens on every unlock and on every save.
type KDF struct {
	Name        string // "AES-KDF", "Argon2d", "Argon2id", "unknown (UUID …)", or "" if the header has none
	Rounds      uint64 // AES-KDF: rounds of AES encryption
	Memory      uint64 // Argon2: memory, in bytes
	Iterations  uint64 // Argon2: passes over the memory
	Parallelism uint32 // Argon2: lanes
}

// String describes the key derivation in a few words, e.g. "Argon2d, 64 MiB,
// 20 iterations, parallelism 2".
func (k KDF) String() string {
	switch k.Name {
	case "":
		return "no key derivation parameters"
	case kdfAES:
		return fmt.Sprintf("%s, %d rounds", k.Name, k.Rounds)
	case kdfArgon2d, kdfArgon2id:
		return fmt.Sprintf("%s, %s, %d iterations, parallelism %d", k.Name, formatMemory(k.Memory), k.Iterations, k.Parallelism)
	}
	return k.Name
}

// formatMemory writes a byte count in the largest binary unit dividing it.
func formatMemory(n uint64) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

// KDF returns the database's key derivation.
func (d *DB) KDF() KDF {
	return kdfOf(d.Database.Header)
}

// kdfOf reads the key derivation a header asks for.
func kdfOf(h *gokeepasslib.DBHeader) KDF {
	fh := h.FileHeaders
	if !h.IsKdbx4() {
		return KDF{Name: kdfAES, Rounds: fh.TransformRounds}
	}
	p := fh.KdfParameters
	if p == nil {
		return KDF{}
	}
	argon2 := func(name string) KDF {
		return KDF{Name: name, Memory: p.Memory, Iterations: p.Iterations, Parallelism: p.Parallelism}
	}
	switch {
	case bytes.Equal(p.UUID, gokeepasslib.KdfArgon2):
		return argon2(kdfArgon2d)
	case bytes.Equal(p.UUID, argon2idUUID):
		return argon2(kdfArgon2id)
	case bytes.Equal(p.UUID, gokeepasslib.KdfAES3), bytes.Equal(p.UUID, gokeepasslib.KdfAES4):
		// KeePass writes AES-KDF in KDBX 4 with the UUID gokeepasslib
		// calls KdfAES3; KeePassXC also reads the other one.
		return KDF{Name: kdfAES, Rounds: p.Rounds}
	}
	return KDF{Name: fmt.Sprintf("unknown (UUID %x)", p.UUID)}
}

// Limits on the key derivation a file's header may ask for. The header is
// not authenticated before the key is derived with it (KDBX 3.1 not at all,
// KDBX 4 by an HMAC keyed with that key), so whoever can write the file
// could otherwise make keetui compute for centuries (2^62 AES rounds) or
// take gigabytes of memory before any password is checked, when unlocking
// and when merging the file on disk. The limits are far above what KeePass
// clients write on their own (about a second of work), so that no real
// database is refused: at the limits, deriving a key takes one to three
// minutes on an Apple M4.
const (
	// maxAESRounds is above what KeePassXC reads (2^31-1); 90 s here.
	maxAESRounds = 1 << 32
	// maxArgon2Memory is 32 times KeePassXC's default (64 MiB), and below
	// the 4 GiB gokeepasslib reads (it takes the memory as 32 bits).
	maxArgon2Memory = 2 << 30
	// maxArgon2Work bounds memory times passes: 2048 passes over 64 MiB,
	// or 64 over 2 GiB; 1 to 3 minutes here, by the number of lanes.
	maxArgon2Work = 128 << 30
	// maxArgon2Parallelism is the most lanes gokeepasslib computes: it
	// takes the count as a byte.
	maxArgon2Parallelism = 255
)

// errKDFLimit is wrapped by the error for a header asking for a key
// derivation beyond the limits.
var errKDFLimit = errors.New("key derivation parameters beyond keetui's limits (damaged or tampered file?)")

// checkLimits refuses a key derivation beyond the limits, and one keetui
// cannot compute: gokeepasslib would derive a wrong key from it. Missing or
// zero parameters pass, as deriving the key then fails before any work.
func (k KDF) checkLimits() error {
	switch k.Name {
	case "":
		return nil
	case kdfAES:
		if k.Rounds > maxAESRounds {
			return fmt.Errorf("%w: %d AES-KDF rounds, at most %d", errKDFLimit, k.Rounds, uint64(maxAESRounds))
		}
		return nil
	case kdfArgon2d:
		if k.Parallelism > maxArgon2Parallelism {
			return fmt.Errorf("%w: %d Argon2 lanes, at most %d", errKDFLimit, k.Parallelism, maxArgon2Parallelism)
		}
		if k.Memory > maxArgon2Memory {
			return fmt.Errorf("%w: %s of Argon2 memory, at most %s", errKDFLimit, formatMemory(k.Memory), formatMemory(maxArgon2Memory))
		}
		// Argon2 counts memory in KiB and uses at least 8 KiB per lane.
		memory := max(k.Memory/1024, 8*uint64(max(k.Parallelism, 1))) * 1024
		if k.Iterations > maxArgon2Work/memory {
			return fmt.Errorf("%w: %d Argon2 passes over %s, at most %d", errKDFLimit, k.Iterations, formatMemory(memory), maxArgon2Work/memory)
		}
		return nil
	}
	return fmt.Errorf("unsupported key derivation: %s; keetui supports Argon2d and AES-KDF", k.Name)
}

// checkFileKDF refuses a KDBX file whose header asks for a key derivation
// beyond the limits or one keetui cannot compute, without deriving a key.
// A header that cannot be read passes: decoding the file fails the same way,
// before deriving a key.
func checkFileKDF(data []byte) error {
	probe := gokeepasslib.NewDatabase()
	// Without credentials, decoding stops where the key would be derived:
	// after reading the header and, for KDBX 4, checking its hash.
	probe.Credentials = nil
	err := gokeepasslib.NewDecoder(bytes.NewReader(data)).Decode(probe)
	var missing gokeepasslib.ErrRequiredAttributeMissing
	if !errors.As(err, &missing) || missing != "Credentials" {
		return nil
	}
	return kdfOf(probe.Header).checkLimits()
}

// productionKDF is the key derivation keetui gives new databases: Argon2d
// with KeePassXC's default memory (64 MiB), two lanes, and 20 passes, about
// 0.65 s on an Apple M4. Each unlock derives the key once, and so does each
// save, which draws a new salt.
var productionKDF = KDF{Name: kdfArgon2d, Memory: 64 << 20, Iterations: 20, Parallelism: 2}

// testKDF is a minimal Argon2d cost, a fraction of a millisecond, for test
// suites that create and save databases by the hundred.
var testKDF = KDF{Name: kdfArgon2d, Memory: 64 << 10, Iterations: 2, Parallelism: 1}

// targetKDF is the key derivation new databases get: productionKDF, unless
// a test asked for testKDF.
var targetKDF = productionKDF

// UseTestKDF makes new databases derive their key at a minimal cost instead
// of the production one, which takes most of a second: test suites create
// databases constantly. Only the TestMain of a package whose tests create
// databases calls it, before running them; production code never does.
func UseTestKDF() {
	targetKDF = testKDF
}

// WeakKDF reports whether the database derives its key more cheaply than
// new databases do: with AES-KDF (every KDBX 3.1 database), which is not
// memory-hard, or with Argon2 over less memory or in fewer passes than the
// target. Fewer lanes do not count: more lanes only make derivation faster
// on more cores, for an attacker as well. StrengthenKDF raises it.
func (d *DB) WeakKDF() bool {
	k := d.KDF()
	if k.Name != kdfArgon2d && k.Name != kdfArgon2id {
		return true
	}
	return k.Memory < targetKDF.Memory || k.Iterations < targetKDF.Iterations
}

// StrengthenKDF raises a weak key derivation (see WeakKDF) to the cost new
// databases get, in memory: the caller saves the database to persist it. A
// KDBX 3.1 database is converted to KDBX 4 (see ConvertToKDBX4); a KDBX 4
// one using AES-KDF switches to Argon2d at the target cost; one using Argon2
// takes the target's memory, passes and lanes where its own are fewer, and
// keeps those already above. It reports whether it changed anything.
func (d *DB) StrengthenKDF() (bool, error) {
	if !d.WeakKDF() {
		return false, nil
	}
	if !d.IsKdbx4() {
		if err := d.ConvertToKDBX4(); err != nil {
			return false, err
		}
		return true, nil
	}
	fh := d.Database.Header.FileHeaders
	if k := d.KDF(); k.Name != kdfArgon2d && k.Name != kdfArgon2id {
		fh.KdfParameters = targetKdfParameters()
		return true, nil
	}
	p := fh.KdfParameters
	p.Memory = max(p.Memory, targetKDF.Memory)
	p.Iterations = max(p.Iterations, targetKDF.Iterations)
	p.Parallelism = max(p.Parallelism, targetKDF.Parallelism)
	return true, nil
}

// targetKdfParameters returns KDBX 4 header parameters for Argon2d at the
// target cost, with a new salt.
func targetKdfParameters() *gokeepasslib.KdfParameters {
	p := &gokeepasslib.KdfParameters{
		UUID:        bytes.Clone(gokeepasslib.KdfArgon2),
		Memory:      targetKDF.Memory,
		Iterations:  targetKDF.Iterations,
		Parallelism: targetKDF.Parallelism,
		Version:     argon2Version,
	}
	// crypto/rand.Read cannot fail: it aborts the program instead.
	_, _ = rand.Read(p.Salt[:])
	return p
}
