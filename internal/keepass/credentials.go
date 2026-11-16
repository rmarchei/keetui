package keepass

import (
	"crypto/subtle"
	"errors"
	"fmt"
)

// ErrWrongCredentials is what CheckCredentials returns for a password or key
// file that is not the database's.
var ErrWrongCredentials = errors.New("wrong password or key file")

// CheckCredentials reports whether password and the key file at keyFilePath
// ("" for none) are the database's master key, the one it was opened or
// since changed to: nil when they are, ErrWrongCredentials when they are
// not. It compares them with the credentials held in memory and never reads
// the database file, so it can unlock a database whose file cannot be
// reached. It only reads the database, so it may run while a save is in
// flight.
func (d *DB) CheckCredentials(password, keyFilePath string) error {
	given, err := buildCredentials(password, keyFilePath)
	if err != nil {
		return fmt.Errorf("create credentials: %w", err)
	}
	held := d.Database.Credentials
	if held == nil {
		return ErrWrongCredentials
	}
	same := subtle.ConstantTimeCompare(given.Passphrase, held.Passphrase) &
		subtle.ConstantTimeCompare(given.Key, held.Key) &
		subtle.ConstantTimeCompare(given.Windows, held.Windows)
	if same != 1 {
		return ErrWrongCredentials
	}
	return nil
}
